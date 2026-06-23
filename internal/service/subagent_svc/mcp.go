package subagent_svc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// subagentMCP 是「调用子 agent」工具的 MCP-over-HTTP server(挂在 gateway /mcp/subagent/)。
// token 与 orgtool 同款无状态签名,绑定 (agent, session) = 发起调用的父 agent 与其会话。
type subagentMCP struct {
	svc    *subagentSvc
	secret []byte
}

func newSubagentMCP(svc *subagentSvc) *subagentMCP {
	return &subagentMCP{svc: svc, secret: randSecret()}
}

func (h *subagentMCP) MintToken(agentID, sessionID int64) string {
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *subagentMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (h *subagentMCP) lookup(tok string) (subagentRef, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return subagentRef{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return subagentRef{}, false
	}
	aStr, sStr, ok := strings.Cut(string(payload), ":")
	if !ok {
		return subagentRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return subagentRef{}, false
	}
	return subagentRef{agentID, sessionID}, true
}

func (h *subagentMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
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
			"serverInfo":      map[string]any{"name": "agentre-subagent", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": subagentToolSchemas()})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.svc.agents == nil { // bootstrap 窗口期(RegisterDeps 未执行)的保险闸
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		a, err := h.svc.agents.Find(r.Context(), ref.agentID)
		if err != nil || a == nil || !a.ToolEnabled(agenttool.KeySubagent) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch rpc.Params.Name {
		case "agent_list":
			h.handleAgentList(w, r, rpc.ID)
		case "agent_call":
			h.handleAgentCall(w, r, rpc.ID, ref, rpc.Params.Arguments)
		default:
			writeRPCError(w, rpc.ID, -32601, "unknown tool")
		}
	default:
		writeRPCError(w, rpc.ID, -32601, "method not found")
	}
}

type agentListItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SystemBadge string `json:"systemBadge,omitempty"`
}

func (h *subagentMCP) handleAgentList(w http.ResponseWriter, r *http.Request, id json.RawMessage) {
	list, err := h.svc.agents.List(r.Context())
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	out := make([]agentListItem, 0, len(list))
	for _, a := range list {
		out = append(out, agentListItem{ID: a.ID, Name: a.Name, Description: a.Description, SystemBadge: a.SystemBadge})
	}
	b, _ := json.Marshal(out)
	writeRPCResult(w, id, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
}

func (h *subagentMCP) handleAgentCall(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref subagentRef, rawArgs json.RawMessage) {
	var args struct {
		AgentName string `json:"agent_name"`
		Prompt    string `json:"prompt"`
	}
	_ = json.Unmarshal(rawArgs, &args)
	if strings.TrimSpace(args.AgentName) == "" || strings.TrimSpace(args.Prompt) == "" {
		writeRPCError(w, id, -32602, "agent_name 和 prompt 均为必填")
		return
	}
	text, err := h.svc.callAgent(r.Context(), ref, args.AgentName, args.Prompt)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
}

func subagentToolSchemas() []any {
	return []any{
		map[string]any{
			"name":        "agent_list",
			"description": "列出可作为子 agent 调用的全部已配置 agent(id/名称/描述)。无参数。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "agent_call",
			"description": "把一段子任务委派给指定的已配置 agent 执行,同步阻塞直至其完成,返回它的最终文本输出。子 agent 在隔离的一次性会话中运行(看不到当前对话),任务须能在数分钟内完成。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"agent_name", "prompt"},
				"properties": map[string]any{
					"agent_name": map[string]any{"type": "string", "description": "目标 agent 名称(见 agent_list)"},
					"prompt":     map[string]any{"type": "string", "description": "交给子 agent 的完整任务描述(它看不到当前对话上下文,需自包含)"},
				},
			},
		},
	}
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func writeRPCResult(w http.ResponseWriter, id json.RawMessage, result any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeRPCError(w http.ResponseWriter, id json.RawMessage, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": code, "message": msg}})
}

func randSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("subagent_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
