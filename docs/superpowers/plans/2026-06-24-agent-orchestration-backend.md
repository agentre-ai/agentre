# Agent 编排能力基座 — 后端引擎 实现计划（plan-1）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建一套 headless 的「Agent 编排引擎」后端域 `orch`：多个 agent 在用户自然语言指导下，通过 `dispatch / ask / send / finish` 四个 MCP 原语自行编排协作；唯一新机制 = 把 subagent 的「同步阻塞单发」改造成「异步并行多发 + 完成回报续轮」。本计划**不含前端**（前端 = plan-1b）、**不删 group**（删除 = plan-2）。

**Architecture:** 新领域包 `orch_entity` / `orch_repo` / `orch_svc`（与 group/chat 平级，依赖方向 `app → svc → repo → entity`）。`orch_svc` 通过消费者侧窄接口（DIP）依赖 `chat_svc`（会话/轮次）、`agent_repo`（花名册）、`chat_svc.ApprovalGateway`（危险操作审批）。编排原语以 MCP server 形态按 `agenttool` 注册表注入到启用了 `orchestrate` 工具的 agent 会话；完成回报续轮复用 `chat_svc` 的「唤起一条会话再跑一轮」能力（与后台任务自主续轮 `AutonomousTurnSource` 同一思路）。并发调度器把 group 的 `pending/inflight` 模式 lift 进编排 runtime。本计划末尾用 e2e fake-runtime 证明一条最小编排（dispatch → 回报 → finish）跑通。

**Tech Stack:** Go 1.26、cago（`github.com/cago-frame/cago`）、gorm + gormigrate、sqlmock（repo 单测）、go.uber.org/mock（svc 单测）、goconvey、Wails v2 绑定。

## Global Constraints

逐条从 spec（`docs/superpowers/specs/2026-06-23-agent-orchestration-design.md`）与仓库纪律（`AGENTS.md`）抄出，**每个 task 的要求隐含包含本节**：

- **严格 TDD：Red → Green → Refactor**，无失败测试不写实现；新功能先写 BDD 行为 spec（happy path + 至少一条边界/错误）。
- **改 bug 先写复现回归测试，跑一次看它以正确理由失败**，再修；不能复现就明确告诉用户。
- **不碰与本任务无关的文件**；不夹带 drive-by 重构/改名/格式化/删死代码/import 重排。
- `orch_repo` 单测一律 `testutils.Database(t)` + sqlmock，**禁真库**（迁移自身 `*_test.go` 是唯一例外）。
- `orch_svc` 单测用 mockgen 生成 repo/gateway mock + `RegisterXxx` 注入，**不连库**。
- **迁移追加到 `migrationList()` 末尾**，禁改既有迁移；DDL 用原生 SQL，不靠 `AutoMigrate`。迁移 ID 命名 `YYYYMMDDNNNN`。
- **关键链路打日志**：`logger.Ctx(ctx)`，消息用小写 `package.Method:` 前缀，动态值走 `zap.Xxx(...)` 字段。
- **SOLID / 高内聚低耦合**：service 只依赖 repository **接口**（实现 bootstrap 注入）；consumer 侧定义窄接口（ISP）；`internal/pkg` 不反向 import service/repo。
- Wails 绑定层只做 `parse → svc.Xxx().Method → return`；业务逻辑不进 `App` 结构体。
- **状态 vs 结果（勿混）**：`Task.Status` = runtime 客观可知生命周期（驱动颜色/计数）；「评审通过/驳回/合并完成」是 agent 自报语义结果，落 `Result`（自由文本），**不是状态枚举**。
- **无「返工/失败(语义)」实体、不计数、无 `⟲×N`**：子任务只 `run → done` 交回报告；成败与后续由派发者读报告判断（再 `dispatch` / `send` / `finish`）。`send` 续做 = 同会话再跑、**不新增节点/边**。
- **完成判定杜绝卡 `运行中`**：从会话真实空闲推导，状态写失败可被对账纠正；premature-done 无害（派发者必读报告，没做完再 `send`）。**根任务例外**：Run 收口只认 Leader 显式 `finish`，根任务不走「安静轮结束」自动收口。
- **危险操作审批 = 各 agent 后端自身权限**，编排层不新增审批层；编排自身写工具走通用 `tool_approval`（4 分钟超时）。
- **能力门控**：只有后端声明 `CapMCPTools`（Claude Code / Codex）的 agent 能调用编排工具；piagent/builtin 不行。
- 子会话不渗进普通会话列表（按 `run_id` 隐藏）。

---

## File Structure（本计划新建/修改）

**新建：**
- `internal/model/entity/orch_entity/run.go` — `OrchestrationRun` 充血实体 + 状态常量。
- `internal/model/entity/orch_entity/task.go` — `Task` 充血实体 + Kind/状态常量。
- `internal/model/entity/orch_entity/{run,task}_test.go` — 实体单测。
- `migrations/202606240001_orchestration.go`(+`_test.go`) — orch 表 + `chat_sessions.run_id` + `tools_json` 种子。
- `internal/repository/orch_repo/run.go` / `task.go`(+ `_test.go` + `mock_orch_repo/`) — 仓储。
- `internal/service/orch_svc/orch.go` — svc 骨架 + `Default()`/`RegisterDeps()`。
- `internal/service/orch_svc/deps.go`(+ `mock_orch_svc/`) — 消费者侧窄接口（ChatGateway / AgentLookup）。
- `internal/service/orch_svc/create.go` — `CreateRun`。
- `internal/service/orch_svc/dispatch.go` — 异步派发 + 完成回报续轮。
- `internal/service/orch_svc/scheduler.go` — 并发调度器（lift 自 group）。
- `internal/service/orch_svc/ask.go` — ask 排队/阻塞 + 死锁检测。
- `internal/service/orch_svc/control.go` — pause/resume/stop/speak。
- `internal/service/orch_svc/mcp.go` — orchestrate MCP handler + token + tool schemas。
- `internal/service/orch_svc/turn.go` — `BuildTurnMCP` / `BuildTurnExtras`（注入到会话）。
- `internal/service/orch_svc/*_test.go` — svc 单测。
- `internal/app/orch.go` — Wails 绑定。
- `e2e/tests/orchestration.spec.ts`（+ fake-runtime 扩展）— 最小编排 e2e。

**修改：**
- `internal/pkg/agenttool/agenttool.go` — 加 `KeyOrchestrate` + 注册表项。
- `internal/repository/chat_repo/session.go` — `defaultSessionScope` 增 `run_id = 0`。
- `internal/service/chat_svc/turn_extras.go` — 若 `RegisterTurnExtrasProvider` 是单槽，改成可叠加 registry（in-scope 小重构）。
- `internal/bootstrap/cago.go` — repo 注册 + gateway 挂载 `/mcp/orchestrate/`。
- `internal/app/app.go` — `orch_svc.Default().RegisterDeps(...)`。
- `migrations/migrations.go` — `migrationList()` 末尾追加。

---

## Phase A — 数据模型与迁移

### Task 1: `orch_entity` — Run 与 Task 充血实体

**Files:**
- Create: `internal/model/entity/orch_entity/run.go`
- Create: `internal/model/entity/orch_entity/task.go`
- Test: `internal/model/entity/orch_entity/run_test.go`
- Test: `internal/model/entity/orch_entity/task_test.go`

**Interfaces:**
- Produces:
  - `OrchestrationRun{ID, Goal, LeaderAgentID, FlowID, FlowContent, Status, RootTaskID, ProjectID, Createtime, Updatetime int64/string}`；方法 `IsActive() bool`、`CanAdvance() bool`。
  - Run 状态常量 `RunPending="pending"`、`RunRunning="running"`、`RunPaused="paused"`、`RunDone="done"`、`RunStopped="stopped"`。
  - `Task{ID, RunID, AgentID, SessionID, ParentTaskID, Kind, Status, Brief, Result, CallSeq, Refs, Createtime, Updatetime}`；方法 `IsTerminal() bool`、`IsWaitingUser() bool`、`IsActive() bool`。
  - Task Kind 常量 `TaskKindDispatch="dispatch"`、`TaskKindAsk="ask"`。
  - Task 状态常量 `TaskPending="pending"`、`TaskRunning="running"`、`TaskAwaitingChildren="awaiting-children"`、`TaskAwaitingUser="awaiting-user"`、`TaskDone="done"`、`TaskCanceled="canceled"`、`TaskPaused="paused"`、`TaskError="error"`。

- [ ] **Step 1: 写失败测试** `run_test.go`

```go
package orch_entity_test

import (
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestRun_TableName(t *testing.T) {
	assert.Equal(t, "orchestration_runs", (&orch_entity.OrchestrationRun{}).TableName())
}

func TestRun_IsActive(t *testing.T) {
	assert.True(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunRunning}).IsActive())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunStopped}).IsActive())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunDone}).IsActive())
	assert.False(t, (*orch_entity.OrchestrationRun)(nil).IsActive())
}

func TestRun_CanAdvance(t *testing.T) {
	assert.True(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunRunning}).CanAdvance())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunPaused}).CanAdvance())
}

func TestRun_StatusConstsAreObjectiveLifecycle(t *testing.T) {
	// 守 spec：Run 不单设 error；客观生命周期仅这 5 个。
	all := []string{
		orch_entity.RunPending, orch_entity.RunRunning, orch_entity.RunPaused,
		orch_entity.RunDone, orch_entity.RunStopped,
	}
	assert.Len(t, all, 5)
	assert.NotContains(t, all, "error")
	_ = consts.ACTIVE
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/model/entity/orch_entity/...`
Expected: FAIL，`package orch_entity is not in std`（包不存在）/ undefined。

- [ ] **Step 3: 写最小实现** `run.go`

```go
// Package orch_entity 维护编排 Run 与 Task 的充血实体。Run = 一次编排（会话容器），
// Task = 树上的一个任务（一个 agent + 一段 brief + 一条持久会话）。
package orch_entity

import "github.com/cago-frame/cago/pkg/consts"

// Run 客观生命周期（与流程无关；Run 不单设 error，根任务技术崩溃由 Task.error 体现）。
const (
	RunPending = "pending"
	RunRunning = "running"
	RunPaused  = "paused"
	RunDone    = "done"
	RunStopped = "stopped"
)

// OrchestrationRun 一次编排 Run。
type OrchestrationRun struct {
	ID            int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Goal          string `gorm:"column:goal;type:text;not null;default:''"`
	LeaderAgentID int64  `gorm:"column:leader_agent_id;type:bigint;not null;default:0"`
	FlowID        int64  `gorm:"column:flow_id;type:bigint;not null;default:0"`        // 编排流程库引用，0=临时/无
	FlowContent   string `gorm:"column:flow_content;type:text;not null;default:''"`   // 创建时快照的流程正文（注入 Leader）
	Status        string `gorm:"column:status;type:text;not null;default:'pending'"`
	RootTaskID    int64  `gorm:"column:root_task_id;type:bigint;not null;default:0"`
	ProjectID     int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	Createtime    int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime    int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*OrchestrationRun) TableName() string { return "orchestration_runs" }

// IsActive Run 是否在跑（仅 running）。
func (r *OrchestrationRun) IsActive() bool { return r != nil && r.Status == RunRunning }

// CanAdvance 是否可推进（暂停/停止/完成则不可）。
func (r *OrchestrationRun) CanAdvance() bool { return r != nil && r.Status == RunRunning }

var _ = consts.ACTIVE
```

- [ ] **Step 4: 写 `task.go`**

```go
package orch_entity

// Task Kind：新节点/边只来自 dispatch；ask 是平级问答。
const (
	TaskKindDispatch = "dispatch"
	TaskKindAsk      = "ask"
)

// Task 客观生命周期状态（驱动 UI 颜色/计数；语义结果走 Result 自由文本，不是状态）。
const (
	TaskPending          = "pending"
	TaskRunning          = "running"
	TaskAwaitingChildren = "awaiting-children" // 等子任务回报
	TaskAwaitingUser     = "awaiting-user"     // 等你审批/回复（唯一琥珀）
	TaskDone             = "done"
	TaskCanceled         = "canceled"
	TaskPaused           = "paused"
	TaskError            = "error" // 技术崩溃（运行时退出/不可恢复异常）
)

// Task 编排树上的一个任务。
type Task struct {
	ID           int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID        int64  `gorm:"column:run_id;type:bigint;not null;default:0"`
	AgentID      int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	SessionID    int64  `gorm:"column:session_id;type:bigint;not null;default:0"` // 复用 chat_entity.Session
	ParentTaskID int64  `gorm:"column:parent_task_id;type:bigint;not null;default:0"`
	Kind         string `gorm:"column:kind;type:text;not null;default:'dispatch'"`
	Status       string `gorm:"column:status;type:text;not null;default:'pending'"`
	Brief        string `gorm:"column:brief;type:text;not null;default:''"`
	Result       string `gorm:"column:result;type:text;not null;default:''"` // agent 自报语义报告（自由文本）
	CallSeq      int    `gorm:"column:call_seq;type:int;not null;default:0"` // 同 agent 第几次被 dispatch
	Refs         string `gorm:"column:refs;type:text;not null;default:''"`   // JSON：被引用的产物/任务
	Createtime   int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime   int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Task) TableName() string { return "orch_tasks" }

// IsTerminal 是否到达终态。
func (t *Task) IsTerminal() bool {
	return t != nil && (t.Status == TaskDone || t.Status == TaskCanceled || t.Status == TaskError)
}

// IsWaitingUser 是否在等你（审批/回复）。
func (t *Task) IsWaitingUser() bool { return t != nil && t.Status == TaskAwaitingUser }

// IsActive 是否仍活着（非终态）。
func (t *Task) IsActive() bool { return t != nil && !t.IsTerminal() }
```

- [ ] **Step 5: 写 `task_test.go`**

```go
package orch_entity_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestTask_TableName(t *testing.T) {
	assert.Equal(t, "orch_tasks", (&orch_entity.Task{}).TableName())
}

func TestTask_IsTerminal(t *testing.T) {
	for _, s := range []string{orch_entity.TaskDone, orch_entity.TaskCanceled, orch_entity.TaskError} {
		assert.True(t, (&orch_entity.Task{Status: s}).IsTerminal(), s)
	}
	for _, s := range []string{orch_entity.TaskRunning, orch_entity.TaskAwaitingChildren, orch_entity.TaskAwaitingUser, orch_entity.TaskPaused, orch_entity.TaskPending} {
		assert.False(t, (&orch_entity.Task{Status: s}).IsTerminal(), s)
	}
}

func TestTask_IsWaitingUser(t *testing.T) {
	assert.True(t, (&orch_entity.Task{Status: orch_entity.TaskAwaitingUser}).IsWaitingUser())
	assert.False(t, (&orch_entity.Task{Status: orch_entity.TaskAwaitingChildren}).IsWaitingUser())
}
```

- [ ] **Step 6: 跑测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/model/entity/orch_entity/...`
Expected: PASS（全部 ok）。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/model/entity/orch_entity/
git commit -m "✨ orch_entity: Run/Task 充血实体 + 客观生命周期常量"
```

---

### Task 2: 迁移 — orch 表 + `chat_sessions.run_id` + 工具种子

**Files:**
- Create: `migrations/202606240001_orchestration.go`
- Modify: `migrations/migrations.go:38`（`migrationList()` 末尾追加）
- Test: `migrations/202606240001_orchestration_test.go`

**Interfaces:**
- Consumes: `gormigrate.Migration` 模式（见 `migrations/202606160001_group_features.go`）。
- Produces: 表 `orchestration_runs`、`orch_tasks`（+ 索引）、`chat_sessions.run_id` 列、DEFAULT agent `tools_json` 追加 `{"key":"orchestrate","enabled":true}`。

- [ ] **Step 1: 写失败测试** `202606240001_orchestration_test.go`

> 迁移自身的 `*_test.go` 是 CLAUDE.md 允许用真 SQLite 的例外。

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n).Error)
	return n == 1
}

func columnExists(t *testing.T, db *gorm.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Raw(`PRAGMA table_info(` + table + `)`).Rows()
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == col {
			return true
		}
	}
	return false
}

func TestMigration202606240001_Orchestration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	require.True(t, tableExists(t, db, "orchestration_runs"))
	require.True(t, tableExists(t, db, "orch_tasks"))
	require.True(t, columnExists(t, db, "chat_sessions", "run_id"))

	// DEFAULT agent 应被种上 orchestrate 工具。
	var toolsJSON string
	require.NoError(t, db.Raw(
		`SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1`).Scan(&toolsJSON).Error)
	require.Contains(t, toolsJSON, `"orchestrate"`)
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/ -run TestMigration202606240001 -v`
Expected: FAIL（`no such table: orchestration_runs` 或 `migration202606240001` undefined）。

- [ ] **Step 3: 写迁移** `202606240001_orchestration.go`

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240001 编排能力基座：Run/Task 表 + chat_sessions.run_id + orchestrate 工具种子。
func migration202606240001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orchestration_runs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				goal TEXT NOT NULL DEFAULT '',
				leader_agent_id INTEGER NOT NULL DEFAULT 0,
				flow_id INTEGER NOT NULL DEFAULT 0,
				flow_content TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending',
				root_task_id INTEGER NOT NULL DEFAULT 0,
				project_id INTEGER NOT NULL DEFAULT 0,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orchestration_runs_status ON orchestration_runs(status, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orch_tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				run_id INTEGER NOT NULL DEFAULT 0,
				agent_id INTEGER NOT NULL DEFAULT 0,
				session_id INTEGER NOT NULL DEFAULT 0,
				parent_task_id INTEGER NOT NULL DEFAULT 0,
				kind TEXT NOT NULL DEFAULT 'dispatch',
				status TEXT NOT NULL DEFAULT 'pending',
				brief TEXT NOT NULL DEFAULT '',
				result TEXT NOT NULL DEFAULT '',
				call_seq INTEGER NOT NULL DEFAULT 0,
				refs TEXT NOT NULL DEFAULT '',
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, status)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_session ON orch_tasks(session_id)`).Error; err != nil {
				return err
			}
			// 隐藏编排子会话用：chat_sessions.run_id（0=非编排会话）。
			if err := tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN run_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			// DEFAULT agent 默认启用 orchestrate（可当 Leader / 子派）。
			return tx.Exec(`UPDATE agents
				SET tools_json = json_insert(
					CASE WHEN json_valid(tools_json) THEN tools_json ELSE '[]' END,
					'$[#]', json('{"key":"orchestrate","enabled":true}'))
				WHERE system_badge = 'DEFAULT'
				  AND (tools_json IS NULL OR instr(tools_json, '"orchestrate"') = 0)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN run_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS orch_tasks`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS orchestration_runs`).Error
		},
	}
}
```

> 注：SQLite 的 `json_insert(... '$[#]' ...)` 追加数组元素需 SQLite ≥ 3.30；本仓库 `glebarez/sqlite` 满足。若 `tableExists` 测试报 `json_insert` 不可用，退化为 `tx.Exec("UPDATE agents SET tools_json='[...]' WHERE ...")` 全量覆写（参考 `202606160001` 第 2 步对 DEFAULT 的整串覆写）。

- [ ] **Step 4: 注册进 `migrationList()`** — 修改 `migrations/migrations.go:38` 之后追加一行：

```go
		migration202606160002(), // chat_sessions.purpose(隐藏 subagent 委派会话)
		migration202606240001(), // 编排能力基座:Run/Task 表 + chat_sessions.run_id + orchestrate 工具种子
	}
}
```

- [ ] **Step 5: 跑测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/ -run TestMigration202606240001 -v`
Expected: PASS。

- [ ] **Step 6: 全量迁移冒烟（确认未破坏既有迁移链）**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./migrations/...`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add migrations/
git commit -m "✨ migration 202606240001: 编排 Run/Task 表 + chat_sessions.run_id + orchestrate 种子"
```

---

### Task 3: `orch_repo` — RunRepo 与 TaskRepo（sqlmock）

**Files:**
- Create: `internal/repository/orch_repo/run.go`
- Create: `internal/repository/orch_repo/task.go`
- Test: `internal/repository/orch_repo/run_test.go`
- Test: `internal/repository/orch_repo/task_test.go`
- Generated: `internal/repository/orch_repo/mock_orch_repo/`（`make mock`）

**Interfaces:**
- Consumes: `db.Ctx(ctx)`、`testutils.Database(t)`（返回 `(ctx, *sql.DB, sqlmock.Sqlmock)`）、`consts.ACTIVE`。
- Produces:
  - `RunRepo`：`Create(ctx, *OrchestrationRun) error`、`Update(ctx, *OrchestrationRun) error`、`Find(ctx, id int64) (*OrchestrationRun, error)`、`List(ctx) ([]*OrchestrationRun, error)`。
  - `TaskRepo`：`Create`、`Update`、`Find(ctx, id) (*Task, error)`、`FindBySession(ctx, sessionID) (*Task, error)`、`ListByRun(ctx, runID) ([]*Task, error)`、`CountByRunAgent(ctx, runID, agentID) (int64, error)`（算 `CallSeq`）。
  - 访问器：`Run()/RegisterRun()/NewRun()`、`Task()/RegisterTask()/NewTask()`。

- [ ] **Step 1: 写失败测试** `run_test.go`（mirror `internal/repository/workflow_repo/workflow_test.go`）

```go
package orch_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
	"github.com/agentre-ai/agentre/internal/testutils"
)

func TestRunRepo_Create_FillsTimestamps(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .orchestration_runs.`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	r := &orch_entity.OrchestrationRun{Goal: "做个登录页", LeaderAgentID: 2, Status: orch_entity.RunPending}
	require.NoError(t, orch_repo.NewRun().Create(ctx, r))
	assert.NotZero(t, r.Createtime)
	assert.NotZero(t, r.Updatetime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunRepo_Find(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT .* FROM .orchestration_runs.`).
		WithArgs(7, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "goal", "status"}).AddRow(7, "g", "running"))

	got, err := orch_repo.NewRun().Find(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(7), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

> `WithArgs(7, 1)`：gorm 的 `First` 会带 `ORDER BY id LIMIT 1`，sqlmock 参数为 `(id=7, LIMIT=1)`；若实际参数不符，按 `mock.ExpectationsWereMet()` 报的差异调正则/参数。

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/repository/orch_repo/...`
Expected: FAIL（包/类型 undefined）。

- [ ] **Step 3: 写实现** `run.go`（mirror `workflow_repo/workflow.go` 的 Create/Update/Find/List）

```go
// Package orch_repo 编排 Run/Task 仓储。
package orch_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

//go:generate mockgen -source run.go -destination mock_orch_repo/mock_run.go

// RunRepo 编排 Run 仓储。
type RunRepo interface {
	Create(ctx context.Context, r *orch_entity.OrchestrationRun) error
	Update(ctx context.Context, r *orch_entity.OrchestrationRun) error
	Find(ctx context.Context, id int64) (*orch_entity.OrchestrationRun, error)
	List(ctx context.Context) ([]*orch_entity.OrchestrationRun, error)
}

var defaultRun RunRepo

func Run() RunRepo             { return defaultRun }
func RegisterRun(impl RunRepo) { defaultRun = impl }
func NewRun() RunRepo          { return &runRepo{} }

type runRepo struct{}

func (r *runRepo) Create(ctx context.Context, m *orch_entity.OrchestrationRun) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *runRepo) Update(ctx context.Context, m *orch_entity.OrchestrationRun) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *runRepo) Find(ctx context.Context, id int64) (*orch_entity.OrchestrationRun, error) {
	var m orch_entity.OrchestrationRun
	err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *runRepo) List(ctx context.Context) ([]*orch_entity.OrchestrationRun, error) {
	var rows []*orch_entity.OrchestrationRun
	err := db.Ctx(ctx).Order("updatetime DESC").Find(&rows).Error
	return rows, err
}
```

- [ ] **Step 4: 写 `task.go`**

```go
package orch_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

//go:generate mockgen -source task.go -destination mock_orch_repo/mock_task.go

// TaskRepo 编排 Task 仓储。
type TaskRepo interface {
	Create(ctx context.Context, t *orch_entity.Task) error
	Update(ctx context.Context, t *orch_entity.Task) error
	Find(ctx context.Context, id int64) (*orch_entity.Task, error)
	FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Task, error)
	ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error)
	CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error)
}

var defaultTask TaskRepo

func Task() TaskRepo             { return defaultTask }
func RegisterTask(impl TaskRepo) { defaultTask = impl }
func NewTask() TaskRepo          { return &taskRepo{} }

type taskRepo struct{}

func (r *taskRepo) Create(ctx context.Context, m *orch_entity.Task) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *taskRepo) Update(ctx context.Context, m *orch_entity.Task) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *taskRepo) Find(ctx context.Context, id int64) (*orch_entity.Task, error) {
	var m orch_entity.Task
	err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *taskRepo) FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Task, error) {
	var m orch_entity.Task
	err := db.Ctx(ctx).Where("session_id = ?", sessionID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *taskRepo) ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error) {
	var rows []*orch_entity.Task
	err := db.Ctx(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *taskRepo) CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error) {
	var n int64
	err := db.Ctx(ctx).Model(&orch_entity.Task{}).
		Where("run_id = ? AND agent_id = ? AND kind = ?", runID, agentID, orch_entity.TaskKindDispatch).
		Count(&n).Error
	return n, err
}
```

- [ ] **Step 5: 写 `task_test.go`**（FindBySession + CountByRunAgent + ListByRun 各一例，mirror run_test 的 sqlmock 写法）

```go
package orch_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
	"github.com/agentre-ai/agentre/internal/testutils"
)

func TestTaskRepo_FindBySession(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT .* FROM .orch_tasks. WHERE session_id`).
		WithArgs(42, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "status"}).AddRow(3, 42, "running"))

	got, err := orch_repo.NewTask().FindBySession(ctx, 42)
	require.NoError(t, err)
	assert.Equal(t, int64(3), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskRepo_CountByRunAgent(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT count\(\*\) FROM .orch_tasks.`).
		WithArgs(1, 5, orch_entity.TaskKindDispatch).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	n, err := orch_repo.NewTask().CountByRunAgent(ctx, 1, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(2), n)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 6: 生成 mock + 跑测试**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre
go generate ./internal/repository/orch_repo/...
go test ./internal/repository/orch_repo/...
```
Expected: PASS；`mock_orch_repo/` 下生成 `mock_run.go` + `mock_task.go`。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/repository/orch_repo/
git commit -m "✨ orch_repo: RunRepo/TaskRepo + sqlmock 单测 + mock 生成"
```

---

## Phase B — `orch_svc` 核心

### Task 4: `orch_svc` 骨架 + 消费者侧窄接口 + `CreateRun`

**Files:**
- Create: `internal/service/orch_svc/deps.go`
- Create: `internal/service/orch_svc/orch.go`
- Create: `internal/service/orch_svc/create.go`
- Test: `internal/service/orch_svc/create_test.go`
- Generated: `internal/service/orch_svc/mock_orch_svc/`（`make mock`）

**Interfaces:**
- Consumes: `orch_repo.RunRepo`/`orch_repo.TaskRepo`（Task 3）、`agent_entity.Agent`、`chat_svc.ApprovalGateway`（复用，见 `chat_svc/tool_approval.go`）。
- Produces:
  - 窄接口（DIP，consumer 侧定义 → bootstrap 注生产适配器 / 测试注 mock）：
    - `ChatGateway`：`EnsureOrchSession(ctx, in EnsureOrchSessionInput) (sessionID int64, err error)`、`SendAndForget(ctx, sessionID int64, text string) error`、`ObserveTurn(sessionID int64) (<-chan TurnDone, func())`、`FinalAssistantText(ctx, sessionID int64) (string, error)`、`AgentStatus(ctx, sessionID int64) (status string, err error)`。
    - `AgentLookup`：`Find(ctx, id int64) (*agent_entity.Agent, error)`、`FindByName(ctx, name string) (*agent_entity.Agent, error)`、`List(ctx) ([]*agent_entity.Agent, error)`。
    - 值类型 `EnsureOrchSessionInput{AgentID, ParentSessionID, ProjectID, RunID int64; Isolate bool}`、`TurnDone{SessionID int64; OK bool}`。
  - `Default() *orchSvc`、`(s *orchSvc) RegisterDeps(chat ChatGateway, agents AgentLookup, runs orch_repo.RunRepo, tasks orch_repo.TaskRepo, approval chat_svc.ApprovalGateway, emit Emitter)`。
  - `CreateRun(ctx, *CreateRunRequest) (*RunDetail, error)`，`CreateRunRequest{Goal string; LeaderAgentID, FlowID, ProjectID int64; FlowContent string; AllowedAgentIDs []int64}`，`RunDetail{Run *orch_entity.OrchestrationRun; RootTask *orch_entity.Task}`。

> **DIP 说明**：`ChatGateway`/`AgentLookup` 由本包定义（ISP：只列 orch 用到的方法）。生产实现是 bootstrap 里的瘦适配器，把这些方法映射到 `chat_svc.Chat()` 既有方法（`EnsureSession`/`Send`/`ObserveTurn`/`FinalAssistantText` 等，见 subagent_svc/deps.go 同款 ChatGateway）与 `agent_repo.Agent()`。这样 orch_svc 单测不依赖 chat_svc 的内部签名。

- [ ] **Step 1: 写 `deps.go`（接口 + mockgen 指令）**

```go
package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

//go:generate mockgen -source deps.go -destination mock_orch_svc/mock_deps.go

// EnsureOrchSessionInput 创建/复用一条编排会话的入参。
type EnsureOrchSessionInput struct {
	AgentID         int64
	ParentSessionID int64 // 0 = 根（Leader）会话
	ProjectID       int64
	RunID           int64
	Isolate         bool // true = 独立 git worktree
}

// TurnDone 一轮跑完的信号（结果文本另经 FinalAssistantText 取）。
type TurnDone struct {
	SessionID int64
	OK        bool
}

// ChatGateway 编排对 chat_svc 的最小依赖（生产用瘦适配器映射到 chat_svc.Chat()）。
type ChatGateway interface {
	EnsureOrchSession(ctx context.Context, in EnsureOrchSessionInput) (int64, error)
	SendAndForget(ctx context.Context, sessionID int64, text string) error
	ObserveTurn(sessionID int64) (<-chan TurnDone, func())
	FinalAssistantText(ctx context.Context, sessionID int64) (string, error)
	AgentStatus(ctx context.Context, sessionID int64) (string, error)
}

// AgentLookup 花名册查询（复用 agent_repo）。
type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
}

// Emitter 向前端推事件（生产包 wails EventsEmit；测试可 nil）。
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}
```

- [ ] **Step 2: 写 `orch.go`（单例 + RegisterDeps）**（mirror `orgtool_svc/orgtool.go:27-36`）

```go
// Package orch_svc 编排引擎：异步并行多发 + 完成回报续轮，让 agent 自行编排成树。
package orch_svc

import (
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

type orchSvc struct {
	chat     ChatGateway
	agents   AgentLookup
	runs     orch_repo.RunRepo
	tasks    orch_repo.TaskRepo
	approval chat_svc.ApprovalGateway
	emit     Emitter

	approvalTimeout time.Duration

	mcp     *orchMCP
	mcpOnce sync.Once

	schedMu    sync.Mutex
	schedulers map[int64]*scheduler // runID -> scheduler

	askMu    sync.Mutex
	pending  map[string]askEnvelope // ask_id -> 在飞的 ask（Task 10 用 ask_id 显式关联）
	askWaits map[int64]int64        // 死锁检测：askerSession -> targetSession（Task 13 用）
}

var defaultOrch = &orchSvc{
	approvalTimeout: 4 * time.Minute,
	schedulers:      map[int64]*scheduler{},
	pending:         map[string]askEnvelope{},
	askWaits:        map[int64]int64{},
}

// Default 取默认服务单例。
func Default() *orchSvc { return defaultOrch }

// RegisterDeps bootstrap 接线；测试注 mock。
func (s *orchSvc) RegisterDeps(chat ChatGateway, agents AgentLookup, runs orch_repo.RunRepo, tasks orch_repo.TaskRepo, approval chat_svc.ApprovalGateway, emit Emitter) {
	s.chat, s.agents, s.runs, s.tasks, s.approval, s.emit = chat, agents, runs, tasks, approval, emit
}
```

> `askEnvelope` 与 `scheduler` 类型在 Task 9/10 引入；本步先在 `orch.go` 顶部放最小占位 `type askEnvelope struct{}` 与 `type scheduler struct{}` 使包可编译，Task 9/10 用真定义替换（届时删占位）。`askWaits` 字段从一开始就在结构体上（Task 13 才填逻辑），避免后续改结构体。

- [ ] **Step 3: 写失败测试** `create_test.go`（mirror `data_svc/data_svc_test.go:45-104` 的 mock 注入 + Convey）

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestCreateRun_BuildsRunRootSessionAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)

	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "架构师"}, nil)
	// 先建 Run（拿 RunID）→ 建根会话 → 建根 Task → 回填 RootTaskID。
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		So(in.RunID, ShouldEqual, 100)
		So(in.ParentSessionID, ShouldEqual, 0)
		So(in.AgentID, ShouldEqual, 2)
		return 500, nil
	})
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.SessionID, ShouldEqual, 500)
		So(tk.Kind, ShouldEqual, orch_entity.TaskKindDispatch)
		tk.ID = 9
		return nil
	})
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	// 用流程注入触发 Leader 首轮。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("CreateRun 建 Run + 根会话 + 根 Task 并触发 Leader 首轮", t, func() {
		got, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "做个登录页", LeaderAgentID: 2, FlowContent: "先拆分再并行",
		})
		So(err, ShouldBeNil)
		So(got.Run.ID, ShouldEqual, 100)
		So(got.Run.RootTaskID, ShouldEqual, 9)
		So(got.Run.Status, ShouldEqual, orch_entity.RunRunning)
	})
}
```

- [ ] **Step 4: 跑测试看它失败**

Run: `go generate ./internal/service/orch_svc/... && go test ./internal/service/orch_svc/ -run TestCreateRun`
Expected: FAIL（`CreateRun` undefined）。

- [ ] **Step 5: 写 `create.go`**

```go
package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// CreateRunRequest 创建编排 Run 入参。
type CreateRunRequest struct {
	Goal            string
	LeaderAgentID   int64
	FlowID          int64
	FlowContent     string
	ProjectID       int64
	AllowedAgentIDs []int64 // 可选限定可调度团队（空=全部）
}

// RunDetail 创建结果。
type RunDetail struct {
	Run      *orch_entity.OrchestrationRun
	RootTask *orch_entity.Task
}

// CreateRun 建 Run + Leader 根会话 + 根 Task，注入编排流程并触发 Leader 首轮。
func (s *orchSvc) CreateRun(ctx context.Context, req *CreateRunRequest) (*RunDetail, error) {
	leader, err := s.agents.Find(ctx, req.LeaderAgentID)
	if err != nil {
		return nil, err
	}
	if leader == nil {
		return nil, errLeaderNotFound
	}

	run := &orch_entity.OrchestrationRun{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID,
		FlowID: req.FlowID, FlowContent: req.FlowContent,
		ProjectID: req.ProjectID, Status: orch_entity.RunRunning,
	}
	if err := s.runs.Create(ctx, run); err != nil {
		return nil, err
	}

	rootSessionID, err := s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{
		AgentID: req.LeaderAgentID, ParentSessionID: 0, ProjectID: req.ProjectID, RunID: run.ID,
	})
	if err != nil {
		return nil, err
	}

	root := &orch_entity.Task{
		RunID: run.ID, AgentID: req.LeaderAgentID, SessionID: rootSessionID,
		Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning, Brief: req.Goal,
	}
	if err := s.tasks.Create(ctx, root); err != nil {
		return nil, err
	}

	run.RootTaskID = root.ID
	if err := s.runs.Update(ctx, run); err != nil {
		return nil, err
	}

	// 触发 Leader 首轮：把目标作为首条消息送进根会话；编排流程经 BuildTurnExtras 注入 system-prompt（Task 15）。
	if err := s.chat.SendAndForget(ctx, rootSessionID, req.Goal); err != nil {
		logger.Ctx(ctx).Error("orch.CreateRun: 触发 Leader 首轮失败", zap.Int64("session", rootSessionID), zap.Error(err))
		return nil, err
	}

	logger.Ctx(ctx).Info("orch.CreateRun: 已创建编排 Run", zap.Int64("run", run.ID), zap.Int64("leader", req.LeaderAgentID))
	return &RunDetail{Run: run, RootTask: root}, nil
}
```

并在 `orch.go`（或新 `errors.go`）补哨兵错误：

```go
import "errors"
var (
	errLeaderNotFound = errors.New("orch: leader agent not found")
	errAgentNotFound  = errors.New("orch: target agent not found")
	errRunNotActive   = errors.New("orch: run not active")
)
```

- [ ] **Step 6: 跑测试看它通过**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestCreateRun`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/orch_svc/
git commit -m "✨ orch_svc: 骨架 + 窄依赖接口 + CreateRun(Run/根会话/根Task)"
```

---

### Task 5: `agenttool` — 注册 `orchestrate` 工具集

**Files:**
- Modify: `internal/pkg/agenttool/agenttool.go`
- Test: `internal/pkg/agenttool/agenttool_test.go`（若不存在则新建）

**Interfaces:**
- Produces: `KeyOrchestrate = "orchestrate"`；注册表项 `{Key: KeyOrchestrate, MCPPath: "/mcp/orchestrate/", ToolNames: []string{"agent_list", "dispatch", "ask", "send", "finish", "reply"}}`。

- [ ] **Step 1: 写失败测试** `agenttool_test.go`

```go
package agenttool_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

func TestRegistry_HasOrchestrate(t *testing.T) {
	d, ok := agenttool.Lookup(agenttool.KeyOrchestrate)
	assert.True(t, ok)
	assert.Equal(t, "/mcp/orchestrate/", d.MCPPath)
	assert.ElementsMatch(t, []string{"agent_list", "dispatch", "ask", "send", "finish", "reply"}, d.ToolNames)
	assert.Contains(t, agenttool.Keys(), agenttool.KeyOrchestrate)
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/pkg/agenttool/...`
Expected: FAIL（`KeyOrchestrate` undefined）。

- [ ] **Step 3: 改 `agenttool.go`** — 在 `KeySubagent` 常量后加：

```go
// KeyOrchestrate 编排工具集(dispatch/ask/reply/send/finish + agent_list):异步并行多发 + 完成回报续轮。
const KeyOrchestrate = "orchestrate"
```

并在 `registry` 切片末尾追加一项：

```go
	{Key: KeySubagent, MCPPath: "/mcp/subagent/", ToolNames: []string{"agent_list", "agent_call"}},
	{Key: KeyOrchestrate, MCPPath: "/mcp/orchestrate/", ToolNames: []string{"agent_list", "dispatch", "ask", "send", "finish", "reply"}},
}
```

- [ ] **Step 4: 跑测试看它通过 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/pkg/agenttool/...
git add internal/pkg/agenttool/
git commit -m "✨ agenttool: 注册 orchestrate 工具集(dispatch/ask/send/finish)"
```

---

### Task 6: orchestrate MCP handler — token + tool schemas + 路由

**Files:**
- Create: `internal/service/orch_svc/mcp.go`
- Test: `internal/service/orch_svc/mcp_test.go`

**Interfaces:**
- Consumes: `chat_svc` 的 HMAC token / RPC 写法（mirror `subagent_svc/mcp.go`：`MintToken`、`writeRPCResult`/`writeRPCError`、`textResult`）。
- Produces:
  - `(s *orchSvc) MintToken(agentID, sessionID int64) string`、`(s *orchSvc) MCPHandler() http.Handler`（`s.mcpOnce` 懒构）。
  - JSON-RPC 路由：`tools/list` 返回 `orchToolSchemas()`；`tools/call` 按 `dispatch/ask/reply/send/finish/agent_list` 分派到 Task 7/10/11/12 的 svc 方法。token 校验失败 → 403。
  - `orchToolSchemas() []any`（6 个工具的 inputSchema，描述即「编排指引」，CLI 自动呈现给模型）。

- [ ] **Step 1: 写失败测试** `mcp_test.go`（断言 `tools/list` 含 5 工具 + token 校验）

```go
package orch_svc_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

func TestMCP_ToolsList(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	tok := orch_svc.Default().MintToken(2, 500)

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	out := rw.Body.String()
	for _, name := range []string{"dispatch", "ask", "send", "finish", "agent_list", "reply"} {
		assert.Contains(t, out, `"`+name+`"`)
	}
}

func TestMCP_RejectsBadToken(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer garbage")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusForbidden, rw.Code)
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestMCP`
Expected: FAIL（`MCPHandler`/`MintToken` undefined）。

- [ ] **Step 3: 写 `mcp.go`** — 结构 mirror `subagent_svc/mcp.go`（token mint/verify、`http.ServeMux`、`tools/list`+`tools/call` 分派）。关键骨架：

```go
package orch_svc

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

type orchMCP struct {
	svc    *orchSvc
	secret []byte
}

type orchRef struct {
	agentID   int64
	sessionID int64
}

// MintToken 绑定 (agentID, sessionID) 的 HMAC token（重启后稳定，mirror subagent_svc.MintToken）。
func (s *orchSvc) MintToken(agentID, sessionID int64) string {
	m := s.ensureMCP()
	payload := fmt.Sprintf("%d:%d", agentID, sessionID)
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func (m *orchMCP) verify(tok string) (orchRef, bool) {
	parts := strings.SplitN(tok, ".", 2)
	if len(parts) != 2 {
		return orchRef{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return orchRef{}, false
	}
	mac := hmac.New(sha256.New, m.secret)
	mac.Write(raw)
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return orchRef{}, false
	}
	var a, sess int64
	if _, err := fmt.Sscanf(string(raw), "%d:%d", &a, &sess); err != nil {
		return orchRef{}, false
	}
	_ = strconv.Itoa
	return orchRef{agentID: a, sessionID: sess}, true
}

func (s *orchSvc) ensureMCP() *orchMCP {
	s.mcpOnce.Do(func() {
		s.mcp = &orchMCP{svc: s, secret: randomSecret()} // randomSecret: 进程级随机，mirror subagent
	})
	return s.mcp
}

// MCPHandler 返回挂到 gateway /mcp/orchestrate/ 的 handler。
func (s *orchSvc) MCPHandler() http.Handler {
	m := s.ensureMCP()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		ref, ok := m.verify(tok)
		if !ok {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var rpc struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&rpc)
		switch rpc.Method {
		case "tools/list":
			writeRPCResult(w, rpc.ID, map[string]any{"tools": orchToolSchemas()})
		case "tools/call":
			m.dispatchTool(w, r, rpc.ID, ref, rpc.Params.Name, rpc.Params.Arguments)
		default:
			writeRPCError(w, rpc.ID, -32601, "method not found")
		}
	})
}
```

> `writeRPCResult`/`writeRPCError`/`textResult`/`randomSecret` 与 subagent_svc 同源；**不要跨包 import 私有函数**——复制一份到 orch_svc（4 个小工具函数），或把它们提进 `internal/pkg/mcprpc`（若做提取，单独成一个 in-scope commit 并同时改 subagent_svc 引用）。默认选「复制一份」以最小化 plan-1 触达面。
>
> **transport 注意（codex spike 实测）**：真 codex 的 streamable-http 客户端除 `POST`(tools/call) 外还会发 `GET`(SSE 流) 与 `DELETE`(会话回收)。orch 的 handler/gateway 挂载必须像 subagent/group 那样支持完整 streamable-http 生命周期（GET/DELETE/会话）——直接复用 subagent 同款 handler 形状即可（subagent 今天就能跑 codex）；**别只处理 POST**，否则 codex 连接降级（虽能 POST-only 调用,但不稳）。

`dispatchTool` 分派（占位到后续任务的 svc 方法；本步先实现 `agent_list`，其余返回 “not implemented” 占位，让 `tools/list` 测试先绿）：

```go
func (m *orchMCP) dispatchTool(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, name string, args json.RawMessage) {
	switch name {
	case "agent_list":
		m.handleAgentList(w, r, id) // mirror subagent_svc.handleAgentList
	case "dispatch", "ask", "send", "finish", "reply":
		writeRPCError(w, id, -32000, "not implemented") // Task 7/10/11/12 替换(ask+reply 见 Task 10)
	default:
		writeRPCError(w, id, -32601, "unknown tool")
	}
}
```

`orchToolSchemas()`（描述即编排指引）：

```go
func orchToolSchemas() []any {
	return []any{
		map[string]any{"name": "agent_list", "description": "列出可调度的全部 agent(id/名称/描述/能力)。据此拆活、按名 dispatch。", "inputSchema": map[string]any{"type": "object", "properties": map[string]any{}}},
		map[string]any{"name": "dispatch", "description": "把一个子任务异步派给某 agent(按名),新起一条子会话并行跑;它完成后结果会自动回报给你、触发你下一轮决策。可对同一 agent 多次 dispatch(即多个子任务)。审核/测试/合并都是 dispatch 给相应 agent。", "inputSchema": map[string]any{"type": "object", "required": []string{"agent", "brief"}, "properties": map[string]any{
			"agent":   map[string]any{"type": "string", "description": "目标 agent 名(取自 agent_list)"},
			"brief":   map[string]any{"type": "string", "description": "任务说明 + 验证目标;需要的产物引用写进来"},
			"isolate": map[string]any{"type": "boolean", "description": "true=独立 git worktree 隔离;默认 false 共享工作区"},
		}}},
		map[string]any{"name": "ask", "description": "向另一个 agent 提问并等待其回复;问题会被注入对方的活会话(它带着自己的上下文回答),答案回报你。用于咨询同伴上下文里才有的信息,不新增任务节点。", "inputSchema": map[string]any{"type": "object", "required": []string{"agent", "question"}, "properties": map[string]any{
			"agent":    map[string]any{"type": "string"},
			"question": map[string]any{"type": "string"},
		}}},
		map[string]any{"name": "reply", "description": "回复别人通过 ask 向你提出的问题。必须原样带上提问消息里给你的 ask_id,否则提问方收不到。", "inputSchema": map[string]any{"type": "object", "required": []string{"ask_id", "answer"}, "properties": map[string]any{
			"ask_id": map[string]any{"type": "string", "description": "提问消息里给出的 ask_id,原样带回"},
			"answer": map[string]any{"type": "string", "description": "你的回答"},
		}}},
		map[string]any{"name": "send", "description": "往你派发的某任务会话里发后续/返工反馈(同会话续做),更新后的结果再回报你。返工=对原任务再 send,不新增节点。", "inputSchema": map[string]any{"type": "object", "required": []string{"task_id", "message"}, "properties": map[string]any{
			"task_id": map[string]any{"type": "integer"},
			"message": map[string]any{"type": "string"},
		}}},
		map[string]any{"name": "finish", "description": "收口当前任务/Run,向上回报一份结构化小结。你是根(Leader)时 finish = 整个 Run 完成。无次数上限,自行判断何时收口。", "inputSchema": map[string]any{"type": "object", "required": []string{"summary"}, "properties": map[string]any{
			"summary": map[string]any{"type": "string"},
		}}},
	}
}
```

- [ ] **Step 4: 跑测试看它通过 + Commit**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestMCP`
Expected: PASS。

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/orch_svc/
git commit -m "✨ orch_svc: orchestrate MCP handler(token + 5 工具 schema + 路由骨架)"
```

---

### Task 7: `dispatch` — 异步派发（新会话 + Task，非阻塞）

**Files:**
- Create: `internal/service/orch_svc/dispatch.go`
- Modify: `internal/service/orch_svc/mcp.go`（`dispatchTool` 接 `dispatch` 分支）
- Test: `internal/service/orch_svc/dispatch_test.go`

**Interfaces:**
- Consumes: `ChatGateway.EnsureOrchSession`/`SendAndForget`、`AgentLookup.FindByName`、`orch_repo.TaskRepo.{Create,Find,CountByRunAgent,Update}`、`scheduler`（Task 9，本任务先直跑、Task 9 再接调度器）。
- Produces: `Dispatch(ctx, parentSessionID int64, agentName, brief string, isolate bool) (taskID int64, err error)`。语义：**一次性调用-返回**，立即返回 `taskID`，子任务后台跑；不阻塞调用方那一轮。

- [ ] **Step 1: 写失败测试** `dispatch_test.go`

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestDispatch_SpawnsChildSessionAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	// 解析派发者会话 → 找到其 Task（拿 RunID）。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		So(in.ParentSessionID, ShouldEqual, 500)
		So(in.RunID, ShouldEqual, 100)
		So(in.AgentID, ShouldEqual, 3)
		So(in.Isolate, ShouldBeTrue)
		return 600, nil
	})
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ParentTaskID, ShouldEqual, 9)
		So(tk.SessionID, ShouldEqual, 600)
		So(tk.CallSeq, ShouldEqual, 1)
		So(tk.Status, ShouldEqual, orch_entity.TaskRunning)
		tk.ID = 11
		return nil
	})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(600), "实现登录表单").Return(nil)
	chat.EXPECT().ObserveTurn(int64(600)).Return(make(chan orch_svc.TurnDone), func() {}).AnyTimes()

	Convey("dispatch 异步起子会话 + Task 并立刻返回 taskID", t, func() {
		id, err := orch_svc.Default().Dispatch(context.Background(), 500, "李", "实现登录表单", true)
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 11)
	})
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test ./internal/service/orch_svc/ -run TestDispatch`
Expected: FAIL（`Dispatch` undefined）。

- [ ] **Step 3: 写 `dispatch.go`**

```go
package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// Dispatch 把子任务异步派给某 agent：建子会话 + Task，触发其首轮，立即返回 taskID。
// 子任务完成由 watchCompletion(Task 8) 回报派发者。
func (s *orchSvc) Dispatch(ctx context.Context, parentSessionID int64, agentName, brief string, isolate bool) (int64, error) {
	parent, err := s.tasks.FindBySession(ctx, parentSessionID)
	if err != nil {
		return 0, err
	}
	if parent == nil {
		return 0, errRunNotActive
	}
	target, err := s.agents.FindByName(ctx, agentName)
	if err != nil {
		return 0, err
	}
	if target == nil {
		return 0, errAgentNotFound
	}
	n, err := s.tasks.CountByRunAgent(ctx, parent.RunID, target.ID)
	if err != nil {
		return 0, err
	}
	childSession, err := s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{
		AgentID: target.ID, ParentSessionID: parentSessionID, RunID: parent.RunID, Isolate: isolate,
	})
	if err != nil {
		return 0, err
	}
	child := &orch_entity.Task{
		RunID: parent.RunID, AgentID: target.ID, SessionID: childSession,
		ParentTaskID: parent.ID, Kind: orch_entity.TaskKindDispatch,
		Status: orch_entity.TaskRunning, Brief: brief, CallSeq: int(n) + 1,
	}
	if err := s.tasks.Create(ctx, child); err != nil {
		return 0, err
	}
	// 派发者进入「等子任务」（Task 8 在子回报时改回 running）。
	parent.Status = orch_entity.TaskAwaitingChildren
	_ = s.tasks.Update(ctx, parent)

	// 经调度器并发执行（Task 9 接管）；本步先直接触发首轮 + 挂完成监听。
	s.enqueueRun(parent.RunID, child, brief)

	logger.Ctx(ctx).Info("orch.Dispatch: 已派发子任务",
		zap.Int64("run", parent.RunID), zap.Int64("task", child.ID),
		zap.Int64("agent", target.ID), zap.Int("callSeq", child.CallSeq))
	return child.ID, nil
}
```

> 本步 `enqueueRun` 先实现为「直接 `SendAndForget` + `go s.watchCompletion(...)`」的极简版，使 dispatch 测试可绿；Task 9 把它替换成真正经 `scheduler` 限流的版本。临时极简实现：

```go
// enqueueRun 触发子任务首轮并挂完成监听（Task 9 用 scheduler 替换为限流版）。
func (s *orchSvc) enqueueRun(runID int64, task *orch_entity.Task, brief string) {
	go func() {
		ctx := context.Background()
		if err := s.chat.SendAndForget(ctx, task.SessionID, brief); err != nil {
			logger.Ctx(ctx).Error("orch.enqueueRun: 触发子任务首轮失败", zap.Int64("task", task.ID), zap.Error(err))
			return
		}
		s.watchCompletion(ctx, task) // Task 8
	}()
}
```

> 注：测试里 `ObserveTurn` 用 `.AnyTimes()` 容许 goroutine 异步触达；`SendAndForget` 在 goroutine 内被调用，gomock 的 `EXPECT().SendAndForget(...600...)` 配 `ctrl.Finish` 于 `t.Cleanup` 等待。若出现竞态，给该期望加 `.MinTimes(0)` 或在测试结尾 `time.Sleep` 一个极短 tick——更稳的做法是 Task 9 把触发改成同步可注入。**首选**：dispatch 单测只断言「建会话 + Task + 返回 id」，把首轮触发的断言移到 Task 8/9（见 Step 5 备注），即本测试删掉 `SendAndForget`/`ObserveTurn` 两个 EXPECT，dispatch 实现里 `enqueueRun` 用一个可在测试关闭的 no-op 替身。按此**简化版**落地以避免异步 flake。

- [ ] **Step 4: 接 MCP 分支** — `mcp.go` 的 `dispatchTool` 把 `case "dispatch"` 改为解析参数后调 `m.svc.Dispatch(r.Context(), ref.sessionID, args.Agent, args.Brief, args.Isolate)`，返回 `textResult(fmt.Sprintf("已派发,task_id=%d", id))`。

- [ ] **Step 5: 跑测试看它通过 + Commit**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestDispatch`
Expected: PASS。

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/orch_svc/
git commit -m "✨ orch_svc: dispatch 异步派发(子会话+Task,非阻塞)"
```

---

### Task 8: 完成回报续轮（核心唯一新机制）

**Files:**
- Create: `internal/service/orch_svc/complete.go`
- Test: `internal/service/orch_svc/complete_test.go`

**Interfaces:**
- Consumes: `ChatGateway.ObserveTurn`/`FinalAssistantText`/`AgentStatus`/`SendAndForget`、`TaskRepo.{Update,Find}`。
- Produces:
  - `watchCompletion(ctx, task *orch_entity.Task)`：订阅子会话轮次；轮结束且会话真实空闲（`AgentStatus=="idle"`、无未决）→ 取 `FinalAssistantText` 作 `Result`、标 `done`、**把报告作为一条续轮消息注入派发者会话并触发其下一轮**。
  - `reportToParent(ctx, parentTaskID int64, childTask *orch_entity.Task, report string)`：构造「子任务 #N(agent X)已完成,报告:…」消息，`SendAndForget` 进父会话；若父处于 `awaiting-children` 且无其它未决子任务 → 改回 `running`。
  - `markTaskError(ctx, task, reason)`：技术崩溃路径——标 `error`，把「运行时崩溃」当回报上抛父会话（与 done 同一条续轮路，spec §4）。

> **这是 spec §3.2「完成回报续轮」+「核心唯一新机制」**。复用 chat_svc「唤起一条会话再跑一轮」的能力（与后台任务自主续轮 `AutonomousTurnSource` 同思路）：父会话被一条新消息唤醒 → 自然跑「决策轮」。

- [ ] **Step 1: 写失败测试** `complete_test.go`

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestWatchCompletion_ReportsToParentAndMarksDone(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().ObserveTurn(int64(600)).Return((<-chan orch_svc.TurnDone)(turnCh), func() {})
	chat.EXPECT().AgentStatus(gomock.Any(), int64(600)).Return("idle", nil)
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(600)).Return("登录表单已实现,见 src/login.tsx", nil)

	child := &orch_entity.Task{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentTaskID: 9, CallSeq: 1, Status: orch_entity.TaskRunning}
	// 子任务标 done + 写 Result。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		if tk.ID == 11 {
			So(tk.Status, ShouldEqual, orch_entity.TaskDone)
			So(tk.Result, ShouldContainSubstring, "登录表单已实现")
		}
		return nil
	}).AnyTimes()
	// 取父任务(用于唤醒 + 状态翻回)。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskAwaitingChildren}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 11, ParentTaskID: 9, Status: orch_entity.TaskDone}}, nil)
	// 报告注入父会话，唤醒决策轮。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		So(msg, ShouldContainSubstring, "登录表单已实现")
		return nil
	})

	Convey("子任务完成 → 标 done + 报告回报父会话续轮", t, func() {
		done := make(chan struct{})
		go func() { orch_svc.Default().WatchCompletionForTest(context.Background(), child); close(done) }()
		turnCh <- orch_svc.TurnDone{SessionID: 600, OK: true}
		close(turnCh)
		<-done
	})
}
```

> 测试钩子：在 `complete.go` 暴露 `func (s *orchSvc) WatchCompletionForTest(ctx, t *orch_entity.Task) { s.watchCompletion(ctx, t) }`（仅测试用导出包装；或把测试放进 `package orch_svc` 内部测直接调私有 `watchCompletion`——**首选内部测**，删掉这个导出包装）。

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestWatchCompletion`
Expected: FAIL（`watchCompletion`/`WatchCompletionForTest` undefined）。

- [ ] **Step 3: 写 `complete.go`**

```go
package orch_svc

import (
	"context"
	"fmt"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// watchCompletion 订阅子会话轮次;轮结束且会话真实空闲 → 标 done + 报告回报派发者续轮。
func (s *orchSvc) watchCompletion(ctx context.Context, task *orch_entity.Task) {
	ch, cancel := s.chat.ObserveTurn(task.SessionID)
	defer cancel()
	for td := range ch {
		if td.SessionID != task.SessionID {
			continue
		}
		status, err := s.chat.AgentStatus(ctx, task.SessionID)
		if err != nil {
			logger.Ctx(ctx).Error("orch.watchCompletion: 取会话状态失败", zap.Int64("session", task.SessionID), zap.Error(err))
			continue
		}
		switch status {
		case "idle":
			// 本轮结束 + 无未决 → done(完成态从真实空闲推导,杜绝卡 running)。
			text, _ := s.chat.FinalAssistantText(ctx, task.SessionID)
			task.Status = orch_entity.TaskDone
			task.Result = text
			_ = s.tasks.Update(ctx, task)
			s.reportToParent(ctx, task.ParentTaskID, task, text)
			s.onTaskSettled(task.RunID) // Task 9：释放调度槽
			return
		case "error":
			s.markTaskError(ctx, task, "运行时崩溃")
			s.onTaskSettled(task.RunID)
			return
		default:
			// running/waiting：还有未决事项(子任务/ask/审批)，继续等下一轮。
			continue
		}
	}
}

// reportToParent 把子任务报告作为续轮消息注入派发者会话,并在无其它未决子任务时把父翻回 running。
func (s *orchSvc) reportToParent(ctx context.Context, parentTaskID int64, child *orch_entity.Task, report string) {
	if parentTaskID == 0 {
		return // 根任务无父：根的收口只认 Leader 显式 finish(Task 12)
	}
	parent, err := s.tasks.Find(ctx, parentTaskID)
	if err != nil || parent == nil {
		return
	}
	if s.allChildrenSettled(ctx, parent) && parent.Status == orch_entity.TaskAwaitingChildren {
		parent.Status = orch_entity.TaskRunning
		_ = s.tasks.Update(ctx, parent)
	}
	msg := fmt.Sprintf("【子任务 #%d 完成 · agent#%d】\n%s", child.ID, child.AgentID, report)
	if err := s.chat.SendAndForget(ctx, parent.SessionID, msg); err != nil {
		logger.Ctx(ctx).Error("orch.reportToParent: 续轮注入失败", zap.Int64("parent", parent.SessionID), zap.Error(err))
	}
}

// markTaskError 技术崩溃:标 error 并把崩溃当回报上抛父会话(与 done 同一条续轮路, spec §4)。
func (s *orchSvc) markTaskError(ctx context.Context, task *orch_entity.Task, reason string) {
	task.Status = orch_entity.TaskError
	task.Result = reason
	_ = s.tasks.Update(ctx, task)
	s.reportToParent(ctx, task.ParentTaskID, task, "技术中断："+reason+"（请决定重试/换 agent/放弃该分支）")
}

// allChildrenSettled 该任务的全部 dispatch 子任务是否都到终态。
func (s *orchSvc) allChildrenSettled(ctx context.Context, parent *orch_entity.Task) bool {
	rows, err := s.tasks.ListByRun(ctx, parent.RunID)
	if err != nil {
		return false
	}
	for _, t := range rows {
		if t.ParentTaskID == parent.ID && t.Kind == orch_entity.TaskKindDispatch && !t.IsTerminal() {
			return false
		}
	}
	return true
}
```

> `onTaskSettled` 在 Task 9 定义（释放调度槽）；本步先加空实现 `func (s *orchSvc) onTaskSettled(runID int64) {}`，Task 9 替换。

- [ ] **Step 4: 跑测试看它通过 + Commit**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestWatchCompletion`
Expected: PASS。

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/service/orch_svc/
git commit -m "✨ orch_svc: 完成回报续轮(核心新机制)+技术崩溃上抛"
```

---

### Task 9: 并发调度器（lift 自 group 的 pending/inflight）

**Files:**
- Create: `internal/service/orch_svc/scheduler.go`
- Modify: `internal/service/orch_svc/dispatch.go`（`enqueueRun` 改走调度器）、`complete.go`（`onTaskSettled` 释放槽 + kick）
- Test: `internal/service/orch_svc/scheduler_test.go`

**Interfaces:**
- Consumes: 无新外部依赖。
- Produces:
  - `scheduler{ mu sync.Mutex; pending []*orch_entity.Task; inflight int; cap int }`（per-Run；mirror `group_svc/scheduler.go`）。
  - `(s *orchSvc) schedulerFor(runID int64) *scheduler`（懒建，cap=`min(16, runtime.NumCPU())`）。
  - `(s *orchSvc) enqueueRun(runID int64, task *orch_entity.Task, brief string)`（入队 + `kick`）。
  - `(s *orchSvc) kick(runID int64)`（填满空闲槽：每个槽 `SendAndForget(brief)` + `go watchCompletion`）。
  - `(s *orchSvc) onTaskSettled(runID int64)`（`inflight--` + `kick`）。

> spec §3.2：「并发上限 N + 排队；这不是工作量上限——所有任务都会跑，只是不同时跑爆机器」。

- [ ] **Step 1: 写失败测试** `scheduler_test.go`（断言：3 个任务、cap=2 → 同时只有 2 个被 `SendAndForget`，第 3 个在某个完成后才触发）

```go
package orch_svc_test

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestScheduler_CapsConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().SetSchedulerCapForTest(2)

	var mu sync.Mutex
	started := 0
	chat.EXPECT().SendAndForget(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, _ string) error {
		mu.Lock(); started++; mu.Unlock(); return nil
	}).AnyTimes()
	// 完成监听永不返回(模拟在跑)，由测试手动 settle。
	never := make(chan orch_svc.TurnDone)
	chat.EXPECT().ObserveTurn(gomock.Any()).Return((<-chan orch_svc.TurnDone)(never), func() {}).AnyTimes()
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	Convey("cap=2 时第三个任务排队", t, func() {
		for i := 1; i <= 3; i++ {
			orch_svc.Default().EnqueueRunForTest(100, &orch_entity.Task{ID: int64(i), RunID: 100, SessionID: int64(600 + i)}, "go")
		}
		time.Sleep(20 * time.Millisecond)
		mu.Lock(); So(started, ShouldEqual, 2); mu.Unlock()

		orch_svc.Default().OnTaskSettledForTest(100) // 释放一个槽
		time.Sleep(20 * time.Millisecond)
		mu.Lock(); So(started, ShouldEqual, 3); mu.Unlock()
	})
}
```

> 测试钩子 `SetSchedulerCapForTest`/`EnqueueRunForTest`/`OnTaskSettledForTest` 仅测试导出（或内部测直调私有）。**首选内部测**直接调私有方法，删掉这些导出包装。

- [ ] **Step 2: 跑测试看它失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && go test ./internal/service/orch_svc/ -run TestScheduler`
Expected: FAIL。

- [ ] **Step 3: 写 `scheduler.go`** + 替换 `enqueueRun`/`onTaskSettled`

```go
package orch_svc

import (
	"context"
	"runtime"
	"sync"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

type queued struct {
	task  *orch_entity.Task
	brief string
}

type scheduler struct {
	mu       sync.Mutex
	pending  []queued
	inflight int
	cap      int
}

func (s *orchSvc) schedulerFor(runID int64) *scheduler {
	s.schedMu.Lock()
	defer s.schedMu.Unlock()
	sc := s.schedulers[runID]
	if sc == nil {
		c := runtime.NumCPU()
		if c > 16 {
			c = 16
		}
		if c < 1 {
			c = 1
		}
		sc = &scheduler{cap: c}
		s.schedulers[runID] = sc
	}
	return sc
}

func (s *orchSvc) enqueueRun(runID int64, task *orch_entity.Task, brief string) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	sc.pending = append(sc.pending, queued{task: task, brief: brief})
	sc.mu.Unlock()
	s.kick(runID)
}

func (s *orchSvc) kick(runID int64) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	var launch []queued
	for sc.inflight < sc.cap && len(sc.pending) > 0 {
		q := sc.pending[0]
		sc.pending = sc.pending[1:]
		sc.inflight++
		launch = append(launch, q)
	}
	sc.mu.Unlock()
	for _, q := range launch { // 锁外启动，避免 Send 阻塞其它任务
		go func(q queued) {
			ctx := context.Background()
			if err := s.chat.SendAndForget(ctx, q.task.SessionID, q.brief); err != nil {
				s.onTaskSettled(runID)
				return
			}
			s.watchCompletion(ctx, q.task)
		}(q)
	}
}

func (s *orchSvc) onTaskSettled(runID int64) {
	sc := s.schedulerFor(runID)
	sc.mu.Lock()
	if sc.inflight > 0 {
		sc.inflight--
	}
	sc.mu.Unlock()
	s.kick(runID)
}
```

> 删除 Task 7/8 里的临时 `enqueueRun`/空 `onTaskSettled`。`orch.go` 里把占位 `type scheduler struct{}` 删掉（已被真定义取代）。

- [ ] **Step 4: 跑测试看它通过 + 全包回归 + Commit**

Run:
```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race ./internal/service/orch_svc/...
```
Expected: PASS（含 dispatch/complete 既有测试）。

```bash
git add internal/service/orch_svc/
git commit -m "✨ orch_svc: 并发调度器(pending/inflight + cap=min(16,核数))"
```

---

### Task 10: `ask` / `reply` — 注入活会话 + 显式 `ask_id` 关联

**Files:**
- Create: `internal/service/orch_svc/ask.go`
- Modify: `internal/service/orch_svc/mcp.go`（`ask`/`reply` 分支）、`orch.go`（`pending` 改 `ask_id` 键 + `askEnvelope` 真定义）
- Test: `internal/service/orch_svc/ask_test.go`

**设计（已用两个真 claude 实测,见附录 A）**：`ask` **不靠"收下一轮"猜答案**——那会把目标会话里别的轮（子回报/用户插话）误当答案,也会丢上下文。改为**显式关联**：
1. `ask(agent, question)` 生成 `ask_id`,把问题**注入目标 agent 的活会话**（带着它自己的上下文）并**阻塞**等回复；
2. 目标 agent 在它那一轮里调 `reply(ask_id, answer)` 回复；
3. `reply` handler 按 `ask_id` 匹配 → 解开提问方阻塞。
→ 关联是显式 token（`ask_id`）、不做时序/消息推断；目标用**自己的活会话上下文**作答（不 fork、不丢上下文）。

**Interfaces:**
- Consumes: `AgentLookup.FindByName`、`ChatGateway.{EnsureOrchSession,SendAndForget}`、`TaskRepo.{FindBySession,ListByRun}`、`uuid.NewString`。
- Produces:
  - `askEnvelope{ askID string; askerSession, targetAgentID, targetSession int64; reply chan string }`。
  - `pending map[string]askEnvelope`（`ask_id → 在飞的 ask`；**替换** Task 4 占位的 `map[int64][]askEnvelope`）。
  - `Ask(ctx, fromSessionID int64, agentName, question string) (answer string, err error)`：mint `ask_id` → 解析/新建目标活会话 → 注入带 `ask_id` 的问题 → 阻塞 ≤4 分钟。
  - `Reply(ctx, replierAgentID int64, askID, answer string) error`：按 `ask_id` 找 pending,**校验 `replierAgentID == 该 ask 的 targetAgentID`**（防别的 agent 串答）,回送答案。
  - 死锁等待边 `recordAskWait`/`clearAskWait`（`askWaits[askerSession]=targetSession`，供 Task 13）。

> spec §5 并发：`ask` 阻塞的是 A 发起那一轮（A 的 CLI 轮冻结在该工具调用上）；A 先前 dispatch 的子回报在 A 这一轮结束（ask 返回）后再续轮唤醒它——不会丢，只是排在 ask 之后。

- [ ] **Step 1: 写失败测试** `ask_test.go`（happy path：ask 注入活会话 → 目标调 reply(ask_id) → 提问方拿到回答）

```go
package orch_svc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

// parseAskID 从注入消息里抽出 ask_id(消息形如 "【收到提问 ask_id=<id>】...")。
func parseAskID(msg string) string {
	const k = "ask_id="
	i := strings.Index(msg, k)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(k):]
	if j := strings.IndexAny(rest, "】\" "); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestAsk_InjectLiveSessionThenReplyResolves(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	// 王 在该 Run 已有「活会话」700 → 问题注入 700(保留王的上下文)。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil)
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		injCh <- msg
		return nil
	})

	Convey("ask 注入活会话, 目标用 reply(ask_id) 解开阻塞", t, func() {
		done := make(chan string, 1)
		go func() {
			ans, _ := orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权用什么?")
			done <- ans
		}()
		askID := parseAskID(<-injCh) // 拿到注入消息里的 ask_id(模拟王读到它)
		So(askID, ShouldNotBeBlank)
		// 王(agentID=1)按 ask_id 回复。
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "用 session+cookie"), ShouldBeNil)
		select {
		case ans := <-done:
			So(ans, ShouldEqual, "用 session+cookie")
		case <-time.After(time.Second):
			t.Fatal("ask 未在超时内返回")
		}
	})
}

func TestReply_RejectsForeignReplier(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil)
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error { injCh <- m; return nil })

	Convey("非接收者(agentID=2)不能 reply 别人的 ask", t, func() {
		go func() { _, _ = orch_svc.Default().Ask(context.Background(), 500, "王", "q") }()
		askID := parseAskID(<-injCh)
		So(orch_svc.Default().Reply(context.Background(), 2, askID, "乱答"), ShouldNotBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败** → `go test ./internal/service/orch_svc/ -run "TestAsk|TestReply"`（FAIL：`Ask`/`Reply` undefined）。

- [ ] **Step 3: 写 `ask.go`**（注入活会话 + 显式 `ask_id` 关联；无轮次/时序推断）

```go
package orch_svc

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	errUnknownAsk      = errors.New("orch: 未知或已过期的 ask_id")
	errReplyForeignAsk = errors.New("orch: 你不是该提问的接收者")
)

// Ask 向另一个 agent 提问:把带 ask_id 的问题注入对方活会话(保留其上下文),阻塞等其 reply(≤4 分钟)。
func (s *orchSvc) Ask(ctx context.Context, fromSessionID int64, agentName, question string) (string, error) {
	from, err := s.tasks.FindBySession(ctx, fromSessionID)
	if err != nil || from == nil {
		return "", errRunNotActive
	}
	target, err := s.agents.FindByName(ctx, agentName)
	if err != nil || target == nil {
		return "", errAgentNotFound
	}
	toSession, err := s.resolveOrCreateAgentSession(ctx, from.RunID, target.ID)
	if err != nil {
		return "", err
	}
	askID := uuid.NewString()
	env := askEnvelope{askID: askID, askerSession: fromSessionID, targetAgentID: target.ID, targetSession: toSession, reply: make(chan string, 1)}

	s.askMu.Lock()
	s.pending[askID] = env
	s.askMu.Unlock()
	s.recordAskWait(fromSessionID, toSession) // 死锁检测边(Task 13)
	defer func() {
		s.askMu.Lock()
		delete(s.pending, askID)
		s.askMu.Unlock()
		s.clearAskWait(fromSessionID)
	}()

	// 注入对方活会话:它带着自己的上下文回答,并被告知用 ask_id 调 reply。
	msg := fmt.Sprintf("【收到提问 ask_id=%s】%s\n请根据你自己的上下文,调用 reply(ask_id=\"%s\", answer=...) 回复。", askID, question, askID)
	if err := s.chat.SendAndForget(ctx, toSession, msg); err != nil {
		return "", err
	}

	select {
	case ans := <-env.reply:
		return ans, nil
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timeAfter(s.approvalTimeout):
		return "", fmt.Errorf("orch.Ask: 等待 %s 回复超时", agentName)
	}
}

// Reply 目标 agent 用 ask_id 回复;校验回复者就是被提问者(防别的 agent 串答)。
func (s *orchSvc) Reply(_ context.Context, replierAgentID int64, askID, answer string) error {
	s.askMu.Lock()
	env, ok := s.pending[askID]
	s.askMu.Unlock()
	if !ok {
		return errUnknownAsk
	}
	if env.targetAgentID != replierAgentID {
		return errReplyForeignAsk
	}
	env.reply <- answer // 缓冲(1),非阻塞;Ask 侧 select 接走
	return nil
}

// resolveOrCreateAgentSession 取目标 agent 在 Run 内的活会话(带上下文);没有则新建一条会话。
func (s *orchSvc) resolveOrCreateAgentSession(ctx context.Context, runID, agentID int64) (int64, error) {
	rows, err := s.tasks.ListByRun(ctx, runID)
	if err != nil {
		return 0, err
	}
	var fallback int64
	for _, t := range rows {
		if t.AgentID != agentID {
			continue
		}
		if !t.IsTerminal() {
			return t.SessionID, nil // 优先活会话(上下文最全)
		}
		fallback = t.SessionID
	}
	if fallback != 0 {
		return fallback, nil // 退而求其次:该 agent 在本 Run 的历史会话(仍带上下文)
	}
	// 该 agent 在本 Run 还没有会话 → 建一条(无前置任务上下文,只能据 persona + 问题答)。
	return s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{AgentID: agentID, RunID: runID})
}

// recordAskWait/clearAskWait 维护 ask 等待边(死锁检测用，Task 13 读 askWaits)。
func (s *orchSvc) recordAskWait(from, to int64) { s.askMu.Lock(); s.askWaits[from] = to; s.askMu.Unlock() }
func (s *orchSvc) clearAskWait(from int64)      { s.askMu.Lock(); delete(s.askWaits, from); s.askMu.Unlock() }

// timeAfter 包装 time.After，便于测试替身(避免真等 4 分钟)。
var timeAfter = time.After
```

并在 `orch.go` 用真定义替换占位 `askEnvelope`（注意 `pending` 改为 `map[string]askEnvelope`，见下方 Task 4 结构体修订）：

```go
type askEnvelope struct {
	askID         string
	askerSession  int64
	targetAgentID int64
	targetSession int64
	reply         chan string
}
```

> 关联完全靠 `ask_id`：`Reply` 按 id 命中 pending 并校验回复者身份,**不依赖"哪一轮结束"**——目标会话里别的轮（它自己的任务续轮/用户插话）一律不会被误当答案。这正是为何不用"收下一轮 + FinalAssistantText"。
>
> **Task 13 注意**：`recordAskWait`/`clearAskWait`/`askWaits` 已在本任务落地——Task 13 不要重复定义,只新增 `detectAskCycle` 并在 `Ask` 入队后调用它。

- [ ] **Step 4: 接 MCP `ask` + `reply` 分支 + 跑测试 + Commit**

`mcp.go` 的 `dispatchTool`：
- `case "ask"`：解析 `{agent,question}` → `ans, err := m.svc.Ask(r.Context(), ref.sessionID, args.Agent, args.Question)` → `textResult(ans)`。
- `case "reply"`：解析 `{ask_id,answer}` → `err := m.svc.Reply(r.Context(), ref.agentID, args.AskID, args.Answer)` → `textResult("已送达提问者")`（`ref.agentID` 来自 token，正是回复者身份）。

Run: `go test ./internal/service/orch_svc/ -run "TestAsk|TestReply"`
Expected: PASS。

```bash
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: ask/reply(注入活会话+ask_id 显式关联,reply 工具回复)"
```

---

### Task 11: `send` — 同会话续做（返工）

**Files:**
- Create: `internal/service/orch_svc/send.go`
- Modify: `internal/service/orch_svc/mcp.go`（`send` 分支）
- Test: `internal/service/orch_svc/send_test.go`

**Interfaces:**
- Consumes: `TaskRepo.{Find,Update}`、`ChatGateway.SendAndForget`、`scheduler`（复用 watchCompletion）。
- Produces: `Send(ctx, callerSessionID, taskID int64, message string) error`：校验 `taskID` 属于 caller 派发的子任务（`ParentTaskID` 链）；把 `message` 注入该子会话、**同节点再次 running**（不新增 Task）、重挂完成监听。

> spec §5：返工 = 对同一任务再 `send`——同一节点再次 running、**不新增节点/边**；不计数、不设返工态。

- [ ] **Step 1: 写失败测试** `send_test.go`

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestSend_ReopensSameTaskNoNewNode(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(&orch_entity.Task{ID: 11, RunID: 100, ParentTaskID: 9, SessionID: 600, Status: orch_entity.TaskDone}, nil)
	// 不得新建 Task（断言 Create 永不被调用——gomock 默认未声明即失败）。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ID, ShouldEqual, 11)
		So(tk.Status, ShouldEqual, orch_entity.TaskRunning)
		return nil
	})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(600), "再补上错误处理").Return(nil)
	chat.EXPECT().ObserveTurn(int64(600)).Return(make(<-chan orch_svc.TurnDone), func() {}).AnyTimes()

	Convey("send 把同一任务重开为 running、不新增节点", t, func() {
		err := orch_svc.Default().Send(context.Background(), 500, 11, "再补上错误处理")
		So(err, ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（`Send` undefined）。

- [ ] **Step 3: 写 `send.go`**

```go
package orch_svc

import (
	"context"
	"errors"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

var errNotYourTask = errors.New("orch: 只能 send 给自己派发的任务")

// Send 往自己派发的某任务会话发后续/返工；同节点再次 running，不新增节点。
func (s *orchSvc) Send(ctx context.Context, callerSessionID, taskID int64, message string) error {
	caller, err := s.tasks.FindBySession(ctx, callerSessionID)
	if err != nil || caller == nil {
		return errRunNotActive
	}
	tk, err := s.tasks.Find(ctx, taskID)
	if err != nil || tk == nil {
		return errAgentNotFound
	}
	if tk.ParentTaskID != caller.ID {
		return errNotYourTask
	}
	tk.Status = orch_entity.TaskRunning
	if err := s.tasks.Update(ctx, tk); err != nil {
		return err
	}
	caller.Status = orch_entity.TaskAwaitingChildren
	_ = s.tasks.Update(ctx, caller)
	s.enqueueRun(tk.RunID, tk, message) // 复用调度器 + watchCompletion
	return nil
}
```

> 注意：测试用极简期望（直跑），但实现走 `enqueueRun`（异步）。为避免 flake，send 测试断言聚焦「Update 同 id + status running + 不 Create」，对 `SendAndForget/ObserveTurn` 用 `.AnyTimes()`（如上）。

- [ ] **Step 4: 接 MCP `send` 分支 + 跑测试 + Commit**

`case "send"`：解析 `{task_id,message}` → `m.svc.Send(...)` → `textResult("已续做")`。

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/orch_svc/ -run TestSend
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: send 同会话续做(同节点 running,不新增节点)"
```

---

### Task 12: `finish` — 收口（根 → Run done）

**Files:**
- Create: `internal/service/orch_svc/finish.go`
- Modify: `internal/service/orch_svc/mcp.go`（`finish` 分支）
- Test: `internal/service/orch_svc/finish_test.go`

**Interfaces:**
- Consumes: `TaskRepo.{FindBySession,Update}`、`RunRepo.{Find,Update}`、`Emitter`。
- Produces: `Finish(ctx, sessionID int64, summary string) error`：写 `Task.Result=summary`、标 `done`；**若该任务是 Run 根任务** → `Run.Status=done`、emit `orch:run:done`、桌面通知（Task 17/绑定层）。非根 finish 仅收口该子任务并回报父（走 `reportToParent`）。

> spec §3.2 根任务例外：Run 收口**只认 Leader 显式 finish**；根不走「安静轮结束」自动收口。

- [ ] **Step 1: 写失败测试** `finish_test.go`（两例：根 finish → Run done；非根 finish → 回报父）

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestFinish_RootCollapsesRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, ParentTaskID: 0, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.Status, ShouldEqual, orch_entity.TaskDone)
		So(tk.Result, ShouldEqual, "全部完成,已交付")
		return nil
	})
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunDone)
		return nil
	})

	Convey("根 finish 收口整个 Run", t, func() {
		So(orch_svc.Default().Finish(context.Background(), 500, "全部完成,已交付"), ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（`Finish` undefined）。

- [ ] **Step 3: 写 `finish.go`**

```go
package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// Finish 收口当前任务；若为 Run 根任务则整个 Run done。
func (s *orchSvc) Finish(ctx context.Context, sessionID int64, summary string) error {
	tk, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil || tk == nil {
		return errRunNotActive
	}
	tk.Status = orch_entity.TaskDone
	tk.Result = summary
	if err := s.tasks.Update(ctx, tk); err != nil {
		return err
	}
	run, err := s.runs.Find(ctx, tk.RunID)
	if err != nil {
		return err
	}
	if run != nil && run.RootTaskID == tk.ID {
		run.Status = orch_entity.RunDone
		if err := s.runs.Update(ctx, run); err != nil {
			return err
		}
		if s.emit != nil {
			s.emit.Emit(ctx, "orch:run:done", map[string]any{"runId": run.ID})
		}
		logger.Ctx(ctx).Info("orch.Finish: Run 收口完成", zap.Int64("run", run.ID))
		return nil
	}
	// 非根：把小结当回报上抛父（与完成回报同路）。
	s.reportToParent(ctx, tk.ParentTaskID, tk, summary)
	s.onTaskSettled(tk.RunID)
	return nil
}
```

- [ ] **Step 4: 接 MCP `finish` 分支 + 跑测试 + Commit**

`case "finish"`：解析 `{summary}` → `m.svc.Finish(r.Context(), ref.sessionID, args.Summary)` → `textResult("已收口")`。

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/orch_svc/ -run TestFinish
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: finish 收口(根→Run done / 非根→回报父)"
```

---

### Task 13: ask 死锁检测（dispatch+ask 合并等待图）

**Files:**
- Create: `internal/service/orch_svc/deadlock.go`
- Modify: `internal/service/orch_svc/ask.go`（`recordAskWait`/`clearAskWait` 真实现 + 在 `Ask` 入队后调 `detectCycle`）
- Test: `internal/service/orch_svc/deadlock_test.go`

**Interfaces:**
- Consumes: `TaskRepo.ListByRun`（取 dispatch 父子边）、内存 ask 等待边。
- Produces:
  - `recordAskWait(from, to int64)` / `clearAskWait(from int64)`（内存 `askWaits map[int64]int64`：fromSession→toSession）。
  - `detectAskCycle(ctx, runID int64) (cycle []int64, found bool)`：把 dispatch 祖先链（`ParentTaskID`，spec 的 `resolveChain`）+ ask 等待边并成有向图，DFS 找环。
  - 命中 → emit `orch:run:deadlock`（payload 含环上 sessionIDs），**交 Leader/用户裁决，不静默强答**（spec §4）。

> spec §4：把 dispatch 祖先链扩成 dispatch+ask 合并等待图做环检测；命中 UI 高亮该环交 Leader/用户决定。

- [ ] **Step 1: 写失败测试** `deadlock_test.go`（构造 700→800、800→700 两条 ask 边 → 检出环）

```go
package orch_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"go.uber.org/mock/gomock"
)

func TestDetectAskCycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	s := &orchSvc{tasks: tasks, pending: map[string]askEnvelope{}, askWaits: map[int64]int64{}}

	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, SessionID: 700, ParentTaskID: 0},
		{ID: 2, SessionID: 800, ParentTaskID: 0},
	}, nil).AnyTimes()

	s.recordAskWait(700, 800)
	s.recordAskWait(800, 700)
	cycle, found := s.detectAskCycle(context.Background(), 100)
	assert.True(t, found)
	assert.Len(t, cycle, 2)
}
```

> 此为 `package orch_svc` 内部测（直接访问私有 `askWaits` 字段 + 私有方法）。`askWaits` 字段 + `recordAskWait`/`clearAskWait` 已在 Task 4（结构体）+ Task 10（helper）落地——本任务只需新增 `detectAskCycle`。

- [ ] **Step 2: 跑测试看它失败** → FAIL（`detectAskCycle` undefined）。

- [ ] **Step 3: 写 `deadlock.go`**（仅 `detectAskCycle`；`recordAskWait`/`clearAskWait` 不要重复定义——已在 `ask.go`）

```go
package orch_svc

import "context"

// detectAskCycle 合并 dispatch 祖先链 + ask 等待边,DFS 找等待环。返回环上 sessionID。
func (s *orchSvc) detectAskCycle(ctx context.Context, runID int64) ([]int64, bool) {
	// 邻接表:sessionID -> 它在等谁的 sessionID。
	edges := map[int64]int64{}
	s.askMu.Lock()
	for from, to := range s.askWaits {
		edges[from] = to
	}
	s.askMu.Unlock()
	// (dispatch 祖先链此处可叠加:子等父回报方向；ask 环已足够触发裁决,父子链按需补。)

	const (
		white = 0
		gray  = 1
		black = 2
	)
	color := map[int64]int{}
	var path []int64
	var dfs func(n int64) ([]int64, bool)
	dfs = func(n int64) ([]int64, bool) {
		color[n] = gray
		path = append(path, n)
		if nxt, ok := edges[n]; ok {
			switch color[nxt] {
			case gray:
				// 找到环:截取 path 上 nxt..n。
				start := 0
				for i, v := range path {
					if v == nxt {
						start = i
						break
					}
				}
				cyc := append([]int64{}, path[start:]...)
				return cyc, true
			case white:
				if c, ok := dfs(nxt); ok {
					return c, true
				}
			}
		}
		path = path[:len(path)-1]
		color[n] = black
		return nil, false
	}
	for n := range edges {
		if color[n] == white {
			path = path[:0]
			if c, ok := dfs(n); ok {
				return c, true
			}
		}
	}
	return nil, false
}
```

并在 `ask.go` 的 `Ask` 入队后追加：

```go
	if cycle, found := s.detectAskCycle(ctx, from.RunID); found && s.emit != nil {
		s.emit.Emit(ctx, "orch:run:deadlock", map[string]any{"runId": from.RunID, "cycle": cycle})
	}
```

- [ ] **Step 4: 跑测试看它通过 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/orch_svc/ -run TestDetectAskCycle
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: ask 死锁检测(等待图环检测→emit 交裁决)"
```

---

### Task 14: 干预面 — pause / resume / hard-stop / speak

**Files:**
- Create: `internal/service/orch_svc/control.go`
- Test: `internal/service/orch_svc/control_test.go`

**Interfaces:**
- Consumes: `RunRepo.{Find,Update}`、`TaskRepo.{ListByRun,Update}`、`ChatGateway.SendAndForget`、`scheduler`。
- Produces:
  - `PauseRun(ctx, runID)`：`Run.Status=paused`（当前轮跑完后挂起：调度器 `kick` 在 paused 时不再起新槽）。
  - `ResumeRun(ctx, runID)`：`Run.Status=running` + `kick`。
  - `StopRun(ctx, runID)`：`Run.Status=stopped` + 级联把活任务标 `canceled`（worktree 清理交 chat/worktree 基建，本任务只标状态 + emit `orch:run:stopped`）。
  - `Speak(ctx, sessionID, message)`：用户对任意会话发言 → `SendAndForget` 触发其下一轮（对 Leader=改全局；对 worker=局部纠偏）。

- [ ] **Step 1: 写失败测试** `control_test.go`（PauseRun 写 paused；StopRun 级联 canceled）

```go
package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestStopRun_CascadeCancels(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, emit)

	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, Status: orch_entity.RunRunning}, nil)
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunStopped)
		return nil
	})
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, Status: orch_entity.TaskRunning}, {ID: 2, Status: orch_entity.TaskDone},
	}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ID, ShouldEqual, 1) // 只取消活任务,不动已 done
		So(tk.Status, ShouldEqual, orch_entity.TaskCanceled)
		return nil
	})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:stopped", gomock.Any())

	Convey("StopRun 级联取消活任务", t, func() {
		So(orch_svc.Default().StopRun(context.Background(), 100), ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（`StopRun` undefined）。

- [ ] **Step 3: 写 `control.go`**

```go
package orch_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func (s *orchSvc) PauseRun(ctx context.Context, runID int64) error {
	return s.setRunStatus(ctx, runID, orch_entity.RunPaused, "orch:run:paused")
}

func (s *orchSvc) ResumeRun(ctx context.Context, runID int64) error {
	if err := s.setRunStatus(ctx, runID, orch_entity.RunRunning, "orch:run:resumed"); err != nil {
		return err
	}
	s.kick(runID)
	return nil
}

func (s *orchSvc) StopRun(ctx context.Context, runID int64) error {
	run, err := s.runs.Find(ctx, runID)
	if err != nil || run == nil {
		return errRunNotActive
	}
	run.Status = orch_entity.RunStopped
	if err := s.runs.Update(ctx, run); err != nil {
		return err
	}
	rows, _ := s.tasks.ListByRun(ctx, runID)
	for _, tk := range rows {
		if tk.IsActive() {
			tk.Status = orch_entity.TaskCanceled
			_ = s.tasks.Update(ctx, tk)
		}
	}
	if s.emit != nil {
		s.emit.Emit(ctx, "orch:run:stopped", map[string]any{"runId": runID})
	}
	return nil
}

// Speak 用户对任意会话发言 → 触发其下一轮。
func (s *orchSvc) Speak(ctx context.Context, sessionID int64, message string) error {
	return s.chat.SendAndForget(ctx, sessionID, message)
}

func (s *orchSvc) setRunStatus(ctx context.Context, runID int64, status, event string) error {
	run, err := s.runs.Find(ctx, runID)
	if err != nil || run == nil {
		return errRunNotActive
	}
	run.Status = status
	if err := s.runs.Update(ctx, run); err != nil {
		return err
	}
	if s.emit != nil {
		s.emit.Emit(ctx, event, map[string]any{"runId": runID})
	}
	return nil
}
```

并在 `scheduler.go` 的 `kick` 开头加暂停门控：

```go
func (s *orchSvc) kick(runID int64) {
	if run, _ := s.runs.Find(context.Background(), runID); run != nil && !run.CanAdvance() {
		return // paused/stopped/done：不再起新槽（当前轮跑完即挂起）
	}
	// ...原逻辑
}
```

> `kick` 加了 `runs.Find` 调用：Task 9 的 `TestScheduler_CapsConcurrency` 需补 `runs` mock（`runs.EXPECT().Find(...).Return(running, nil).AnyTimes()`）。回到 scheduler_test 补这条期望并重跑。

- [ ] **Step 4: 跑测试看它通过 + 全包回归 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test -race ./internal/service/orch_svc/...
git add internal/service/orch_svc/ && git commit -m "✨ orch_svc: 干预面 pause/resume/hard-stop/speak"
```

---

## Phase C — chat_svc 接缝接入

### Task 15: 注入 orchestrate 工具 + 编排流程/指引 + 会话域隔离

**Files:**
- Create: `internal/service/orch_svc/turn.go`
- Modify: `internal/service/chat_svc/turn_extras.go`（若 `RegisterTurnExtrasProvider` 单槽 → 改可叠加 registry）
- Modify: `internal/repository/chat_repo/session.go`（`defaultSessionScope` 增 `run_id = 0`）
- Test: `internal/service/orch_svc/turn_test.go`
- Test: `internal/repository/chat_repo/session_test.go`（追加 run_id 过滤用例）

**Interfaces:**
- Consumes: `chat_svc.RegisterTurnMCPProvider`（已验证可叠加）、`chat_svc.RegisterTurnExtrasProvider`、`agent_entity.Agent.ToolEnabled`、`agenttool.KeyOrchestrate`、`RunRepo`/`TaskRepo`。
- Produces:
  - `BuildTurnMCP(ctx, a *agent_entity.Agent, sessionID, _ int64) []agentruntime.MCPServerSpec`：`a.ToolEnabled(KeyOrchestrate)` 时返回 orchestrate MCP server（URL=gateway+`/mcp/orchestrate/`，Authorization=`Bearer `+`MintToken(a.ID, sessionID)`，ToolNames 取自 registry）。mirror `subagent_svc.BuildTurnMCP`。
  - `BuildTurnExtras(ctx, a, sessionID, _ int64) (mcp []spec, suffix string, ok bool)`：`ToolEnabled` 时 ok=true；suffix = 通用编排框架语（"一切结果都会回到你、由你决定下一步；无次数上限，自己判断何时收口"）；若该 sessionID = 某 Run 的根会话 → 追加 `run.FlowContent`。
  - `chat_repo` 的 `defaultSessionScope` 改为 `db.Where("group_id = ? AND run_id = ?", 0, 0)`，把编排子会话挡出普通会话列表。

- [ ] **Step 1: 写失败测试** `turn_test.go`

```go
package orch_svc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

func enableOrch(a *agent_entity.Agent) *agent_entity.Agent {
	_ = a.SetTools([]agent_entity.AgentTool{{Key: agenttool.KeyOrchestrate, Enabled: true}}) // 按 agent_entity 真实 API 调整
	return a
}

func TestBuildTurnMCP_InjectsWhenEnabled(t *testing.T) {
	a := enableOrch(&agent_entity.Agent{ID: 2, Name: "架构师"})
	specs := orch_svc.Default().BuildTurnMCP(context.Background(), a, 500, 0)
	assert.NotEmpty(t, specs)
	assert.Equal(t, agenttool.KeyOrchestrate, specs[0].Name)
}

func TestBuildTurnExtras_AppendsFlowForRootSession(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, ParentTaskID: 0}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, FlowContent: "先拆分再并行"}, nil)

	a := enableOrch(&agent_entity.Agent{ID: 2})
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 500, 0)
	assert.True(t, ok)
	assert.Contains(t, suffix, "先拆分再并行")
	assert.Contains(t, suffix, "一切结果")
}
```

> `agent_entity.Agent` 的工具读写 API（`ToolEnabled`/`SetTools`）以真实签名为准（见 `agent_entity` 包，`ToolEnabled(key string) bool` 已由 grounding 确认存在；写法按其测试惯例）。

- [ ] **Step 2: 跑测试看它失败** → FAIL（`BuildTurnMCP`/`BuildTurnExtras` undefined）。

- [ ] **Step 3: 写 `turn.go`**（mirror `subagent_svc.BuildTurnMCP` 取 gatewayBaseURL + MintToken 的写法）

```go
package orch_svc

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

const orchGuidance = `你被授予编排能力(dispatch/ask/send/finish + agent_list)。模型:一切结果都会回到你、由你决定下一步;` +
	`并行 dispatch 子任务,审核/测试/合并也是 dispatch,返工用 send,补信息用 ask,收口用 finish。` +
	`无次数/时长/成本上限——自己判断何时收口或换策略。用户可能随时插话。`

// BuildTurnMCP 启用了 orchestrate 的 agent 注入 orchestrate MCP server。
func (s *orchSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeyOrchestrate) {
		return nil
	}
	def, _ := agenttool.Lookup(agenttool.KeyOrchestrate)
	return []agentruntime.MCPServerSpec{{
		Name:    agenttool.KeyOrchestrate,
		URL:     strings.TrimRight(s.gatewayBaseURL, "/") + def.MCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + s.MintToken(a.ID, sessionID)},
		Tools:   def.ToolNames,
	}}
}

// BuildTurnExtras 注入编排框架语 + (根会话)编排流程正文。
func (s *orchSvc) BuildTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
	if a == nil || !a.ToolEnabled(agenttool.KeyOrchestrate) {
		return nil, "", false
	}
	suffix := "\n\n### 编排指引\n" + orchGuidance
	if tk, _ := s.tasks.FindBySession(ctx, sessionID); tk != nil && tk.ParentTaskID == 0 {
		if run, _ := s.runs.Find(ctx, tk.RunID); run != nil && run.RootTaskID == tk.ID && strings.TrimSpace(run.FlowContent) != "" {
			suffix += "\n\n### 本次编排流程(用户首条消息可临时覆盖)\n" + strings.TrimSpace(run.FlowContent)
		}
	}
	return nil, suffix, true
}
```

> `gatewayBaseURL` 字段 + `SetGatewayBaseURL(base string)` setter 加到 `orch.go`（bootstrap 在 gateway 起好后注入，mirror subagent_svc）。`agentruntime.MCPServerSpec` 字段名（Name/URL/Headers/Tools）以真实定义为准（grounding 已确认该类型存在）。

- [ ] **Step 4: chat_svc 接缝 + 会话域** — 改 `chat_repo/session.go` 的 `defaultSessionScope`：

```go
func defaultSessionScope(db *gorm.DB) *gorm.DB {
	return db.Where("group_id = ? AND run_id = ?", 0, 0)
}
```

补 `session_test.go` 一例断言：listByAgent(ordinaryOnly=true) 生成的 SQL 含 `run_id`（mirror 既有 group_id 过滤测试）。

若 `RegisterTurnExtrasProvider` 为单槽（会 clobber group 的）：把 `turn_extras.go` 改成与 `turn_mcp.go` 同款可叠加 registry（`var turnExtrasProviders []TurnExtrasProvider` + `fillGroupTurnExtras` 改为遍历，第一个 `ok=true` 生效）。这是 in-scope 小重构（plan-1 不删 group，须保 group 的 provider 仍生效）。

- [ ] **Step 5: 跑测试 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/orch_svc/ ./internal/repository/chat_repo/...
git add internal/service/orch_svc/turn.go internal/service/chat_svc/ internal/repository/chat_repo/
git commit -m "✨ orch: 注入 orchestrate 工具+编排流程/指引 + 会话按 run_id 隔离"
```

---

### Task 16: bootstrap 接线 + 生产适配器

**Files:**
- Modify: `internal/bootstrap/cago.go`（repo 注册 + gateway 挂 `/mcp/orchestrate/` + RegisterTurnMCPProvider/RegisterTurnExtrasProvider + SetGatewayBaseURL）
- Modify: `internal/app/app.go`（`registerChatService()` 内 `orch_svc.Default().RegisterDeps(...)`）
- Create: `internal/app/orch_adapter.go`（ChatGateway/AgentLookup/Emitter 生产适配器）
- Test: 编译 + `make test-backend` 冒烟（无新单测；适配器是瘦封装）

**Interfaces:**
- Consumes: `chat_svc.Chat()`、`agent_repo.Agent()`、`orch_repo.New{Run,Task}()`、`gw.BaseURL`/`gw.RegisterMCP`、wails `EventsEmit`。
- Produces: `orchChatAdapter`/`orchAgentAdapter`/`orchEmitter` 实现 orch_svc 的窄接口。

- [ ] **Step 1: 写适配器** `internal/app/orch_adapter.go`（把 orch 窄接口映射到 chat_svc / agent_repo 真方法）

```go
package app

import (
	"context"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

// orchChatAdapter 把 orch_svc.ChatGateway 映射到 chat_svc.Chat()。
type orchChatAdapter struct{}

func (orchChatAdapter) EnsureOrchSession(ctx context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
	// 映射到 chat_svc.EnsureSession：purpose=orchestration, 传 in.RunID/ParentSessionID/AgentID/ProjectID/Isolate。
	// 具体字段名以 chat_svc.EnsureSessionRequest 为准（grounding：EnsureSession 已存在）。
	resp, err := chat_svc.Chat().EnsureSession(ctx, &chat_svc.EnsureSessionRequest{ /* ...按真实字段填... */ })
	if err != nil {
		return 0, err
	}
	return resp.SessionID, nil
}

func (orchChatAdapter) SendAndForget(ctx context.Context, sessionID int64, text string) error {
	// 映射到 chat_svc.Send(EmitTurnStartedBypass:true)，非阻塞触发该会话下一轮。
	_, err := chat_svc.Chat().Send(ctx, &chat_svc.SendRequest{ /* SessionID:sessionID, Text:text, EmitTurnStartedBypass:true */ })
	return err
}

func (orchChatAdapter) ObserveTurn(sessionID int64) (<-chan orch_svc.TurnDone, func()) {
	src, cancel := chat_svc.Chat().ObserveTurn(sessionID)
	out := make(chan orch_svc.TurnDone)
	go func() {
		defer close(out)
		for r := range src {
			out <- orch_svc.TurnDone{SessionID: sessionID, OK: r.Err == nil} // 按 TurnResult 真实字段调整
		}
	}()
	return out, cancel
}

func (orchChatAdapter) FinalAssistantText(ctx context.Context, sessionID int64) (string, error) {
	return chat_svc.Chat().FinalAssistantText(ctx, sessionID)
}

func (orchChatAdapter) AgentStatus(ctx context.Context, sessionID int64) (string, error) {
	// 读 chat_entity.Session.AgentStatus（idle/running/waiting/error）。
	return chat_svc.Chat().SessionStatus(ctx, sessionID) // 若无此方法则经 session repo 取 AgentStatus
}

type orchAgentAdapter struct{}

func (orchAgentAdapter) Find(ctx context.Context, id int64) (*agent_entity.Agent, error) {
	return agent_repo.Agent().Find(ctx, id)
}
func (orchAgentAdapter) FindByName(ctx context.Context, name string) (*agent_entity.Agent, error) {
	return agent_repo.Agent().FindByName(ctx, name) // 若无则 List 后按 Name 匹配
}
func (orchAgentAdapter) List(ctx context.Context) ([]*agent_entity.Agent, error) {
	return agent_repo.Agent().List(ctx)
}

type orchEmitter struct{ a *App }

func (e orchEmitter) Emit(_ context.Context, name string, payload any) {
	wailsruntime.EventsEmit(e.a.ctx, name, payload)
}
```

> 适配器里 `/* ... */` 处按 chat_svc 真实 `EnsureSessionRequest`/`SendRequest`/`TurnResult` 字段填实（grounding 已确认这些类型与 `EnsureSession`/`Send`/`ObserveTurn`/`FinalAssistantText` 存在；若 `SessionStatus`/`FindByName` 缺，按注释退化方案实现）。

- [ ] **Step 2: bootstrap repo 注册** — `internal/bootstrap/cago.go` 在其它 repo 注册旁加：

```go
orch_repo.RegisterRun(orch_repo.NewRun())
orch_repo.RegisterTask(orch_repo.NewTask())
```

并在挂载 MCP gateway 处（subagent 同段）加：

```go
gw.RegisterMCP("/mcp/orchestrate/", orch_svc.Default().MCPHandler())
orch_svc.Default().SetGatewayBaseURL(gw.BaseURL)
chat_svc.RegisterTurnMCPProvider(orch_svc.Default().BuildTurnMCP)
chat_svc.RegisterTurnExtrasProvider(orch_svc.Default().BuildTurnExtras)
```

> 远端 agentred 自动获得编排工具：`remote.RegisterMCPProxyDispatcher` 是**对全部 `/mcp/*` 通用反向隧道**（grounding 确认），新增 `/mcp/orchestrate/` 无需额外远端接线——但应在 plan-1 末做一次真机 daemon 手验（记 TODO，不阻塞）。

- [ ] **Step 3: svc 依赖注入** — `internal/app/app.go` 的 `registerChatService()` 末尾加：

```go
orch_svc.Default().RegisterDeps(
	orchChatAdapter{}, orchAgentAdapter{},
	orch_repo.Run(), orch_repo.Task(),
	chat_svc.Chat(), // ApprovalGateway（chat_svc 实现 BeginToolApproval/FinishToolApproval）
	orchEmitter{a: a},
)
```

- [ ] **Step 4: 编译 + 冒烟 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
make test-backend
git add internal/bootstrap/cago.go internal/app/app.go internal/app/orch_adapter.go
git commit -m "🔧 orch: bootstrap 接线 + 生产适配器(chat/agent/emit)+ gateway 挂载"
```

---

## Phase D — Wails 绑定

### Task 17: `internal/app/orch.go` — App 绑定层

**Files:**
- Create: `internal/app/orch.go`
- Test: `internal/app/orch_test.go`（薄；绑定层只做 parse→svc→return，主断言「转发参数 + DTO 转换」）

**Interfaces:**
- Consumes: `orch_svc.Default()`、`orch_entity`。
- Produces（Wails 自动生成 TS 绑定，供 plan-1b 前端调用）：
  - `RunCreate(req *RunCreateRequest) (*RunDetailDTO, error)`、`RunList() ([]*RunItemDTO, error)`、`RunLoad(id int64) (*RunDetailDTO, error)`、`RunPause/RunResume/RunStop(id int64) error`、`RunSpeak(sessionID int64, message string) error`。
  - DTO：`RunItemDTO{ID, Goal, LeaderAgentID, Status, ProjectID, Createtime, Updatetime}`、`RunDetailDTO{Run *RunItemDTO; Tasks []*TaskDTO}`、`TaskDTO{ID, RunID, AgentID, SessionID, ParentTaskID, Kind, Status, Brief, Result, CallSeq}`。

- [ ] **Step 1: 写 `orch.go`**（mirror `internal/app/group.go` 的 DTO→converter→方法三段式）

```go
package app

import (
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

type RunItemDTO struct {
	ID            int64  `json:"id"`
	Goal          string `json:"goal"`
	LeaderAgentID int64  `json:"leaderAgentId"`
	Status        string `json:"status"`
	ProjectID     int64  `json:"projectId"`
	Createtime    int64  `json:"createtime"`
	Updatetime    int64  `json:"updatetime"`
}

type TaskDTO struct {
	ID           int64  `json:"id"`
	RunID        int64  `json:"runId"`
	AgentID      int64  `json:"agentId"`
	SessionID    int64  `json:"sessionId"`
	ParentTaskID int64  `json:"parentTaskId"`
	Kind         string `json:"kind"`
	Status       string `json:"status"`
	Brief        string `json:"brief"`
	Result       string `json:"result"`
	CallSeq      int    `json:"callSeq"`
}

type RunDetailDTO struct {
	Run   *RunItemDTO `json:"run"`
	Tasks []*TaskDTO  `json:"tasks"`
}

type RunCreateRequest struct {
	Goal            string  `json:"goal"`
	LeaderAgentID   int64   `json:"leaderAgentId"`
	FlowID          int64   `json:"flowId"`
	FlowContent     string  `json:"flowContent"`
	ProjectID       int64   `json:"projectId"`
	AllowedAgentIDs []int64 `json:"allowedAgentIds"`
}

func toRunItem(r *orch_entity.OrchestrationRun) *RunItemDTO {
	return &RunItemDTO{ID: r.ID, Goal: r.Goal, LeaderAgentID: r.LeaderAgentID, Status: r.Status, ProjectID: r.ProjectID, Createtime: r.Createtime, Updatetime: r.Updatetime}
}

func toTaskDTO(t *orch_entity.Task) *TaskDTO {
	return &TaskDTO{ID: t.ID, RunID: t.RunID, AgentID: t.AgentID, SessionID: t.SessionID, ParentTaskID: t.ParentTaskID, Kind: t.Kind, Status: t.Status, Brief: t.Brief, Result: t.Result, CallSeq: t.CallSeq}
}

func (a *App) RunCreate(req *RunCreateRequest) (*RunDetailDTO, error) {
	d, err := orch_svc.Default().CreateRun(a.ctx, &orch_svc.CreateRunRequest{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID, FlowID: req.FlowID,
		FlowContent: req.FlowContent, ProjectID: req.ProjectID, AllowedAgentIDs: req.AllowedAgentIDs,
	})
	if err != nil {
		return nil, err
	}
	return &RunDetailDTO{Run: toRunItem(d.Run), Tasks: []*TaskDTO{toTaskDTO(d.RootTask)}}, nil
}

func (a *App) RunList() ([]*RunItemDTO, error) {
	rs, err := orch_svc.Default().ListRuns(a.ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*RunItemDTO, 0, len(rs))
	for _, r := range rs {
		out = append(out, toRunItem(r))
	}
	return out, nil
}

func (a *App) RunLoad(id int64) (*RunDetailDTO, error) {
	d, err := orch_svc.Default().LoadRun(a.ctx, id)
	if err != nil {
		return nil, err
	}
	tasks := make([]*TaskDTO, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		tasks = append(tasks, toTaskDTO(t))
	}
	return &RunDetailDTO{Run: toRunItem(d.Run), Tasks: tasks}, nil
}

func (a *App) RunPause(id int64) error  { return orch_svc.Default().PauseRun(a.ctx, id) }
func (a *App) RunResume(id int64) error { return orch_svc.Default().ResumeRun(a.ctx, id) }
func (a *App) RunStop(id int64) error   { return orch_svc.Default().StopRun(a.ctx, id) }
func (a *App) RunSpeak(sessionID int64, message string) error {
	return orch_svc.Default().Speak(a.ctx, sessionID, message)
}
```

> 需在 `orch_svc` 补两个只读查询：`ListRuns(ctx) ([]*orch_entity.OrchestrationRun, error)`（= `runs.List`）与 `LoadRun(ctx, id) (*RunLoadResult{Run; Tasks}, error)`（= `runs.Find` + `tasks.ListByRun`）。各写一个最小 svc 单测（mock repo）再实现——遵循 TDD。

- [ ] **Step 2: 写薄绑定测试** `orch_test.go`（用 svc mock 或直接断言 DTO 转换函数 `toRunItem`/`toTaskDTO` 的字段映射；绑定层不塞逻辑）。

- [ ] **Step 3: 生成绑定 + 测试 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
make generate        # 刷新 frontend/wailsjs（RunCreate/RunList/... 出现在 App.d.ts）
make test-backend
git add internal/app/orch.go internal/app/orch_test.go internal/service/orch_svc/ frontend/wailsjs/
git commit -m "✨ orch: Wails 绑定层(RunCreate/List/Load/Pause/Resume/Stop/Speak)"
```

---

## Phase E — 端到端验证（无 UI）

### Task 18: e2e fake-runtime 最小编排（dispatch → 回报 → finish）

**Files:**
- Modify: e2e fake runtime（`e2e/` 包，按 `docs/e2e-harness-guide.md`）——让 fake agent 能"调用"orchestrate 工具（dispatch 一次 + finish）。
- Create: `e2e/tests/orchestration.spec.ts`
- Test: `make e2e`（或 `make e2e-scratch` 先在 `e2e/scratch/` 验证）

**Interfaces:**
- Consumes: e2e fake-runtime 的 MCP 客户端能力（参考 group_send 接缝：fake runtime 充当 MCP HTTP 客户端，见 `reference_e2e_group_reply_needs_group_send`）。
- Produces: 一条断言——创建 Run → Leader fake 调 `dispatch(李, "做X")` → 子 fake 完成回报 → Leader fake 调 `finish` → DB 孪生：`orchestration_runs.status='done'`、`orch_tasks` 有根 + 1 子且均 `done`。

- [ ] **Step 1: 扩展 fake runtime** — 让指定 fake agent 在收到首条消息时，向注入的 `/mcp/orchestrate/` 发一次 `dispatch`（用 turn 内拿到的 MCP server URL + token），子 fake 收到 brief 后回一段文本即自然 done；Leader fake 在收到子回报续轮时发 `finish`。参考 group e2e 里 fake 充当 group_send HTTP 客户端的写法（`findGroupSendServer→postGroupSend`）。

- [ ] **Step 2: 写 spec** `e2e/tests/orchestration.spec.ts`

```ts
import { test, expect } from "@playwright/test";
import { openApp, dbQuery } from "../helpers"; // 按 e2e harness 真实 helper 名调整

test("最小编排: dispatch → 回报 → finish 收口 Run", async ({ page }) => {
  const app = await openApp(page);
  // 经 UI 不可用(plan-1b 才有前端)→ 直接调绑定或 seed：用 e2e 既有「调 App 方法」通道触发 RunCreate。
  await app.call("RunCreate", { goal: "做个登录页", leaderAgentId: /* seeded leader */ 2, flowContent: "拆分并行" });

  // 轮询 DB 孪生直到 Run done。
  await expect.poll(async () => {
    const rows = await dbQuery(`SELECT status FROM orchestration_runs ORDER BY id DESC LIMIT 1`);
    return rows[0]?.status;
  }, { timeout: 15000 }).toBe("done");

  const tasks = await dbQuery(`SELECT kind,status,parent_task_id FROM orch_tasks ORDER BY id ASC`);
  expect(tasks.length).toBeGreaterThanOrEqual(2);        // 根 + ≥1 子
  expect(tasks.every((t: any) => t.status === "done")).toBeTruthy();
  expect(tasks.some((t: any) => t.parent_task_id !== 0)).toBeTruthy(); // 有子任务边
});
```

> 若 e2e harness 无「直接调 App 方法」通道，则在 `e2e/scratch/` 先用一条 seed + fake-runtime 脚本驱动；最终把稳定版收进 `e2e/tests/`。spec 的 DB 孪生断言是核心（不依赖 UI）。

- [ ] **Step 3: 跑 e2e + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
make e2e        # 或先 make e2e-scratch 调通
git add e2e/
git commit -m "✅ e2e: 最小编排(dispatch→回报→finish)DB 孪生断言"
```

---

## 收尾校验

- [ ] **全量后端测试**：`make test-backend` → 全绿。
- [ ] **竞态**：`go test -race ./internal/service/orch_svc/...` → 无 DATA RACE。
- [ ] **lint**：`make lint`（golangci-lint v2 + 前端 ESLint）→ 干净。
- [ ] **真机 daemon 手验（TODO，不阻塞合并）**：远端 agentred 上的 CLI 调 orchestrate 工具经反向隧道回 desktop 执行（org/subagent 已验证通用隧道可用；orchestrate 走同一 `/mcp/*` 通道，理应自动通）。

## 交付边界（本计划范围）

- **含**：orch 数据模型 + 仓储 + svc(dispatch/ask/send/finish + 完成回报续轮 + 调度器 + 死锁检测 + 干预面) + MCP 工具 + chat_svc 接缝 + Wails 绑定 + e2e 证明。这是一个**可独立验收的 headless 编排引擎**。
- **不含**（后续计划）：
  - **plan-1b 前端**：创建 Run 弹窗、结构图/活动流双视图、runHeader、任务看板、节点钻入、生命周期/边缘态、大图、i18n、桌面通知 UI（设计稿已就绪：`agentre.pen` 14 帧 + spec §8）。
  - **plan-2 删 group**：砍 group 域、把流程库从「群计数」改「Run 计数」、`chat_svc` ~300 行接缝清理（spec §11）。

---

## 附录 · 实现方案可行性（已用真 agent 后端实测）

> 结论：**GREEN，可实现**。涌现式编排（仅靠 `dispatch/ask/send/finish` 让真 agent 自行编排）已用**真 `claude` CLI（Sonnet 4.6）** 实测通过；核心机制的两半（① 真 CLI 同步委派、② 唤起会话再跑一轮）都已在仓库现有代码中被证明，本计划只是把它们组合 + 异步化。

### A. 真后端实测（2026-06-24，spike：standalone HTTP MCP server 暴露四原语 + 真 claude `-p`）

用计划里的真实工具 schema + 编排指引，给真 claude 一个目标"实现登录页（前端表单 + 后端接口 + e2e）"，观察其工具调用轨迹：

- **场景①（happy path）**：`agent_list → dispatch(王) ∥ dispatch(李) → dispatch(评审) ∥ dispatch(测试) → ask(评审) → finish`。
  - 印证：自主拆分 + 并行派发；**"评审/测试"就是 dispatch 给相应 agent**（无内置阶段，守 spec §2.4）；用 `ask` 补信息；`finish` 收口并交结构化小结。
- **场景②（rework）**：把"测试"任务的回报设为**失败** → Leader 反应：`send→#李 任务`（修错误处理）+ `send→#测试 任务`（重测）→ `dispatch(评审)` → `ask` → `finish`。
  - 印证：**返工 = 对同一任务 `send`、不新增节点/边**（守 spec §5）；Leader 自行读报告判成败并决定后续（再 dispatch / send / finish），runtime 不替它决策。

- **场景③（ask/reply,两个真 agent）**：A=架构师 `ask(王, "鉴权方案是什么?")` → 把问题注入"王"的活会话（王=另一个真 claude,私有上下文被设为"session+cookie,已否决 JWT"）→ 王调 `reply(ask_id, answer)` → A 收到**源自王上下文**的答案（"服务端 session + httpOnly cookie … 否决 JWT 因要支持服务端即时失效",**不是**泛泛的 JWT）→ A `finish`。
  - 印证：`reply` 工具 + `ask_id` 显式关联可行（B 可靠地带对 id 调 reply）；**上下文未丢**（答案来自 B 的私有上下文,而非猜测）；不 fork、不靠轮次时序推断。

- **场景④（递归子编排,嵌套真 claude）**：Leader `dispatch(李=前端负责人)` → 李本身也带编排工具,自己 `agent_list` 后 `dispatch(前端A)` + `dispatch(前端B)` → 汇报回 Leader。`dispatch→前端*` 只可能出自李 → **证明树可递归(深度≥2)**,印证 spec §3.1"编排天然递归成树"。
- **场景⑤（跨后端:真 codex）**：同一套 orchestrate HTTP MCP 工具,经 `codex exec -c mcp_servers.orch.url/http_headers/enabled_tools`（与仓库 codex runtime 同款注入)→ 真 codex `agent_list → dispatch(王)∥dispatch(李)∥dispatch(测试) → finish`,与 claude 同构。**跨后端可行**。
  - 发现:codex 的 streamable-http 客户端会额外发 `GET`(SSE 流)与 `DELETE`(会话回收)。spike 的极简 server 对这两者回 400、codex 自动降级为 POST-only 仍正常调工具——但**真 orchestrate gateway 挂载点必须复用 subagent/group 同款 streamable-http handler(支持 GET/DELETE/会话生命周期),不能是 bespoke POST-only**(Task 6/16 注意)。

- **场景⑥（`blocked` 上抛,嵌套真 claude）**：Leader `dispatch(支付工程师)`,brief 里埋一个产品决策（Stripe vs 支付宝,不该工程师拍板）→ 支付工程师调 `blocked(question)` **上抛而非瞎选** → Leader 收到后**自行拍板**并 `send(task_id, 决策)` 解阻塞 → `finish`。印证 spec §3.2"agent 拿不准则 `blocked` 回报派发者,由 Leader 自决,多数不必惊动用户"。

> 这六条把整个设计最大的不确定性（"真 agent 到底会不会用这些原语编排/互答/递归/跨后端/遇决策上抛"）从纸面降为已观测事实。spike 程序：`/tmp/orchspike/`（编排 happy/`rework`）、`/tmp/askspike/`（两真 agent 的 ask/reply）、`/tmp/recspike/`（`SCENARIO=recursion|blocked`）、`/tmp/codexspike/`（真 codex）——均 throwaway、stdlib-only。

### B. 机制可组合性（代码佐证，无需新建即成立）

| 机制 | 现有佐证 | 本计划做的 |
| --- | --- | --- |
| 真 CLI 同步委派子 agent | `subagent_svc/call.go callAgent`：`EnsureSession→Send→ObserveTurn→FinalAssistantText` 阻塞拿结果 | 改为**非阻塞** + 完成回报续轮（Task 7/8） |
| 唤起会话再跑一轮（非用户触发） | `chat_svc/autonomous_turn.go driveAutonomousTurn`（后台任务自主续轮） | 用同款"注入消息→触发下一轮"把子报告送回派发者（Task 8） |
| 注入第 3 套 MCP 工具到真 CLI 轮 | `subagent`/`group_create` 已经这么做；`claudecode/session.go buildMcpConfigJSON` 产出 `--mcp-config`（HTTP type） | `BuildTurnMCP` 注入 orchestrate（Task 15）；spike 用的就是这套 `--mcp-config` 形状 |
| 会话状态客观可读 | `chat_entity.Session.AgentStatus`(idle/running/waiting/error) | 据其推导 done / error（Task 8，杜绝卡 running） |
| 远端 agentred 工具透传 | `remote.RegisterMCPProxyDispatcher` 对全部 `/mcp/*` 通用反向隧道 | 新增 `/mcp/orchestrate/` 自动复用（Task 16，真机手验留作 TODO） |

> orch_svc 的 `ChatGateway` 适配器纯由现有 chat_svc 方法装配（`EnsureSession`/`Send`/`ObserveTurn`/`FinalAssistantText` + 读 `Session.AgentStatus`）；唯一新增持久化 = `chat_sessions.run_id`（Task 2 迁移）。无其它 chat_svc 缺口。

### C. 验证场景矩阵（实现时逐条验，按层）

**单元 / svc 层（mock + 确定性，已在各 Task 内）：** CreateRun 建 Run+根会话+根Task+触发首轮 · dispatch 起子会话/Task/CallSeq/非阻塞 · 完成回报续轮(done→写Result→注入父→唤醒) · 技术崩溃 error 上抛(同续轮路) · 调度器 cap 限流+settle 放槽 · ask 注入活会话/reply 按 ask_id 解阻塞/超时/防串答 · ask 死锁环检测→emit 裁决 · send 同节点重开+不新增+归属校验 · finish 根→Run done / 非根→回报父 · pause(不起新槽)/resume/hard-stop(级联取消) · speak 注入触发下一轮 · 会话按 run_id 隔离 · ToolEnabled 门控 + 根会话注流程正文。

**真后端层（real claude/codex）：** ✅ happy path · ✅ rework(send) · ✅ ask/reply(两真 agent) · ✅ 递归子编排(树深≥2) · ✅ 跨后端 codex · ✅ `blocked` 上抛→Leader 自决（**六条均已实测**,见附录 A）；**仍待补验**：危险操作走后端权限→`awaiting-user`（按分支阻塞）· 无上限失控边界（靠可见 + 可停,不自动封顶）· 真机 daemon 远端隧道。

**集成层（真 chat_svc + fake runtime，半确定性）：** 端到端经真会话 CreateRun→dispatch→子完成→真续轮→finish，DB 落库正确（Run `done`、tasks `done`、父子边）· 多子任务并发不互踩 + 限流真实生效 · 远端 daemon orchestrate 经反向隧道（真机手验）。

**本地 e2e（Playwright + 真 Wails + fake runtime，Task 18 + plan-1b）：** 最小编排 dispatch→回报→finish 的 DB 孪生断言（Task 18，本计划内）· 子会话不渗入普通会话侧栏 · 暂停/恢复/硬停止经 UI、ask 死锁高亮、节点钻入 transcript（均待 plan-1b 前端就绪后纳入 e2e）。
