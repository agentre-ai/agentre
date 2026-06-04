package group_svc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type memberRef struct{ groupID, memberID int64 }

// groupMCP 是 group_send tool 的 MCP-over-HTTP server(挂在 gateway /mcp/group/)。
// 身份: per-member token(投递时塞进 mcp-config 的 Authorization header)。
type groupMCP struct {
	mu     sync.Mutex
	tokens map[string]memberRef
	ingest func(ctx context.Context, memberID int64, body string, mentions []string) error
	newTok func() string
}

func newGroupMCP(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return &groupMCP{tokens: map[string]memberRef{}, ingest: ingest, newTok: randToken}
}

// MintToken 为某成员会话签一个绑定 (group, member) 的 token。
func (h *groupMCP) MintToken(groupID, memberID int64) string {
	tok := h.newTok()
	h.mu.Lock()
	h.tokens[tok] = memberRef{groupID, memberID}
	h.mu.Unlock()
	return tok
}

func (h *groupMCP) lookup(tok string) (memberRef, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.tokens[tok]
	return r, ok
}

func (h *groupMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet { // claude 开 server→client SSE; 我们不推送 → 405(claude 容忍)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string `json:"protocolVersion"`
			Name            string `json:"name"`
			Arguments       struct {
				Body     string   `json:"body"`
				Mentions []string `json:"mentions"`
			} `json:"arguments"`
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
			"serverInfo":      map[string]any{"name": "agentre-group", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": []any{groupSendToolSchema()}})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if rpc.Params.Name != "group_send" {
			writeRPCError(w, rpc.ID, -32601, "unknown tool")
			return
		}
		if h.ingest == nil { // 防御: 未装配 ingest(理论上 D2 后必非 nil)
			writeRPCError(w, rpc.ID, -32000, "ingest not wired")
			return
		}
		if err := h.ingest(r.Context(), ref.memberID, rpc.Params.Arguments.Body, rpc.Params.Arguments.Mentions); err != nil {
			writeRPCError(w, rpc.ID, -32000, err.Error())
			return
		}
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "sent"}}})
	default:
		writeRPCError(w, rpc.ID, -32601, "method not found")
	}
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func groupSendToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_send",
		"description": "向群聊发送一条消息。mentions 填收件成员的显示名(@用户 = 回复人类)。一个回合可多次调用。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"body"},
			"properties": map[string]any{
				"body":     map[string]any{"type": "string", "description": "消息正文"},
				"mentions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "收件成员显示名"},
			},
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

func randToken() string {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand 失败是不可恢复的灾难; auth token 绝不能退化为可预测值, 必须 fail loud。
		panic("group_svc: crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}
