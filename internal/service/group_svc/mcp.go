package group_svc

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type memberRef struct{ groupID, memberID int64 }

// createTokenPrefix 标记「单聊建群」token 的 payload 前缀,与成员 token(groupID:memberID)
// 共用同一 secret 与验签;两类 token 互不通行(group_create 只认 create token,群工具只认成员 token)。
const createTokenPrefix = "create:"

type createRef struct{ agentID, sessionID int64 }

// groupMCP 是群聊工具(group_send/invite/task 三件套/group_create)的 MCP-over-HTTP server(挂在 gateway /mcp/group/)。
//
// 身份: 无状态签名 token —— `b64url(group:member).b64url(HMAC(secret, group:member))`,
// 投递时塞进 mcp-config 的 Authorization header。token 只在 CLI spawn 时随 --mcp-config
// 注入、复用轮不会重发,因此必须与子进程同寿命:确定性(同成员每次同值)+ 跨重启/恢复稳定。
// 旧实现用内存 map 存随机 token + 停止/归档/离群时 delete,但被复用的常驻子进程仍持旧 token,
// 一旦 map 里没了(停止吊销 / 进程重启)group_send 就拿 401,被 CLI 误报"需要重新授权"。
// 现改为:lookup 只验签(无状态),发言权由 authorized 按 DB 成员资格实时判定(见 memberCanPost)。
type groupMCP struct {
	secret []byte // per-process HMAC 签名密钥(群聊为本机回投,进程内即可;重启随子进程池一并重建)
	ingest func(ctx context.Context, memberID int64, body string, mentions []string) error
	invite func(ctx context.Context, memberID int64, names []string, ids []int64, reason string) ([]InviteResult, error)
	authz  func(ctx context.Context, groupID, memberID int64) bool // 发言权判定;nil=放行(测试默认)
	// 任务三件套回调(返回 task_no / err,保持 mcp 层与实体解耦)。
	taskCreate   func(ctx context.Context, memberID int64, assignee, title, brief string, parentTaskNo int) (int, error)
	taskComplete func(ctx context.Context, memberID int64, taskNo int, result string) error
	taskCancel   func(ctx context.Context, memberID int64, taskNo int, reason string) error
	// groupCreate 是 group_create tool 的回调:审批 + 建群在 svc 层完成,返回写回 CLI 的
	// result 文本。error 仅用于内部故障;审批拒绝/超时必须编码为返回的 text(不走 RPC error,
	// 镜像 orgtool 审批语义),否则 CLI 会把用户拒绝当工具故障。
	groupCreate func(ctx context.Context, agentID, sessionID int64, title string, memberNames []string, brief string, workflowID int64, memberNicknames map[string]string) (string, error)
}

func newGroupMCP(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return &groupMCP{secret: randSecret(), ingest: ingest}
}

// MintToken 为某成员签一个绑定 (group, member) 的无状态签名 token。同一 (group, member)
// 每次返回相同值(确定性),保证被复用/恢复的子进程持有的 token 始终验签通过。
func (h *groupMCP) MintToken(groupID, memberID int64) string {
	payload := strconv.FormatInt(groupID, 10) + ":" + strconv.FormatInt(memberID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *groupMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// MintCreateToken 为某 (agent, session) 签一个单聊建群 token(确定性,跨重启前提同 MintToken)。
func (h *groupMCP) MintCreateToken(agentID, sessionID int64) string {
	payload := createTokenPrefix + strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

// verifyPayload 验签并还原 payload;签名不符 / 格式非法 → !ok。
func (h *groupMCP) verifyPayload(tok string) (string, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return "", false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return "", false
	}
	return string(payload), true
}

// lookupCreate 验签并解出 create token 绑定的 (agent, session);成员 token → !ok。
func (h *groupMCP) lookupCreate(tok string) (createRef, bool) {
	payload, ok := h.verifyPayload(tok)
	if !ok {
		return createRef{}, false
	}
	rest, found := strings.CutPrefix(payload, createTokenPrefix)
	if !found {
		return createRef{}, false
	}
	aStr, sStr, ok := strings.Cut(rest, ":")
	if !ok {
		return createRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return createRef{}, false
	}
	return createRef{agentID, sessionID}, true
}

// lookup 验签并解出 token 绑定的 (group, member)。仅做密码学校验(无状态),不查成员资格 ——
// 是否仍有发言权(离群 / 归档)由 authorized 按 DB 现状判定。验签失败 / 格式非法 → !ok。
func (h *groupMCP) lookup(tok string) (memberRef, bool) {
	payload, ok := h.verifyPayload(tok)
	if !ok || strings.HasPrefix(payload, createTokenPrefix) {
		return memberRef{}, false
	}
	gStr, mStr, ok := strings.Cut(payload, ":")
	if !ok {
		return memberRef{}, false
	}
	groupID, err1 := strconv.ParseInt(gStr, 10, 64)
	memberID, err2 := strconv.ParseInt(mStr, 10, 64)
	if err1 != nil || err2 != nil {
		return memberRef{}, false
	}
	return memberRef{groupID, memberID}, true
}

// authorized 报告 (group, member) 当前是否仍可发言。authz 未装配(测试默认)→ 放行。
func (h *groupMCP) authorized(ctx context.Context, groupID, memberID int64) bool {
	return h.authz == nil || h.authz(ctx, groupID, memberID)
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
				Body            string            `json:"body"`
				Mentions        []string          `json:"mentions"`
				AgentNames      []string          `json:"agentNames"`
				AgentIDs        []int64           `json:"agentIds"`
				Reason          string            `json:"reason"`
				Assignee        string            `json:"assignee"`
				Title           string            `json:"title"`
				Brief           string            `json:"brief"`
				MemberNames     []string          `json:"memberNames"`
				MemberNicknames map[string]string `json:"memberNicknames"`
				WorkflowID      int64             `json:"workflowId"`
				// ParentTaskID/TaskID 是任务编号(#N, per-group),不是 group_tasks.id。
				ParentTaskID int    `json:"parentTaskId"`
				TaskID       int    `json:"taskId"`
				Result       string `json:"result"`
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
		writeRPCResult(w, rpc.ID, map[string]any{"tools": []any{
			groupSendToolSchema(), groupInviteToolSchema(),
			groupTaskCreateToolSchema(), groupTaskCompleteToolSchema(), groupTaskCancelToolSchema(),
			groupCreateToolSchema(),
		}})
	case "tools/call":
		if rpc.Params.Name == "group_create" {
			cref, ok := h.lookupCreate(bearer(r))
			if !ok {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			if h.groupCreate == nil {
				writeRPCError(w, rpc.ID, -32000, "group create not wired")
				return
			}
			text, err := h.groupCreate(r.Context(), cref.agentID, cref.sessionID,
				rpc.Params.Arguments.Title, rpc.Params.Arguments.MemberNames, rpc.Params.Arguments.Brief,
				rpc.Params.Arguments.WorkflowID, rpc.Params.Arguments.MemberNicknames)
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}})
			return
		}
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !h.authorized(r.Context(), ref.groupID, ref.memberID) { // 离群 / 群归档即失权(按 DB 现状)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch rpc.Params.Name {
		case "group_send":
			if h.ingest == nil { // 防御: 生产装配后应始终非 nil
				writeRPCError(w, rpc.ID, -32000, "ingest not wired")
				return
			}
			if err := h.ingest(r.Context(), ref.memberID, rpc.Params.Arguments.Body, rpc.Params.Arguments.Mentions); err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "sent"}}})
		case "group_invite":
			if h.invite == nil {
				writeRPCError(w, rpc.ID, -32000, "invite not wired")
				return
			}
			results, err := h.invite(r.Context(), ref.memberID, rpc.Params.Arguments.AgentNames, rpc.Params.Arguments.AgentIDs, rpc.Params.Arguments.Reason)
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			names := make([]string, 0, len(results))
			for _, x := range results {
				names = append(names, x.Name)
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "invited: " + strings.Join(names, ", ")}}})
		case "group_task_create":
			if h.taskCreate == nil {
				writeRPCError(w, rpc.ID, -32000, "task create not wired")
				return
			}
			no, err := h.taskCreate(r.Context(), ref.memberID, rpc.Params.Arguments.Assignee,
				rpc.Params.Arguments.Title, rpc.Params.Arguments.Brief, rpc.Params.Arguments.ParentTaskID)
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
				"text": fmt.Sprintf("task #%d created", no)}}})
		case "group_task_complete":
			if h.taskComplete == nil {
				writeRPCError(w, rpc.ID, -32000, "task complete not wired")
				return
			}
			if err := h.taskComplete(r.Context(), ref.memberID, rpc.Params.Arguments.TaskID, rpc.Params.Arguments.Result); err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
				"text": fmt.Sprintf("task #%d completed", rpc.Params.Arguments.TaskID)}}})
		case "group_task_cancel":
			if h.taskCancel == nil {
				writeRPCError(w, rpc.ID, -32000, "task cancel not wired")
				return
			}
			if err := h.taskCancel(r.Context(), ref.memberID, rpc.Params.Arguments.TaskID, rpc.Params.Arguments.Reason); err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
				"text": fmt.Sprintf("task #%d canceled", rpc.Params.Arguments.TaskID)}}})
		default:
			writeRPCError(w, rpc.ID, -32601, "unknown tool")
		}
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

func groupInviteToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_invite",
		"description": "把可用的 Agent 拉进当前群聊(可跨部门;优先同部门,跨部门请在 reason 说明理由)。只有主持人可调用。agentNames 或 agentIds 二选一。",
		"inputSchema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agentNames": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "被邀请成员显示名"},
				"agentIds":   map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": "被邀请成员 agent id"},
				"reason":     map[string]any{"type": "string", "description": "邀请理由(可选)"},
			},
		},
	}
}

func groupTaskCreateToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_create",
		"description": "建一张任务卡并派给某成员(建卡即派活,对方会立即收到;assignee 不能是自己)。跨成员派活/交接一律用任务卡而不是裸 group_send。brief 写清楚要做什么+验收标准;验证类任务用 parentTaskId 回指被验证的任务编号。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"assignee", "title", "brief"},
			"properties": map[string]any{
				"assignee":     map[string]any{"type": "string", "description": "执行成员的显示名(不能是自己)"},
				"title":        map[string]any{"type": "string", "description": "任务短标题"},
				"brief":        map[string]any{"type": "string", "description": "任务说明,含验收标准;交付物路径建议 .agentre/handoff/<群ID>/task-<编号>-<slug>.md"},
				"parentTaskId": map[string]any{"type": "integer", "description": "回指的任务编号(#N,可选,验证类任务用)"},
			},
		},
	}
}

func groupTaskCompleteToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_complete",
		"description": "交付你名下的任务。result 必填:写清改动/产出了什么、自测/验证情况——这是交付物,会投回建卡人。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"taskId", "result"},
			"properties": map[string]any{
				"taskId": map[string]any{"type": "integer", "description": "任务编号(#N)"},
				"result": map[string]any{"type": "string", "description": "交付说明(改动文件、自测结论、产物路径)"},
			},
		},
	}
}

func groupTaskCancelToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_cancel",
		"description": "取消一张未完成的任务卡(仅建卡人或主持人)。打回返工不要用取消,新建一张任务卡。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"taskId", "reason"},
			"properties": map[string]any{
				"taskId": map[string]any{"type": "integer", "description": "任务编号(#N)"},
				"reason": map[string]any{"type": "string", "description": "取消原因"},
			},
		},
	}
}

func groupCreateToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_create",
		"description": "为一项需要多人协作的任务创建群聊并自任主持人(需用户在聊天里批准后才执行)。memberNames 填初始成员 agent 的显示名(可跨部门,后续也可在群内 group_invite 招募)。你的当前对话上下文不会带进群,brief 必须完整转述需求与验收标准——它会作为首条群消息发给你的群内分身,作为拆解任务的唯一依据。需要按既定流程协作时,先建/查流程再用 workflowId 绑定。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"title", "memberNames", "brief"},
			"properties": map[string]any{
				"title":           map[string]any{"type": "string", "description": "群标题"},
				"memberNames":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "初始成员显示名(不含你自己;最多 7 个,主持人占 1 席)"},
				"memberNicknames": map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "可选;成员显示名→该成员在本群的备注名(群昵称)的映射,如 {\"Codex\":\"后端工程师\"}。只在本群显示、不改 Agent 全局名;未列出的成员沿用原名。"},
				"brief":           map[string]any{"type": "string", "description": "完整需求转述 + 验收标准(首条群消息,拆任务的依据)"},
				"workflowId":      map[string]any{"type": "integer", "description": "可选;绑定一个协作流程(SOP)的 id,主持人每轮注入其最新正文。先用 workflow_list 查或 workflow_create 建;省略或 0 = 不绑定。"},
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

// randSecret 生成本进程的 HMAC 签名密钥(32 字节)。crypto/rand 失败是不可恢复的灾难;
// 签名密钥绝不能退化为可预测值, 必须 fail loud。
func randSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("group_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
