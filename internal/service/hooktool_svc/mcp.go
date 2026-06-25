package hooktool_svc

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
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

// hookRef 是 hook MCP token 绑定的 (agent, session)。
type hookRef struct{ agentID, sessionID int64 }

// hookMCP 是脚本 Hook 工具的 MCP-over-HTTP server(挂在 gateway /mcp/hook/)。
// 身份模型与 orgMCP 一致:无状态签名 token `b64url(agent:session).b64url(HMAC(secret, agent:session))`,
// 投递时塞进 mcp-config 的 Authorization header。lookup 只验签(无状态),工具开关由 tools/call 时
// 实时查 DB(agentLookup.Find + ToolEnabled)判定——用户关掉开关后旧 token 立即失效。
type hookMCP struct {
	svc    *hooktoolSvc
	secret []byte // per-process HMAC 签名密钥(本机回投,进程内即可)
}

func newHookMCP(svc *hooktoolSvc) *hookMCP {
	return &hookMCP{svc: svc, secret: randSecret()}
}

// MintToken 为某 (agent, session) 签一个无状态签名 token(确定性,复用轮不重发)。
func (h *hookMCP) MintToken(agentID, sessionID int64) string {
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *hookMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// lookup 验签并解出 token 绑定的 (agent, session);验签失败 / 格式非法 → !ok。
func (h *hookMCP) lookup(tok string) (hookRef, bool) {
	payloadB64, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return hookRef{}, false
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil || !hmac.Equal([]byte(h.sign(string(payload))), []byte(sig)) {
		return hookRef{}, false
	}
	aStr, sStr, ok := strings.Cut(string(payload), ":")
	if !ok {
		return hookRef{}, false
	}
	agentID, err1 := strconv.ParseInt(aStr, 10, 64)
	sessionID, err2 := strconv.ParseInt(sStr, 10, 64)
	if err1 != nil || err2 != nil {
		return hookRef{}, false
	}
	return hookRef{agentID, sessionID}, true
}

func (h *hookMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet { // 不推送 server→client SSE → 405(claude 容忍)
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
			"serverInfo":      map[string]any{"name": "agentre-hook", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": hookToolSchemas()})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if h.svc.agentLookup == nil || h.svc.hooks == nil { // bootstrap 窗口期保险闸
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
		// 实时开关校验:用户关掉开关后旧 token 立即失效
		a, err := h.svc.agentLookup.Find(r.Context(), ref.agentID)
		if err != nil || a == nil || !a.ToolEnabled(agenttool.KeyHook) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		switch rpc.Params.Name {
		case "hook_list":
			h.svc.handleList(w, r, rpc.ID)
		case "hook_get":
			h.svc.handleGet(w, r, rpc.ID, rpc.Params.Arguments)
		default:
			if !isHookWriteTool(rpc.Params.Name) {
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

// isHookWriteTool 判断 tool 是否是 hook server 暴露的写工具(注册表里除两个读工具之外的全部)。
func isHookWriteTool(name string) bool {
	def, ok := agenttool.Lookup(agenttool.KeyHook)
	if !ok {
		return false
	}
	return name != "hook_list" && name != "hook_get" && slices.Contains(def.ToolNames, name)
}

// ---- 读工具 ----

// handleList 列全部 hook 的精简视图(无 command 正文 / 无 env 值,省 token)。
func (s *hooktoolSvc) handleList(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage) {
	resp, err := s.hooks.Load(r.Context(), &hook_svc.LoadHooksRequest{})
	if err != nil {
		writeRPCError(w, rpcID, -32000, err.Error())
		return
	}
	rows := make([]hookListRow, 0, len(resp.Hooks))
	for _, h := range resp.Hooks {
		rows = append(rows, hookListRow{
			ID: h.ID, Name: h.Name, Interpreter: h.Interpreter, InterpreterPath: h.InterpreterPath,
			ScheduleExpr: h.ScheduleExpr, Enabled: h.Enabled,
			LastStatus: h.LastStatus, LastRunAt: h.LastRunAt,
			NextRunAt: h.NextRunAt, TotalCount: h.TotalCount,
		})
	}
	b, _ := json.Marshal(map[string]any{"hooks": rows})
	writeRPCResult(w, rpcID, textResult(string(b)))
}

// handleGet 取单 hook 全文(command + 脱敏 env)+ 最近事件。
func (s *hooktoolSvc) handleGet(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, rawArgs json.RawMessage) {
	var args getHookArgs
	_ = json.Unmarshal(rawArgs, &args)
	if args.ID <= 0 {
		writeRPCError(w, rpcID, -32602, "缺少 id")
		return
	}
	resp, err := s.hooks.Load(r.Context(), &hook_svc.LoadHooksRequest{HookID: args.ID, Limit: 20})
	if err != nil {
		writeRPCError(w, rpcID, -32000, err.Error())
		return
	}
	var found *hook_svc.HookItem
	for _, h := range resp.Hooks {
		if h.ID == args.ID {
			found = h
			break
		}
	}
	if found == nil {
		writeRPCError(w, rpcID, -32000, "hook 不存在")
		return
	}
	b, _ := json.Marshal(map[string]any{"hook": found, "events": resp.Events})
	writeRPCResult(w, rpcID, textResult(string(b)))
}

// hookListRow 是 hook_list 的精简行(剔除 command 正文与 env,省 token)。
type hookListRow struct {
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Interpreter     string `json:"interpreter"`
	InterpreterPath string `json:"interpreterPath"`
	ScheduleExpr    string `json:"scheduleExpr"`
	Enabled         bool   `json:"enabled"`
	LastStatus      string `json:"lastStatus"`
	LastRunAt       int64  `json:"lastRunAt"`
	NextRunAt       int64  `json:"nextRunAt"`
	TotalCount      int64  `json:"totalCount"`
}

// hookToolSchemas 返回 hook server 暴露的 6 个 MCP 工具 schema。4 个写工具(create/update/delete/run)
// 的描述都注明需要用户审批、调用会挂起。
func hookToolSchemas() []any {
	const approvalNote = "（需要用户审批,调用会挂起直至批准/拒绝/超时）"
	const interpDesc = "解释器,取值之一:bash|sh|node|python|pwsh|powershell|cmd(须在运行机器的 PATH 中)"
	const envItems = "环境变量/密钥条目;secret=true 的值读取时脱敏为 ********"
	envSchema := func(desc string) map[string]any {
		return map[string]any{
			"type":        "array",
			"description": desc,
			"items": map[string]any{
				"type":     "object",
				"required": []string{"key", "value"},
				"properties": map[string]any{
					"key":    map[string]any{"type": "string"},
					"value":  map[string]any{"type": "string"},
					"secret": map[string]any{"type": "boolean"},
				},
			},
		}
	}
	return []any{
		map[string]any{
			"name":        "hook_list",
			"description": "列出全部脚本 Hook(名称/解释器/cron 调度/启用状态/上次结果/累计事件数)。无参数。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
		map[string]any{
			"name":        "hook_get",
			"description": "取单个 Hook 全文:脚本正文 command、env(密钥脱敏为 ********)、调度、最近产出事件。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "hook id(必填)"},
				},
			},
		},
		map[string]any{
			"name": "hook_create",
			"description": "新建脚本 Hook。脚本契约:host 注入环境变量 HOOK_STATE(上次返回的 state JSON)/HOOK_NAME/HOOK_ID 及各 env 条目;" +
				"脚本须向 stdout 打印单个 JSON 对象 {\"events\":[{\"title\":\"必填\",\"dedupeKey\":\"可选去重键\",\"payload\":{...}}],\"state\":{...可选,整体替换游标}}。" +
				"退出码非 0 或 stdout 非合法 JSON 即判失败。" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"name", "interpreter", "command", "scheduleExpr"},
				"properties": map[string]any{
					"name":            map[string]any{"type": "string", "description": "Hook 名称(唯一)"},
					"interpreter":     map[string]any{"type": "string", "description": interpDesc},
					"interpreterPath": map[string]any{"type": "string", "description": "可选:解释器二进制的自定义路径;留空则按 interpreter 自动解析(LookPath)。"},
					"command":         map[string]any{"type": "string", "description": "脚本正文(按 interpreter 解释)"},
					"scheduleExpr":    map[string]any{"type": "string", "description": "cron 表达式,如 */5 * * * *"},
					"timezone":        map[string]any{"type": "string", "description": "cron 时区,默认 Asia/Shanghai"},
					"enabled":         map[string]any{"type": "boolean", "description": "是否启用,省略=启用"},
					"env":             envSchema(envItems),
				},
			},
		},
		map[string]any{
			"name":        "hook_update",
			"description": "更新 Hook;仅传要改的字段,未传字段沿用现值。env 传入即整体替换,其中值为 ******** 的条目保留原密钥。" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":              map[string]any{"type": "integer", "description": "hook id(必填)"},
					"name":            map[string]any{"type": "string"},
					"interpreter":     map[string]any{"type": "string", "description": interpDesc},
					"interpreterPath": map[string]any{"type": "string", "description": "可选:解释器二进制的自定义路径;留空则按 interpreter 自动解析(LookPath)。"},
					"command":         map[string]any{"type": "string"},
					"scheduleExpr":    map[string]any{"type": "string"},
					"timezone":        map[string]any{"type": "string"},
					"enabled":         map[string]any{"type": "boolean"},
					"env":             envSchema("传入即整体替换 env;不传则不动"),
				},
			},
		},
		map[string]any{
			"name":        "hook_delete",
			"description": "删除 Hook" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id": map[string]any{"type": "integer", "description": "hook id(必填)"},
				},
			},
		},
		map[string]any{
			"name": "hook_run",
			"description": "立即执行一次 Hook 脚本。dryRun=true(默认)只回 stdout/解析事件/去重预览,不落库不改 state;" +
				"dryRun=false 真执行并落库。注意:即便 dryRun 也会在本机真实运行该脚本。" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":     map[string]any{"type": "integer", "description": "hook id(必填)"},
					"dryRun": map[string]any{"type": "boolean", "description": "省略=true(试运行)"},
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

// textResult 把一段文本包成 MCP tool result 结构。
func textResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

// randSecret 生成本进程的 HMAC 签名密钥(32 字节)。crypto/rand 失败是不可恢复的灾难,必须 fail loud。
func randSecret() []byte {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic("hooktool_svc: crypto/rand failed: " + err.Error())
	}
	return b
}
