# 编排执行节点改名 dispatch + 任务清单子系统 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把编排执行节点从「task」全量改名为「dispatch」，腾出「task」给全新的待办清单子系统（Run 内所有 agent 共享的 TodoWrite 式白板），并顺带删 `cancel`、拆死参 `isolate`、给 `agent_list` 补在跑计数。

**Architecture:** 后端分三段——①执行侧原子改名（迁移 rename + entity/repo/service/DTO/agenttool/wiring）②工具清理 ③清单子系统（新迁移建同名新表 + entity/repo/service/3 工具）；前端最后统一切换（regenerate + 删 TaskBoard + 新 TaskList）。全程 TDD、频繁提交、每次 `git commit <files>` 带 pathspec。

**Tech Stack:** Go 1.26 / cago / gorm + gormigrate / go.uber.org/mock / goconvey + sqlmock；React 19 / TS / Vitest；Wails v2 bindings。

## Global Constraints

- 严格 TDD：Red → Green → Refactor，先失败测试再实现。
- 迁移只追加到 `migrations/migrations.go` 的 `migrationList()` 末尾，禁止改既有迁移；DDL 用原生 SQL（`tx.Exec`）。
- repo 单测用 sqlmock（`testutils.Database(t)`），禁止真库；migration 的 `*_test.go` 与 `bootstrap/cago_test.go` 是唯一真库例外。
- service 单测用 mockgen 注入 repo mock，不连库。
- 分支 `develop/wyz` 有并发会话 staged 的 `internal/service/orch_svc/mcp.go`、`mcp_test.go` 未提交改动（注入门控），**本任务不得改动其语义**；每次提交 `git commit <显式文件路径>`，禁止裸 `git commit`。
- 前端可见文案走 i18n（`react-i18next` `t(...)` + `zh-CN`/`en` 双份 common.json），禁止硬编码中文；只读文本用 `data-selectable-text="true"`，不加复制按钮。
- 收尾闸门：`make test-backend` + `make lint`（含 `gofmt -l`）+ 全量 `cd frontend && pnpm test`，看真 exit code。
- 命名反转最终态：执行节点=`dispatch`（表 `orch_dispatches`、`dispatch_id`）；待办清单=`task`（表 `orch_tasks`、`task_id`）。

---

## 文件结构（改动全景）

**Phase 1 执行侧改名（后端）**
- Rename: `internal/model/entity/orch_entity/task.go` → `dispatch.go`（`Task`→`Dispatch`）
- Rename: `internal/repository/orch_repo/task.go` → `dispatch.go`（`TaskRepo`→`DispatchRepo`）
- Modify: `internal/service/orch_svc/*.go`（约 200 处 `Task` 标识符 + `s.tasks`→`s.dispatches`）
- Modify: `internal/app/orch.go`（`TaskDTO`→`DispatchDTO`，`RunDetailDTO.Tasks`→`Dispatches`）
- Modify: `internal/pkg/agenttool/agenttool.go:38`（ToolNames 内 `task_id` 语义无变，工具名不变；此处仅第 2/8 任务改）
- Modify: `internal/app/app.go:234`、`internal/bootstrap/cago.go:120`（wiring accessor 改名）
- Create: `migrations/202607090001_rename_orch_tasks_to_dispatches.go` + `_test.go`

**Phase 2 工具清理（后端）**
- Delete: `internal/service/orch_svc/cancel.go` + `cancel_test.go`
- Modify: `internal/service/orch_svc/mcp.go`（删 cancel，拆 isolate，agent_list 补 running）
- Modify: `internal/service/orch_svc/dispatch.go`（拆 isolate）、`deps.go`（`EnsureOrchSessionInput.Isolate` 删）、`internal/app/orch_adapter.go`
- Modify: `internal/repository/orch_repo/dispatch.go`（新增 `CountActiveByRunAgent`）
- Modify: `internal/pkg/agenttool/agenttool.go:38`（ToolNames 删 `cancel`）

**Phase 3 清单子系统（后端）**
- Create: `migrations/202607090002_create_orch_tasks_checklist.go` + `_test.go`
- Create: `internal/model/entity/orch_entity/task.go`（全新语义 `Task`）+ `task_test.go`
- Create: `internal/repository/orch_repo/task.go`（全新 `TaskRepo`）+ `task_test.go`
- Create: `internal/service/orch_svc/todo.go`（`TaskList/TaskAdd/TaskUpdate`）+ `todo_test.go`
- Modify: `internal/service/orch_svc/mcp.go`（加 `task_list/task_add/task_update`）、`orch.go`（svc 加 `todos` 字段 + RegisterDeps 参数）、`deps.go`
- Modify: `internal/pkg/agenttool/agenttool.go:38`（ToolNames 加 3 个）、`internal/app/app.go:234`、`internal/bootstrap/cago.go`

**Phase 4 前端**
- Modify: `internal/app/orch.go`（`RunDetailDTO` 加清单 `Tasks []*TaskItemDTO`，`RunLoad` 拉清单）
- Regenerate: `frontend/wailsjs/go/models.ts`（`make generate`）
- Modify: `frontend/src/components/agentre/orchestration/structure-graph.tsx`、`index.tsx`、`stores/orch-run-store.ts`（`task`→`dispatch` 字段）
- Delete: `frontend/src/components/agentre/orchestration/task-board.tsx` + `__tests__/task-board.test.tsx`
- Create: `frontend/src/components/agentre/orchestration/task-list.tsx` + `__tests__/task-list.test.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

---

## Phase 1 — 执行侧改名 `task` → `dispatch`

### Task 1: 迁移 rename + 全量执行侧改名（原子）

一次性完成，因为 Go 类型改名跨包不可半途编译。产出一个 `make test-backend` 全绿的提交。

**Files:**
- Create: `migrations/202607090001_rename_orch_tasks_to_dispatches.go`
- Create: `migrations/202607090001_rename_orch_tasks_to_dispatches_test.go`
- Modify: `migrations/migrations.go`（追加注册）
- Rename: `internal/model/entity/orch_entity/task.go` → `dispatch.go`
- Rename: `internal/repository/orch_repo/task.go` → `dispatch.go`
- Modify: `internal/service/orch_svc/*.go`、`internal/app/orch.go`、`internal/app/app.go`、`internal/bootstrap/cago.go`
- Modify: repo/service 所有 `*_test.go`（随改名）

**Interfaces:**
- Produces:
  - `orch_entity.Dispatch`（原 `Task`，`TableName()="orch_dispatches"`，`ParentDispatchID int64` 列 `parent_dispatch_id`）
  - 常量 `orch_entity.DispatchKindDispatch/DispatchKindAsk`、`DispatchPending/DispatchRunning/DispatchAwaitingChildren/DispatchAwaitingUser/DispatchDone/DispatchCanceled/DispatchPaused/DispatchError`
  - `orch_repo.DispatchRepo`（方法 `Create/Update/Find/FindBySession/ListByRun/CountByRunAgent`，签名内 `*orch_entity.Dispatch`）；accessor `orch_repo.Dispatch()/RegisterDispatch(impl)/NewDispatch()`
  - `app.DispatchDTO`；`app.RunDetailDTO.Dispatches []*DispatchDTO`（json `dispatches`）

- [ ] **Step 1: 写迁移 rename 的失败测试**

`migrations/202607090001_rename_orch_tasks_to_dispatches_test.go`：

```go
package migrations

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/testutils"
	. "github.com/smartystreets/goconvey/convey"
)

func TestMigration202607090001_RenameOrchTasksToDispatches(t *testing.T) {
	Convey("Given 全部迁移跑到 202607090001", t, func() {
		d := testutils.MigrateTo(t, "202607090001")
		Convey("Then orch_dispatches 存在、orch_tasks 不存在、parent_dispatch_id 列在", func() {
			So(testutils.TableExists(d, "orch_dispatches"), ShouldBeTrue)
			So(testutils.TableExists(d, "orch_tasks"), ShouldBeFalse)
			So(testutils.ColumnExists(d, "orch_dispatches", "parent_dispatch_id"), ShouldBeTrue)
			So(testutils.ColumnExists(d, "orch_dispatches", "parent_task_id"), ShouldBeFalse)
		})
	})
}
```

> 若 `testutils` 无 `MigrateTo/TableExists/ColumnExists` 助手，照 `migrations/202607080002_drop_flow_dag_steps_test.go` 现有断言方式改写（它已跑通同类校验）；先读那个文件对齐 helper 名。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./migrations/ -run TestMigration202607090001 -v`
Expected: FAIL（`migration202607090001` 未定义 / 未注册）

- [ ] **Step 3: 写迁移实现**

`migrations/202607090001_rename_orch_tasks_to_dispatches.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607090001 执行节点改名:orch_tasks → orch_dispatches。
// 语义 = 一次派发(brief 派给某 agent,绑一条子会话)。腾出 orch_tasks 名字给待办清单(202607090002)。
// SQLite 无 ALTER INDEX RENAME,故索引 DROP+CREATE 换名,避免与新 orch_tasks 索引撞名。
func migration202607090001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607090001",
		Migrate: func(tx *gorm.DB) error {
			stmts := []string{
				`ALTER TABLE orch_tasks RENAME TO orch_dispatches`,
				`ALTER TABLE orch_dispatches RENAME COLUMN parent_task_id TO parent_dispatch_id`,
				`DROP INDEX IF EXISTS idx_orch_tasks_run`,
				`DROP INDEX IF EXISTS idx_orch_tasks_session`,
				`CREATE INDEX IF NOT EXISTS idx_orch_dispatches_run ON orch_dispatches(run_id, status)`,
				`CREATE INDEX IF NOT EXISTS idx_orch_dispatches_session ON orch_dispatches(session_id)`,
			}
			for _, s := range stmts {
				if err := tx.Exec(s).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			stmts := []string{
				`DROP INDEX IF EXISTS idx_orch_dispatches_run`,
				`DROP INDEX IF EXISTS idx_orch_dispatches_session`,
				`ALTER TABLE orch_dispatches RENAME COLUMN parent_dispatch_id TO parent_task_id`,
				`ALTER TABLE orch_dispatches RENAME TO orch_tasks`,
				`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, status)`,
				`CREATE INDEX IF NOT EXISTS idx_orch_tasks_session ON orch_tasks(session_id)`,
			}
			for _, s := range stmts {
				if err := tx.Exec(s).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}
```

在 `migrations/migrations.go` 的 `migrationList()` 末尾追加：

```go
		migration202607090001(), // 执行节点改名:orch_tasks → orch_dispatches
```

- [ ] **Step 4: 跑迁移测试确认通过**

Run: `go test ./migrations/ -run TestMigration202607090001 -v`
Expected: PASS

- [ ] **Step 5: 实体改名 `Task` → `Dispatch`**

`git mv internal/model/entity/orch_entity/task.go internal/model/entity/orch_entity/dispatch.go`，改名内容：`Task`→`Dispatch`、`TableName()` 返回 `"orch_dispatches"`、字段 `ParentTaskID`→`ParentDispatchID`（tag `column:parent_dispatch_id`）、Kind 常量 `TaskKind*`→`DispatchKind*`、状态常量 `Task*`→`Dispatch*`。同步 `git mv task_test.go dispatch_test.go` 并改名内标识符。

- [ ] **Step 6: 仓储改名 `TaskRepo` → `DispatchRepo`**

`git mv internal/repository/orch_repo/task.go internal/repository/orch_repo/dispatch.go`：`TaskRepo`→`DispatchRepo`、`defaultTask`→`defaultDispatch`、`Task()/RegisterTask()/NewTask()`→`Dispatch()/RegisterDispatch()/NewDispatch()`、`taskRepo`→`dispatchRepo`、`*orch_entity.Task`→`*orch_entity.Dispatch`、`//go:generate` 的 `-source` 改 `dispatch.go`、`-destination mock_orch_repo/mock_dispatch.go`。`CountByRunAgent` 内 `orch_entity.TaskKindDispatch`→`DispatchKindDispatch`。`git mv task_test.go dispatch_test.go`，sqlmock 期望 SQL 表名 `orch_tasks`→`orch_dispatches`。删除旧 `mock_orch_repo/mock_task.go`。

- [ ] **Step 7: service 全量改名**

`internal/service/orch_svc/` 下：`orch_repo.TaskRepo`→`DispatchRepo`、svc 字段 `tasks`→`dispatches`、`s.tasks`→`s.dispatches`、`enqueue func(... task *orch_entity.Task ...)`→`*orch_entity.Dispatch`、所有 `orch_entity.Task*` 常量/类型改名、`RegisterDeps` 形参 `tasks orch_repo.TaskRepo`→`dispatches orch_repo.DispatchRepo`。`create.go` 的 `RunDetail.RootTask *orch_entity.Task`→`*orch_entity.Dispatch`；`query.go` 的 `Tasks []*orch_entity.Task`→`Dispatches []*orch_entity.Dispatch`（LoadRun 返回结构，字段名一并改）。`status.go` 的 `statusRow` json tag `task_id`→`dispatch_id`、`parent_task_id`→`parent_dispatch_id`，注释「任务树」→「派发树」。同步全部 `*_test.go`。

> 用编辑器全局改名或 `gofmt -r 'orch_entity.Task -> orch_entity.Dispatch'` 分批做；每改一批 `go build ./internal/...` 收敛。

- [ ] **Step 8: DTO 改名（`internal/app/orch.go`）**

`TaskDTO`→`DispatchDTO`（字段 `ParentTaskID`→`ParentDispatchID`，json `parentTaskId`→`parentDispatchId`）、`toTaskDTO`→`toDispatchDTO`、`RunDetailDTO.Tasks []*TaskDTO`→`Dispatches []*DispatchDTO`（json `tasks`→`dispatches`）、`RunCreate`/`RunLoad` 内 `Tasks:`→`Dispatches:`、遍历 `d.Tasks`→`d.Dispatches`。同步 `orch_test.go`。

- [ ] **Step 9: wiring 改名**

`internal/bootstrap/cago.go:120` `orch_repo.RegisterTask(orch_repo.NewTask())`→`orch_repo.RegisterDispatch(orch_repo.NewDispatch())`；`internal/app/app.go:234` `RegisterDeps(... orch_repo.Task() ...)`→`orch_repo.Dispatch()`。`mock_orch_svc` 里 RegisterDeps mock 形参随之改。

- [ ] **Step 10: 重生 mock + 编译**

Run: `make mock && go build ./internal/... ./migrations/...`
Expected: 编译通过

- [ ] **Step 11: 跑后端测试确认全绿**

Run: `make test-backend`
Expected: PASS（前端 `make generate` 与前端改名留到 Phase 4；此刻仅后端绿）

- [ ] **Step 12: 提交**

```bash
gofmt -l internal migrations | grep . && echo "需 gofmt -w" || true
git add -A internal/model/entity/orch_entity internal/repository/orch_repo internal/service/orch_svc internal/app migrations
git commit internal/model/entity/orch_entity internal/repository/orch_repo internal/service/orch_svc internal/app/orch.go internal/app/app.go internal/bootstrap/cago.go migrations -m "♻️ orch: 执行节点全量改名 task → dispatch(表/实体/repo/service/DTO + 迁移rename)"
```

---

## Phase 2 — 执行侧工具清理

### Task 2: 删除 `cancel` 工具

**Files:**
- Delete: `internal/service/orch_svc/cancel.go`、`cancel_test.go`
- Modify: `internal/service/orch_svc/mcp.go`、`mcp_test.go`
- Modify: `internal/pkg/agenttool/agenttool.go:38`

**Interfaces:**
- Consumes: Task 1 的改名后类型
- Produces: 编排工具集不再含 `cancel`

- [ ] **Step 1: 写 mcp 层「无 cancel」的失败测试**

在 `internal/service/orch_svc/mcp_test.go` 追加（**只加你自己的测试函数，勿动并发会话已 staged 的既有改动**）：

```go
func TestOrchToolSchemas_NoCancel(t *testing.T) {
	for _, s := range orchToolSchemas() {
		if m, ok := s.(map[string]any); ok && m["name"] == "cancel" {
			t.Fatalf("cancel 工具应已移除")
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/orch_svc/ -run TestOrchToolSchemas_NoCancel -v`
Expected: FAIL（cancel 仍在 schema）

- [ ] **Step 3: 移除 cancel**

- `mcp.go`：删 `orchToolSchemas()` 里 `cancel` 那个 map；删 `dispatchTool` 的 `case "cancel"`；删 `handleCancel` 函数。
- 删文件 `cancel.go`（`CancelTask` 及级联逻辑）与 `cancel_test.go`：`git rm internal/service/orch_svc/cancel.go internal/service/orch_svc/cancel_test.go`。
- `agenttool.go:38`：从 ToolNames 去掉 `"cancel"`。
- 若 `AbortTurn`（deps.go）仅被 cancel 使用则一并删；否则保留（`grep -rn AbortTurn internal/service/orch_svc` 确认）。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/orch_svc/ ./internal/pkg/agenttool/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/mcp.go internal/service/orch_svc/mcp_test.go internal/pkg/agenttool/agenttool.go -m "🔥 orch: 删除 cancel 工具"
git rm internal/service/orch_svc/cancel.go internal/service/orch_svc/cancel_test.go && git commit internal/service/orch_svc/cancel.go internal/service/orch_svc/cancel_test.go -m "🔥 orch: 删 CancelTask service"
```

> 若 `mcp.go` 有并发会话未提交改动，`git commit <file>` 只会提交当前工作树该文件的全部内容——先 `git diff internal/service/orch_svc/mcp.go` 确认没把别人的门控改动一起带走；如混入，改用 `git add -p` 精确挑选后 `git commit`（不带 pathspec 覆盖）。

### Task 3: 拆除死参 `isolate`

**Files:**
- Modify: `internal/service/orch_svc/mcp.go`（dispatch schema + handleDispatch）
- Modify: `internal/service/orch_svc/dispatch.go`（`Dispatch` 签名）
- Modify: `internal/service/orch_svc/deps.go`（删 `EnsureOrchSessionInput.Isolate`）
- Modify: `internal/app/orch_adapter.go`（删 Isolate 注释/透传）
- Modify: `dispatch_test.go`、`mcp_test.go`、`mock_orch_svc/mock_deps.go`

**Interfaces:**
- Produces: `Dispatch(ctx, parentSessionID int64, agentName, brief string) (int64, error)`（去掉 `isolate bool`）；`EnsureOrchSessionInput` 无 `Isolate` 字段

- [ ] **Step 1: 改 dispatch 测试删 isolate 入参**

`dispatch_test.go`：把对 `s.Dispatch(...)` 的调用去掉末位 `isolate` 实参；`EnsureOrchSession` mock 的 `DoAndReturn` 里对 `in.Isolate` 的断言删除。新增一条断言 `dispatch` schema 不含 `isolate` property（在 mcp_test.go）：

```go
func TestDispatchSchema_NoIsolate(t *testing.T) {
	for _, s := range orchToolSchemas() {
		m := s.(map[string]any)
		if m["name"] != "dispatch" {
			continue
		}
		props := m["inputSchema"].(map[string]any)["properties"].(map[string]any)
		if _, ok := props["isolate"]; ok {
			t.Fatalf("dispatch 不应再暴露 isolate")
		}
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/orch_svc/ -run 'TestDispatch|TestDispatchSchema_NoIsolate' -v`
Expected: FAIL（编译错误：签名不匹配 / isolate 仍在）

- [ ] **Step 3: 拆 isolate**

- `dispatch.go`：`Dispatch` 去掉 `isolate bool` 形参；`EnsureOrchSessionInput{...}` 去掉 `Isolate: isolate`。
- `deps.go`：`EnsureOrchSessionInput` 删 `Isolate bool` 字段及注释。
- `orch_adapter.go`：删「Isolate/worktree 暂不支持」注释与相关传参。
- `mcp.go`：`handleDispatch` 的 `p` 结构删 `Isolate bool`；调用 `m.svc.Dispatch(r.Context(), ref.sessionID, p.Agent, p.Brief)`；`orchToolSchemas()` 的 dispatch properties 删 `isolate`。
- `make mock` 重生 `mock_deps.go`。

- [ ] **Step 4: 跑测试确认通过**

Run: `make mock && go test ./internal/service/orch_svc/ ./internal/app/ -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/mcp.go internal/service/orch_svc/dispatch.go internal/service/orch_svc/deps.go internal/service/orch_svc/dispatch_test.go internal/service/orch_svc/mcp_test.go internal/service/orch_svc/mock_orch_svc/mock_deps.go internal/app/orch_adapter.go -m "🔥 orch: 拆除 dispatch 死参 isolate(worktree 未实现)"
```

### Task 4: `agent_list` 补在跑计数

**Files:**
- Modify: `internal/repository/orch_repo/dispatch.go`（新增 `CountActiveByRunAgent`）+ `dispatch_test.go`
- Modify: `internal/repository/orch_repo/mock_orch_repo/mock_dispatch.go`（`make mock`）
- Modify: `internal/service/orch_svc/query.go`（新增带负载的花名册方法）+ `query_test.go`
- Modify: `internal/service/orch_svc/mcp.go`（`agentListItem.Running` + handler 富化）+ `mcp_test.go`

**Interfaces:**
- Produces:
  - `orch_repo.DispatchRepo.CountActiveByRunAgent(ctx, runID, agentID int64) (int64, error)`（仅统计 `kind='dispatch'` 且非终态）
  - `agentListItem.Running int`（json `running`）

- [ ] **Step 1: 写 repo `CountActiveByRunAgent` 失败测试**

`orch_repo/dispatch_test.go` 追加 sqlmock 测试：期望 SQL 含 `run_id = ? AND agent_id = ? AND kind = ?` 且 `status NOT IN (?,?,?)`（done/canceled/error），返回计数 2。

```go
func TestDispatchRepo_CountActiveByRunAgent(t *testing.T) {
	// 参照本文件既有 CountByRunAgent 测试骨架:testutils.Database(t) + sqlmock ExpectQuery
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/repository/orch_repo/ -run TestDispatchRepo_CountActiveByRunAgent -v`
Expected: FAIL（方法未定义）

- [ ] **Step 3: 实现 repo 方法 + 接口**

`orch_repo/dispatch.go`：接口加 `CountActiveByRunAgent(ctx context.Context, runID, agentID int64) (int64, error)`；实现：

```go
func (r *dispatchRepo) CountActiveByRunAgent(ctx context.Context, runID, agentID int64) (int64, error) {
	var n int64
	err := db.Ctx(ctx).Model(&orch_entity.Dispatch{}).
		Where("run_id = ? AND agent_id = ? AND kind = ?", runID, agentID, orch_entity.DispatchKindDispatch).
		Where("status NOT IN (?)", []string{
			orch_entity.DispatchDone, orch_entity.DispatchCanceled, orch_entity.DispatchError,
		}).
		Count(&n).Error
	return n, err
}
```

- [ ] **Step 4: repo 测试通过；写 service 富化花名册的失败测试**

`make mock`；在 `query_test.go` 加：`ListAllowedAgentsWithLoad(ctx, sessionID)` 返回每个 agent 附 `Running` 计数（mock `CountActiveByRunAgent` 返回固定值，断言映射正确）。定义返回类型：

```go
// query.go
type AgentWithLoad struct {
	Agent   *agent_entity.Agent
	Running int
}
func (s *orchSvc) ListAllowedAgentsWithLoad(ctx context.Context, sessionID int64) ([]AgentWithLoad, error)
```

实现：`FindBySession(sessionID)` 拿 RunID → 复用 `ListAllowedAgents` → 对每个 agent `dispatches.CountActiveByRunAgent(runID, agent.ID)`。

- [ ] **Step 5: 跑 service 测试确认失败→实现→通过**

Run: `go test ./internal/service/orch_svc/ -run TestListAllowedAgentsWithLoad -v`
先 FAIL（未定义）→ 实现 → PASS。

- [ ] **Step 6: mcp `agent_list` 用富化方法 + Running 字段**

`mcp.go`：`agentListItem` 加 `Running int json:"running"`；`handleAgentList` 改调 `m.svc.ListAllowedAgentsWithLoad(r.Context(), ref.sessionID)`，映射 `Running`。`agent_list` schema 描述补「含每个 agent 当前在跑的派发数 running」。mcp_test 断言返回项含 `running`。

- [ ] **Step 7: 跑测试 + 提交**

Run: `go test ./internal/repository/orch_repo/ ./internal/service/orch_svc/ -v`
Expected: PASS

```bash
git commit internal/repository/orch_repo/dispatch.go internal/repository/orch_repo/dispatch_test.go internal/repository/orch_repo/mock_orch_repo/mock_dispatch.go internal/service/orch_svc/query.go internal/service/orch_svc/query_test.go internal/service/orch_svc/mcp.go internal/service/orch_svc/mcp_test.go -m "✨ orch: agent_list 补每个 agent 在跑派发计数 running"
```

---

## Phase 3 — 待办清单子系统 `task`（后端）

### Task 5: 迁移 2 — 新建 `orch_tasks` 清单表

**Files:**
- Create: `migrations/202607090002_create_orch_tasks_checklist.go` + `_test.go`
- Modify: `migrations/migrations.go`

**Interfaces:**
- Produces: 表 `orch_tasks`（列见下），必须在 `202607090001`（rename）之后

- [ ] **Step 1: 写建表迁移失败测试**

`202607090002_create_orch_tasks_checklist_test.go`：迁移到 `202607090002` 后断言 `orch_tasks` 存在且含列 `text`/`status`/`assignee_agent_id`；同时 `orch_dispatches` 仍在（两表并存）。参照 202607080002 的 helper 风格。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./migrations/ -run TestMigration202607090002 -v`
Expected: FAIL

- [ ] **Step 3: 写建表迁移**

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607090002 待办清单表(与派发树零联动的 TodoWrite 式协作白板)。
// 复用被 202607090001 腾空的 orch_tasks 名字,但列结构全新。
func migration202607090002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607090002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orch_tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				run_id INTEGER NOT NULL DEFAULT 0,
				seq INTEGER NOT NULL DEFAULT 0,
				text TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending',
				assignee_agent_id INTEGER NOT NULL DEFAULT 0,
				created_by_agent_id INTEGER NOT NULL DEFAULT 0,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, seq)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS orch_tasks`).Error
		},
	}
}
```

`migrations.go` 末尾追加：`migration202607090002(), // 待办清单表 orch_tasks(新语义)`。

- [ ] **Step 4: 跑测试确认通过 + 提交**

Run: `go test ./migrations/ -run 'TestMigration20260709000' -v`
Expected: PASS

```bash
git commit migrations/202607090002_create_orch_tasks_checklist.go migrations/202607090002_create_orch_tasks_checklist_test.go migrations/migrations.go -m "✨ migrate: 202607090002 待办清单表 orch_tasks(新语义)"
```

### Task 6: 清单实体 + 仓储

**Files:**
- Create: `internal/model/entity/orch_entity/task.go` + `task_test.go`
- Create: `internal/repository/orch_repo/task.go` + `task_test.go`

**Interfaces:**
- Produces:
  - `orch_entity.Task{ID, RunID, Seq, Text, Status, AssigneeAgentID, CreatedByAgentID, Createtime, Updatetime}`，`TableName()="orch_tasks"`
  - 常量 `orch_entity.TaskStatusPending="pending"` / `TaskStatusInProgress="in_progress"` / `TaskStatusDone="done"`；`func ValidTaskStatus(s string) bool`
  - `orch_repo.TaskRepo{ Create, Update, Find, ListByRun, MaxSeq }`；accessor `Task()/RegisterTask(impl)/NewTask()`

- [ ] **Step 1: 写实体校验失败测试**

`orch_entity/task_test.go`：`ValidTaskStatus("pending")==true`、`ValidTaskStatus("bogus")==false`、`(&Task{}).TableName()=="orch_tasks"`。

- [ ] **Step 2: 跑确认失败 → 写实体 → 通过**

```go
package orch_entity

const (
	TaskStatusPending    = "pending"
	TaskStatusInProgress = "in_progress"
	TaskStatusDone       = "done"
)

// Task 编排待办清单条目(与派发节点 Dispatch 无关的协作白板)。
type Task struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	RunID            int64  `gorm:"column:run_id;type:bigint;not null;default:0"`
	Seq              int    `gorm:"column:seq;type:int;not null;default:0"`
	Text             string `gorm:"column:text;type:text;not null;default:''"`
	Status           string `gorm:"column:status;type:text;not null;default:'pending'"`
	AssigneeAgentID  int64  `gorm:"column:assignee_agent_id;type:bigint;not null;default:0"`
	CreatedByAgentID int64  `gorm:"column:created_by_agent_id;type:bigint;not null;default:0"`
	Createtime       int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime       int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Task) TableName() string { return "orch_tasks" }

func ValidTaskStatus(s string) bool {
	return s == TaskStatusPending || s == TaskStatusInProgress || s == TaskStatusDone
}
```

Run: `go test ./internal/model/entity/orch_entity/ -v` → PASS

- [ ] **Step 3: 写仓储 sqlmock 失败测试**

`orch_repo/task_test.go`：`Create` 写入并回填 ID/时间；`ListByRun` 按 `run_id=?` `ORDER BY seq ASC`；`MaxSeq` 返回 `COALESCE(MAX(seq),0)`。骨架照 `dispatch_test.go`（原 task_test）现有 sqlmock 用法。

- [ ] **Step 4: 跑确认失败 → 写仓储 → 通过**

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

// TaskRepo 编排待办清单仓储(表 orch_tasks;与 DispatchRepo 无关)。
type TaskRepo interface {
	Create(ctx context.Context, t *orch_entity.Task) error
	Update(ctx context.Context, t *orch_entity.Task) error
	Find(ctx context.Context, id int64) (*orch_entity.Task, error)
	ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error)
	MaxSeq(ctx context.Context, runID int64) (int, error)
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

func (r *taskRepo) ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error) {
	var rows []*orch_entity.Task
	err := db.Ctx(ctx).Where("run_id = ?", runID).Order("seq ASC").Find(&rows).Error
	return rows, err
}

func (r *taskRepo) MaxSeq(ctx context.Context, runID int64) (int, error) {
	var max struct{ M int }
	err := db.Ctx(ctx).Model(&orch_entity.Task{}).
		Select("COALESCE(MAX(seq),0) AS m").Where("run_id = ?", runID).Scan(&max).Error
	return max.M, err
}
```

Run: `make mock && go test ./internal/repository/orch_repo/ -v` → PASS

- [ ] **Step 5: 提交**

```bash
git commit internal/model/entity/orch_entity/task.go internal/model/entity/orch_entity/task_test.go internal/repository/orch_repo/task.go internal/repository/orch_repo/task_test.go internal/repository/orch_repo/mock_orch_repo/mock_task.go -m "✨ orch: 待办清单实体 + 仓储 TaskRepo"
```

### Task 7: 清单 service（`TaskList`/`TaskAdd`/`TaskUpdate`）

**Files:**
- Create: `internal/service/orch_svc/todo.go` + `todo_test.go`
- Modify: `internal/service/orch_svc/orch.go`（svc 加 `todos orch_repo.TaskRepo` 字段 + RegisterDeps 参数）、`deps.go`（如需）
- Modify: `internal/app/app.go`、`internal/bootstrap/cago.go`（wiring）

**Interfaces:**
- Consumes: `orch_repo.TaskRepo`（Task 6）、`s.dispatches.FindBySession`（拿 RunID）、`s.sessionInRun`
- Produces:
  - `func (s *orchSvc) TaskList(ctx, sessionID int64) (string, error)`（JSON 数组文本）
  - `func (s *orchSvc) TaskAdd(ctx, sessionID, agentID int64, text string) (int64, error)`
  - `func (s *orchSvc) TaskUpdate(ctx, sessionID, agentID, taskID int64, status string, claim bool) error`（`status==""` 表示不改状态）

- [ ] **Step 1: 写 service 失败测试（mockgen 注入）**

`todo_test.go` 覆盖：
- `TaskAdd`：seq=MaxSeq+1、created_by=agentID、status=pending，返回新 id；
- `TaskUpdate` 改 status：非法 status 返回错误、合法则写回；`claim=true` 时 assignee=agentID；
- `TaskUpdate` 越 Run（task.RunID != 调用者 RunID）返回 `errForeignTask`；
- `TaskList` 渲染 JSON 含 id/seq/text/status/assignee。
注入 `orch_repo.RegisterTask(mockTask)` + `RegisterDispatch(mockDispatch)`（FindBySession 返回带 RunID 的 Dispatch）。

- [ ] **Step 2: 跑确认失败**

Run: `go test ./internal/service/orch_svc/ -run 'TestTaskAdd|TestTaskUpdate|TestTaskList' -v`
Expected: FAIL（未定义）

- [ ] **Step 3: 写 service**

```go
package orch_svc

import (
	"context"
	"encoding/json"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// runIDForSession 复用派发绑定推出调用者所在 Run(清单与派发共用 Run 作用域)。
func (s *orchSvc) runIDForSession(ctx context.Context, sessionID int64) (int64, error) {
	d, err := s.dispatches.FindBySession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	if d == nil {
		return 0, errRunNotActive
	}
	return d.RunID, nil
}

func (s *orchSvc) TaskAdd(ctx context.Context, sessionID, agentID int64, text string) (int64, error) {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return 0, err
	}
	max, err := s.todos.MaxSeq(ctx, runID)
	if err != nil {
		return 0, err
	}
	t := &orch_entity.Task{
		RunID: runID, Seq: max + 1, Text: text,
		Status: orch_entity.TaskStatusPending, CreatedByAgentID: agentID,
	}
	if err := s.todos.Create(ctx, t); err != nil {
		return 0, err
	}
	s.emitRunUpdated(ctx, runID)
	return t.ID, nil
}

func (s *orchSvc) TaskUpdate(ctx context.Context, sessionID, agentID, taskID int64, status string, claim bool) error {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return err
	}
	t, err := s.todos.Find(ctx, taskID)
	if err != nil {
		return err
	}
	if t == nil || t.RunID != runID {
		return errForeignTask
	}
	if status != "" {
		if !orch_entity.ValidTaskStatus(status) {
			return errInvalidTaskStatus
		}
		t.Status = status
	}
	if claim {
		t.AssigneeAgentID = agentID
	}
	if err := s.todos.Update(ctx, t); err != nil {
		return err
	}
	s.emitRunUpdated(ctx, runID)
	return nil
}

type taskRow struct {
	ID       int64  `json:"task_id"`
	Seq      int    `json:"seq"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Assignee int64  `json:"assignee_agent_id,omitempty"`
}

func (s *orchSvc) TaskList(ctx context.Context, sessionID int64) (string, error) {
	runID, err := s.runIDForSession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	rows, err := s.todos.ListByRun(ctx, runID)
	if err != nil {
		return "", err
	}
	out := make([]taskRow, 0, len(rows))
	for _, t := range rows {
		out = append(out, taskRow{ID: t.ID, Seq: t.Seq, Text: t.Text, Status: t.Status, Assignee: t.AssigneeAgentID})
	}
	b, _ := json.Marshal(out)
	return string(b), nil
}
```

在 `orch.go` 加错误 `errInvalidTaskStatus = errors.New("orch: invalid task status")`；svc 结构加 `todos orch_repo.TaskRepo`；`RegisterDeps` 增末位形参 `todos orch_repo.TaskRepo` 并赋值。

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/service/orch_svc/ -v`
Expected: PASS

- [ ] **Step 5: wiring**

`bootstrap/cago.go`：加 `orch_repo.RegisterTask(orch_repo.NewTask())`；`app.go:234` `RegisterDeps(...)` 末位补 `orch_repo.Task()`。`make mock` 重生 RegisterDeps mock。

- [ ] **Step 6: 编译 + 提交**

Run: `make mock && make test-backend`
Expected: PASS

```bash
git commit internal/service/orch_svc/todo.go internal/service/orch_svc/todo_test.go internal/service/orch_svc/orch.go internal/service/orch_svc/mock_orch_svc internal/app/app.go internal/bootstrap/cago.go -m "✨ orch: 待办清单 service(TaskList/TaskAdd/TaskUpdate)"
```

### Task 8: 清单 MCP 工具（`task_list`/`task_add`/`task_update`）

**Files:**
- Modify: `internal/service/orch_svc/mcp.go`（schema + 3 handler + dispatchTool case）+ `mcp_test.go`
- Modify: `internal/pkg/agenttool/agenttool.go:38`（ToolNames 加 3 个）

**Interfaces:**
- Consumes: Task 7 的 service 方法；`ref.agentID`/`ref.sessionID`
- Produces: 编排工具集含 `task_list`/`task_add`/`task_update`

- [ ] **Step 1: 写工具存在 + 分发的失败测试**

`mcp_test.go` 追加：`orchToolSchemas()` 含三个新工具；调用 `task_add`（args `{"text":"写测试"}`）经 `dispatchTool` 命中 `handleTaskAdd`（service 用 mock，断言返回文本含 task_id）。

- [ ] **Step 2: 跑确认失败**

Run: `go test ./internal/service/orch_svc/ -run TestTask -v`
Expected: FAIL

- [ ] **Step 3: 加 schema + handler + case**

`orchToolSchemas()` 追加：

```go
map[string]any{
	"name":        "task_list",
	"description": "读取本次编排的待办清单(所有 agent 共享的协作白板;与派发树无关)。返回每条 task_id/seq/文本/状态/认领人。",
	"inputSchema": map[string]any{"type": "object", "properties": map[string]any{}},
},
map[string]any{
	"name":        "task_add",
	"description": "往待办清单加一条(一句话)。返回 task_id。清单纯组织思路、给人看进度,不触发任何执行。",
	"inputSchema": map[string]any{
		"type": "object", "required": []string{"text"},
		"properties": map[string]any{"text": map[string]any{"type": "string"}},
	},
},
map[string]any{
	"name":        "task_update",
	"description": "更新某待办:改状态(pending/in_progress/done)或认领(claim=true 把自己设为负责人)。至少给一个。",
	"inputSchema": map[string]any{
		"type": "object", "required": []string{"task_id"},
		"properties": map[string]any{
			"task_id": map[string]any{"type": "integer"},
			"status":  map[string]any{"type": "string", "enum": []string{"pending", "in_progress", "done"}},
			"claim":   map[string]any{"type": "boolean"},
		},
	},
},
```

`dispatchTool` 加：

```go
case "task_list":
	m.handleTaskList(w, r, id, ref)
case "task_add":
	m.handleTaskAdd(w, r, id, ref, args)
case "task_update":
	m.handleTaskUpdate(w, r, id, ref, args)
```

handler：

```go
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
```

`agenttool.go:38` ToolNames 追加 `"task_list", "task_add", "task_update"`。

- [ ] **Step 4: 跑测试 + 全后端闸门**

Run: `go test ./internal/service/orch_svc/ ./internal/pkg/agenttool/ -v && make test-backend`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/mcp.go internal/service/orch_svc/mcp_test.go internal/pkg/agenttool/agenttool.go -m "✨ orch: 待办清单 MCP 工具 task_list/task_add/task_update"
```

---

## Phase 4 — 前端切换

### Task 9: RunDetail 返回清单 + 重生绑定 + 派发字段改名

**Files:**
- Modify: `internal/app/orch.go`（`RunDetailDTO` 加清单字段 + `RunLoad` 拉清单）+ `orch_test.go`
- Regenerate: `frontend/wailsjs/**`（`make generate`）
- Modify: `frontend/src/components/agentre/orchestration/structure-graph.tsx`、`index.tsx`、`stores/orch-run-store.ts`
- Delete: `frontend/src/components/agentre/orchestration/task-board.tsx`、`__tests__/task-board.test.tsx`

**Interfaces:**
- Produces:
  - `app.TaskItemDTO{ID,RunID,Seq,Text,Status,AssigneeAgentId,Createtime,Updatetime}`（json `id/runId/seq/text/status/assigneeAgentId/...`）
  - `app.RunDetailDTO.Tasks []*TaskItemDTO`（json `tasks`）— 清单；`Dispatches []*DispatchDTO`（json `dispatches`）— 派发（Task 1 已改名）

- [ ] **Step 1: 写 RunLoad 返回清单的失败测试**

`orch_test.go`：`RunLoad` 返回的 `RunDetailDTO.Tasks` 含 service 清单条目（mock `orch_svc` 层或经 LoadRun 扩展）。需在 `orch_svc` 加 `LoadRun` 一并返回清单，或新增 `App` 侧取清单方法。推荐：`orch_svc` 新增 `ListTasks(ctx, runID) ([]*orch_entity.Task, error)`（直接 `todos.ListByRun`），`RunLoad` 调用后填 DTO。

- [ ] **Step 2: 跑确认失败 → 实现**

`orch.go`：

```go
type TaskItemDTO struct {
	ID              int64  `json:"id"`
	RunID           int64  `json:"runId"`
	Seq             int    `json:"seq"`
	Text            string `json:"text"`
	Status          string `json:"status"`
	AssigneeAgentID int64  `json:"assigneeAgentId"`
	Createtime      int64  `json:"createtime"`
	Updatetime      int64  `json:"updatetime"`
}
// RunDetailDTO 追加: Tasks []*TaskItemDTO `json:"tasks"`
```

`RunLoad` 末尾：`d2, _ := orch_svc.Default().ListTasks(a.ctx, id)` → 映射进 `Tasks`。`orch_svc` 加：

```go
func (s *orchSvc) ListTasks(ctx context.Context, runID int64) ([]*orch_entity.Task, error) {
	return s.todos.ListByRun(ctx, runID)
}
```

Run: `go test ./internal/app/ ./internal/service/orch_svc/ -v` → PASS

- [ ] **Step 3: 重生绑定**

Run: `make generate`
Expected: `frontend/wailsjs/go/models.ts` 出现 `DispatchDTO`、`TaskItemDTO`；`RunDetailDTO` 有 `dispatches` + `tasks`

- [ ] **Step 4: 前端派发字段改名 + 删 TaskBoard**

- `structure-graph.tsx`：数据源 `detail.tasks`→`detail.dispatches`；节点属性 `task.taskId`→`dispatch.dispatchId`、`parentTaskId`→`parentDispatchId` 等（跟随生成类型）。
- `orch-run-store.ts`：`tasks` 相关字段/选择器改 `dispatches`。
- `index.tsx`：`detail.tasks`→`detail.dispatches`；右栏回落分支暂时留空占位（下个 Task 换 TaskList）。
- `git rm frontend/src/components/agentre/orchestration/task-board.tsx frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx`。
- 更新 `structure-graph.test.tsx` 等随字段改名。

- [ ] **Step 5: 前端编译/测试**

Run: `cd frontend && pnpm test -- structure-graph run-store index`
Expected: PASS（TaskBoard 相关测试已删）

- [ ] **Step 6: 提交**

```bash
git commit internal/app/orch.go internal/app/orch_test.go internal/service/orch_svc/query.go frontend/wailsjs frontend/src/components/agentre/orchestration/structure-graph.tsx frontend/src/components/agentre/orchestration/index.tsx frontend/src/stores/orch-run-store.ts -m "♻️ orch(fe): 派发字段 task→dispatch + RunDetail 返回清单 + 删 TaskBoard"
git rm frontend/src/components/agentre/orchestration/task-board.tsx frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx && git commit frontend/src/components/agentre/orchestration/task-board.tsx frontend/src/components/agentre/orchestration/__tests__/task-board.test.tsx -m "🔥 orch(fe): 删除 TaskBoard(导航归 StructureGraph)"
```

### Task 10: 新 TaskList 只读清单组件

**Files:**
- Create: `frontend/src/components/agentre/orchestration/task-list.tsx` + `__tests__/task-list.test.tsx`
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`（右栏回落用 TaskList）
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

**Interfaces:**
- Consumes: `app.RunDetailDTO.Tasks`（Task 9）

- [ ] **Step 1: 写组件失败测试**

`__tests__/task-list.test.tsx`：给 `detail.tasks` 三条（pending/in_progress/done），断言渲染三行文本、顶部进度「1/3」、done 行有对应状态图标 testid；空清单显示 `orchestration.tasks.empty` 文案；根容器有 `data-selectable-text="true"`。参照 `task-board.test.tsx`（已删，从 git 历史取骨架）与 `reference_frontend_wails_runtime_test_mock`（若组件间接 import runtime 需 per-file `vi.mock`）。

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- task-list`
Expected: FAIL（组件不存在）

- [ ] **Step 3: 写组件**

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";
import { Check, Circle, Loader } from "lucide-react";
import { cn } from "@/lib/utils";
import type { app } from "../../../../wailsjs/go/models";

function StatusIcon({ status }: { status: string }) {
  if (status === "done")
    return <Check data-testid={`task-status-${status}`} className="size-3.5 shrink-0 text-status-running" strokeWidth={2.5} />;
  if (status === "in_progress")
    return <Loader data-testid={`task-status-${status}`} className="size-3.5 shrink-0 text-status-running motion-safe:animate-spin" strokeWidth={2.5} />;
  return <Circle data-testid={`task-status-${status}`} className="size-3.5 shrink-0 text-status-idle" strokeWidth={2.5} />;
}

export function TaskList({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const tasks = React.useMemo(() => detail.tasks ?? [], [detail.tasks]);
  const doneCount = React.useMemo(() => tasks.filter((t) => t.status === "done").length, [tasks]);

  return (
    <div className="flex h-full flex-col bg-sidebar" data-selectable-text="true">
      <div className="flex shrink-0 items-center gap-2 border-b border-border bg-card px-[14px] py-3">
        <span className="font-sans text-[13px] font-semibold text-foreground">
          {t("orchestration.tasks.title")}
        </span>
        <span className="flex-1" />
        <span className="font-mono text-[11px] text-muted-foreground tabular-nums">
          {t("orchestration.tasks.progress", { done: doneCount, total: tasks.length })}
        </span>
      </div>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {tasks.length === 0 ? (
          <p className="p-3 text-center text-xs text-muted-foreground">
            {t("orchestration.tasks.empty")}
          </p>
        ) : (
          <ul className="flex flex-col gap-0.5 p-2">
            {tasks.map((task) => (
              <li
                key={task.id}
                data-testid={`task-row-${task.id}`}
                className="flex items-center gap-2 rounded-md py-[7px] pl-[14px] pr-2"
              >
                <StatusIcon status={task.status} />
                <span className={cn("min-w-0 flex-1 truncate text-[12.5px]", task.status === "done" ? "text-muted-foreground line-through" : "text-foreground")}>
                  {task.text}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}
```

`index.tsx` 右栏回落：`selectedSessionId ? <ConversationPanel .../> : <TaskList detail={detail} />`。

- [ ] **Step 4: i18n 补键（双份）**

`zh-CN/common.json` 的 `orchestration.tasks`：`{"title":"任务清单","empty":"暂无待办","progress":"{{done}}/{{total}} 完成"}`；`en/common.json`：`{"title":"Tasks","empty":"No tasks yet","progress":"{{done}}/{{total}} done"}`。

- [ ] **Step 5: 跑测试确认通过 + i18n 覆盖测试**

Run: `cd frontend && pnpm test -- task-list i18n`
Expected: PASS

- [ ] **Step 6: 全量前端 + 收尾闸门**

Run: `make test-backend && make lint && cd frontend && pnpm test`
Expected: 全 PASS（看真 exit code；`gofmt -l internal migrations` 为空）

- [ ] **Step 7: 提交**

```bash
git commit frontend/src/components/agentre/orchestration/task-list.tsx frontend/src/components/agentre/orchestration/__tests__/task-list.test.tsx frontend/src/components/agentre/orchestration/index.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ orch(fe): 只读任务清单 TaskList 替代任务板"
```

---

## Self-Review

**Spec coverage：**
- 执行侧改名 → Task 1 ✅（迁移 rename + entity/repo/service/DTO/wiring）
- 删 cancel → Task 2 ✅
- 拆 isolate → Task 3 ✅
- agent_list 补 running → Task 4 ✅
- 清单表迁移 → Task 5 ✅
- 清单 entity/repo → Task 6 ✅
- 清单 service → Task 7 ✅
- 清单 3 工具 + 注入名单 → Task 8 ✅
- 前端删 TaskBoard + 派发字段改名 + RunDetail 返回清单 → Task 9 ✅
- 前端 TaskList + i18n → Task 10 ✅
- 迁移次序（先 rename 后建新表）→ Task 1 在 Task 5 之前，且 migrationList 顺序 090001→090002 ✅

**Placeholder scan：** 无 TBD/TODO；新代码均给出完整实现。`task-board.test.tsx` 骨架引用「从 git 历史取」是明确指令（文件将删）。

**Type consistency：**
- 执行侧统一 `Dispatch`/`DispatchRepo`/`DispatchDTO`/`dispatch_id`/`parentDispatchId`；清单侧统一 `Task`/`TaskRepo`（新）/`TaskItemDTO`/`task_id`。两套无交叉。
- `s.dispatches`（派发 repo）与 `s.todos`（清单 repo）字段名不撞；`RegisterDeps` 末位新增 `todos` 参数，Task 1 改签名、Task 7 加参数，前后一致。
- service 方法名 `TaskList/TaskAdd/TaskUpdate` 与 mcp handler `handleTaskList/handleTaskAdd/handleTaskUpdate`、工具名 `task_list/task_add/task_update` 一致。
- `ValidTaskStatus`（entity）在 Task 6 定义、Task 7 使用，名字一致。
