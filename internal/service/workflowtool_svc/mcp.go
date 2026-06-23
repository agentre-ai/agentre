package workflowtool_svc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// workflowRef 是 workflow MCP token 绑定的 (agent, session)。
type workflowRef struct{ agentID, sessionID int64 }

// workflowMCP 是流程管理工具的 MCP-over-HTTP server(挂在 gateway /mcp/workflow/)。
//
// 身份: 与 orgtool_svc 同款无状态签名 token —— `b64url(agent:session).b64url(HMAC(secret, agent:session))`,
// 投递时塞进 mcp-config 的 Authorization header。token 在 CLI spawn 时随 --mcp-config 注入、
// 复用轮不重发,因此必须与子进程同寿命:确定性(同 (agent, session) 每次同值)。lookup 只验签
// (无状态),工具开关由 tools/call 时实时查 DB(lookup.Find + ToolEnabled)判定 —— 用户
// 关掉开关后旧 token 立即失效。
type workflowMCP struct {
	svc    *workflowtoolSvc
	secret []byte // per-process HMAC 签名密钥(流程库为本机回投,进程内即可)
}

func newWorkflowMCP(svc *workflowtoolSvc) *workflowMCP {
	return &workflowMCP{svc: svc, secret: randSecret()}
}

// MintToken 为某 (agent, session) 签一个无状态签名 token。同一 (agent, session) 每次返回
// 相同值(确定性),保证被复用/恢复的子进程持有的 token 始终验签通过。
func (h *workflowMCP) MintToken(agentID, sessionID int64) string {
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *workflowMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// lookup 验签并解出 token 绑定的 (agent, session)。仅做密码学校验(无状态);验签失败 /
// 格式非法 → !ok。
func (h *workflowMCP) lookup(tok string) (workflowRef, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return workflowRef{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return workflowRef{}, false
	}
	aStr, sStr, ok := strings.Cut(string(payload), ":")
	if !ok {
		return workflowRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return workflowRef{}, false
	}
	return workflowRef{agentID, sessionID}, true
}

func (h *workflowMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet { // claude 开 server→client SSE; 我们不推送 → 405(claude 容忍)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string          `json:"protocolVersion"`
			Name            string          `json:"name"`
			Arguments       json.RawMessage `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}
	switch rpc.Method {
	case "initialize":
		pv := rpc.Params.ProtocolVersion
		if pv == "" {
			pv = "2025-06-18"
		}
		writeRPCResult(w, rpc.ID, map[string]any{
			"protocolVersion": pv,
			"serverInfo":      map[string]any{"name": "agentre-workflow", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": workflowToolSchemas()})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.svc.lookup == nil { // bootstrap 窗口期(RegisterDeps 未执行)的保险闸
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		// 实时开关校验:用户关掉开关后旧 token 立即失效
		a, err := h.svc.lookup.Find(r.Context(), ref.agentID)
		if err != nil || a == nil || !a.ToolEnabled(agenttool.KeyWorkflow) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch rpc.Params.Name {
		case "workflow_list":
			resp, err := h.svc.query.List(r.Context(), &workflow_svc.ListWorkflowsRequest{})
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			b, _ := json.Marshal(workflowListView(resp))
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
		default:
			if !isWorkflowWriteTool(rpc.Params.Name) {
				writeRPCError(w, rpc.ID, -32601, "unknown tool")
				return
			}
			h.svc.handleWriteTool(w, r, rpc.ID, ref, rpc.Params.Name, rpc.Params.Arguments)
		}
	default:
		writeRPCError(w, rpc.ID, -32601, "method not found")
	}
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

// isWorkflowWriteTool 判断 tool 是否是 workflow server 暴露的写工具(注册表里除 workflow_list 之外的全部)。
func isWorkflowWriteTool(name string) bool {
	def, ok := agenttool.Lookup(agenttool.KeyWorkflow)
	if !ok {
		return false
	}
	return name != "workflow_list" && slices.Contains(def.ToolNames, name)
}

type workflowListItemView struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	GroupCount int    `json:"groupCount"`
	Content    string `json:"content"`
}

func workflowListView(resp *workflow_svc.ListWorkflowsResponse) any {
	items := make([]workflowListItemView, 0, len(resp.Items))
	for _, it := range resp.Items {
		items = append(items, workflowListItemView{ID: it.ID, Name: it.Name, GroupCount: it.GroupCount, Content: it.Content})
	}
	return map[string]any{"workflows": items}
}

func workflowToolSchemas() []any {
	const approvalNote = "（需要用户审批,调用会挂起直至批准/拒绝/超时）"
	return []any{
		map[string]any{
			"name":        "workflow_list",
			"description": "列出全部协作流程(SOP):id、名称、使用中群数、正文。无参数。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name": "workflow_create", "description": "新建协作流程" + approvalNote,
			"inputSchema": map[string]any{"type": "object", "required": []string{"name"}, "properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "流程名称(必填)"},
				"content": map[string]any{"type": "string", "description": "流程正文(Markdown:角色/步骤/交付物/验收)"},
			}},
		},
		map[string]any{
			"name": "workflow_update", "description": "更新协作流程(只传要改的字段)" + approvalNote,
			"inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{
				"id":      map[string]any{"type": "integer", "description": "流程 id(必填)"},
				"name":    map[string]any{"type": "string", "description": "新名称"},
				"content": map[string]any{"type": "string", "description": "新正文(Markdown)"},
			}},
		},
		map[string]any{
			"name": "workflow_delete", "description": "删除协作流程;绑定它的群将按「不绑定流程」处理" + approvalNote,
			"inputSchema": map[string]any{"type": "object", "required": []string{"id"}, "properties": map[string]any{
				"id": map[string]any{"type": "integer", "description": "流程 id(必填)"},
			}},
		},
	}
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

// randSecret 生成本进程的 HMAC 签名密钥(32 字节)。crypto/rand 失败是不可恢复的灾难;
// 签名密钥绝不能退化为可预测值, 必须 fail loud。
func randSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("workflowtool_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
