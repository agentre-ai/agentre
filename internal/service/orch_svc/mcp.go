package orch_svc

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// orchMCP is the MCP-over-HTTP server for the orchestrate tool set (mounted at /mcp/orchestrate/).
// Token format mirrors subagent_svc: base64(agentID:sessionID).HMAC-SHA256.
type orchMCP struct {
	svc    *orchSvc
	secret []byte
}

type orchRef struct {
	agentID   int64
	sessionID int64
}

// ensureMCP lazily constructs the orchMCP singleton (process-scoped random secret).
func (s *orchSvc) ensureMCP() *orchMCP {
	s.mcpOnce.Do(func() {
		s.mcp = &orchMCP{svc: s, secret: randSecret()}
	})
	return s.mcp
}

// MintToken returns an HMAC-signed token binding (agentID, sessionID).
// Called by the bootstrap layer when injecting the orchestrate tool URL into an agent session.
func (s *orchSvc) MintToken(agentID, sessionID int64) string {
	m := s.ensureMCP()
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + m.sign(payload)
}

func (m *orchMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// lookup verifies the token and returns the embedded orchRef.
func (m *orchMCP) lookup(tok string) (orchRef, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return orchRef{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(m.sign(string(payload))), []byte(sig)) {
		return orchRef{}, false
	}
	aStr, sStr, ok := strings.Cut(string(payload), ":")
	if !ok {
		return orchRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return orchRef{}, false
	}
	return orchRef{agentID: agentID, sessionID: sessionID}, true
}

// MCPHandler returns the http.Handler to mount at /mcp/orchestrate/.
// The handler implements the MCP streamable-HTTP transport shape used by subagent_svc:
//   - GET → 405 (codex gracefully degrades; no SSE stream needed)
//   - POST → JSON-RPC dispatch with HMAC token verification at the top
func (s *orchSvc) MCPHandler() http.Handler {
	m := s.ensureMCP()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET is not supported (codex uses graceful degradation per subagent_svc pattern).
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		// Verify token on EVERY request (real claude/codex sends Authorization on all requests
		// including initialize/tools/list). On failure → 403 before touching the body.
		ref, ok := m.lookup(bearer(r))
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
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
				"serverInfo":      map[string]any{"name": "agentre-orch", "version": "1"},
				"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeRPCResult(w, rpc.ID, map[string]any{"tools": orchToolSchemas()})
		case "tools/call":
			// Capability gate — mirror the injection gate in turn.go BuildTurnMCP:
			// 放行条件 = agent 勾了 orchestrate 工具 或 会话本身是编排会话(绑定了编排
			// Dispatch)。后者让经编排创建的 Leader/子任务会话即便其 agent 未勾 orchestrate,
			// 也能真正调用注进去的工具 —— 否则注入门放行、调用门 403,整条 Run 静默降级。
			if m.svc.agents == nil {
				http.Error(w, "service unavailable", http.StatusServiceUnavailable)
				return
			}
			a, err := m.svc.agents.Find(r.Context(), ref.agentID)
			if err != nil || a == nil ||
				(!a.ToolEnabled(agenttool.KeyOrchestrate) && !m.svc.sessionInRun(r.Context(), ref.sessionID)) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			m.dispatchTool(w, r, rpc.ID, ref, rpc.Params.Name, rpc.Params.Arguments)
		default:
			writeRPCError(w, rpc.ID, -32601, "method not found")
		}
	})
}

func (m *orchMCP) dispatchTool(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, name string, args json.RawMessage) {
	switch name {
	case "agent_list":
		m.handleAgentList(w, r, id, ref)
	case "dispatch":
		m.handleDispatch(w, r, id, ref, args)
	case "ask":
		m.handleAsk(w, r, id, ref, args)
	case "reply":
		m.handleReply(w, r, id, ref, args)
	case "send":
		m.handleSend(w, r, id, ref, args)
	case "finish":
		m.handleFinish(w, r, id, ref, args)
	case "report":
		m.handleReport(w, r, id, ref, args)
	case "read":
		m.handleRead(w, r, id, ref, args)
	case "status":
		m.handleStatus(w, r, id, ref)
	case "task_list":
		m.handleTaskList(w, r, id, ref)
	case "task_add":
		m.handleTaskAdd(w, r, id, ref, args)
	case "task_update":
		m.handleTaskUpdate(w, r, id, ref, args)
	default:
		writeRPCError(w, id, -32601, "unknown tool")
	}
}

func (m *orchMCP) handleDispatch(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Agent string `json:"agent"`
		Brief string `json:"brief"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Agent == "" || p.Brief == "" {
		writeRPCError(w, id, -32602, "agent and brief are required")
		return
	}
	taskID, err := m.svc.Dispatch(r.Context(), ref.sessionID, p.Agent, p.Brief)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(fmt.Sprintf("已派发,task_id=%d", taskID)))
}

func (m *orchMCP) handleAsk(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Agent    string `json:"agent"`
		Question string `json:"question"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Agent == "" || p.Question == "" {
		writeRPCError(w, id, -32602, "agent and question are required")
		return
	}
	ans, err := m.svc.Ask(r.Context(), ref.sessionID, p.Agent, p.Question)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(ans))
}

func (m *orchMCP) handleReply(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		AskID  string `json:"ask_id"`
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.AskID == "" || p.Answer == "" {
		writeRPCError(w, id, -32602, "ask_id and answer are required")
		return
	}
	// ref.agentID is the replier's identity (from the HMAC token) — this is how 防串答 works.
	if err := m.svc.Reply(r.Context(), ref.agentID, p.AskID, p.Answer); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已送达提问者"))
}

func (m *orchMCP) handleSend(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		DispatchID int64  `json:"task_id"`
		Message    string `json:"message"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.DispatchID <= 0 || p.Message == "" {
		writeRPCError(w, id, -32602, "task_id and message are required")
		return
	}
	if err := m.svc.Send(r.Context(), ref.sessionID, p.DispatchID, p.Message); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已续做"))
}

func (m *orchMCP) handleFinish(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Summary == "" {
		writeRPCError(w, id, -32602, "summary is required")
		return
	}
	if err := m.svc.Finish(r.Context(), ref.sessionID, p.Summary); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已收口"))
}

func (m *orchMCP) handleReport(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Note == "" {
		writeRPCError(w, id, -32602, "note is required")
		return
	}
	if err := m.svc.Report(r.Context(), ref.sessionID, p.Note); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已汇报"))
}

func (m *orchMCP) handleRead(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		DispatchID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.DispatchID <= 0 {
		writeRPCError(w, id, -32602, "task_id is required")
		return
	}
	out, err := m.svc.ReadDispatch(r.Context(), ref.sessionID, p.DispatchID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(out))
}

func (m *orchMCP) handleStatus(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef) {
	out, err := m.svc.RunStatus(r.Context(), ref.sessionID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(out))
}

func (m *orchMCP) handleTaskList(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef) {
	out, err := m.svc.TaskList(r.Context(), ref.sessionID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(out))
}

func (m *orchMCP) handleTaskAdd(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Text == "" {
		writeRPCError(w, id, -32602, "text is required")
		return
	}
	tid, err := m.svc.TaskAdd(r.Context(), ref.sessionID, ref.agentID, p.Text)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(fmt.Sprintf("已加入清单,task_id=%d", tid)))
}

func (m *orchMCP) handleTaskUpdate(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		TaskID int64  `json:"task_id"`
		Status string `json:"status"`
		Claim  bool   `json:"claim"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.TaskID <= 0 || (p.Status == "" && !p.Claim) {
		writeRPCError(w, id, -32602, "task_id required; give status or claim")
		return
	}
	if err := m.svc.TaskUpdate(r.Context(), ref.sessionID, ref.agentID, p.TaskID, p.Status, p.Claim); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已更新"))
}

// textResult 将文本包装成 MCP content 格式（Tasks 10/11/12 复用）。
func textResult(s string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": s}}}
}

type agentListItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	SystemBadge string `json:"systemBadge,omitempty"`
	Running     int    `json:"running"`
}

func (m *orchMCP) handleAgentList(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef) {
	list, err := m.svc.ListAllowedAgentsWithLoad(r.Context(), ref.sessionID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	out := make([]agentListItem, 0, len(list))
	for _, aw := range list {
		out = append(out, agentListItem{
			ID: aw.Agent.ID, Name: aw.Agent.Name, Description: aw.Agent.Description,
			SystemBadge: aw.Agent.SystemBadge, Running: aw.Running,
		})
	}
	b, _ := json.Marshal(out)
	writeRPCResult(w, id, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
}

func orchToolSchemas() []any {
	return []any{
		map[string]any{
			"name":        "agent_list",
			"description": "列出你本次可调度的 agent(受可参与范围约束;id/名称/描述/能力/running——每个 agent 当前在跑的派发数)。据此拆活、按名 dispatch,优先派给 running 低的 agent。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "dispatch",
			"description": "把一个子任务异步派给某 agent(按名),新起一条子会话并行跑;它完成后结果会自动回报给你、触发你下一轮决策。可对同一 agent 多次 dispatch(即多个子任务)。审核/测试/合并都是 dispatch 给相应 agent。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"agent", "brief"},
				"properties": map[string]any{
					"agent": map[string]any{"type": "string", "description": "目标 agent 名(取自 agent_list)"},
					"brief": map[string]any{"type": "string", "description": "任务说明 + 验证目标;需要的产物引用写进来"},
				},
			},
		},
		map[string]any{
			"name":        "ask",
			"description": "向另一个 agent 提问并等待其回复;问题会被注入对方的活会话(它带着自己的上下文回答),答案回报你。用于咨询同伴上下文里才有的信息,不新增任务节点。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"agent", "question"},
				"properties": map[string]any{
					"agent":    map[string]any{"type": "string"},
					"question": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "reply",
			"description": "回复别人通过 ask 向你提出的问题。必须原样带上提问消息里给你的 ask_id,否则提问方收不到。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"ask_id", "answer"},
				"properties": map[string]any{
					"ask_id": map[string]any{"type": "string", "description": "提问消息里给出的 ask_id,原样带回"},
					"answer": map[string]any{"type": "string", "description": "你的回答"},
				},
			},
		},
		map[string]any{
			"name":        "send",
			"description": "往你派发的某任务会话里发后续/返工反馈(同会话续做),更新后的结果再回报你。返工=对原任务再 send,不新增节点。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"task_id", "message"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer"},
					"message": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "finish",
			"description": "收口当前任务/Run,向上回报一份结构化小结。你是根(Leader)时 finish = 整个 Run 完成。无次数上限,自行判断何时收口。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"summary"},
				"properties": map[string]any{
					"summary": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "report",
			"description": "运行中向派发你的上级主动汇报一条中途进展/中间结论(不收口、不改状态)。默认完成时上级只收到通知,主动 report/finish 才把内容内联给它。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"note"},
				"properties": map[string]any{
					"note": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "read",
			"description": "读取你派发/同 Run 内某任务的输出:已完成→拉小结+完整正文;运行中→peek 它当前最新进展。传通知/status 里给出的 task_id。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer"},
				},
			},
		},
		map[string]any{
			"name":        "status",
			"description": "查看本次编排整棵任务树的实时快照(每个子任务的 id/agent/类型/状态/brief/是否已主动汇报/所属流程节点/在等哪些子任务)。两次回报之间用它掌握全局。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "task_list",
			"description": "读取本次编排的待办清单(所有 agent 共享的协作白板;与派发树无关)。返回每条 task_id/seq/文本/状态/认领人。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "task_add",
			"description": "往待办清单加一条(一句话)。返回 task_id。清单纯组织思路、给人看进度,不触发任何执行。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"text"},
				"properties": map[string]any{
					"text": map[string]any{"type": "string"},
				},
			},
		},
		map[string]any{
			"name":        "task_update",
			"description": "更新某待办:改状态(pending/in_progress/done)或认领(claim=true 把自己设为负责人)。至少给一个。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer"},
					"status":  map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done"}},
					"claim":   map[string]any{"type": "boolean"},
				},
			},
		},
	}
}

// ── helpers (mirrored from subagent_svc; different package so no name collision) ──

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
		panic("orch_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
