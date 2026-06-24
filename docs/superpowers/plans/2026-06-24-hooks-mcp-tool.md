# Hooks MCP 创作工具(Plan 2)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增内置 agent 工具 `hook`(MCP server `/mcp/hook/`),让对话里的 agent 能 `hook_list`/`hook_get`/`hook_create`/`hook_update`/`hook_delete`/`hook_run`,边写脚本边试跑、注册调度——严格镜像现有 `orgtool_svc`(token + 实时开关门控 + 通用 `tool_approval`)。

**Architecture:** 新建 `internal/service/hooktool_svc`,只做 token/开关校验 + 审批挂起,业务全部委托给 Plan 1 的 `hook_svc`(narrow consumer interface,DIP)。`agenttool` 注册表追加一条 `KeyHook`;bootstrap 三行挂载(`RegisterMCP` + `SetGatewayBaseURL` + `RegisterTurnMCPProvider`);`RegisterDeps` 延迟到 `app.go registerChatService()`(需 `chat_svc.Chat()` 非 nil)。`hook_get` 复用 `hook_svc.Load` 客户端过滤——**不改 Plan 1 代码**(纯加法)。

**Tech Stack:** Go 1.26, cago, MCP-over-HTTP(无外部 MCP 库,手写 JSON-RPC),HMAC 无状态 token,`go.uber.org/mock`(mockgen),goconvey + testify。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每个写工具/读工具先写失败测试再实现。
- **不改 Plan 1 的 `hook_svc`/`hook_repo`/`hook_entity`。** Plan 2 纯加法:新包 + 注册表追加 + bootstrap/app 接线 + 前端 i18n 标签。唯一允许修改的既有文件:`internal/pkg/agenttool/agenttool.go`(+其测试)、`internal/bootstrap/cago.go`、`internal/app/app.go`、前端 `tool-catalog.ts` + 两份 `common.json`。
- **Repository 单测用 sqlmock,service 单测注 mock,绝不连库。** 本计划无 repo 改动;hooktool_svc 单测注 `mock_hooktool_svc` + 走 `httptest`。
- **新可见前端文案走 i18n**(`zh-CN` + `en` 双份),`i18next/no-literal-string` 把关。
- **gitmoji commit,golangci-lint v2 零 issue,gofmt/goimports。**
- **`hook_run`(含 dryRun)走审批**(每次执行用户机上脚本都需点头);§12.1 的「dry-run 免审」是未来可选放宽,本期不做。
- **worktree:`GOWORK=off` 跑所有 Go 命令**(父 `go.work` 不含 worktree 路径);后端测试不需要 `frontend/dist`/`wailsjs`。前端单测需 `make generate` 或 `cp` wailsjs(见 [[agentre-worktree-build-gotchas]])。

---

## File Structure

| 层 | 文件 | 动作 |
| --- | --- | --- |
| registry | `internal/pkg/agenttool/agenttool.go` | 改:加 `KeyHook` 常量 + 注册表条目(6 工具) |
| registry test | `internal/pkg/agenttool/agenttool_test.go` | 改:`Len` 5→6、`Keys()` 加 `"hook"`、加 `TestRegistry_HasHook` |
| svc deps | `internal/service/hooktool_svc/deps.go` | 新增:`HookService`/`AgentLookup`/`ApprovalGateway` 窄接口 + `go:generate` |
| svc types | `internal/service/hooktool_svc/types.go` | 新增:写工具 arg struct |
| svc shell | `internal/service/hooktool_svc/hooktool.go` | 新增:`hooktoolSvc` + `Default`/`RegisterDeps`/`MCPHandler`/`SetGatewayBaseURL`/`BuildTurnMCP` |
| svc mcp | `internal/service/hooktool_svc/mcp.go` | 新增:`hookMCP`(token)+ `ServeHTTP` + 读工具(list/get)+ schema + RPC 助手 |
| svc approval | `internal/service/hooktool_svc/approval.go` | 新增:`handleWriteTool` + `execWriteTool` + create/update/delete/run handler |
| svc mocks | `internal/service/hooktool_svc/mock_hooktool_svc/*` | 生成 |
| svc tests | `internal/service/hooktool_svc/*_test.go` | 新增 |
| bootstrap | `internal/bootstrap/cago.go` | 改:挂 `/mcp/hook/` + `SetGatewayBaseURL` + `RegisterTurnMCPProvider` |
| app | `internal/app/app.go` | 改:`registerChatService()` 里 `hooktool_svc.Default().RegisterDeps(...)` |
| i18n | `frontend/src/i18n/locales/{zh-CN,en}/common.json` | 改:`org.agent.tools.names.hook` + `descriptions.hook` |
| 前端 | `frontend/src/components/agentre/org/tool-catalog.ts` | 改:`APPROVAL_TOOLS` 加 `"hook"` |

依赖方向:`app → hooktool_svc → hook_svc`;`hooktool_svc` 只调 `hook_svc.Hook()`(narrow interface),不绕层。

---

## Task 1: agenttool 注册表追加 KeyHook

**Files:**
- Modify: `internal/pkg/agenttool/agenttool.go`
- Test: `internal/pkg/agenttool/agenttool_test.go`

**Interfaces:**
- Produces: `agenttool.KeyHook = "hook"`;`Lookup("hook")` → `{Key:"hook", MCPPath:"/mcp/hook/", ToolNames:[hook_list,hook_get,hook_create,hook_update,hook_delete,hook_run]}`;`Keys()` 末尾追加 `"hook"`。

- [ ] **Step 1: 更新注册表测试(Red)**

在 `agenttool_test.go` 把 `TestRegistry` 的两处断言改为含 hook,并新增 `TestRegistry_HasHook`:

```go
func TestRegistry(t *testing.T) {
	defs := Registry()
	require.Len(t, defs, 6)
	require.Equal(t, "org", defs[0].Key)
	require.Equal(t, "/mcp/org/", defs[0].MCPPath)
	require.Contains(t, defs[0].ToolNames, "org_get")
	require.Len(t, defs[0].ToolNames, 7)

	d, ok := Lookup("org")
	require.True(t, ok)
	require.Equal(t, KeyOrg, d.Key)
	_, ok = Lookup("nope")
	require.False(t, ok)

	require.Equal(t, []string{"org", "workflow", "group_create", "subagent", "orchestrate", "hook"}, Keys())
}

func TestRegistry_HasHook(t *testing.T) {
	d, ok := Lookup(KeyHook)
	require.True(t, ok)
	require.Equal(t, "hook", d.Key)
	require.Equal(t, "/mcp/hook/", d.MCPPath)
	require.Equal(t, []string{
		"hook_list", "hook_get", "hook_create", "hook_update", "hook_delete", "hook_run",
	}, d.ToolNames)
	require.Contains(t, Keys(), KeyHook)
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `GOWORK=off go test ./internal/pkg/agenttool/...`
Expected: FAIL(`Len 5 != 6`,`undefined: KeyHook`)

- [ ] **Step 3: 实现注册表条目**

`agenttool.go` 在 `KeyOrchestrate` 常量后加:

```go
// KeyHook 脚本 Hook 读写/试运行工具。
const KeyHook = "hook"
```

并在 `registry` 切片末尾(`KeyOrchestrate` 条目后)加:

```go
	{Key: KeyHook, MCPPath: "/mcp/hook/", ToolNames: []string{
		"hook_list", "hook_get", "hook_create", "hook_update", "hook_delete", "hook_run",
	}},
```

- [ ] **Step 4: 跑测试看通过**

Run: `GOWORK=off go test ./internal/pkg/agenttool/... ./internal/service/department_svc/...`
Expected: PASS(department_svc 的 `AvailableTools == Keys()` 断言是动态比较,自动跟随)

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/agenttool/agenttool.go internal/pkg/agenttool/agenttool_test.go
git commit -m "✨ hook: agenttool 注册表追加 KeyHook(6 工具)"
```

---

## Task 2: hooktool_svc deps + types + mockgen

**Files:**
- Create: `internal/service/hooktool_svc/deps.go`
- Create: `internal/service/hooktool_svc/types.go`
- Generate: `internal/service/hooktool_svc/mock_hooktool_svc/mock_deps.go`

**Interfaces:**
- Consumes(from Plan 1 `hook_svc`):`Load(ctx, *hook_svc.LoadHooksRequest) (*hook_svc.LoadHooksResponse, error)`、`CreateHook`、`UpdateHook`、`DeleteHook`、`RunHook`。
- Consumes(from `agent_repo`):`AgentLookup.Find(ctx, id) (*agent_entity.Agent, error)`。
- Consumes(from `chat_svc`):`ApprovalGateway.BeginToolApproval/FinishToolApproval`。
- Produces:三个窄接口 + arg struct,供 Task 3/4/5 使用。

- [ ] **Step 1: 写 deps.go**

```go
// Package hooktool_svc 脚本 Hook 工具(agent 内置工具 key="hook")的 MCP 接入与审批编排。
// 业务执行全部委托 hook_svc,本包只做 token/开关校验 + 审批挂起。
package hooktool_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

//go:generate mockgen -source deps.go -destination mock_hooktool_svc/mock_deps.go

// HookService 是 hook_svc 的窄投影(读 + CRUD + 试运行/立即运行)。
type HookService interface {
	Load(ctx context.Context, req *hook_svc.LoadHooksRequest) (*hook_svc.LoadHooksResponse, error)
	CreateHook(ctx context.Context, req *hook_svc.CreateHookRequest) (*hook_svc.HookItem, error)
	UpdateHook(ctx context.Context, req *hook_svc.UpdateHookRequest) (*hook_svc.HookItem, error)
	DeleteHook(ctx context.Context, id int64) error
	RunHook(ctx context.Context, req *hook_svc.RunHookRequest) (*hook_svc.RunHookResult, error)
}

// AgentLookup 实时校验调用者 agent 的工具开关(agent_repo 的窄投影)。
type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
}

// ApprovalGateway 审批卡登记/决议(chat_svc 通用工具审批网关的窄投影)。
type ApprovalGateway interface {
	BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
	FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}
```

- [ ] **Step 2: 写 types.go(写工具 arg struct)**

```go
package hooktool_svc

import "github.com/agentre-ai/agentre/internal/service/hook_svc"

// 写工具参数 struct。create 用值类型(全量提供);update 用指针区分"不传(沿用现值)"
// 与"显式置空";env 用 *[]EnvVar 区分"不动 env"与"清空 env"。

type createHookArgs struct {
	Name         string           `json:"name"`
	Interpreter  string           `json:"interpreter"`
	Command      string           `json:"command"`
	ScheduleExpr string           `json:"scheduleExpr"`
	Timezone     string           `json:"timezone"`
	Env          []hook_svc.EnvVar `json:"env"`
	Enabled      *bool            `json:"enabled"` // 省略=默认启用
}

type updateHookArgs struct {
	ID           int64              `json:"id"`
	Name         *string            `json:"name"`
	Interpreter  *string            `json:"interpreter"`
	Command      *string            `json:"command"`
	ScheduleExpr *string            `json:"scheduleExpr"`
	Timezone     *string            `json:"timezone"`
	Env          *[]hook_svc.EnvVar `json:"env"`
	Enabled      *bool              `json:"enabled"`
}

type deleteHookArgs struct {
	ID int64 `json:"id"`
}

type runHookArgs struct {
	ID     int64 `json:"id"`
	DryRun *bool `json:"dryRun"` // 省略=true(默认试运行,不落库)
}

type getHookArgs struct {
	ID int64 `json:"id"`
}
```

- [ ] **Step 3: 生成 mock**

Run: `cd internal/service/hooktool_svc && GOWORK=off go generate .`
Expected: 生成 `mock_hooktool_svc/mock_deps.go`(含 `MockHookService`/`MockAgentLookup`/`MockApprovalGateway`)。若 `go generate` 因缺其它文件编译失败,先放占位 `hooktool.go`(见 Task 3)再生成,或 Task 2/3 合并提交。

> 说明:`deps.go` 仅声明接口,自身可独立编译;mockgen `-source` 模式只解析该文件,不需要包内其它文件先存在。但包内若有**别的**不编译文件会拖累。Task 2 只引入 deps.go/types.go,二者自洽,可直接 generate。

- [ ] **Step 4: 验证编译**

Run: `GOWORK=off go build ./internal/service/hooktool_svc/...`
Expected: 成功(仅接口 + types + mock,无逻辑)。

- [ ] **Step 5: Commit**

```bash
git add internal/service/hooktool_svc/deps.go internal/service/hooktool_svc/types.go internal/service/hooktool_svc/mock_hooktool_svc/
git commit -m "✨ hook: hooktool_svc deps 窄接口 + 写工具 arg struct + mock"
```

---

## Task 3: hooktool_svc 服务壳 + BuildTurnMCP + 读工具

**Files:**
- Create: `internal/service/hooktool_svc/hooktool.go`
- Create: `internal/service/hooktool_svc/mcp.go`
- Test: `internal/service/hooktool_svc/mcp_test.go`

**Interfaces:**
- Consumes: Task 2 的 `HookService`/`AgentLookup`/`ApprovalGateway`、`agenttool.KeyHook`。
- Produces: `Default()`、`(*hooktoolSvc).RegisterDeps(HookService, AgentLookup, ApprovalGateway)`、`MCPHandler() http.Handler`、`SetGatewayBaseURL(string)`、`BuildTurnMCP(ctx, *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec`;`(*hookMCP).MintToken(agentID, sessionID int64) string`。

- [ ] **Step 1: 写 hooktool.go(服务壳)**

镜像 `orgtool_svc/orgtool.go`,3 个依赖(无 query/command 拆分,统一 `hooks HookService`):

```go
package hooktool_svc

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

type hooktoolSvc struct {
	mcp             *hookMCP
	mcpOnce         sync.Once
	gatewayBaseURL  string
	approvalTimeout time.Duration

	hooks       HookService
	agentLookup AgentLookup
	approval    ApprovalGateway
}

var defaultHooktool = &hooktoolSvc{approvalTimeout: 4 * time.Minute}

// Default 取默认服务单例。
func Default() *hooktoolSvc { return defaultHooktool }

// RegisterDeps bootstrap 接线(生产传 hook_svc.Hook()/agent_repo.Agent()/chat_svc.Chat());测试注 mock。
func (s *hooktoolSvc) RegisterDeps(h HookService, l AgentLookup, ap ApprovalGateway) {
	s.hooks, s.agentLookup, s.approval = h, l, ap
}

func (s *hooktoolSvc) mcpHandlerInit() *hookMCP {
	s.mcpOnce.Do(func() { s.mcp = newHookMCP(s) })
	return s.mcp
}

// MCPHandler 返回挂到 gateway /mcp/hook/ 的 HTTP handler。
func (s *hooktoolSvc) MCPHandler() http.Handler { return s.mcpHandlerInit() }

// SetGatewayBaseURL 由 bootstrap 在 gateway 起好后注入。
func (s *hooktoolSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider:agent 开启 hook 工具时返回注入 spec。
func (s *hooktoolSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID int64, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeyHook) || s.gatewayBaseURL == "" {
		return nil
	}
	def, ok := agenttool.Lookup(agenttool.KeyHook)
	if !ok {
		return nil
	}
	return []agentruntime.MCPServerSpec{{
		Name:    def.Key,
		URL:     s.gatewayBaseURL + def.MCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + s.mcpHandlerInit().MintToken(a.ID, sessionID)},
		Tools:   def.ToolNames,
	}}
}
```

- [ ] **Step 2: 写 mcp.go(token + ServeHTTP + 读工具 + schema + RPC 助手)**

token/sign/lookup/randSecret/writeRPCResult/writeRPCError/bearer 与 org **逐字镜像**(包私有,各包独立一份)。`serverInfo.name` 用 `"agentre-hook"`。读工具路由 `hook_list`/`hook_get` 在 `tools/call` 内联;其余 default 走 `isHookWriteTool` → `handleWriteTool`(Task 4 实现)。

```go
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
// 身份模型与 orgMCP 一致:无状态签名 token,工具开关由 tools/call 时实时查 DB 判定。
type hookMCP struct {
	svc    *hooktoolSvc
	secret []byte
}

func newHookMCP(svc *hooktoolSvc) *hookMCP {
	return &hookMCP{svc: svc, secret: randSecret()}
}

func (h *hookMCP) MintToken(agentID, sessionID int64) string {
	payload := strconv.FormatInt(agentID, 10) + ":" + strconv.FormatInt(sessionID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + h.sign(payload)
}

func (h *hookMCP) sign(payload string) string {
	mac := hmac.New(sha256.New, h.secret)
	mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

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
		if h.svc.agentLookup == nil || h.svc.hooks == nil {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}
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

func isHookWriteTool(name string) bool {
	def, ok := agenttool.Lookup(agenttool.KeyHook)
	if !ok {
		return false
	}
	return name != "hook_list" && name != "hook_get" && slices.Contains(def.ToolNames, name)
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
		panic("hooktool_svc: crypto/rand failed: " + err.Error())
	}
	return b
}

// ---- 读工具 ----

// handleList 列全部 hook 的精简视图(无 command 正文 / 无 env 值)。
func (s *hooktoolSvc) handleList(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage) {
	resp, err := s.hooks.Load(r.Context(), &hook_svc.LoadHooksRequest{})
	if err != nil {
		writeRPCError(w, rpcID, -32000, err.Error())
		return
	}
	rows := make([]hookListRow, 0, len(resp.Hooks))
	for _, h := range resp.Hooks {
		rows = append(rows, hookListRow{
			ID: h.ID, Name: h.Name, Interpreter: h.Interpreter,
			ScheduleExpr: h.ScheduleExpr, Enabled: h.Enabled,
			LastStatus: h.LastStatus, LastRunAt: h.LastRunAt,
			NextRunAt: h.NextRunAt, TotalCount: h.TotalCount,
		})
	}
	b, _ := json.Marshal(map[string]any{"hooks": rows})
	writeRPCResult(w, rpcID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
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
	writeRPCResult(w, rpcID, map[string]any{"content": []any{map[string]any{"type": "text", "text": string(b)}}})
}

// hookListRow 是 hook_list 的精简行(剔除 command 正文与 env,省 token)。
type hookListRow struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Interpreter  string `json:"interpreter"`
	ScheduleExpr string `json:"scheduleExpr"`
	Enabled      bool   `json:"enabled"`
	LastStatus   string `json:"lastStatus"`
	LastRunAt    int64  `json:"lastRunAt"`
	NextRunAt    int64  `json:"nextRunAt"`
	TotalCount   int64  `json:"totalCount"`
}

// hookToolSchemas 返回 hook server 暴露的 6 个 MCP 工具 schema。
func hookToolSchemas() []any {
	const approvalNote = "（需要用户审批,调用会挂起直至批准/拒绝/超时）"
	const interpDesc = "解释器,取值之一:bash|sh|node|python|pwsh|powershell|cmd(须在运行机器的 PATH 中)"
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
					"name":         map[string]any{"type": "string", "description": "Hook 名称(唯一)"},
					"interpreter":  map[string]any{"type": "string", "description": interpDesc},
					"command":      map[string]any{"type": "string", "description": "脚本正文(按 interpreter 解释)"},
					"scheduleExpr": map[string]any{"type": "string", "description": "cron 表达式,如 */5 * * * *"},
					"timezone":     map[string]any{"type": "string", "description": "cron 时区,默认 Asia/Shanghai"},
					"enabled":      map[string]any{"type": "boolean", "description": "是否启用,省略=启用"},
					"env": map[string]any{
						"type":        "array",
						"description": "环境变量/密钥;secret=true 的值在读取时脱敏",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"key", "value"},
							"properties": map[string]any{
								"key":    map[string]any{"type": "string"},
								"value":  map[string]any{"type": "string"},
								"secret": map[string]any{"type": "boolean"},
							},
						},
					},
				},
			},
		},
		map[string]any{
			"name":        "hook_update",
			"description": "更新 Hook;仅传要改的字段,未传字段沿用现值。env 传 ******** 的值保留原密钥。" + approvalNote,
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"id"},
				"properties": map[string]any{
					"id":           map[string]any{"type": "integer", "description": "hook id(必填)"},
					"name":         map[string]any{"type": "string"},
					"interpreter":  map[string]any{"type": "string", "description": interpDesc},
					"command":      map[string]any{"type": "string"},
					"scheduleExpr": map[string]any{"type": "string"},
					"timezone":     map[string]any{"type": "string"},
					"enabled":      map[string]any{"type": "boolean"},
					"env": map[string]any{
						"type":        "array",
						"description": "传入即整体替换 env;不传则不动",
						"items": map[string]any{
							"type":     "object",
							"required": []string{"key", "value"},
							"properties": map[string]any{
								"key":    map[string]any{"type": "string"},
								"value":  map[string]any{"type": "string"},
								"secret": map[string]any{"type": "boolean"},
							},
						},
					},
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
```

- [ ] **Step 3: 写 mcp_test.go(Red)**

镜像 `orgtool_svc/mcp_test.go`。helper:`newTestSvc(lookup AgentLookup, hooks HookService) *hooktoolSvc`、`hookEnabledAgent(id)`/`hookDisabledAgent(id)`(`agent_entity.Agent{ID:id, ...}` 带 `tools_json` 开/关 hook)、`rpcCall(h, body, token)`。

```go
func TestHookMCP_BuildTurnMCP(t *testing.T) {
	Convey("BuildTurnMCP", t, func() {
		s := &hooktoolSvc{approvalTimeout: time.Minute}
		s.SetGatewayBaseURL("http://127.0.0.1:52401")

		Convey("hook 开关 ON → 返回 1 个 spec", func() {
			specs := s.BuildTurnMCP(context.Background(), hookEnabledAgent(7), 99, 0)
			So(len(specs), ShouldEqual, 1)
			So(specs[0].Name, ShouldEqual, "hook")
			So(specs[0].URL, ShouldEqual, "http://127.0.0.1:52401/mcp/hook/")
			So(specs[0].Headers["Authorization"], ShouldStartWith, "Bearer ")
			So(len(specs[0].Tools), ShouldEqual, 6)
		})
		Convey("hook 开关 OFF → nil", func() {
			So(s.BuildTurnMCP(context.Background(), hookDisabledAgent(7), 99, 0), ShouldBeNil)
		})
	})
}

func TestHookMCP_TokenAndList(t *testing.T) {
	Convey("合法 token + hook_list 路由到 HookService.Load", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_hooktool_svc.NewMockAgentLookup(ctrl)
		hooks := mock_hooktool_svc.NewMockHookService(ctrl)
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(hookEnabledAgent(7), nil)
		hooks.EXPECT().Load(gomock.Any(), gomock.Any()).Return(&hook_svc.LoadHooksResponse{
			Hooks: []*hook_svc.HookItem{{ID: 1, Name: "巡检", Interpreter: "bash", ScheduleExpr: "*/5 * * * *", Enabled: true}},
		}, nil)

		s := newTestSvc(lookup, hooks)
		token := s.mcpHandlerInit().MintToken(7, 99)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_list"}}`, token)
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "巡检")
	})

	Convey("篡改 token → 401", t, func() {
		s := newTestSvc(nil, nil)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_list"}}`, "bad.token")
		So(w.Code, ShouldEqual, http.StatusUnauthorized)
	})

	Convey("hook 开关 OFF → 403", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_hooktool_svc.NewMockAgentLookup(ctrl)
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(hookDisabledAgent(7), nil)
		s := newTestSvc(lookup, mock_hooktool_svc.NewMockHookService(ctrl))
		token := s.mcpHandlerInit().MintToken(7, 99)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_list"}}`, token)
		So(w.Code, ShouldEqual, http.StatusForbidden)
	})

	Convey("tools/list 无需 token → 返回 6 工具", t, func() {
		s := newTestSvc(nil, nil)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "hook_create")
	})
}
```

> `hookEnabledAgent`/`hookDisabledAgent` 怎么写:看 `orgtool_svc/mcp_test.go` 里 `orgEnabledAgent` 的实现——它构造 `agent_entity.Agent` 并设 `Tools`/`tools_json` 使 `ToolEnabled("org")` 返回真。照搬,把 key 换成 `"hook"`。先读那个 helper 再落地。

- [ ] **Step 4: 跑测试看失败→实现→通过**

Run: `GOWORK=off go test ./internal/service/hooktool_svc/...`
Expected: 先 FAIL(handleWriteTool 未定义——Task 4 才有,本 Task 暂时用空桩或先只测读路径)。

> **执行提示**:本 Task 只实现读工具 + 壳;`handleWriteTool` 在 Task 4。为让本 Task 编译通过,在 `mcp.go` 末尾或 approval.go 先放一个最小桩:
> ```go
> // 占位,Task 4 实现完整审批流。
> func (s *hooktoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref hookRef, tool string, rawArgs json.RawMessage) {
> 	writeRPCError(w, rpcID, -32000, "not implemented")
> }
> ```
> Task 4 用真实现替换该桩。

- [ ] **Step 5: Commit**

```bash
git add internal/service/hooktool_svc/hooktool.go internal/service/hooktool_svc/mcp.go internal/service/hooktool_svc/mcp_test.go
git commit -m "✨ hook: hooktool_svc 服务壳 + BuildTurnMCP + 读工具(list/get)"
```

---

## Task 4: 写工具 + 审批(approval.go)

**Files:**
- Create: `internal/service/hooktool_svc/approval.go`(替换 Task 3 的 `handleWriteTool` 桩)
- Test: `internal/service/hooktool_svc/approval_test.go`

**Interfaces:**
- Consumes: `ApprovalGateway`、`HookService`、`blocks.ToolApprovalBlock`、Task 2 arg structs。
- Produces: `(*hooktoolSvc).handleWriteTool(...)` 真实现 + `execWriteTool` 分发 create/update/delete/run。

- [ ] **Step 1: 写 approval.go**

`handleWriteTool` 与 org **逐字镜像**(把 `agenttool.KeyOrg`→`agenttool.KeyHook`、`orgRef`→`hookRef`)。`execWriteTool` 分发 4 个写工具:

```go
package hooktool_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

func (s *hooktoolSvc) handleWriteTool(w http.ResponseWriter, r *http.Request, rpcID json.RawMessage, ref hookRef, tool string, rawArgs json.RawMessage) {
	var input map[string]any
	_ = json.Unmarshal(rawArgs, &input)
	requestID := uuid.NewString()
	blk := &blocks.ToolApprovalBlock{ToolKey: agenttool.KeyHook, RequestID: requestID, ToolName: tool, ToolInput: input, Status: "pending"}

	ch, err := s.approval.BeginToolApproval(r.Context(), ref.sessionID, blk)
	if err != nil {
		writeRPCError(w, rpcID, -32000, "审批通道不可用: "+err.Error())
		return
	}

	select {
	case allow := <-ch:
		if !allow {
			_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "denied", "")
			writeRPCResult(w, rpcID, textResult("用户拒绝了此操作"))
			return
		}
		result, execErr := s.execWriteTool(r.Context(), tool, rawArgs)
		if execErr != nil {
			_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", "执行失败: "+execErr.Error())
			writeRPCResult(w, rpcID, textResult("已批准但执行失败: "+execErr.Error()))
			return
		}
		_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "approved", result)
		writeRPCResult(w, rpcID, textResult(result))
	case <-time.After(s.approvalTimeout):
		_ = s.approval.FinishToolApproval(r.Context(), ref.sessionID, requestID, "expired", "")
		writeRPCResult(w, rpcID, textResult("审批超时，操作未执行"))
	case <-r.Context().Done():
		_ = s.approval.FinishToolApproval(context.Background(), ref.sessionID, requestID, "expired", "")
	}
}

func textResult(text string) map[string]any {
	return map[string]any{"content": []any{map[string]any{"type": "text", "text": text}}}
}

func (s *hooktoolSvc) execWriteTool(ctx context.Context, tool string, rawArgs json.RawMessage) (string, error) {
	switch tool {
	case "hook_create":
		return s.createHook(ctx, rawArgs)
	case "hook_update":
		return s.updateHook(ctx, rawArgs)
	case "hook_delete":
		return s.deleteHook(ctx, rawArgs)
	case "hook_run":
		return s.runHook(ctx, rawArgs)
	default:
		return "", fmt.Errorf("未知写工具: %s", tool)
	}
}

func (s *hooktoolSvc) createHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args createHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	enabled := true
	if args.Enabled != nil {
		enabled = *args.Enabled
	}
	item, err := s.hooks.CreateHook(ctx, &hook_svc.CreateHookRequest{
		Name:         args.Name,
		Interpreter:  args.Interpreter,
		Command:      args.Command,
		ScheduleExpr: args.ScheduleExpr,
		Timezone:     args.Timezone,
		Env:          args.Env,
		Enabled:      enabled,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已创建 Hook「%s」(id=%d)", item.Name, item.ID), nil
}

// updateHook 先取现值再 merge 未提供字段(env 传入即整体替换;密钥用 ******** 占位由 hook_svc 保留)。
func (s *hooktoolSvc) updateHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args updateHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	cur, err := s.loadHook(ctx, args.ID)
	if err != nil {
		return "", err
	}
	req := &hook_svc.UpdateHookRequest{ID: args.ID}
	req.Name = orStr(args.Name, cur.Name)
	req.Interpreter = orStr(args.Interpreter, cur.Interpreter)
	req.Command = orStr(args.Command, cur.Command)
	req.ScheduleExpr = orStr(args.ScheduleExpr, cur.ScheduleExpr)
	req.Timezone = orStr(args.Timezone, cur.Timezone)
	if args.Env != nil {
		req.Env = *args.Env
	} else {
		req.Env = cur.Env // cur.Env 的密钥已是 ******** ,hook_svc.preserveSecrets 会保留原值
	}
	req.Enabled = cur.Enabled
	if args.Enabled != nil {
		req.Enabled = *args.Enabled
	}
	item, err := s.hooks.UpdateHook(ctx, req)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("已更新 Hook「%s」(id=%d)", item.Name, item.ID), nil
}

func (s *hooktoolSvc) deleteHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args deleteHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	if err := s.hooks.DeleteHook(ctx, args.ID); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除 Hook(id=%d)", args.ID), nil
}

func (s *hooktoolSvc) runHook(ctx context.Context, rawArgs json.RawMessage) (string, error) {
	var args runHookArgs
	if err := json.Unmarshal(rawArgs, &args); err != nil {
		return "", err
	}
	dryRun := true
	if args.DryRun != nil {
		dryRun = *args.DryRun
	}
	res, err := s.hooks.RunHook(ctx, &hook_svc.RunHookRequest{ID: args.ID, DryRun: dryRun})
	if err != nil {
		return "", err
	}
	b, _ := json.Marshal(res)
	return string(b), nil
}

// loadHook 从全量里按 id 找 hook 现值(update merge 需要)。
func (s *hooktoolSvc) loadHook(ctx context.Context, id int64) (*hook_svc.HookItem, error) {
	resp, err := s.hooks.Load(ctx, &hook_svc.LoadHooksRequest{HookID: id})
	if err != nil {
		return nil, err
	}
	for _, h := range resp.Hooks {
		if h.ID == id {
			return h, nil
		}
	}
	return nil, fmt.Errorf("找不到 Hook(id=%d)", id)
}

// orStr 指针非 nil 取其值,否则取兜底。
func orStr(p *string, fallback string) string {
	if p != nil {
		return *p
	}
	return fallback
}
```

> 删掉 Task 3 在 mcp.go 里放的 `handleWriteTool` 占位桩。

- [ ] **Step 2: 写 approval_test.go(Red)**

镜像 `orgtool_svc/approval_test.go`。helper:`newWriteSvc(ctrl) (*hooktoolSvc, *writeDeps)`,`writeDeps{lookup, hooks, apv}`,`callWrite(s, body, token)`。

```go
func TestHookApproval_CreateApproved(t *testing.T) {
	Convey("hook_create 挂起 → allow=true → CreateHook 执行", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(hookEnabledAgent(7), nil)
		apvCh := make(chan bool, 1)
		var mu sync.Mutex
		var gotKey, gotName string
		d.apv.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error) {
				mu.Lock(); gotKey = blk.ToolKey; gotName = blk.ToolName; mu.Unlock()
				return apvCh, nil
			})
		d.hooks.EXPECT().CreateHook(gomock.Any(), gomock.Any()).Return(&hook_svc.HookItem{ID: 5, Name: "巡检"}, nil)
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_create","arguments":{"name":"巡检","interpreter":"bash","command":"echo {}","scheduleExpr":"*/5 * * * *"}}}`, token)
		apvCh <- true
		<-done

		mu.Lock(); So(gotKey, ShouldEqual, "hook"); So(gotName, ShouldEqual, "hook_create"); mu.Unlock()
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "巡检")
	})
}

func TestHookApproval_Denied(t *testing.T) {
	Convey("allow=false → 不调 CreateHook,回拒绝文案", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(hookEnabledAgent(7), nil)
		apvCh := make(chan bool, 1)
		d.apv.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).Return((<-chan bool)(apvCh), nil)
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "denied", "").Return(nil)
		// 不 EXPECT CreateHook → 调用即 fail

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_create","arguments":{"name":"x","interpreter":"bash","command":"echo {}","scheduleExpr":"* * * * *"}}}`, token)
		apvCh <- false
		<-done
		So(w.Body.String(), ShouldContainSubstring, "拒绝")
	})
}

func TestHookApproval_RunDryApproved(t *testing.T) {
	Convey("hook_run dryRun 默认 true → RunHook(DryRun=true) → 回 RunHookResult", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(hookEnabledAgent(7), nil)
		apvCh := make(chan bool, 1)
		d.apv.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).Return((<-chan bool)(apvCh), nil)
		d.hooks.EXPECT().RunHook(gomock.Any(), &hook_svc.RunHookRequest{ID: 3, DryRun: true}).
			Return(&hook_svc.RunHookResult{ExitCode: 0, Persisted: false, NewCount: 2}, nil)
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"hook_run","arguments":{"id":3}}}`, token)
		apvCh <- true
		<-done
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "newCount")
	})
}
```

- [ ] **Step 3: 跑测试看失败→实现→通过**

Run: `GOWORK=off go test ./internal/service/hooktool_svc/...`
Expected: 全 PASS。

- [ ] **Step 4: lint**

Run: `cd /Users/codfrm/Code/agentre/agentre && GOWORK=off golangci-lint run ./internal/service/hooktool_svc/... ./internal/pkg/agenttool/...`
Expected: 0 issue。

- [ ] **Step 5: Commit**

```bash
git add internal/service/hooktool_svc/approval.go internal/service/hooktool_svc/approval_test.go internal/service/hooktool_svc/mcp.go
git commit -m "✨ hook: hooktool_svc 写工具 + 通用 tool_approval(create/update/delete/run)"
```

---

## Task 5: bootstrap + app.go 接线 + 全量后端绿

**Files:**
- Modify: `internal/bootstrap/cago.go`(挂 `/mcp/hook/`)
- Modify: `internal/app/app.go`(`registerChatService()` 里 RegisterDeps)

**Interfaces:**
- Consumes: `hooktool_svc.Default()`、`hook_svc.Hook()`、`agent_repo.Agent()`、`chat_svc.Chat()`、`gw`。

- [ ] **Step 1: bootstrap 挂载**

`cago.go` 在 subagent 挂载块之后(或 orchestrate 块附近)加:

```go
	// 挂脚本 Hook 工具 MCP handler(/mcp/hook/) + 注册 TurnMCPProvider:
	// agent 开了 hook 工具的会话 turn 注入该 MCP server(写操作/执行审批在服务端,见 hooktool_svc)。
	// RegisterDeps(含 chat_svc.Chat())延迟到 app.go registerChatService()。
	gw.RegisterMCP("/mcp/hook/", hooktool_svc.Default().MCPHandler())
	hooktool_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(hooktool_svc.Default().BuildTurnMCP)
```

并加 import `"github.com/agentre-ai/agentre/internal/service/hooktool_svc"`。

- [ ] **Step 2: app.go RegisterDeps**

`registerChatService()` 末尾(`orch_svc.Default().RegisterDeps(...)` 之后)加:

```go
	// hooktool_svc 依赖:hook_svc.Hook() 满足 HookService;agent_repo.Agent() 满足 AgentLookup;
	// chat_svc.Chat() 满足 ApprovalGateway。须在 RegisterChat 之后(chat_svc.Chat() 非 nil)。
	hooktool_svc.Default().RegisterDeps(hook_svc.Hook(), agent_repo.Agent(), chat_svc.Chat())
```

并确认/补 import `hook_svc`、`hooktool_svc`(`agent_repo`/`chat_svc` 已在)。

- [ ] **Step 3: 编译 + 全量后端测试**

Run: `cd /Users/codfrm/Code/agentre/agentre && GOWORK=off go build ./... && GOWORK=off go test ./internal/...`
Expected: 全 PASS(忽略既有 flaky `orch_svc/TestFinish_RootCollapsesRun`,重跑即过——见 [[project_hooks_script_redesign]])。

- [ ] **Step 4: lint(全量)**

Run: `cd /Users/codfrm/Code/agentre/agentre && GOWORK=off golangci-lint run ./internal/...`
Expected: 0 issue。

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/cago.go internal/app/app.go
git commit -m "✨ hook: bootstrap 挂 /mcp/hook/ + app RegisterDeps 接线 hooktool_svc"
```

---

## Task 6: 前端工具标签 i18n + APPROVAL_TOOLS(surface 新工具)

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Modify: `frontend/src/components/agentre/org/tool-catalog.ts`

**Interfaces:**
- Produces: `org.agent.tools.names.hook` / `descriptions.hook` 双语;`APPROVAL_TOOLS` 含 `"hook"`。

> 背景:Task 1 把 `hook` 加进 `agenttool.Keys()`,后端 `AvailableTools` 随之含 `hook`,前端 `org-detail-agent.tsx` 用 `t('org.agent.tools.names.hook')` 渲染 agent 工具开关行。缺标签会渲染原始 key(违反 i18n 纪律),故必须补。这是 Plan 2 的最小前端面,完整 Hooks 页重做在 Plan 3。

- [ ] **Step 1: 加 zh-CN 标签**

在 `org.agent.tools.names` 对象加 `"hook": "脚本 Hook"`;在 `descriptions` 加:
`"hook": "允许该 Agent 编写、试运行并按 cron 调度脚本 Hook,定时产出事件(创建/修改/删除/执行均需你审批)"`。

- [ ] **Step 2: 加 en 标签**

`names.hook`: `"Script Hooks"`;`descriptions.hook`: `"Let this agent author, dry-run and schedule script Hooks on a cron to emit events (create/update/delete/run all require your approval)."`

- [ ] **Step 3: APPROVAL_TOOLS 加 hook**

`tool-catalog.ts`:`export const APPROVAL_TOOLS = new Set(["org", "workflow", "group_create", "hook"]);`

- [ ] **Step 4: 跑前端相关测试**

> worktree 需先备好 wailsjs(见 [[agentre-worktree-build-gotchas]]):`cp -R /Users/codfrm/Code/agentre/agentre/frontend/wailsjs <worktree>/frontend/wailsjs`(或 `make generate`,需 `frontend/dist/index.html` 占位)。

Run: `cd frontend && npx vitest run src/__tests__/i18n.test.ts src/components/agentre/org/__tests__/tool-catalog.test.ts`
Expected: PASS(i18n key 覆盖 + tool-catalog 现有断言不受影响)。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json frontend/src/components/agentre/org/tool-catalog.ts
git commit -m "✨ hook: agent 工具目录上架脚本 Hook(i18n 标签 + 审批徽标)"
```

---

## 完成后

- 用 superpowers:finishing-a-development-branch 收尾:验证 `GOWORK=off go test ./internal/...` 全绿 + 前端相关 vitest 绿 → 呈现 4 选项 → 执行。
- 沿用 Plan 1 的合并方式(rebase develop/wyz 后 `--ff-only`,保线性历史)。

## Self-Review 检查点(写计划后自查,已过)

- **Spec 覆盖**:§7.1 注册表(Task 1)/§7.1 bootstrap(Task 5)/§7.2 六工具(Task 3 读 + Task 4 写)/§7.2 审批复用(Task 4)/§10.5 镜像 orgtool 测试(Task 3/4)。前端页面(§8.2)属 Plan 3,本计划仅做工具上架最小面(Task 6)。
- **类型一致**:`HookService` 方法签名逐字对齐 Plan 1 `hook_svc`(Load/CreateHook/UpdateHook/DeleteHook/RunHook + 其 DTO);`BuildTurnMCP` 签名对齐 `chat_svc.TurnMCPProvider`;`ApprovalGateway` 对齐 `blocks.ToolApprovalBlock`。
- **占位**:Task 3 的 `handleWriteTool` 桩在 Task 4 被真实现替换(已显式标注)。
- **不碰 Plan 1**:`hook_get` 走 `Load` 过滤,零改 hook_svc。
