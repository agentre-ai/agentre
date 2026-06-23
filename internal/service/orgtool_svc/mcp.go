package orgtool_svc

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
	"github.com/agentre-ai/agentre/internal/service/department_svc"
)

// orgRef 是 org MCP token 绑定的 (agent, session)。
type orgRef struct{ agentID, sessionID int64 }

// orgMCP 是组织架构工具的 MCP-over-HTTP server(挂在 gateway /mcp/org/)。
//
// 身份: 与 group_send 同款无状态签名 token —— `b64url(agent:session).b64url(HMAC(secret, agent:session))`,
// 投递时塞进 mcp-config 的 Authorization header。token 在 CLI spawn 时随 --mcp-config 注入、
// 复用轮不重发,因此必须与子进程同寿命:确定性(同 (agent, session) 每次同值)。lookup 只验签
// (无状态),工具开关由 tools/call 时实时查 DB(agentLookup.Find + ToolEnabled)判定 —— 用户
// 关掉开关后旧 token 立即失效。
type orgMCP struct {
	svc    *orgtoolSvc
	secret []byte // per-process HMAC 签名密钥(组织架构为本机回投,进程内即可)
}

func newOrgMCP(svc *orgtoolSvc) *orgMCP {
	return &orgMCP{svc: svc, secret: randSecret()}
}

// MintToken 为某 (agent, session) 签一个无状态签名 token。同一 (agent, session) 每次返回
// 相同值(确定性),保证被复用/恢复的子进程持有的 token 始终验签通过。
func (h *orgMCP) MintToken(agentID, sessionID int64) string {
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *orgMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// lookup 验签并解出 token 绑定的 (agent, session)。仅做密码学校验(无状态);验签失败 /
// 格式非法 → !ok。
func (h *orgMCP) lookup(tok string) (orgRef, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return orgRef{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return orgRef{}, false
	}
	aStr, sStr, ok := strings.Cut(string(payload), ":")
	if !ok {
		return orgRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return orgRef{}, false
	}
	return orgRef{agentID, sessionID}, true
}

func (h *orgMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
			"serverInfo":      map[string]any{"name": "agentre-org", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": orgToolSchemas()})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.svc.agentLookup == nil { // bootstrap 窗口期(RegisterDeps 未执行)的保险闸
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		// 实时开关校验:用户关掉开关后旧 token 立即失效
		a, err := h.svc.agentLookup.Find(r.Context(), ref.agentID)
		if err != nil || a == nil || !a.ToolEnabled(agenttool.KeyOrg) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch rpc.Params.Name {
		case "org_get":
			resp, err := h.svc.orgQuery.Load(r.Context(), &department_svc.LoadOrgRequest{})
			if err != nil {
				writeRPCError(w, rpc.ID, -32000, err.Error())
				return
			}
			b, _ := json.Marshal(orgGetView(resp))
			writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
		default:
			if !isOrgWriteTool(rpc.Params.Name) {
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

// isOrgWriteTool 判断 tool 是否是 org server 暴露的写工具(注册表里除 org_get 之外的全部)。
func isOrgWriteTool(name string) bool {
	def, ok := agenttool.Lookup(agenttool.KeyOrg)
	if !ok {
		return false
	}
	return name != "org_get" && slices.Contains(def.ToolNames, name)
}

// orgGetDeptView / orgGetAgentView 是 org_get 的 LLM 投影。LoadOrgResponse 是给 Wails
// 前端渲染的 DTO,直接序列化会把 AvatarDataURL(base64 头像,单个可达数百 KB)/avatar 配色/
// prompt/skills/tools/时间戳等对 LLM 无信息量的字段整个灌进 tool result —— 这里只保留
// 组织结构与挂载关系。
type orgGetDeptView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ParentID    int64  `json:"parentId"`
	LeadAgentID int64  `json:"leadAgentId"`
	SortOrder   int    `json:"sortOrder"`
}

type orgGetAgentView struct {
	ID              int64                          `json:"id"`
	Name            string                         `json:"name"`
	Description     string                         `json:"description"`
	SystemBadge     string                         `json:"systemBadge,omitempty"`
	DepartmentID    int64                          `json:"departmentId"`
	DepartmentName  string                         `json:"departmentName"`
	ParentAgentID   int64                          `json:"parentAgentId"`
	ParentAgentName string                         `json:"parentAgentName"`
	Backend         *department_svc.BackendSummary `json:"backend,omitempty"` // BackendSummary 本身是安全子集
	SortOrder       int                            `json:"sortOrder"`
}

// orgGetView 把 LoadOrgResponse 投影成 org_get 的返回视图。
func orgGetView(resp *department_svc.LoadOrgResponse) any {
	depts := make([]orgGetDeptView, 0, len(resp.Departments))
	for _, d := range resp.Departments {
		depts = append(depts, orgGetDeptView{
			ID: d.ID, Name: d.Name, Description: d.Description,
			ParentID: d.ParentID, LeadAgentID: d.LeadAgentID, SortOrder: d.SortOrder,
		})
	}
	agents := make([]orgGetAgentView, 0, len(resp.Agents))
	for _, a := range resp.Agents {
		agents = append(agents, orgGetAgentView{
			ID: a.ID, Name: a.Name, Description: a.Description, SystemBadge: a.SystemBadge,
			DepartmentID: a.DepartmentID, DepartmentName: a.DepartmentName,
			ParentAgentID: a.ParentAgentID, ParentAgentName: a.ParentAgentName,
			Backend: a.Backend, SortOrder: a.SortOrder,
		})
	}
	return map[string]any{"departments": depts, "agents": agents}
}

// orgToolSchemas 返回 org server 暴露的 7 个 MCP 工具 schema。6 个写工具(create/update/delete
// × department/agent)的描述都注明需要用户审批、调用会挂起。
func orgToolSchemas() []any {
	const approvalNote = "（需要用户审批,调用会挂起直至批准/拒绝/超时）"
	return []any{
		map[string]any{
			"name":        "org_get",
			"description": "获取完整组织架构:部门树、agent 及挂载关系(部门/上级 agent)、各 agent 的 backend 摘要。无参数。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{},
			},
		},
		map[string]any{
			"name":        "org_create_department",
			"description": "新建部门" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":        map[string]any{"type": "string", "description": "部门名称(必填)"},
					"description": map[string]any{"type": "string", "description": "部门描述"},
					"parentId":    map[string]any{"type": "integer", "description": "上级部门 id(0/省略=顶级部门)"},
				},
			},
		},
		map[string]any{
			"name":        "org_update_department",
			"description": "更新部门;改 parentId 即把部门移动到新的上级下" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":          map[string]any{"type": "integer", "description": "部门 id(必填)"},
					"name":        map[string]any{"type": "string", "description": "新部门名称"},
					"description": map[string]any{"type": "string", "description": "新部门描述"},
					"leadAgentId": map[string]any{"type": "integer", "description": "负责人 agent id"},
					"parentId":    map[string]any{"type": "integer", "description": "新上级部门 id(改此值即移动部门)"},
				},
			},
		},
		map[string]any{
			"name":        "org_delete_department",
			"description": "删除部门;strategy=reparent 把下级挂到上一层,strategy=cascade 连同子部门/agent 一并删除" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":       map[string]any{"type": "integer", "description": "部门 id(必填)"},
					"strategy": map[string]any{"type": "string", "enum": []string{"reparent", "cascade"}, "description": "reparent=下级上移;cascade=级联删除"},
				},
			},
		},
		map[string]any{
			"name":        "org_create_agent",
			"description": "新建 agent" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"name"},
				"properties": map[string]any{
					"name":          map[string]any{"type": "string", "description": "agent 名称(必填)"},
					"description":   map[string]any{"type": "string", "description": "agent 描述"},
					"departmentId":  map[string]any{"type": "integer", "description": "所属部门 id"},
					"parentAgentId": map[string]any{"type": "integer", "description": "上级 agent id"},
					"backendId":     map[string]any{"type": "integer", "description": "agent 后端 id(0=继承调用者后端)"},
					"prompt":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "系统提示词(逐段)"},
				},
			},
		},
		map[string]any{
			"name":        "org_update_agent",
			"description": "更新 agent;改 departmentId/parentAgentId 即把 agent 移动到新的挂载位置" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":            map[string]any{"type": "integer", "description": "agent id(必填)"},
					"name":          map[string]any{"type": "string", "description": "新 agent 名称"},
					"description":   map[string]any{"type": "string", "description": "新 agent 描述"},
					"prompt":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "新系统提示词(逐段)"},
					"departmentId":  map[string]any{"type": "integer", "description": "新所属部门 id(改此值即移动)"},
					"parentAgentId": map[string]any{"type": "integer", "description": "新上级 agent id(改此值即移动)"},
				},
			},
		},
		map[string]any{
			"name":        "org_delete_agent",
			"description": "删除 agent" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "agent id(必填)"},
				},
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
		panic("orgtool_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
