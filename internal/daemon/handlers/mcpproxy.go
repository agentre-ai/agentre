package handlers

import (
	"io"
	"net/http"
	"net/url"

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
// MCP HTTP 请求装包,经当前活跃连接的 NotifierPort 反向请求(MethodMCPProxy)隧道回 desktop
// 执行,再把应答原样写回 CLI。MCP-over-HTTP 是纯请求/应答,单帧足够。notifierFn 在请求时
// 取当前活跃连接(daemon 单客户端 MVP 下同一时刻一条);无连接 → 503。
func NewMCPTunnelHandler(notifierFn func() NotifierPort) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := notifierFn()
		if n == nil {
			http.Error(w, "mcp tunnel: no active desktop connection", http.StatusServiceUnavailable)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "mcp tunnel: read body", http.StatusBadRequest)
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
