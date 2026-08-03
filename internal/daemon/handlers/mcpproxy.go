package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// daemonGatewayBase 取 daemon 本机 gateway base URL;gateway 未装配(测试 / 未启)时返回空,
// rewriteMCPServersForDaemon 据此保守保留原 URL。
func daemonGatewayBase(g GatewayPort) string {
	if g == nil {
		return ""
	}
	return g.URL()
}

// rewriteMCPServersForDaemon 把 desktop 下发的内置工具 MCP server URL 的 scheme+host 改写成
// daemon 本机 gateway 的(只换 host,保留 /mcp/<name>/ 路径 + Headers + Tools + Name),
// 让 daemon 上的 CLI 子进程把请求打到 daemon 本地的隧道入口,而不是 desktop 的 127.0.0.1
// (在 daemon 主机上拨不到)。token 等鉴权头原样保留,隧道回 desktop 后由 desktop 侧校验。
// 返回新 slice,不就地改入参;daemonBaseFn 惰性求值 —— 无 MCP server 时根本不取(也就不
// 触碰 gateway),base 为空 / 解析失败时保守返回原 specs。
func rewriteMCPServersForDaemon(specs []agentruntime.MCPServerSpec, daemonBaseFn func() string) []agentruntime.MCPServerSpec {
	if len(specs) == 0 || daemonBaseFn == nil {
		return specs
	}
	daemonBase := daemonBaseFn()
	if daemonBase == "" {
		return specs
	}
	base, err := url.Parse(daemonBase)
	if err != nil || base.Host == "" {
		return specs
	}
	out := make([]agentruntime.MCPServerSpec, len(specs))
	for i, s := range specs {
		out[i] = s
		u, perr := url.Parse(s.URL)
		if perr != nil {
			continue // 解析失败保留原样,不丢这条 server
		}
		u.Scheme = base.Scheme
		u.Host = base.Host
		out[i].URL = u.String()
	}
	return out
}

// hopByHopTunnelHeaders 是不该跨隧道转发的逐跳头(+ Host/Content-Length,desktop 重放时
// 由 http.Client 按目标 URL / body 重算)。其余头(Authorization / Content-Type / Accept /
// Mcp-* 等)原样转发。
var hopByHopTunnelHeaders = map[string]bool{
	"Host": true, "Content-Length": true, "Connection": true, "Keep-Alive": true,
	"Transfer-Encoding": true, "Te": true, "Trailer": true, "Upgrade": true, "Proxy-Connection": true,
}

func sanitizeTunnelHeaders(h http.Header) map[string][]string {
	if len(h) == 0 {
		return nil
	}
	out := make(map[string][]string, len(h))
	for k, vs := range h {
		if hopByHopTunnelHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		cp := make([]string, len(vs))
		copy(cp, vs)
		out[k] = cp
	}
	return out
}

// NewMCPTunnelHandler 返回挂在 daemon 本机 gateway /mcp/ 上的隧道入口:把 CLI 子进程的
// MCP HTTP 请求装包,经 NotifierPort 反向请求(MethodMCPProxy)隧道回 desktop 执行,再把
// 应答原样写回 CLI。MCP-over-HTTP 是纯请求/应答,单帧足够。
//
// notifierFn 在请求时解析目标,与会话通知共用 daemon 的同一份连接状态(没有第二个「当前
// 连接」的全局):取最近被认领的那条会话的属主连接,一条会话都还没被认领时回落到最近完成
// 鉴权的活连接 —— 完成 WS 升级却从不认证的连接永远不构成隧道目标。请求身上**没有**会话
// 标识可用(路径是 /mcp/<server>/,鉴权头是 desktop 签的不透明 token,daemon 既不签也验不
// 了),所以隧道只能定位到「跑着会话的那台设备」;好在 desktop 侧的隧道 handler 是无状态的
// (把请求重放到它本机 gateway),同一台设备的哪条连接送达都等价。
//
// 无目标(发起会话的桌面端不在线)时,不能回裸 HTTP 503 —— 非 2xx 状态码会让 CLI 内嵌的
// MCP 客户端把整条应答当传输层故障丢弃,body 里的话模型永远读不到,会话也因此白白等一个
// 读不出语义的错误(R17)。改为 HTTP 200 包一个 JSON-RPC error:这正是本仓库其余几个内置
// 工具 MCP server(org/subagent/hooktool_svc 的 writeRPCError)在工具执行失败时使用的
// 形状,MCP 客户端读它就是读一次普通的工具调用失败,原样喂给模型当 tool 输出——而不是让
// CLI 报一个模型看不懂的基础设施错误。见 writeMCPTunnelUnavailable。
func NewMCPTunnelHandler(notifierFn func() NotifierPort) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "mcp tunnel: read body", http.StatusBadRequest)
			return
		}
		n := notifierFn()
		if n == nil {
			writeMCPTunnelUnavailable(w, r.URL.Path, body)
			return
		}
		req := wire.MCPProxyRequest{
			Path:    r.URL.Path,
			Method:  r.Method,
			Headers: sanitizeTunnelHeaders(r.Header),
			Body:    body,
		}
		var resp wire.MCPProxyResponse
		if err := n.Request(r.Context(), wire.MethodMCPProxy, req, &resp); err != nil {
			http.Error(w, "mcp tunnel: "+err.Error(), http.StatusBadGateway)
			return
		}
		for k, vs := range resp.Headers {
			for _, v := range vs {
				w.Header().Add(k, v)
			}
		}
		if resp.Status == 0 {
			resp.Status = http.StatusOK
		}
		w.WriteHeader(resp.Status)
		_, _ = w.Write(resp.Body)
	})
}

// mcpUnavailableErrorCode 是隧道无目标时用的 JSON-RPC「server error」码,复用
// org/subagent/hooktool_svc 的 writeRPCError 已经在用的 -32000(工具执行期失败的通用码,
// 落在 JSON-RPC 保留的 -32000~-32099 应用错误段)——让这条从 daemon 代答的应答,在 MCP
// 客户端眼里与「真服务器执行工具时失败了」的应答别无二致。
const mcpUnavailableErrorCode = -32000

// mcpTunnelRequestID 尽力从 MCP 请求 body 里取出 JSON-RPC 的 "id",让代答的错误应答能
// 对上号(MCP-over-HTTP 客户端按 id 关联请求/应答)。body 不是合法 JSON 或没带 id 时退化
// 成 JSON null —— 仍是一个格式合法的 JSON-RPC id,只是关联不上而已,好过直接不回。
func mcpTunnelRequestID(body []byte) json.RawMessage {
	var env struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(body, &env); err != nil || len(env.ID) == 0 {
		return json.RawMessage("null")
	}
	return env.ID
}

// mcpTunnelUnavailableLabel 从隧道路径 "/mcp/<name>/..." 里取出 server 名,拼成
// 「哪个能力不可用」那半句话的主语。取不出时退化成一个通用短语而不是拼出个畸形句子。
func mcpTunnelUnavailableLabel(path string) string {
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 3)
	if len(parts) >= 2 && parts[0] == "mcp" && parts[1] != "" {
		return fmt.Sprintf("the %q built-in tool", parts[1])
	}
	return "this built-in tool"
}

// writeMCPTunnelUnavailable 见 NewMCPTunnelHandler 顶部注释:HTTP 200 + JSON-RPC error,
// 措辞把三件事都讲给模型听——哪个能力不可用、它依赖发起会话的那台桌面端在线、以及这不是
// 瞬时故障,不要重试,绕过去或留到之后。纯英文措辞,理由见调用方文档:这段字符串喂给 LLM,
// 不进 UI,不受 AGENTS.md 的前端 i18n 规则约束(该规则管的是 react-i18next 的可见 UI 文案)。
func writeMCPTunnelUnavailable(w http.ResponseWriter, path string, body []byte) {
	msg := fmt.Sprintf(
		"%s is unavailable: it depends on the desktop client that started this session, "+
			"and that client is offline right now. This is not a transient failure — do not "+
			"retry; proceed without it, or leave this for later until the client reconnects.",
		mcpTunnelUnavailableLabel(path),
	)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      mcpTunnelRequestID(body),
		"error": map[string]any{
			"code":    mcpUnavailableErrorCode,
			"message": msg,
		},
	})
}
