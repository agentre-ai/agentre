# 编排 Harness 切片 B — Leader 可观测 & 控制工具 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给编排 Leader 三个新 MCP 工具 —— `status()`(看整棵任务树)、`read(task_id)` 的运行中 peek 分支(拉在跑子任务的当前输出)、`cancel(task_id)`(软取消 + 尽力硬打断 + 级联子孙)——让 Leader 在两次回报之间不再全盲、且能中止跑偏/卡住的子任务。

**Architecture:** 纯后端。新增/扩展 orch_svc 的三个 service 方法(`RunStatus` / `ReadTask` running 分支 / `CancelTask`)+ 三个 MCP handler,复用既有 `ChatGateway`/`TaskRepo`。`read` running 分支依赖新增的 `chat_svc.LatestAssistantText(sessionID)`(按会话取末条 assistant 文本);`cancel` 硬打断复用既有 `chat_svc.Stop`(UI 停止按钮那条),并靠 watcher 让位守卫避免被取消的任务被 watcher 误翻成 done/error 或误回报父。**前置**:先修 slice A 遗留的工具白名单缺口(`read`/`report` 未进 `agenttool.ToolNames`,CLI `--allowedTools`/codex `enabled_tools`/piagent `allow` 会拦掉 → agent 调不到),并加 schema↔白名单 parity 守卫测试。

**Tech Stack:** Go 1.26、cago、gorm、goconvey + gomock(service 单测走 mock,不连库)、sqlmock(repo 单测)。

## Global Constraints

- **不做硬资源上限**(次数/深度/时长/成本封顶)。本切片全是更强信号 + 拉取/纠偏,唯一的"打断"是 `cancel` —— 由 Leader/用户显式发起的针对性取消,不是自动封顶。
- **归属校验**:`status`/`read`/`cancel` 都经 token `ref.sessionID` → `FindBySession` → `RunID` 定位;`read`/`cancel` 的 `task_id` 必须属于调用者所在 Run,否则返回 `errForeignTask`(现有,文案 `... not in this run`)。
- **注入 Leader 的 XML 一律 HTML 转义**(复用 `envelope.go` 现做法)——但本切片工具的**返回值**是给调用者的 MCP 文本结果,不是注入第三方会话的 XML,故 `status`/`read`/`cancel` 返回纯文本/JSON、不需要 XML 信封;唯一注入路径(watcher 让位)是**不注入**。
- **service 单测走 mock、repo 单测走 sqlmock,均不连库**;service 用 `orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, approval, emit)` 注入 mock。
- **改了 `deps.go` 的 `ChatGateway` 接口必须 `make mock` 重生 `mock_orch_svc`**,否则编译不过。
- **新工具必须同时进三处**:①`orchToolSchemas()` 的 schema、②`dispatchTool` 的 case、③`agenttool.go` 的 `KeyOrchestrate.ToolNames`(否则 CLI 白名单拦掉)。parity 测试(Task 1)会守住第 ③ 处。
- **无 i18n / 前端改动**:工具返回 + 注入内容都是 agent 面向的动态框架语,不入 `t(...)`(与现有 guidance 一致)。
- 共享分支 `develop/wyz`,并发会话共用 index:**提交一律带 pathspec**(`git commit <files>`),复审用 `git show <commit>`。

---

## File Structure

| 文件 | 职责 | 动作 |
|---|---|---|
| `internal/pkg/agenttool/agenttool.go` | `KeyOrchestrate.ToolNames` 白名单 | 修:补 `report`/`read`(Task1)、`status`(Task3)、`cancel`(Task5) |
| `internal/service/orch_svc/export_test.go` | 测试导出 | 加 `OrchToolSchemaNames()`(Task1) |
| `internal/service/orch_svc/mcp_test.go` | MCP/parity 测试 | 加 parity 守卫(Task1) |
| `internal/repository/chat_repo/message.go` | 消息仓储 | 加 `LatestAssistant(sessionID)`(Task2) |
| `internal/repository/chat_repo/message_test.go` | 仓储 sqlmock 测试 | 加(Task2) |
| `internal/service/chat_svc/subagent_text.go` | 会话文本读取 | 加 `LatestAssistantText(sessionID)`(Task2) |
| `internal/service/chat_svc/chat.go` | `ChatSvc` 接口 | 加接口方法(Task2) |
| `internal/service/chat_svc/subagent_text_test.go` | svc 测试 | 加/建(Task2) |
| `internal/service/orch_svc/status.go` | `status()` = 任务树快照 | 建(Task3) |
| `internal/service/orch_svc/status_test.go` | 纯 formatter + svc 测试 | 建(Task3) |
| `internal/service/orch_svc/deps.go` | `ChatGateway` 接口 | 加 `LatestAssistantText`(Task4)、`AbortTurn`(Task5) |
| `internal/app/orch_adapter.go` | 生产适配器 | 加两个适配方法(Task4/5) |
| `internal/service/orch_svc/read.go` | `read` 工具 | 改:加 running/peek 分支(Task4) |
| `internal/service/orch_svc/read_test.go` | read 测试 | 加 running 用例(Task4) |
| `internal/service/orch_svc/cancel.go` | `cancel()` + `collectSubtree` | 建(Task5) |
| `internal/service/orch_svc/cancel_test.go` | cancel 测试 | 建(Task5) |
| `internal/service/orch_svc/complete.go` | watcher | 改:让位守卫(Task5) |
| `internal/service/orch_svc/complete_test.go` | watcher 测试 | 加取消让位用例(Task5) |
| `internal/service/orch_svc/mcp.go` | MCP handlers + schemas | 改:加 `status`/`cancel` handler + schema、`read` 描述(Task3/4/5) |
| `internal/service/orch_svc/turn.go` | `orchGuidance` | 改:教三个新能力(Task6) |

---

### Task 1: 修复工具白名单缺口 + schema↔白名单 parity 守卫

**背景:** slice A 加了 `report`/`read` 两个 MCP 工具(handler + `orchToolSchemas()` schema 都在),但漏了把它们加进 `agenttool.KeyOrchestrate.ToolNames`。`ToolNames` 是硬白名单:claudecode 进 `--allowedTools`、codex 进 `enabled_tools`、piagent bridge 进 `allow` 集合(见 `runtimes/{claudecode,codex,piagent}` 各自消费)。所以 `read`/`report` 在 codex/piagent 后端**必被拦、agent 调不到**(claudecode 非 bypass 模式同样拦)。本切片的 `read`/`status`/`cancel` 都要进这份白名单,先补齐既有缺口并加 parity 守卫,后续 Task 只需各自补一行。

**Files:**
- Modify: `internal/service/orch_svc/export_test.go`
- Modify: `internal/service/orch_svc/mcp_test.go`
- Modify: `internal/pkg/agenttool/agenttool.go:38`

**Interfaces:**
- Consumes: `orchToolSchemas()`(unexported,`orch_svc` 包内)、`agenttool.Lookup(KeyOrchestrate)`。
- Produces: `orch_svc.OrchToolSchemaNames() []string`(test-only 导出)。

- [ ] **Step 1: 加测试导出**（`export_test.go` 末尾追加）

```go
// OrchToolSchemaNames 仅测试用:返回 orchToolSchemas() 暴露的全部工具名，
// 供 parity 守卫比对 agenttool 白名单。
func OrchToolSchemaNames() []string {
	schemas := orchToolSchemas()
	names := make([]string, 0, len(schemas))
	for _, s := range schemas {
		m, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if n, ok := m["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}
```

- [ ] **Step 2: 写 parity 守卫测试**（`mcp_test.go` 追加；同时把 import 补上 `agenttool`）

```go
func TestOrchestrateToolNamesCoverSchemas(t *testing.T) {
	def, ok := agenttool.Lookup(agenttool.KeyOrchestrate)
	require.True(t, ok)
	allow := map[string]bool{}
	for _, n := range def.ToolNames {
		allow[n] = true
	}
	for _, name := range orch_svc.OrchToolSchemaNames() {
		assert.Truef(t, allow[name],
			"工具 %q 有 schema 却不在 agenttool ToolNames 白名单里"+
				"(CLI --allowedTools / codex enabled_tools / piagent allow 会拦掉,agent 调不到)", name)
	}
}
```

`mcp_test.go` 顶部 import 增加：`"github.com/agentre-ai/agentre/internal/pkg/agenttool"`。

- [ ] **Step 3: 跑测试看它失败**

Run: `go test ./internal/service/orch_svc/ -run TestOrchestrateToolNamesCoverSchemas -v`
Expected: FAIL —— 断言报 `工具 "read" ...` / `工具 "report" ...` 不在白名单。

- [ ] **Step 4: 修白名单**（`agenttool.go:38`）

```go
	{Key: KeyOrchestrate, MCPPath: "/mcp/orchestrate/", ToolNames: []string{"agent_list", "dispatch", "ask", "send", "finish", "reply", "report", "read"}},
```

- [ ] **Step 5: 跑测试看它通过 + 回归既有 MCP 测试**

Run: `go test ./internal/service/orch_svc/ -run 'TestOrchestrateToolNamesCoverSchemas|TestMCP_' -v`
Expected: PASS(3 个测试全绿)。

- [ ] **Step 6: Commit**

```bash
git commit internal/service/orch_svc/export_test.go internal/service/orch_svc/mcp_test.go internal/pkg/agenttool/agenttool.go \
  -m "🐛 orchestration: 补 read/report 进工具白名单(修 slice A 遗留:未进 --allowedTools 致 agent 调不到)+ parity 守卫"
```

---

### Task 2: `chat_svc.LatestAssistantText` —— 按会话取末条 assistant 文本

**背景:** `read` 的 running/peek 分支要拉在跑子任务的"当前最新 assistant 文本"。现有 `FinalAssistantText(messageID)` 按已完成轮的 messageID 取,running 时 stash 里还没有 messageID。新增按 sessionID 取末条 assistant 消息的原语。

**Files:**
- Modify: `internal/repository/chat_repo/message.go`(接口 + 实现)
- Test: `internal/repository/chat_repo/message_test.go`
- Modify: `internal/service/chat_svc/subagent_text.go`
- Modify: `internal/service/chat_svc/chat.go`（`ChatSvc` 接口）
- Test: `internal/service/chat_svc/subagent_text_test.go`（无则新建）

**Interfaces:**
- Produces: `chat_repo.MessageRepo.LatestAssistant(ctx, sessionID int64) (*chat_entity.Message, error)`；`chat_svc.ChatSvc.LatestAssistantText(ctx, sessionID int64) (string, error)`。

- [ ] **Step 1: repo 层写失败测试**（`message_test.go` 追加；对齐现有 sqlmock 风格）

```go
func TestMessageRepo_LatestAssistant(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(7), "assistant", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "seq"}).
			AddRow(42, 7, "assistant", 9))
	got, err := chat_repo.NewMessage().LatestAssistant(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_LatestAssistant_None(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(7), "assistant", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	got, err := chat_repo.NewMessage().LatestAssistant(ctx, 7)
	require.NoError(t, err)
	assert.Nil(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

> `message_test.go` 顶部若尚无 `chat_repo`/`testutils`/`sqlmock`/`assert`/`require` import,按现有其它测试补齐（照抄同文件既有测试的 import 块）。

- [ ] **Step 2: repo 接口 + 实现**（`message.go`）

接口块（`MessageRepo` interface，`Find` 附近）加：

```go
	// LatestAssistant 取某会话 seq 最大的一条 assistant 消息(无 → nil,nil)。
	// 用于 peek 运行中子任务的当前输出（read 工具的 running 分支）。
	LatestAssistant(ctx context.Context, sessionID int64) (*chat_entity.Message, error)
```

实现（`Find` 方法附近）加：

```go
func (r *messageRepo) LatestAssistant(ctx context.Context, sessionID int64) (*chat_entity.Message, error) {
	var m chat_entity.Message
	err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").
		Limit(1).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}
```

- [ ] **Step 3: 跑 repo 测试通过**

Run: `go test ./internal/repository/chat_repo/ -run TestMessageRepo_LatestAssistant -v`
Expected: PASS（两个用例）。若正则不匹配 gorm 实际 SQL，调正则到匹配（`LIMIT ?` 的占位与 arg `1` 已按 gorm v2 约定给出）。

- [ ] **Step 4: svc 层写失败测试**（`subagent_text_test.go`，无则新建 `package chat_svc_test`）

```go
func TestLatestAssistantText(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	// 造一条 assistant 消息(blocks 里一个 TextBlock)。
	blocksJSON := `[{"type":"text","text":"进行到一半"}]`
	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? AND role = \\? ORDER BY seq DESC LIMIT \\?").
		WithArgs(int64(3), "assistant", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "seq", "blocks_json"}).
			AddRow(5, 3, "assistant", 2, blocksJSON))
	got, err := chat_svc.Chat().LatestAssistantText(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "进行到一半", got)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

> 实现前先确认 `chat_entity.Message` 存 blocks 的列名（`blocks_json`，见 message_test.go 既有用例的 `BlocksJSON`）与 `GetBlocks()` 反序列化格式；若列名/JSON 形态与上不符，按 `chat_entity.Message` 的真实定义调整 `AddRow` 的列与 JSON。`testutils.Database` 走的是 sqlmock，`chat_svc.Chat()` 复用默认单例（不必 RegisterDeps）。

- [ ] **Step 5: svc 实现**（`subagent_text.go`，加在 `FinalAssistantText` 下方）

```go
// LatestAssistantText 取某会话「最近一条」assistant 文本(running 任务 peek 用;
// 无 assistant 消息 → 空串)。与 FinalAssistantText(按 messageID,已收尾)互补:
// 这里按 sessionID 取末条,不要求轮已完成。
func (s *chatSvc) LatestAssistantText(ctx context.Context, sessionID int64) (string, error) {
	if sessionID <= 0 {
		return "", nil
	}
	msg, err := chat_repo.Message().LatestAssistant(ctx, sessionID)
	if err != nil {
		return "", err
	}
	return messageText(msg)
}
```

`chat.go` 的 `ChatSvc` 接口（`FinalAssistantText` 声明附近）加：

```go
	// LatestAssistantText 按 sessionID 取末条 assistant 文本(running peek;无 → 空串)。
	LatestAssistantText(ctx context.Context, sessionID int64) (string, error)
```

- [ ] **Step 6: 跑 svc 测试通过**

Run: `go test ./internal/service/chat_svc/ -run TestLatestAssistantText -v`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git commit internal/repository/chat_repo/message.go internal/repository/chat_repo/message_test.go \
  internal/service/chat_svc/subagent_text.go internal/service/chat_svc/chat.go internal/service/chat_svc/subagent_text_test.go \
  -m "✨ chat_svc: LatestAssistantText(按 sessionID 取末条 assistant 文本，供编排 read peek)"
```

---

### Task 3: `status()` 工具 —— 本 Run 任务树快照

**Files:**
- Create: `internal/service/orch_svc/status.go`
- Test: `internal/service/orch_svc/status_test.go`
- Modify: `internal/service/orch_svc/mcp.go`（`dispatchTool` case + `handleStatus` + schema）
- Modify: `internal/pkg/agenttool/agenttool.go`（ToolNames 加 `status`）

**Interfaces:**
- Consumes: `s.tasks.FindBySession` / `s.tasks.ListByRun` / `s.agents.List`；`orch_entity.Task` 字段。
- Produces: `orch_svc.orchSvc.RunStatus(ctx, sessionID int64) (string, error)`；纯函数 `formatRunStatus(tasks []*orch_entity.Task, agentNames map[int64]string) string`。

> **范围说明(实现者照做,勿加戏)**:status 返回 JSON 数组,每项 `{task_id, agent, kind, status, brief, call_seq, has_summary, node?, parent_task_id?, blocked_on?}`。**不含 duration/时长**——避免引时钟依赖(卡住时长归 slice D 健康 sweep 负责),保证 formatter 是无时钟纯函数。`blocked_on` 仅对 `awaiting-children` 任务给出其仍活跃的 dispatch 子任务 id 列表。

- [ ] **Step 1: 纯 formatter 写失败测试**（`status_test.go`，`package orch_svc_test`）

```go
func TestFormatRunStatus(t *testing.T) {
	tasks := []*orch_entity.Task{
		{ID: 1, AgentID: 10, Kind: "dispatch", Status: orch_entity.TaskAwaitingChildren, Brief: "根", CallSeq: 1, ParentTaskID: 0},
		{ID: 2, AgentID: 11, Kind: "dispatch", Status: orch_entity.TaskRunning, Brief: "前端", CallSeq: 1, ParentTaskID: 1, NodeRef: "FE"},
		{ID: 3, AgentID: 12, Kind: "dispatch", Status: orch_entity.TaskDone, Brief: "后端", CallSeq: 1, ParentTaskID: 1, Summary: "完工"},
	}
	names := map[int64]string{10: "组长", 11: "小前", 12: "小后"}

	Convey("status JSON 覆盖任务树 + agent 名 + has_summary + blocked_on", t, func() {
		out := orch_svc.FormatRunStatusForTest(tasks, names)
		So(out, ShouldContainSubstring, `"task_id":2`)
		So(out, ShouldContainSubstring, `"agent":"小前"`)
		So(out, ShouldContainSubstring, `"node":"FE"`)
		So(out, ShouldContainSubstring, `"has_summary":true`)  // task#3 有 Summary
		So(out, ShouldContainSubstring, `"blocked_on":[2]`)    // task#1 awaiting-children,活跃子=#2
	})
}
```

在 `export_test.go` 加导出（纯函数便于测）：

```go
// FormatRunStatusForTest 仅测试用:暴露私有 formatRunStatus。
func FormatRunStatusForTest(tasks []*orch_entity.Task, agentNames map[int64]string) string {
	return formatRunStatus(tasks, agentNames)
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test ./internal/service/orch_svc/ -run TestFormatRunStatus -v`
Expected: FAIL（`formatRunStatus`/`FormatRunStatusForTest` 未定义）。

- [ ] **Step 3: 实现 formatter + RunStatus**（`status.go`）

```go
package orch_svc

import (
	"context"
	"encoding/json"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

type statusRow struct {
	TaskID     int64   `json:"task_id"`
	Agent      string  `json:"agent"`
	Kind       string  `json:"kind"`
	Status     string  `json:"status"`
	Brief      string  `json:"brief"`
	CallSeq    int     `json:"call_seq"`
	HasSummary bool    `json:"has_summary"`
	Node       string  `json:"node,omitempty"`
	ParentTask int64   `json:"parent_task_id,omitempty"`
	BlockedOn  []int64 `json:"blocked_on,omitempty"`
}

// formatRunStatus 把 Run 任务树渲染成紧凑 JSON 快照(供 Leader status 工具)。
// 无时钟依赖的纯函数;agentNames 缺省用 "agent#<id>"。
func formatRunStatus(tasks []*orch_entity.Task, agentNames map[int64]string) string {
	// 预计算每个父任务下仍活跃的 dispatch 子任务 id(blocked_on 只给 awaiting-children)。
	activeChildren := map[int64][]int64{}
	for _, t := range tasks {
		if t.Kind == orch_entity.TaskKindDispatch && t.IsActive() && t.ParentTaskID != 0 {
			activeChildren[t.ParentTaskID] = append(activeChildren[t.ParentTaskID], t.ID)
		}
	}
	rows := make([]statusRow, 0, len(tasks))
	for _, t := range tasks {
		name := agentNames[t.AgentID]
		if name == "" {
			name = "agent#" + itoa(t.AgentID)
		}
		row := statusRow{
			TaskID:     t.ID,
			Agent:      name,
			Kind:       t.Kind,
			Status:     t.Status,
			Brief:      t.Brief,
			CallSeq:    t.CallSeq,
			HasSummary: t.Summary != "",
			Node:       t.NodeRef,
			ParentTask: t.ParentTaskID,
		}
		if t.Status == orch_entity.TaskAwaitingChildren {
			row.BlockedOn = activeChildren[t.ID]
		}
		rows = append(rows, row)
	}
	b, _ := json.Marshal(rows)
	return string(b)
}

// RunStatus 返回调用者所在 Run 的任务树 JSON 快照。
func (s *orchSvc) RunStatus(ctx context.Context, sessionID int64) (string, error) {
	caller, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if caller == nil {
		return "", errRunNotActive
	}
	rows, err := s.tasks.ListByRun(ctx, caller.RunID)
	if err != nil {
		return "", err
	}
	names := map[int64]string{}
	if agents, aerr := s.agents.List(ctx); aerr != nil {
		logger.Ctx(ctx).Warn("orch.RunStatus: 取 agent 花名册失败,退回 agent#id", zap.Error(aerr))
	} else {
		for _, a := range agents {
			names[a.ID] = a.Name
		}
	}
	return formatRunStatus(rows, names), nil
}
```

`itoa` 辅助（若 `orch_svc` 内已有等价的 int64→string，直接用它，别新增）：本包已有 `strconv` 使用（`mcp.go`）。为避免重复，`status.go` 里 `name = "agent#" + itoa(...)` 改为 `import "strconv"` 并用 `strconv.FormatInt(t.AgentID, 10)`：

```go
		name := agentNames[t.AgentID]
		if name == "" {
			name = "agent#" + strconv.FormatInt(t.AgentID, 10)
		}
```

（即删掉 `itoa` 说法，`status.go` import 加 `"strconv"`。）

- [ ] **Step 4: 跑 formatter 测试通过**

Run: `go test ./internal/service/orch_svc/ -run TestFormatRunStatus -v`
Expected: PASS。

- [ ] **Step 5: RunStatus service 测试（走 mock）**（`status_test.go` 追加）

```go
func TestRunStatus_ScopesToCallerRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, RunID: 100, AgentID: 10, Kind: "dispatch", Status: orch_entity.TaskRunning, Brief: "根"},
	}, nil)
	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{{ID: 10, Name: "组长"}}, nil)

	Convey("status 定位调用者 Run 并渲染任务树", t, func() {
		out, err := orch_svc.Default().RunStatus(context.Background(), 500)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, `"agent":"组长"`)
	})
}
```

> import：`agent_entity`、`mock_orch_repo`、`mock_orch_svc`、goconvey、gomock（照抄 read_test.go 的 import 块并加 `agent_entity`）。

- [ ] **Step 6: MCP 接线**（`mcp.go`）

`dispatchTool` switch 加：

```go
	case "status":
		m.handleStatus(w, r, id, ref)
	case "cancel":
		m.handleCancel(w, r, id, ref, args) // Task 5 补 handler；本步先只加 status 也可,cancel 留 Task5
```

> 本 Task 只加 `status` 的 case + handler；`cancel` 的 case 留到 Task 5 再加（避免引用未定义的 `handleCancel`）。即本步只加：

```go
	case "status":
		m.handleStatus(w, r, id, ref)
```

`handleStatus`（放在 `handleRead` 附近）：

```go
func (m *orchMCP) handleStatus(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef) {
	out, err := m.svc.RunStatus(r.Context(), ref.sessionID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(out))
}
```

`orchToolSchemas()` 数组追加：

```go
		map[string]any{
			"name":        "status",
			"description": "查看本次编排整棵任务树的实时快照(每个子任务的 id/agent/类型/状态/brief/是否已主动汇报/所属流程节点/在等哪些子任务)。两次回报之间用它掌握全局。",
			"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
		},
```

- [ ] **Step 7: 白名单加 `status`**（`agenttool.go`，在 Task1 基础上追加）

```go
	{Key: KeyOrchestrate, MCPPath: "/mcp/orchestrate/", ToolNames: []string{"agent_list", "dispatch", "ask", "send", "finish", "reply", "report", "read", "status"}},
```

- [ ] **Step 8: 跑 orch_svc 全包 + parity**

Run: `go test ./internal/service/orch_svc/ -run 'TestFormatRunStatus|TestRunStatus|TestOrchestrateToolNamesCoverSchemas|TestMCP_' -v`
Expected: PASS。

- [ ] **Step 9: Commit**

```bash
git commit internal/service/orch_svc/status.go internal/service/orch_svc/status_test.go \
  internal/service/orch_svc/export_test.go internal/service/orch_svc/mcp.go internal/pkg/agenttool/agenttool.go \
  -m "✨ orchestration: status 工具(本 Run 任务树 JSON 快照,Leader 两次回报间可观测)"
```

---

### Task 4: `read` 运行中 peek 分支

**背景:** 现 `ReadTask` 只有 settled 分支;撞到 running 任务时也走 settled 格式(Result 多半空)。加:非终态任务 → 取会话当前最新 assistant 文本(`LatestAssistantText`),空则回兜底文案。

**Files:**
- Modify: `internal/service/orch_svc/deps.go`（`ChatGateway` 加 `LatestAssistantText`）
- Modify: `internal/app/orch_adapter.go`（适配）
- 重生 mock：`make mock`
- Modify: `internal/service/orch_svc/read.go`
- Test: `internal/service/orch_svc/read_test.go`
- Modify: `internal/service/orch_svc/mcp.go`（`read` schema 描述补 peek）

**Interfaces:**
- Consumes: `chat_svc.Chat().LatestAssistantText`（Task 2 产出）。
- Produces: `ChatGateway.LatestAssistantText(ctx, sessionID int64) (string, error)`。

- [ ] **Step 1: `ChatGateway` 加方法**（`deps.go`，`FinalAssistantText` 声明下方）

```go
	// LatestAssistantText 取会话「当前/末条」assistant 文本(running 任务 peek;settled 用 FinalAssistantText)。
	LatestAssistantText(ctx context.Context, sessionID int64) (string, error)
```

- [ ] **Step 2: 适配器实现**（`orch_adapter.go`，`FinalAssistantText` 方法下方）

```go
// LatestAssistantText 按 sessionID 取会话末条 assistant 文本(running peek)。
func (a *orchChatAdapter) LatestAssistantText(ctx context.Context, sessionID int64) (string, error) {
	return chat_svc.Chat().LatestAssistantText(ctx, sessionID)
}
```

- [ ] **Step 3: 重生 mock**

Run: `make mock`
Expected: `internal/service/orch_svc/mock_orch_svc/mock_deps.go` 里 `MockChatGateway` 多出 `LatestAssistantText`。

- [ ] **Step 4: 写 running 分支失败测试**（`read_test.go` 追加）

```go
func TestReadTask_RunningPeek(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning}, nil)
	chat.EXPECT().LatestAssistantText(gomock.Any(), int64(800)).Return("我正在写测试", nil)

	Convey("read 撞 running 任务 → 取会话当前最新 assistant 文本(peek)", t, func() {
		out, err := orch_svc.Default().ReadTask(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, "我正在写测试")
	})
}

func TestReadTask_RunningPeek_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning}, nil)
	chat.EXPECT().LatestAssistantText(gomock.Any(), int64(800)).Return("", nil)

	Convey("read 撞 running 且尚无输出 → 兜底文案", t, func() {
		out, err := orch_svc.Default().ReadTask(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, "尚无输出")
	})
}
```

- [ ] **Step 5: 跑测试看失败**

Run: `go test ./internal/service/orch_svc/ -run TestReadTask_RunningPeek -v`
Expected: FAIL（现 ReadTask 无 running 分支，走 settled 格式，不含 peek 文本 / 兜底）。

- [ ] **Step 6: 实现 running 分支**（`read.go`，在 foreign 校验后、settled 构造前插入）

```go
	if !tk.IsTerminal() {
		var b strings.Builder
		fmt.Fprintf(&b, "task #%d · agent#%d · %s(运行中)", tk.ID, tk.AgentID, tk.Status)
		latest, _ := s.chat.LatestAssistantText(ctx, tk.SessionID)
		if strings.TrimSpace(latest) == "" {
			b.WriteString("\n(运行中,尚无输出)")
		} else {
			fmt.Fprintf(&b, "\n【当前】%s", latest)
		}
		return b.String(), nil
	}
```

- [ ] **Step 7: 跑 read 全组测试通过**

Run: `go test ./internal/service/orch_svc/ -run TestReadTask -v`
Expected: PASS（settled + 跨 Run 拒绝 + running peek + 兜底 四组）。

- [ ] **Step 8: 更新 read 工具描述**（`mcp.go` 的 `read` schema `description`）

```go
			"description": "读取你派发/同 Run 内某任务的输出:已完成→拉小结+完整正文;运行中→peek 它当前最新进展。传通知/status 里给出的 task_id。",
```

- [ ] **Step 9: Commit**

```bash
git commit internal/service/orch_svc/deps.go internal/app/orch_adapter.go \
  internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/service/orch_svc/read.go \
  internal/service/orch_svc/read_test.go internal/service/orch_svc/mcp.go \
  -m "✨ orchestration: read 运行中 peek 分支(拉在跑子任务当前 assistant 文本,空则兜底)"
```

---

### Task 5: `cancel()` 工具 + watcher 取消让位守卫

**背景:** `cancel(task_id)` = 软取消(标 `TaskCanceled`)+ 尽力硬打断(复用 `chat_svc.Stop`)+ 级联 dispatch 子孙。关键交互:被取消任务的 watcher goroutine 仍在跑,硬打断令其轮 abort → watcher 醒来见 status="error" → 会把我标的 `TaskCanceled` 覆盖成 `TaskError` 并 `<task_error>` 注入父。故须加 **watcher 让位守卫**:结算前重读 `fresh`,若已 `TaskCanceled` 则静默 return(不改状态、不回报)。同一守卫也覆盖"取消排队中未起轮任务"——它之后被 kick 起轮、结算时同样让位(最多空耗一轮,best-effort 可接受)。

**Files:**
- Modify: `internal/service/orch_svc/deps.go`（`ChatGateway` 加 `AbortTurn`）
- Modify: `internal/app/orch_adapter.go`（适配，吞 `ChatStopNoActive`）
- 重生 mock：`make mock`
- Create: `internal/service/orch_svc/cancel.go`
- Test: `internal/service/orch_svc/cancel_test.go`
- Modify: `internal/service/orch_svc/complete.go`（让位守卫）
- Test: `internal/service/orch_svc/complete_test.go`（取消让位用例）
- Modify: `internal/service/orch_svc/mcp.go`（`dispatchTool` case + `handleCancel` + schema）
- Modify: `internal/pkg/agenttool/agenttool.go`（ToolNames 加 `cancel`）

**Interfaces:**
- Consumes: `chat_svc.Chat().Stop`、`httputils.Error` / `code.ChatStopNoActive`（`orch_adapter.go` 已 import 二者）。
- Produces: `ChatGateway.AbortTurn(ctx, sessionID int64) error`；`orchSvc.CancelTask(ctx, sessionID, taskID int64) (int, error)`；`collectSubtree(rows, rootID) []*orch_entity.Task`。

- [ ] **Step 1: watcher 让位守卫先写失败测试**（`complete_test.go` 追加；用既有 `WatchCompletionForTest` 驱动）

```go
func TestWatchCompletion_YieldsToCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().SetSchedulerCapForTest(1)
	t.Cleanup(func() { orch_svc.Default().ResetSchedulersForTest(); orch_svc.Default().SetSchedulerCapForTest(0) })

	task := &orch_entity.Task{ID: 9, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning}
	// 会话 abort → 状态 error;watcher 重读 fresh 发现已被取消 → 让位。
	chat.EXPECT().AgentStatus(gomock.Any(), int64(800)).Return("error", nil)
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, Status: orch_entity.TaskCanceled}, nil)
	// 关键:被取消 → 不得 Update、不得 injectToParent(无其它 mock EXPECT 即验证)。

	ch := make(chan orch_svc.TurnDone, 1)
	ch <- orch_svc.TurnDone{SessionID: 800, OK: false}
	close(ch)

	Convey("watcher 见任务已取消 → 让位:不覆盖状态、不回报父", t, func() {
		orch_svc.Default().WatchCompletionForTest(context.Background(), task, ch, func() {})
		// 无 tasks.Update / chat.SendAndForget 期望被调用即通过(gomock 严格模式会在多余调用时 fail)。
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test ./internal/service/orch_svc/ -run TestWatchCompletion_YieldsToCanceled -v`
Expected: FAIL —— 现 error 分支直接 `markTaskError`(会调用未 EXPECT 的 `tasks.Update`),gomock 报意外调用。

- [ ] **Step 3: 加让位守卫**（`complete.go` 的 `watchCompletion`）

`idle` case 改为(把既有的 fresh 读上移并加守卫)：

```go
		case "idle":
			fresh, _ := s.tasks.Find(ctx, task.ID)
			if fresh != nil && fresh.Status == orch_entity.TaskCanceled {
				logger.Ctx(ctx).Info("orch.watchCompletion: 任务已被取消,watcher 让位不回报", zap.Int64("task", task.ID))
				return
			}
			result, _ := s.chat.FinalAssistantText(ctx, task.SessionID)
			task.Status = orch_entity.TaskDone
			task.Result = result
			if fresh != nil && fresh.Summary != "" {
				task.Summary = fresh.Summary
			}
			if err := s.tasks.Update(ctx, task); err != nil {
				logger.Ctx(ctx).Error("orch.watchCompletion: 写子任务终态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
			}
			s.emitRunUpdated(ctx, task.RunID)
			s.reportToParent(ctx, task.ParentTaskID, task)
			return
```

`error` case 改为：

```go
		case "error":
			if fresh, _ := s.tasks.Find(ctx, task.ID); fresh != nil && fresh.Status == orch_entity.TaskCanceled {
				logger.Ctx(ctx).Info("orch.watchCompletion: 任务已被取消,watcher 让位(不标 error)", zap.Int64("task", task.ID))
				return
			}
			s.markTaskError(ctx, task, "运行时崩溃")
			s.emitRunUpdated(ctx, task.RunID)
			return
```

- [ ] **Step 4: 跑让位测试 + watcher 既有回归**

Run: `go test ./internal/service/orch_svc/ -run 'TestWatchCompletion' -v`
Expected: PASS（新让位用例 + 既有 done/error 用例全绿——既有用例的任务非 canceled，守卫不触发，行为不变）。

- [ ] **Step 5: `ChatGateway.AbortTurn` + 适配器**（`deps.go` + `orch_adapter.go`）

`deps.go`（`LatestAssistantText` 下方）：

```go
	// AbortTurn 尽力硬打断会话在跑的一轮(复用 chat_svc.Stop;无活跃 turn → 视作无害成功)。
	AbortTurn(ctx context.Context, sessionID int64) error
```

`orch_adapter.go`（`LatestAssistantText` 方法下方）：

```go
// AbortTurn 尽力硬打断会话在跑的一轮(复用 chat_svc.Stop)。
// 无活跃 turn(ChatStopNoActive)视作无害成功:软取消已生效,硬打断无对象。
func (a *orchChatAdapter) AbortTurn(ctx context.Context, sessionID int64) error {
	_, err := chat_svc.Chat().Stop(ctx, &chat_svc.StopRequest{SessionID: sessionID})
	if err != nil {
		var herr *httputils.Error
		if errors.As(err, &herr) && herr.Code == code.ChatStopNoActive {
			return nil
		}
	}
	return err
}
```

> `orch_adapter.go` 已 import `errors` / `httputils` / `code`(见 `SendAndForget`),无需新增 import。若 `chat_svc.StopRequest` 除 `SessionID` 外有其它必填字段,按其定义补零值——现只需 `SessionID`。

- [ ] **Step 6: 重生 mock**

Run: `make mock`
Expected: `MockChatGateway` 多出 `AbortTurn`。

- [ ] **Step 7: `cancel.go` 写失败测试**（`cancel_test.go`）

```go
func TestCancelTask_CascadesAndAborts(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(2)).Return(
		&orch_entity.Task{ID: 2, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning, ParentTaskID: 1}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 2, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning, ParentTaskID: 1, Kind: "dispatch"},
		{ID: 3, RunID: 100, SessionID: 801, Status: orch_entity.TaskRunning, ParentTaskID: 2, Kind: "dispatch"}, // 子孙
		{ID: 4, RunID: 100, SessionID: 802, Status: orch_entity.TaskDone, ParentTaskID: 2, Kind: "dispatch"},    // 已终态,跳过
	}, nil)
	// #2 #3 被标 canceled + AbortTurn;#4 已 done 跳过。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	chat.EXPECT().AbortTurn(gomock.Any(), int64(800)).Return(nil)
	chat.EXPECT().AbortTurn(gomock.Any(), int64(801)).Return(nil)
	emit.EXPECT().Emit(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	Convey("cancel 级联标记子孙活任务 + 尽力硬打断,返回取消数", t, func() {
		n, err := orch_svc.Default().CancelTask(context.Background(), 500, 2)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 2)
	})
}

func TestCancelTask_RejectsForeignRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(77)).Return(
		&orch_entity.Task{ID: 77, RunID: 999}, nil)

	Convey("cancel 跨 Run 任务 → 拒绝", t, func() {
		_, err := orch_svc.Default().CancelTask(context.Background(), 500, 77)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in this run")
	})
}
```

- [ ] **Step 8: 跑测试看失败**

Run: `go test ./internal/service/orch_svc/ -run TestCancelTask -v`
Expected: FAIL（`CancelTask` 未定义）。

- [ ] **Step 9: 实现 `cancel.go`**

```go
package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// CancelTask 软取消 + 尽力硬打断目标任务及其全部 dispatch 子孙(限调用者同 Run)。
// 返回被标记取消的活任务数。watcher 侧的让位守卫保证被取消任务不会被误翻/误回报。
func (s *orchSvc) CancelTask(ctx context.Context, sessionID, taskID int64) (int, error) {
	caller, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if caller == nil {
		return 0, errRunNotActive
	}
	target, err := s.tasks.Find(ctx, taskID)
	if err != nil {
		return 0, err
	}
	if target == nil || target.RunID != caller.RunID {
		return 0, errForeignTask
	}
	rows, err := s.tasks.ListByRun(ctx, caller.RunID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, tk := range collectSubtree(rows, taskID) {
		if !tk.IsActive() {
			continue
		}
		tk.Status = orch_entity.TaskCanceled
		if uerr := s.tasks.Update(ctx, tk); uerr != nil {
			logger.Ctx(ctx).Error("orch.CancelTask: 标记取消失败", zap.Int64("task", tk.ID), zap.Error(uerr))
			continue
		}
		n++
		// 尽力硬打断在跑的一轮(无活跃 turn → 适配器吞成功)。
		if aerr := s.chat.AbortTurn(ctx, tk.SessionID); aerr != nil {
			logger.Ctx(ctx).Warn("orch.CancelTask: 硬打断失败(软取消已生效)", zap.Int64("task", tk.ID), zap.Error(aerr))
		}
	}
	s.emitRunUpdated(ctx, caller.RunID)
	return n, nil
}

// collectSubtree 返回 rootID 及其全部 dispatch 子孙任务(BFS,防环)。
func collectSubtree(rows []*orch_entity.Task, rootID int64) []*orch_entity.Task {
	byID := map[int64]*orch_entity.Task{}
	byParent := map[int64][]*orch_entity.Task{}
	for _, t := range rows {
		byID[t.ID] = t
		byParent[t.ParentTaskID] = append(byParent[t.ParentTaskID], t)
	}
	var out []*orch_entity.Task
	seen := map[int64]bool{}
	queue := []int64{rootID}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		if t := byID[id]; t != nil {
			out = append(out, t)
		}
		for _, c := range byParent[id] {
			if c.Kind == orch_entity.TaskKindDispatch {
				queue = append(queue, c.ID)
			}
		}
	}
	return out
}
```

- [ ] **Step 10: 跑 cancel 测试通过**

Run: `go test ./internal/service/orch_svc/ -run TestCancelTask -v`
Expected: PASS。

- [ ] **Step 11: MCP 接线**（`mcp.go`）

`dispatchTool` switch 加：

```go
	case "cancel":
		m.handleCancel(w, r, id, ref, args)
```

`handleCancel`：

```go
func (m *orchMCP) handleCancel(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		TaskID int64 `json:"task_id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.TaskID <= 0 {
		writeRPCError(w, id, -32602, "task_id is required")
		return
	}
	n, err := m.svc.CancelTask(r.Context(), ref.sessionID, p.TaskID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(fmt.Sprintf("已请求取消 %d 个任务(目标 #%d 及其子孙);进行中的一轮会尽力打断。", n, p.TaskID)))
}
```

`orchToolSchemas()` 追加：

```go
		map[string]any{
			"name":        "cancel",
			"description": "中止一个跑偏/卡住的子任务(软取消 + 尽力打断在跑的一轮),并级联取消它派生的全部子孙任务。仅能取消你所在编排内的任务。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer"},
				},
			},
		},
```

- [ ] **Step 12: 白名单加 `cancel`**（`agenttool.go`）

```go
	{Key: KeyOrchestrate, MCPPath: "/mcp/orchestrate/", ToolNames: []string{"agent_list", "dispatch", "ask", "send", "finish", "reply", "report", "read", "status", "cancel"}},
```

- [ ] **Step 13: 跑 orch_svc 全包**

Run: `go test ./internal/service/orch_svc/... `
Expected: PASS（含 parity 守卫——此时 schemas 有 status/cancel,白名单也有,parity 绿）。

- [ ] **Step 14: Commit**

```bash
git commit internal/service/orch_svc/deps.go internal/app/orch_adapter.go \
  internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/service/orch_svc/cancel.go \
  internal/service/orch_svc/cancel_test.go internal/service/orch_svc/complete.go \
  internal/service/orch_svc/complete_test.go internal/service/orch_svc/mcp.go \
  internal/pkg/agenttool/agenttool.go \
  -m "✨ orchestration: cancel 工具(软取消+尽力硬打断+级联子孙)+ watcher 取消让位守卫"
```

---

### Task 6: guidance 教会新能力

**Files:**
- Modify: `internal/service/orch_svc/turn.go`（`orchGuidance`）
- Test: `internal/service/orch_svc/turn_test.go`（若有 guidance 断言则更新；否则跳过测试改动，仅确保编译）

**Interfaces:** 无新增。

- [ ] **Step 1: 更新 `orchGuidance`**（`turn.go:13-18`）

在现有 guidance 基础上补 status/read-peek/cancel 三句（替换 `const orchGuidance = ...` 整块）：

```go
const orchGuidance = `你被授予编排能力(dispatch/ask/send/finish/report/read/status/cancel + agent_list)。模型:` +
	`一切结果都会回到你、由你决定下一步;` +
	`并行 dispatch 子任务,审核/测试/合并也是 dispatch,返工用 send,补信息用 ask,收口用 finish。` +
	`子任务完成/报错默认只给你一条轻量通知(task_done/task_error),要看输出用 read(task_id=…)按需拉全文;` +
	`read 运行中的子任务会返回它当前进展(peek)。用 status() 随时看整棵任务树(谁在跑/谁在等/谁已完成),两次回报之间不再全盲。` +
	`发现某子任务跑偏或卡住,用 cancel(task_id=…)中止它(会级联取消其子孙)。` +
	`子任务想主动汇报中途进展用 report、收口小结用 finish,才会把内容内联给你。` +
	`agent_list 即你本次可调度的全集。无次数/时长/成本上限——自己判断何时收口或换策略。用户可能随时插话。`
```

- [ ] **Step 2: 确认 turn_test 是否钉 guidance 文案**

Run: `go test ./internal/service/orch_svc/ -run TestBuildTurnExtras -v`（或 `grep -n orchGuidance internal/service/orch_svc/turn_test.go`）
- 若测试断言 guidance 含某旧子串且仍成立 → 直接绿；
- 若断言精确等于旧全文 → 更新断言到新文案；
- 若无 guidance 断言 → 无需改测试。

- [ ] **Step 3: 跑 turn 测试通过**

Run: `go test ./internal/service/orch_svc/ -run 'TestBuildTurn' -v`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git commit internal/service/orch_svc/turn.go internal/service/orch_svc/turn_test.go \
  -m "✨ orchestration: guidance 教会 status/read-peek/cancel(turn_test 同步)"
```

> 若 Step 2 判定 turn_test.go 无需改动，则 commit 只带 `turn.go`。

---

### Task 7: 收尾全量 gate

**Files:** 无代码改动（仅验证 + 台账）。

- [ ] **Step 1: 后端全量测试**

Run: `make test-backend 2>&1 | tail -30`
Expected: `ok`（关注真 exit code：`make test-backend; echo "EXIT=$?"`，须 `EXIT=0`）。已知旁路负债 `pkg/piagent TestStreamDiagnostics*` 预存在 FAIL——若仅它红，记录不修（rule #4，非本切片引入）；orch_svc goroutine-leak flake 重跑确认。

- [ ] **Step 2: lint**

Run: `make lint 2>&1 | tail -30`（真 exit code）
Expected: 0 —— 本切片新增文件无 lint 问题。既有旁路 lint 债(control/deadlock/query_test 等,非本切片文件)不修。若报**本切片文件**的 goimports/prealloc,当场修。

- [ ] **Step 3: 复核工具三处齐全**

Run: `go test ./internal/service/orch_svc/ -run TestOrchestrateToolNamesCoverSchemas -v`
Expected: PASS —— 守住 schema↔白名单 parity（新 status/cancel 都在）。

- [ ] **Step 4: 更新 SDD 台账 + memory**

- 台账 `.superpowers/sdd/harness-b-progress.md` 记录各 Task 完成 commit。
- 更新 memory `project_orch_harness_slices.md`：切片 B 从"待做"→"已完成"，记 read/report 白名单修复(pre-existing)、三工具、watcher 让位守卫、commit 范围。

---

## Self-Review 记录

- **spec 覆盖**：status/read-running/cancel 三工具 + 归属校验 + 级联 + 硬打断复用 chat_svc.Stop + LatestAssistantText 薄接口——对齐 spec §B 全部条目。spec 里 status 的"时长/blocked_on"：blocked_on 做了(awaiting-children),时长**显式裁掉**（避免时钟依赖，归 slice D），已在 Task3 范围说明标注。
- **pre-existing 修复**：read/report 白名单缺口是 slice B 的**前置阻塞**（read 是 slice B 核心），故纳入本 plan Task1、独立 commit、带 parity 守卫——非 drive-by。
- **类型一致**：`ChatGateway` 两个新方法（`LatestAssistantText`/`AbortTurn`）签名在 deps.go/adapter/mock 三处一致；`CancelTask` 返回 `(int, error)`，handler 用 `%d` 打印。
- **占位符扫描**：无 TBD；每个改动步都给了完整代码或明确的"照抄既有 import 块"指令。
- **YAGNI**：不做 duration、不做 cancel 的父通知（取消由 Leader 显式发起，它自己知道）、不做排队任务的 pre-launch 拦截（watcher 让位已保证正确性，空耗一轮可接受）。
