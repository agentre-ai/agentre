# Agent 工具「调用子 Agent」(subagent) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 新增第三个内置 agent 工具 `subagent`，让一个 agent 能把子任务同步委派给另一个已配置的具名 agent（隔离的一次性会话），并拿回其最终文本输出。

**Architecture:** 完全复用既有「Agent 可配置工具体系」接缝（`agenttool` 注册表 + `chat_svc.RegisterTurnMCPProvider` + gateway `RegisterMCP` + 无状态 HMAC token + `agents.tools_json` 门控）。新域 `subagent_svc` 提供 MCP server（`agent_list` / `agent_call`）；`agent_call` 复用 `group_svc.launchDelivery` 的 `EnsureSession → ObserveTurn → Send` 起轮机制，但**前台同步阻塞**等结果，并按 `TurnResult.AssistantMessageID` 读回子 agent 最终文本。递归靠进程内 call-chain 映射做深度上限 + 环检测。

**Tech Stack:** Go 1.26、cago、MCP-over-HTTP、HMAC-SHA256 token、gomock（service 层）、`github.com/cago-frame/agents/agent/blocks`、React/TS + react-i18next（前端开关仅需 i18n 文案）。

参考实现（照抄模式）：`internal/service/orgtool_svc/{orgtool.go,mcp.go}`、`internal/service/group_svc/{gateway.go,scheduler.go}`。

---

## File Structure

- **Modify** `internal/pkg/agenttool/agenttool.go` — 加 `KeySubagent` + registry 条目。
- **Modify** `internal/service/chat_svc/types.go` — 加 `SessionPurposeSubagentCall`。
- **Modify** `internal/service/chat_svc/chat.go` — `ChatSvc` 接口加 `FinalAssistantText`；`EnsureSession` 加 subagent 分支 + `createSubagentSession`；实现 `FinalAssistantText` + 纯函数 `messageText`。
- **Create** `internal/service/subagent_svc/gateway.go` — `AgentGateway` / `ChatGateway` 窄接口（ISP）+ 生产网关 + `//go:generate mockgen`。
- **Create** `internal/service/subagent_svc/subagent.go` — svc 结构、`Default`、`RegisterDeps`、`SetGatewayBaseURL`、`MCPHandler`、`BuildTurnMCP`、call-chain helpers。
- **Create** `internal/service/subagent_svc/call.go` — `agent_call` 编排（解析 → 建会话 → 阻塞起轮 → 读文本 → 清理 + 超时）。
- **Create** `internal/service/subagent_svc/mcp.go` — MCP-over-HTTP server（token + initialize/tools/list/tools/call + schemas + agent_list）。
- **Modify** `internal/bootstrap/cago.go` — `RegisterMCP("/mcp/subagent/")` + `RegisterTurnMCPProvider`。
- **Modify** `internal/app/app.go` — `subagent_svc.Default().RegisterDeps(...)`（必须在 `RegisterChat` 之后）。
- **Modify** `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `org.agent.tools.names.subagent` + `descriptions.subagent`（开关自动渲染，无需改组件）。
- **Create** 测试：`subagent_svc/*_test.go`、`chat_svc/...` 增量测试、`agenttool/agenttool_test.go` 增量。

所有 Go 命令用 `make test-backend`（根 `go test ./...` 会扫 `frontend/node_modules`）。

---

## Task 1: agenttool 注册表加 `subagent`

**Files:**
- Modify: `internal/pkg/agenttool/agenttool.go`
- Test: `internal/pkg/agenttool/agenttool_test.go`

- [ ] **Step 1: Write the failing test**

追加到 `agenttool_test.go`（若文件不存在则新建，`package agenttool`）：

```go
func TestSubagentRegistered(t *testing.T) {
	def, ok := Lookup(KeySubagent)
	if !ok {
		t.Fatal("subagent tool not registered")
	}
	if def.MCPPath != "/mcp/subagent/" {
		t.Fatalf("MCPPath = %q", def.MCPPath)
	}
	want := []string{"agent_list", "agent_call"}
	if !slices.Equal(def.ToolNames, want) {
		t.Fatalf("ToolNames = %v, want %v", def.ToolNames, want)
	}
	if !slices.Contains(Keys(), KeySubagent) {
		t.Fatal("KeySubagent missing from Keys()")
	}
}
```

（测试文件需 `import "slices"` 和 `"testing"`。）

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/pkg/agenttool/ -run TestSubagentRegistered -v`
Expected: FAIL（`undefined: KeySubagent`）

- [ ] **Step 3: Write minimal implementation**

在 `agenttool.go` 加常量（紧跟 `KeyGroupCreate` 之后）：

```go
// KeySubagent 调用子 agent 工具(把子任务委派给另一具名 agent,同步拿回输出)。
const KeySubagent = "subagent"
```

在 `registry` 切片末尾追加条目：

```go
	{Key: KeySubagent, MCPPath: "/mcp/subagent/", ToolNames: []string{"agent_list", "agent_call"}},
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/pkg/agenttool/ -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/agenttool/
git commit -m "✨ agenttool: register subagent tool"
```

---

## Task 2: chat_svc — 一次性会话 purpose + FinalAssistantText

**Files:**
- Modify: `internal/service/chat_svc/types.go`
- Modify: `internal/service/chat_svc/chat.go`
- Create: `internal/service/chat_svc/subagent_text.go`（放 `messageText` 纯函数 + `FinalAssistantText`）
- Test: `internal/service/chat_svc/subagent_text_test.go`

### 2a. `messageText` 纯函数 + FinalAssistantText

- [ ] **Step 1: Write the failing test**

Create `internal/service/chat_svc/subagent_text_test.go`：

```go
package chat_svc

import (
	"testing"

	"github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
)

func TestMessageText(t *testing.T) {
	if got, _ := messageText(nil); got != "" {
		t.Fatalf("nil message: got %q", got)
	}
	m := &chat_entity.Message{}
	if err := m.SetBlocks([]blocks.ContentBlock{
		&blocks.TextBlock{Text: "hello "},
		&blocks.TextBlock{Text: "world"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := messageText(m)
	if err != nil {
		t.Fatal(err)
	}
	if got != "hello world" {
		t.Fatalf("got %q, want %q", got, "hello world")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/chat_svc/ -run TestMessageText -v`
Expected: FAIL（`undefined: messageText`）

- [ ] **Step 3: Write minimal implementation**

Create `internal/service/chat_svc/subagent_text.go`：

```go
package chat_svc

import (
	"context"
	"strings"

	"github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)

// messageText 拼接某消息内全部 TextBlock 的文本(value/pointer 两种形态都收)。
func messageText(m *chat_entity.Message) (string, error) {
	if m == nil {
		return "", nil
	}
	bs, err := m.GetBlocks()
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, b := range bs {
		switch t := b.(type) {
		case blocks.TextBlock:
			sb.WriteString(t.Text)
		case *blocks.TextBlock:
			sb.WriteString(t.Text)
		}
	}
	return sb.String(), nil
}

// FinalAssistantText 读取某 assistant message 的纯文本。子 agent 工具用它把子 agent
// 最终回复回灌给调用方(TurnResult 只带 message id, 不含文本)。
func (s *chatSvc) FinalAssistantText(ctx context.Context, messageID int64) (string, error) {
	if messageID <= 0 {
		return "", nil
	}
	msg, err := chat_repo.Message().Find(ctx, messageID)
	if err != nil {
		return "", err
	}
	return messageText(msg)
}
```

把方法加进 `ChatSvc` 接口（`chat.go`，紧跟 `FinishToolApproval` 那行之后，line ~117）：

```go
	// FinalAssistantText 读取某 assistant message 的纯文本(拼接所有 TextBlock)。
	// 子 agent 工具用它回灌子 agent 最终输出。
	FinalAssistantText(ctx context.Context, messageID int64) (string, error)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/chat_svc/ -run TestMessageText -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat_svc/subagent_text.go internal/service/chat_svc/subagent_text_test.go internal/service/chat_svc/chat.go
git commit -m "✨ chat_svc: add FinalAssistantText for subagent result readback"
```

### 2b. `SessionPurposeSubagentCall` + always-new createSubagentSession

- [ ] **Step 1: Write the failing test**

追加到 `subagent_text_test.go`（用 `testutils.Database(t)` + sqlmock，照 `chat_svc` 既有 session 测试的 sqlmock 套路；参考 `ensureGroupMemberSession` 相关测试文件取 mock 行格式）：

```go
func TestEnsureSession_SubagentCall_AlwaysCreatesNew(t *testing.T) {
	db, mock := testutils.Database(t) // 返回 *gorm.DB + sqlmock.Sqlmock(按现有 helper 签名)
	_ = db
	s := &chatSvc{ /* 与既有 chat_svc 测试相同的零值装配 */ }

	// 期望两次调用各 INSERT 一行 chat_sessions(group_id=0),互不复用。
	// INSERT 期望按 chat_repo.Session().Create 的实际 SQL 写 mock.ExpectExec/ExpectQuery。
	// 关键断言:两次返回的 SessionID 不同, Created 均为 true。
	mock.MatchExpectationsInOrder(false)
	// ... ExpectBegin/ExpectQuery(INSERT...).WillReturnRows(id=101) ... 第二次 id=102 ...

	r1, err := s.EnsureSession(context.Background(), &EnsureSessionRequest{
		Purpose: SessionPurposeSubagentCall, AgentID: 7, Title: "子任务",
	})
	if err != nil || r1 == nil || !r1.Created || r1.SessionID == 0 {
		t.Fatalf("call1: %+v err=%v", r1, err)
	}
	r2, err := s.EnsureSession(context.Background(), &EnsureSessionRequest{
		Purpose: SessionPurposeSubagentCall, AgentID: 7, Title: "子任务",
	})
	if err != nil || r2 == nil || r2.SessionID == r1.SessionID {
		t.Fatalf("call2 should create a distinct session: r1=%+v r2=%+v err=%v", r1, r2, err)
	}
}
```

> 注：sqlmock 行的精确 SQL/列顺序请打开同包既有的 session 创建测试（搜索 `ExpectQuery` + `chat_sessions`）对齐；本步只规定行为断言（两次创建、SessionID 不同、Created=true、group_id=0、permission_mode 由 `launchPermissionModeForAgent` 解析，解析不出为空串可接受）。

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/chat_svc/ -run TestEnsureSession_SubagentCall -v`
Expected: FAIL（`SessionPurposeSubagentCall` 未定义 / EnsureSession default 分支返回 InvalidParameter）

- [ ] **Step 3: Write minimal implementation**

`types.go`，在 `SessionPurposeGroupMember` 常量块加：

```go
	// SessionPurposeSubagentCall 子 agent 调用的一次性隔离会话(每次新建, 不复用, group_id=0)。
	SessionPurposeSubagentCall SessionPurpose = "subagent_call"
```

`chat.go` `EnsureSession` 的 `switch` 加分支（在 `case SessionPurposeGroupMember:` 之后）：

```go
	case SessionPurposeSubagentCall:
		return s.createSubagentSession(ctx, req.AgentID, req.ProjectID, req.Title)
```

在 `ensureGroupMemberSession` 附近新增（always-create，不查复用）：

```go
// createSubagentSession 为子 agent 调用建一个全新的一次性隔离会话(group_id=0,每次新建)。
// 与 group backing session 不同:不做幂等复用 —— 每次 agent_call 都要干净的隔离上下文。
func (s *chatSvc) createSubagentSession(ctx context.Context, agentID, projectID int64, title string) (*EnsureSessionResponse, error) {
	if agentID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	permissionMode := s.launchPermissionModeForAgent(ctx, agentID)
	sess := &chat_entity.Session{
		AgentID:                agentID,
		ProjectID:              projectID,
		PermissionMode:         permissionMode,
		PermissionModeAtLaunch: permissionMode,
		Title:                  strings.TrimSpace(title),
		AgentStatus:            "idle",
		Status:                 consts.ACTIVE,
	}
	if err := chat_repo.Session().Create(ctx, sess); err != nil {
		logger.Ctx(ctx).Error("chat_svc.createSubagentSession: create failed",
			zap.Int64("agentId", agentID), zap.Error(err))
		return nil, i18n.NewError(ctx, code.OperationFailed)
	}
	return &EnsureSessionResponse{SessionID: sess.ID, Created: true}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/service/chat_svc/ -run TestEnsureSession_SubagentCall -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat_svc/types.go internal/service/chat_svc/chat.go internal/service/chat_svc/subagent_text_test.go
git commit -m "✨ chat_svc: add subagent_call ephemeral session purpose"
```

---

## Task 3: subagent_svc — 窄接口 + mocks + svc 骨架 + BuildTurnMCP

**Files:**
- Create: `internal/service/subagent_svc/gateway.go`
- Create: `internal/service/subagent_svc/subagent.go`
- Test: `internal/service/subagent_svc/subagent_test.go`

### 3a. gateway.go + mocks

- [ ] **Step 1: Write gateway.go（含 go:generate）**

```go
package subagent_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

//go:generate mockgen -source gateway.go -destination mock_subagent_svc/mock_gateway.go

// AgentGateway 子 agent 工具对 agent 数据的窄依赖(ISP)。agent_repo.Agent() 直接满足。
type AgentGateway interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
}

// ChatGateway 子 agent 工具对 chat_svc 的窄依赖:建一次性会话 → 起轮 → 读最终文本 → 清理。
type ChatGateway interface {
	EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
	FinalAssistantText(ctx context.Context, messageID int64) (string, error)
	DeleteSession(ctx context.Context, sessionID int64) error
}

// chatSvcGateway 委托给 chat_svc 默认单例(生产实现;测试注 mock)。
type chatSvcGateway struct{}

func (chatSvcGateway) EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error) {
	return chat_svc.Chat().EnsureSession(ctx, req)
}
func (chatSvcGateway) Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
	return chat_svc.Chat().Send(ctx, req)
}
func (chatSvcGateway) ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func()) {
	return chat_svc.Chat().ObserveTurn(sessionID)
}
func (chatSvcGateway) Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error) {
	return chat_svc.Chat().Stop(ctx, req)
}
func (chatSvcGateway) FinalAssistantText(ctx context.Context, messageID int64) (string, error) {
	return chat_svc.Chat().FinalAssistantText(ctx, messageID)
}
func (chatSvcGateway) DeleteSession(ctx context.Context, sessionID int64) error {
	_, err := chat_svc.Chat().Delete(ctx, &chat_svc.DeleteRequest{SessionID: sessionID})
	return err
}

// ChatSvcGateway 生产用 chat_svc 网关(供 bootstrap 接线)。
func ChatSvcGateway() ChatGateway { return chatSvcGateway{} }
```

- [ ] **Step 2: 生成 mocks**

Run: `cd /Users/codfrm/Code/agentre/agentre && go generate ./internal/service/subagent_svc/...`
Expected: 生成 `internal/service/subagent_svc/mock_subagent_svc/mock_gateway.go`（含 `MockAgentGateway` / `MockChatGateway`）。

> 若 `go generate` 因 subagent.go 尚未存在而编译失败，先完成 3b 再生成；二者一并提交。

### 3b. subagent.go（svc 骨架 + BuildTurnMCP + chain helpers）

- [ ] **Step 3: Write the failing test**

Create `internal/service/subagent_svc/subagent_test.go`：

```go
package subagent_svc

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

func enabledAgent(id int64) *agent_entity.Agent {
	a := &agent_entity.Agent{ID: id, Name: "Reviewer"}
	a.SetTools([]agent_entity.AgentToolItem{{Key: agenttool.KeySubagent, Enabled: true}})
	return a
}

func TestBuildTurnMCP(t *testing.T) {
	s := &subagentSvc{gatewayBaseURL: "http://127.0.0.1:9/", chains: map[int64][]int64{}}

	// 未开启 → nil
	if got := s.BuildTurnMCP(context.Background(), &agent_entity.Agent{ID: 1}, 5, 0); got != nil {
		t.Fatalf("disabled agent should get no spec, got %v", got)
	}
	// 开启 → 一个 spec, Name=subagent, URL 带挂载路径, 带 Bearer
	specs := s.BuildTurnMCP(context.Background(), enabledAgent(1), 5, 0)
	if len(specs) != 1 {
		t.Fatalf("want 1 spec, got %d", len(specs))
	}
	if specs[0].Name != agenttool.KeySubagent || specs[0].URL != "http://127.0.0.1:9//mcp/subagent/" {
		t.Fatalf("bad spec: %+v", specs[0])
	}
	if specs[0].Headers["Authorization"] == "" {
		t.Fatal("missing Authorization header")
	}
	// 无 gatewayBaseURL → nil
	s.gatewayBaseURL = ""
	if got := s.BuildTurnMCP(context.Background(), enabledAgent(1), 5, 0); got != nil {
		t.Fatalf("no gateway should get nil, got %v", got)
	}
}

func TestResolveChain_DepthAndCycle(t *testing.T) {
	s := &subagentSvc{chains: map[int64][]int64{}}

	// 顶层(父会话无链): A(=10) 调 B(=20) → newChain=[10], 放行
	chain, _, ok := s.resolveChain(100, 10, 20)
	if !ok || len(chain) != 1 || chain[0] != 10 {
		t.Fatalf("top-level call should pass: chain=%v ok=%v", chain, ok)
	}
	// 环: 父链=[10], 父=20, 调 10 → 10 已在链上 → 拒绝
	s.registerChain(200, []int64{10})
	if _, _, ok := s.resolveChain(200, 20, 10); ok {
		t.Fatal("cycle should be rejected")
	}
	// 自调用: 父=30 调 30 → 拒绝
	if _, _, ok := s.resolveChain(300, 30, 30); ok {
		t.Fatal("self-call should be rejected")
	}
	// 深度: 父链=[1,2,3](已 3 层), 父=4 调 5 → newChain=[1,2,3,4] len=4 > 3 → 拒绝
	s.registerChain(400, []int64{1, 2, 3})
	if _, _, ok := s.resolveChain(400, 4, 5); ok {
		t.Fatal("depth > 3 should be rejected")
	}
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `go test ./internal/service/subagent_svc/ -run 'TestBuildTurnMCP|TestResolveChain' -v`
Expected: FAIL（`undefined: subagentSvc`）

- [ ] **Step 5: Write minimal implementation**

Create `internal/service/subagent_svc/subagent.go`：

```go
package subagent_svc

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// maxSubagentDepth 子 agent 调用链最大深度(防无限委派);newChain 超过即拒绝。
const maxSubagentDepth = 3

type subagentSvc struct {
	mcp            *subagentMCP
	mcpOnce        sync.Once
	gatewayBaseURL string
	callTimeout    time.Duration

	agents AgentGateway
	chat   ChatGateway

	chainsMu sync.Mutex
	chains   map[int64][]int64 // 一次性会话ID → 祖先 agentID 链(不含被调者)
}

// callTimeout 取 4min:CLI MCP 调用默认 env 已设 600000ms,但实测有 ~285s 二级硬顶
// (见 orgtool_svc spike),留余量取 240s,超时即中止子 agent 并返回错误。
var defaultSubagent = &subagentSvc{callTimeout: 4 * time.Minute, chains: map[int64][]int64{}}

// Default 取默认服务单例。
func Default() *subagentSvc { return defaultSubagent }

// RegisterDeps bootstrap 接线(生产传 agent_repo.Agent() + ChatSvcGateway());测试注 mock。
func (s *subagentSvc) RegisterDeps(agents AgentGateway, chat ChatGateway) {
	s.agents, s.chat = agents, chat
}

func (s *subagentSvc) mcpHandlerInit() *subagentMCP {
	s.mcpOnce.Do(func() { s.mcp = newSubagentMCP(s) })
	return s.mcp
}

// MCPHandler 返回挂到 gateway /mcp/subagent/ 的 HTTP handler。
func (s *subagentSvc) MCPHandler() http.Handler { return s.mcpHandlerInit() }

// SetGatewayBaseURL 由 bootstrap 在 gateway 起好后注入。
func (s *subagentSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider:agent 开启 subagent 工具时返回注入 spec。
func (s *subagentSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID int64, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeySubagent) || s.gatewayBaseURL == "" {
		return nil
	}
	def, ok := agenttool.Lookup(agenttool.KeySubagent)
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

// resolveChain 按父会话取祖先链, 拼上父 agent, 校验深度/环。ok=false 时 reason 是 MCP error 文本。
func (s *subagentSvc) resolveChain(parentSessionID, parentAgentID, calleeAgentID int64) (newChain []int64, reason string, ok bool) {
	s.chainsMu.Lock()
	parent := s.chains[parentSessionID]
	s.chainsMu.Unlock()
	newChain = make([]int64, 0, len(parent)+1)
	newChain = append(newChain, parent...)
	newChain = append(newChain, parentAgentID)
	if len(newChain) > maxSubagentDepth {
		return nil, "已达到子 agent 调用深度上限,拒绝进一步委派", false
	}
	for _, id := range newChain {
		if id == calleeAgentID {
			return nil, "检测到循环调用(目标 agent 已在调用链上),拒绝", false
		}
	}
	return newChain, "", true
}

func (s *subagentSvc) registerChain(sessionID int64, chain []int64) {
	s.chainsMu.Lock()
	s.chains[sessionID] = chain
	s.chainsMu.Unlock()
}

func (s *subagentSvc) clearChain(sessionID int64) {
	s.chainsMu.Lock()
	delete(s.chains, sessionID)
	s.chainsMu.Unlock()
}
```

> `mcp.go`（Task 5）会定义 `subagentMCP` / `newSubagentMCP` / `MintToken`。本步先写到这里编译会因缺 `subagentMCP` 失败 —— 与 Task 5 一起跑测试。若想本步独立通过，可临时在文件底部加占位（不推荐）；推荐：本步只提交代码，测试在 Task 5 末尾统一跑。**调整：把 3b 的 `go test` 验证挪到 Task 5 Step「全包测试」一并执行。**

- [ ] **Step 6: Commit（含 gateway + mocks + 骨架）**

```bash
go generate ./internal/service/subagent_svc/...   # 若此时仍缺 subagentMCP 编译不过, 留到 Task 5 后再 generate+commit
git add internal/service/subagent_svc/
git commit -m "✨ subagent_svc: gateways, svc skeleton, BuildTurnMCP, chain guard"
```

---

## Task 4: subagent_svc — agent_call 编排

**Files:**
- Create: `internal/service/subagent_svc/call.go`
- Test: `internal/service/subagent_svc/call_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/subagent_svc/call_test.go`：

```go
package subagent_svc

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/subagent_svc/mock_subagent_svc"
)

func TestCallAgent_HappyPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	chat := mock_subagent_svc.NewMockChatGateway(ctrl)
	s := &subagentSvc{agents: agents, chat: chat, callTimeout: 2 * time.Second, chains: map[int64][]int64{}}

	agents.EXPECT().FindByName(gomock.Any(), "Reviewer").Return(&agent_entity.Agent{ID: 20, Name: "Reviewer"}, nil)
	chat.EXPECT().EnsureSession(gomock.Any(), gomock.Any()).Return(&chat_svc.EnsureSessionResponse{SessionID: 999, Created: true}, nil)

	// ObserveTurn 返回一个已塞好终态的 buffered channel(cap1), select 立即可读。
	ch := make(chan chat_svc.TurnResult, 1)
	ch <- chat_svc.TurnResult{SessionID: 999, AssistantMessageID: 555}
	chat.EXPECT().ObserveTurn(int64(999)).Return((<-chan chat_svc.TurnResult)(ch), func() {})
	chat.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil)
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(555)).Return("done: looks good", nil)
	chat.EXPECT().DeleteSession(gomock.Any(), int64(999)).Return(nil)

	out, err := s.callAgent(context.Background(), subagentRef{agentID: 10, sessionID: 100}, "Reviewer", "review the diff")
	if err != nil {
		t.Fatal(err)
	}
	if out != "done: looks good" {
		t.Fatalf("got %q", out)
	}
	// chain 已清理
	if _, exists := s.chains[999]; exists {
		t.Fatal("chain not cleaned up")
	}
}

func TestCallAgent_UnknownAgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	chat := mock_subagent_svc.NewMockChatGateway(ctrl)
	s := &subagentSvc{agents: agents, chat: chat, callTimeout: time.Second, chains: map[int64][]int64{}}

	agents.EXPECT().FindByName(gomock.Any(), "Ghost").Return(nil, nil)
	if _, err := s.callAgent(context.Background(), subagentRef{agentID: 1, sessionID: 1}, "Ghost", "x"); err == nil {
		t.Fatal("expected error for unknown agent")
	}
}

func TestCallAgent_CycleRejected(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	chat := mock_subagent_svc.NewMockChatGateway(ctrl)
	s := &subagentSvc{agents: agents, chat: chat, callTimeout: time.Second, chains: map[int64][]int64{}}
	s.registerChain(100, []int64{20}) // 父会话链已含 20
	agents.EXPECT().FindByName(gomock.Any(), "Reviewer").Return(&agent_entity.Agent{ID: 20, Name: "Reviewer"}, nil)
	// EnsureSession/Send 不应被调用(环在建会话前拦截)
	if _, err := s.callAgent(context.Background(), subagentRef{agentID: 10, sessionID: 100}, "Reviewer", "x"); err == nil {
		t.Fatal("expected cycle rejection")
	}
}

func TestCallAgent_Timeout(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	chat := mock_subagent_svc.NewMockChatGateway(ctrl)
	s := &subagentSvc{agents: agents, chat: chat, callTimeout: 30 * time.Millisecond, chains: map[int64][]int64{}}

	agents.EXPECT().FindByName(gomock.Any(), "Slow").Return(&agent_entity.Agent{ID: 20, Name: "Slow"}, nil)
	chat.EXPECT().EnsureSession(gomock.Any(), gomock.Any()).Return(&chat_svc.EnsureSessionResponse{SessionID: 999}, nil)
	never := make(chan chat_svc.TurnResult) // 永不就绪
	chat.EXPECT().ObserveTurn(int64(999)).Return((<-chan chat_svc.TurnResult)(never), func() {})
	chat.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil)
	chat.EXPECT().Stop(gomock.Any(), gomock.Any()).Return(&chat_svc.StopResponse{}, nil) // 超时中止
	chat.EXPECT().DeleteSession(gomock.Any(), int64(999)).Return(nil)

	if _, err := s.callAgent(context.Background(), subagentRef{agentID: 10, sessionID: 100}, "Slow", "x"); err == nil ||
		!errors.Is(err, err) { // 仅断言非 nil
		t.Fatal("expected timeout error")
	}
}

func TestCallAgent_SubagentErr(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	chat := mock_subagent_svc.NewMockChatGateway(ctrl)
	s := &subagentSvc{agents: agents, chat: chat, callTimeout: time.Second, chains: map[int64][]int64{}}

	agents.EXPECT().FindByName(gomock.Any(), "R").Return(&agent_entity.Agent{ID: 20, Name: "R"}, nil)
	chat.EXPECT().EnsureSession(gomock.Any(), gomock.Any()).Return(&chat_svc.EnsureSessionResponse{SessionID: 999}, nil)
	ch := make(chan chat_svc.TurnResult, 1)
	ch <- chat_svc.TurnResult{SessionID: 999, Err: errors.New("boom")}
	chat.EXPECT().ObserveTurn(int64(999)).Return((<-chan chat_svc.TurnResult)(ch), func() {})
	chat.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil)
	chat.EXPECT().DeleteSession(gomock.Any(), int64(999)).Return(nil)

	if _, err := s.callAgent(context.Background(), subagentRef{agentID: 10, sessionID: 100}, "R", "x"); err == nil {
		t.Fatal("expected error propagated from sub-agent")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/subagent_svc/ -run TestCallAgent -v`
Expected: FAIL（`undefined: subagentRef` / `s.callAgent`）

- [ ] **Step 3: Write minimal implementation**

Create `internal/service/subagent_svc/call.go`：

```go
package subagent_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// subagentRef 是 subagent MCP token 绑定的 (agent, session) —— 即发起调用的父 agent 与其会话。
type subagentRef struct{ agentID, sessionID int64 }

// callAgent 解析目标 agent → 校验深度/环 → 建一次性隔离会话 → 阻塞起轮 → 回灌最终文本。
// 返回 tool result 文本或错误(错误文本回给调用 agent, 供其纠正/重试)。
func (s *subagentSvc) callAgent(ctx context.Context, ref subagentRef, agentName, prompt string) (string, error) {
	callee, err := s.agents.FindByName(ctx, agentName)
	if err != nil || callee == nil {
		return "", fmt.Errorf("未找到名为 %q 的 agent,请先用 agent_list 查看可调用 agent", agentName)
	}
	chain, reason, ok := s.resolveChain(ref.sessionID, ref.agentID, callee.ID)
	if !ok {
		return "", errors.New(reason)
	}

	resp, err := s.chat.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
		Purpose: chat_svc.SessionPurposeSubagentCall,
		AgentID: callee.ID,
		Title:   "子任务: " + agentName,
	})
	if err != nil || resp == nil || resp.SessionID <= 0 {
		return "", errors.New("创建子 agent 会话失败")
	}
	sessionID := resp.SessionID
	s.registerChain(sessionID, chain)
	defer s.clearChain(sessionID)
	defer func() { _ = s.chat.DeleteSession(context.Background(), sessionID) }()

	// 订阅必须在 Send 之前(快 turn 的回执会丢)。
	turnCh, observe := s.chat.ObserveTurn(sessionID)
	defer observe()
	if _, err := s.chat.Send(ctx, &chat_svc.SendRequest{
		SessionID:             sessionID,
		AgentID:               callee.ID,
		Text:                  prompt,
		EmitTurnStartedBypass: true,
	}); err != nil {
		return "", errors.New("子 agent 起轮失败")
	}

	timer := time.NewTimer(s.callTimeout)
	defer timer.Stop()
	select {
	case res := <-turnCh:
		if res.Err != nil {
			return "", fmt.Errorf("子 agent 执行出错: %v", res.Err)
		}
		if res.Aborted {
			return "", errors.New("子 agent 被中止")
		}
		text, terr := s.chat.FinalAssistantText(ctx, res.AssistantMessageID)
		if terr != nil {
			return "", errors.New("读取子 agent 输出失败")
		}
		if strings.TrimSpace(text) == "" {
			return "(子 agent 完成,但没有产生文本输出)", nil
		}
		return text, nil
	case <-ctx.Done():
		_, _ = s.chat.Stop(context.Background(), &chat_svc.StopRequest{SessionID: sessionID})
		return "", errors.New("子 agent 调用被取消")
	case <-timer.C:
		_, _ = s.chat.Stop(context.Background(), &chat_svc.StopRequest{SessionID: sessionID})
		return "", errors.New("子 agent 超出时间预算,已中止;请缩小子任务范围后重试")
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

（需先有 Task 5 的 `mcp.go` 才能编译整包；若按顺序做，到这里 `subagentMCP` 仍缺。**先做 Task 5 再回来跑**，或本步只写代码、Step 5 合并提交。）

Run（Task 5 完成后）: `go test ./internal/service/subagent_svc/ -run TestCallAgent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/service/subagent_svc/call.go internal/service/subagent_svc/call_test.go
git commit -m "✨ subagent_svc: agent_call orchestration (sync turn + depth/cycle/timeout)"
```

---

## Task 5: subagent_svc — MCP-over-HTTP server

**Files:**
- Create: `internal/service/subagent_svc/mcp.go`
- Test: `internal/service/subagent_svc/mcp_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/service/subagent_svc/mcp_test.go`：

```go
package subagent_svc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/service/subagent_svc/mock_subagent_svc"
)

func TestMCP_TokenRoundTrip(t *testing.T) {
	h := newSubagentMCP(&subagentSvc{chains: map[int64][]int64{}})
	tok := h.MintToken(7, 42)
	ref, ok := h.lookup(tok)
	if !ok || ref.agentID != 7 || ref.sessionID != 42 {
		t.Fatalf("roundtrip failed: %+v ok=%v", ref, ok)
	}
	if _, ok := h.lookup(tok + "x"); ok {
		t.Fatal("tampered token should fail")
	}
}

func TestMCP_ToolsList(t *testing.T) {
	h := newSubagentMCP(&subagentSvc{chains: map[int64][]int64{}})
	rr := httptest.NewRecorder()
	body := `{"id":1,"method":"tools/list"}`
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(body)))
	var resp struct {
		Result struct {
			Tools []map[string]any `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Result.Tools) != 2 {
		t.Fatalf("want 2 tools, got %d", len(resp.Result.Tools))
	}
}

func TestMCP_AgentList(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	svc := &subagentSvc{agents: agents, chains: map[int64][]int64{}}
	caller := enabledAgent(7) // 同包 subagent_test.go 的 helper
	agents.EXPECT().Find(gomock.Any(), int64(7)).Return(caller, nil)
	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 1, Name: "Reviewer", Description: "审查代码"},
		{ID: 2, Name: "Writer"},
	}, nil)

	h := newSubagentMCP(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"agent_list"}}`))
	req.Header.Set("Authorization", "Bearer "+h.MintToken(7, 42))
	h.ServeHTTP(rr, req)
	if !strings.Contains(rr.Body.String(), "Reviewer") {
		t.Fatalf("agent_list missing agent: %s", rr.Body.String())
	}
}

func TestMCP_ForbiddenWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	agents := mock_subagent_svc.NewMockAgentGateway(ctrl)
	svc := &subagentSvc{agents: agents, chains: map[int64][]int64{}}
	agents.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{ID: 7}, nil) // 未开启 subagent

	h := newSubagentMCP(svc)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp/subagent/", strings.NewReader(`{"id":1,"method":"tools/call","params":{"name":"agent_list"}}`))
	req.Header.Set("Authorization", "Bearer "+h.MintToken(7, 42))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("want 403, got %d", rr.Code)
	}
}

var _ = context.Background // 防 import 未用(按需删)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/subagent_svc/ -run TestMCP -v`
Expected: FAIL（`undefined: newSubagentMCP`）

- [ ] **Step 3: Write minimal implementation**

Create `internal/service/subagent_svc/mcp.go`（token 段与 RPC helpers 照 `orgtool_svc/mcp.go` 同款；secret per-process）：

```go
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

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
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

var _ = agent_entity.Agent{} // 若 agent_entity 未在本文件直接使用则删除此行
```

- [ ] **Step 4: 生成 mocks + 跑全包测试（Task 3/4/5 的测试在此统一变绿）**

```bash
cd /Users/codfrm/Code/agentre/agentre
go generate ./internal/service/subagent_svc/...
go test ./internal/service/subagent_svc/ -v
```
Expected: 全部 PASS（`TestBuildTurnMCP`/`TestResolveChain`/`TestCallAgent*`/`TestMCP*`）。

- [ ] **Step 5: Commit**

```bash
git add internal/service/subagent_svc/
git commit -m "✨ subagent_svc: MCP server (agent_list/agent_call) + tests"
```

---

## Task 6: bootstrap + app 接线

**Files:**
- Modify: `internal/bootstrap/cago.go`
- Modify: `internal/app/app.go`
- Test: 接线由 `make test-backend` 编译 + 既有启动测试覆盖；无新单测（与 org 接线一致，纯 wiring）。

- [ ] **Step 1: Wire gateway + provider（cago.go）**

在 workflow 注册块之后（line ~170，`group_create` provider 之前或之后均可）追加：

```go
	// 挂「调用子 agent」工具 MCP handler(/mcp/subagent/) + 注册 TurnMCPProvider:
	// agent 开了 subagent 工具的会话 turn 注入该 MCP server(无审批门, 见 subagent_svc)。
	gw.RegisterMCP("/mcp/subagent/", subagent_svc.Default().MCPHandler())
	subagent_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(subagent_svc.Default().BuildTurnMCP)
```

加 import：`"github.com/agentre-ai/agentre/internal/service/subagent_svc"`。

- [ ] **Step 2: Wire deps（app.go，必须在 RegisterChat 之后）**

在 `workflowtool_svc.Default().RegisterDeps(...)` 之后（line ~226）追加：

```go
	// subagent_svc 同样需 chat_svc.Chat() 非 nil(起子 agent 轮),故也在 RegisterChat 之后接线。
	// agent_repo.Agent() 直接满足 AgentGateway(Find/FindByName/List)。
	subagent_svc.Default().RegisterDeps(agent_repo.Agent(), subagent_svc.ChatSvcGateway())
```

加 import：`"github.com/agentre-ai/agentre/internal/service/subagent_svc"`（`agent_repo` 已 import）。

- [ ] **Step 3: Build + 全量后端测试**

```bash
cd /Users/codfrm/Code/agentre/agentre
make test-backend
```
Expected: 编译通过 + 全绿。

- [ ] **Step 4: Commit**

```bash
git add internal/bootstrap/cago.go internal/app/app.go
git commit -m "✨ bootstrap: wire subagent tool MCP + deps"
```

---

## Task 7: 前端 i18n 文案（开关自动渲染）

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/__tests__/i18n.test.ts`（既有，校验 key 平价）

> 工具开关由后端 `availableTools`(=`agenttool.Keys()`，Task 1 已含 subagent) 驱动 `tool-catalog.ts` 自动渲染，无需改组件。`subagent` 非审批工具，**不要**加进 `tool-catalog.ts` 的 `APPROVAL_TOOLS`。

- [ ] **Step 1: Add zh-CN keys**

在 `zh-CN/common.json` 的 `org.agent.tools.names` 对象加 `"subagent"`，`org.agent.tools.descriptions` 对象加 `"subagent"`（与既有 `org`/`workflow` 同级）：

```json
"names": { "...": "...", "subagent": "调用子 Agent" },
"descriptions": { "...": "...", "subagent": "把子任务委派给另一个已配置的 Agent 执行并同步取回结果。子 Agent 在隔离的一次性会话中运行；用作子 Agent 的成员建议使用自动批准的权限模式。" }
```

- [ ] **Step 2: Add en keys（同结构）**

```json
"names": { "...": "...", "subagent": "Call Sub-agent" },
"descriptions": { "...": "...", "subagent": "Delegate a subtask to another configured agent and get its result back synchronously. The sub-agent runs in an isolated one-shot session; agents used as sub-agents should use an auto-approving permission mode." }
```

- [ ] **Step 3: Run i18n + lint**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
pnpm test -- src/__tests__/i18n.test.ts
pnpm lint
```
Expected: i18n key 平价测试 PASS；lint 无新错误。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "🌐 i18n: subagent tool name/description"
```

---

## Task 8 (可选): e2e — fake runtime 验证 agent_call 真起轮

**Files:**
- Create: `e2e/scratch/subagent-call.spec.ts`（先在 gitignored scratch 验证，过了再决定是否进 `e2e/tests/`）

> fake runtime 已能充当 MCP HTTP 客户端（group_send / org 先例，见 [[reference_e2e_group_reply_needs_group_send]]）。本任务把它扩展成调 `agent_call` 并断言子 agent 真起轮 + 结果回灌。**坑**：e2e gateway 端口非 data-dir-scoped，正式 Agentre 在跑会占用 → 须 `AGENTRE_PROXY_PORT=0` 绑临时端口。

- [ ] **Step 1**: 种子两个 agent（调用者开启 `subagent`，被调者任意 backend），调用者一轮里发 `agent_call(agent_name=<被调>, prompt=...)`。
- [ ] **Step 2**: 断言被调 agent 起了一轮（`node:sqlite` oracle 查新增 `chat_sessions` 行随后被软删 / 查被调 agent 产生 assistant message），且调用者 tool result 含子 agent 文本。
- [ ] **Step 3**: `make e2e-scratch` 跑通后，与用户确认是否固化进 `e2e/tests/`。

> 此任务为可选验证；不阻断特性合并（单测 + 集成测已覆盖核心逻辑）。

---

## Self-Review

**Spec coverage（逐条核对 `2026-06-15-subagent-call-tool-design.md`）：**
- §3.1 注册表 `KeySubagent` → Task 1 ✓
- §3.2 门控（ToolEnabled + CapMCPTools + 实时校验）→ BuildTurnMCP（Task 3）+ MCP handler 实时 `Find().ToolEnabled`（Task 5）；CapMCPTools 由 `chat_svc.appendTurnMCP` 既有逻辑负责（无需改）✓
- §4.1 挂载 + token（agentID,sessionID）→ Task 5 + Task 6 ✓
- §4.2 `agent_list` / `agent_call` schema → Task 5 ✓（**偏差**：agent_list 暂不含 backend 摘要，避免额外 backend 依赖；id/name/description/systemBadge 足够发现，已在 plan 标注）
- §4.3 执行流程（解析→深度/环→建会话→Send/Observe 阻塞→回灌→清理）→ Task 4 ✓
- §5 深度/环 + 进程内 chain 映射 → Task 3 `resolveChain`/registerChain/clearChain + Task 4 注册/清理 ✓
- §6.1 超时（4min，< ~285s 硬顶；env 已 600000 无需改）→ Task 3 `callTimeout` + Task 4 timer/Stop ✓
- §6.2 permission mode（继承 launch 默认 + 文档建议 acceptEdits/bypass）→ createSubagentSession 用 `launchPermissionModeForAgent`（Task 2）+ i18n 描述提示（Task 7）✓
- §7 注入复用 appendTurnMCP + remote 限制 → Task 6 接线（remote 限制为已知，无代码）✓
- §8 前端开关 + i18n → Task 7 ✓
- §10 测试矩阵 → Task 1/2/3/4/5 单测 + Task 8 可选 e2e ✓

**Placeholder scan：** 无 TBD/TODO。两处「实现顺序依赖」已显式说明（3b/4 的整包测试挪到 Task 5 末尾统一跑，因 `subagentMCP` 跨文件）。sqlmock 行格式（Task 2b）指向同包既有 session 创建测试对齐——这是定位指引非占位，行为断言完整。

**Type consistency：** `subagentRef{agentID,sessionID}`、`AgentGateway.{Find,FindByName,List}`、`ChatGateway.{EnsureSession,Send,ObserveTurn,Stop,FinalAssistantText,DeleteSession}`、`SessionPurposeSubagentCall`、`FinalAssistantText(ctx,messageID)`、`callAgent(ctx,ref,name,prompt)`、`resolveChain/registerChain/clearChain`、`maxSubagentDepth=3` 在各任务间一致；`blocks.TextBlock` value+pointer 两态与 `chat.go:698-701` 既有处理一致。
