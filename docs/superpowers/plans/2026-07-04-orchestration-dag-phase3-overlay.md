# 编排流程 DAG Phase 3a Implementation Plan（运行时进度 overlay）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把当前 Run 的实时进度叠加到 flow DAG 上——Run 详情页新增「Flow」Tab,用带状态着色的 `FlowGraphView` 展示每个委派任务节点的 pending/running/done/error。只读可视化,不改运行时行为。

**Architecture:** 后端追加两列(`orch_tasks.node_ref`、`orchestration_runs.flow_graph`),`dispatch` 工具可选带 `node` 标(Leader 传流程步骤名 label),建 Run 时快照 graph JSON;DTO 暴露给前端。前端纯函数 `deriveNodeOverlay(graph, tasks)` 按 label 聚合出每节点状态+计数,`FlowGraphView` 加可选 `overlay` prop 着色,新 `RunFlowOverlay` 组件挂到新「Flow」Tab。实时更新复用既有 `orch:run:updated`→`loadRun` refetch,无新事件。

**Tech Stack:** Go 1.26 + cago(gormigrate/gomock/goconvey/testify)+ Wails v2;React 19 + TS + Vitest + Tailwind v4 + shadcn + react-i18next。

## Global Constraints

- **只做 overlay(可视化),不做硬门控。** 不碰 `scheduler.go` 的 dispatch 时序,不阻塞任何 `SendAndForget`。
- **只点亮 task-kind 节点。** leader-kind 节点(See/Break/Wrap)恒为 `neutral`(无状态、dim)。
- **节点↔任务链路 best-effort。** `dispatch` 的 `node` 是**可选**;未打标任务不归属任何节点(照常在 Structure/TaskBoard 可见)。**不阻塞、不校验**打标。
- **按 label 打标 + 匹配。** Leader 传流程步骤名(如 `FE`);前端按 label(trim + 大小写不敏感)把任务匹配到图节点。**不改 `ProjectGraph` 输出**(保 Phase 1「seed content==投影」tripwire)。
- **迁移只追加**,append 到 `migrationList()` 末尾,native SQL DDL,不改既有迁移。
- **不引入新前端依赖**;复用 `flow-graph.ts` 的 `parseFlowGraph`/`layoutFlowGraph` + `FlowGraphView`。
- **所有可见 UI 文案走 `t(...)`** 双 locale(`zh-CN`+`en`);节点 label / 任务 brief / 状态色是动态或样式,不入 i18n。
- **`frontend/wailsjs/` 是 gitignore 生成物**:改 Go DTO 后跑 `make generate` 在磁盘重生 `models.ts`,**不提交** wailsjs。
- **gotcha**:`pnpm exec tsc`/`| tail` 吞退出码(重定向文件 grep `error TS`);`make … | tail` 吞 make 退出码;共享分支 `git commit <files>` 带 pathspec,不 `git add -A`。

---

### Task 1: 迁移 + entity 字段（`orch_tasks.node_ref` + `orchestration_runs.flow_graph`）

**Files:**
- Create: `migrations/202607040002_orch_flow_overlay.go`
- Create: `migrations/202607040002_orch_flow_overlay_test.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾追加）
- Modify: `internal/model/entity/orch_entity/task.go`（加 `NodeRef` 字段）
- Modify: `internal/model/entity/orch_entity/run.go`（加 `FlowGraph` 字段）

**Interfaces:**
- Produces:
  - `orch_entity.Task.NodeRef string`（`gorm:"column:node_ref"`）— 该委派任务对应的流程节点 label,空=未打标。
  - `orch_entity.OrchestrationRun.FlowGraph string`（`gorm:"column:flow_graph"`）— 建 Run 时快照的 graph JSON。

- [ ] **Step 1: Write the failing migration test**

Create `migrations/202607040002_orch_flow_overlay_test.go`:

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040002_AddsFlowOverlayColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// node_ref 列可写可读
	assert.NoError(t, db.Exec(`INSERT INTO orch_tasks (run_id, node_ref) VALUES (1, 'FE')`).Error)
	var nodeRef string
	assert.NoError(t, db.Raw(`SELECT node_ref FROM orch_tasks WHERE run_id = 1`).Scan(&nodeRef).Error)
	assert.Equal(t, "FE", nodeRef)

	// flow_graph 列可写可读
	assert.NoError(t, db.Exec(`INSERT INTO orchestration_runs (goal, flow_graph) VALUES ('g', '{"version":1}')`).Error)
	var fg string
	assert.NoError(t, db.Raw(`SELECT flow_graph FROM orchestration_runs WHERE goal = 'g'`).Scan(&fg).Error)
	assert.Equal(t, `{"version":1}`, fg)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./migrations/ -run TestMigration202607040002 -count=1`
Expected: FAIL — 迁移不存在(`RunMigrations` 不含该 ID)或列不存在(`no such column: node_ref`)。

- [ ] **Step 3: Write the migration**

Create `migrations/202607040002_orch_flow_overlay.go`:

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607040002 运行时进度 overlay：orch_tasks.node_ref（任务对应的流程节点 label，空=未打标）
// + orchestration_runs.flow_graph（建 Run 时快照的 graph JSON）。
func migration202607040002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607040002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE orch_tasks ADD COLUMN node_ref TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE orchestration_runs ADD COLUMN flow_graph TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE orch_tasks DROP COLUMN node_ref`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE orchestration_runs DROP COLUMN flow_graph`).Error
		},
	}
}
```

- [ ] **Step 4: Register the migration**

In `migrations/migrations.go`, append to the end of the `migrationList()` slice (after the `migration202607040001()` line):

```go
		migration202607040002(), // 运行时进度 overlay:orch_tasks.node_ref + orchestration_runs.flow_graph
```

- [ ] **Step 5: Add entity fields**

In `internal/model/entity/orch_entity/task.go`, add `NodeRef` to the `Task` struct (after the `Refs` field):

```go
	Refs         string `gorm:"column:refs;type:text;not null;default:''"`    // JSON：被引用的产物/任务
	NodeRef      string `gorm:"column:node_ref;type:text;not null;default:''"` // 对应的流程节点 label(Leader 打标)；空=未打标
	Createtime   int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
```

In `internal/model/entity/orch_entity/run.go`, add `FlowGraph` to the `OrchestrationRun` struct (after `FlowContent`):

```go
	FlowContent     string `gorm:"column:flow_content;type:text;not null;default:''"` // 创建时快照的流程正文（注入 Leader）
	FlowGraph       string `gorm:"column:flow_graph;type:text;not null;default:''"`   // 创建时快照的 graph JSON（overlay 用）
	Status          string `gorm:"column:status;type:text;not null;default:'pending'"`
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./migrations/ -run TestMigration202607040002 -count=1`
Expected: PASS。

- [ ] **Step 7: Confirm the whole migration suite + build still green**

Run: `go test ./migrations/... ./internal/bootstrap/... -count=1 && go build ./...`
Expected: PASS / build OK（新列不破坏既有迁移与 bootstrap 全量迁移）。

- [ ] **Step 8: Commit**

```bash
git commit migrations/202607040002_orch_flow_overlay.go \
  migrations/202607040002_orch_flow_overlay_test.go \
  migrations/migrations.go \
  internal/model/entity/orch_entity/task.go \
  internal/model/entity/orch_entity/run.go \
  -m "✨ orchestration: overlay 迁移 + entity(orch_tasks.node_ref / orchestration_runs.flow_graph)"
```

---

### Task 2: `WorkflowReader.FlowGraphByID` + adapter + CreateRun 快照 flow_graph

**Files:**
- Modify: `internal/service/orch_svc/deps.go`（`WorkflowReader` 加方法）
- Modify: `internal/service/orch_svc/mock_orch_svc/mock_deps.go`（`make mock` 重生）
- Modify: `internal/app/orch_adapter.go`（`orchWorkflowAdapter` 实现新方法）
- Modify: `internal/service/orch_svc/create.go`（快照 flow_graph）
- Modify: `internal/service/orch_svc/create_test.go`（既有两测补 `FlowGraphByID` 期望 + 新增快照断言测试）

**Interfaces:**
- Consumes: `orch_entity.OrchestrationRun.FlowGraph`（Task 1）；`workflow_entity.Workflow.Graph`（Phase 1 已有）。
- Produces: `WorkflowReader.FlowGraphByID(ctx context.Context, id int64) (string, error)`。

- [ ] **Step 1: Add the interface method**

In `internal/service/orch_svc/deps.go`, extend `WorkflowReader`:

```go
// WorkflowReader 编排对流程库的最小依赖：按 ID 取已投影的流程正文 + 原始 graph JSON(用于 CreateRun 快照)。
type WorkflowReader interface {
	FlowContentByID(ctx context.Context, id int64) (string, error)
	FlowGraphByID(ctx context.Context, id int64) (string, error)
}
```

- [ ] **Step 2: Regenerate the mock**

Run: `make mock`
Expected: `internal/service/orch_svc/mock_orch_svc/mock_deps.go` now has `MockWorkflowReader.FlowGraphByID`. (Verify: `grep -n FlowGraphByID internal/service/orch_svc/mock_orch_svc/mock_deps.go` prints matches.)

- [ ] **Step 3: Implement the adapter**

In `internal/app/orch_adapter.go`, add below `FlowContentByID`:

```go
func (orchWorkflowAdapter) FlowGraphByID(ctx context.Context, id int64) (string, error) {
	w, err := workflow_repo.Workflow().Find(ctx, id)
	if err != nil {
		return "", err
	}
	if w == nil || !w.IsActive() {
		return "", nil
	}
	return w.Graph, nil
}
```

- [ ] **Step 4: Write the failing snapshot test**

In `internal/service/orch_svc/create_test.go`, add a new test (mirrors `TestCreateRun_LibraryModeSnapshotsFlowContent`):

```go
func TestCreateRun_LibraryModeSnapshotsFlowGraph(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	wf := mock_orch_svc.NewMockWorkflowReader(ctrl)

	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().RegisterWorkflowReader(wf)
	t.Cleanup(func() { orch_svc.Default().RegisterWorkflowReader(nil) })

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "L"}, nil)
	wf.EXPECT().FlowContentByID(gomock.Any(), int64(9)).Return("# Flow", nil)
	wf.EXPECT().FlowGraphByID(gomock.Any(), int64(9)).Return(`{"version":1,"nodes":[{"id":"n1","label":"FE","kind":"task"}],"edges":[]}`, nil)

	var savedGraph string
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		savedGraph = r.FlowGraph
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(500), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error { tk.ID = 9; return nil })
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("库模式 → 快照 workflow.graph 进 run.FlowGraph", t, func() {
		_, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "g", LeaderAgentID: 2, FlowID: 9,
		})
		So(err, ShouldBeNil)
		So(savedGraph, ShouldContainSubstring, `"label":"FE"`)
	})
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/service/orch_svc/ -run TestCreateRun_LibraryModeSnapshotsFlowGraph -count=1`
Expected: FAIL — `savedGraph` 为空(CreateRun 尚未快照 graph);或 mock 报「unexpected call FlowGraphByID」尚未被消费。

- [ ] **Step 6: Snapshot flow_graph in CreateRun**

In `internal/service/orch_svc/create.go`, right after the existing `flowContent` snapshot block (the `if flowContent == "" && req.FlowID > 0 && s.wf != nil { ... }`), add a parallel `flowGraph` snapshot:

```go
	// 库模式额外快照原始 graph JSON 进 run.FlowGraph(overlay 用；取失败按无图继续)。
	var flowGraph string
	if req.FlowID > 0 && s.wf != nil {
		if g, err := s.wf.FlowGraphByID(ctx, req.FlowID); err == nil {
			flowGraph = g
		} else {
			logger.Ctx(ctx).Warn("orch.CreateRun: 取流程 graph 失败,按无图继续", zap.Int64("flow", req.FlowID), zap.Error(err))
		}
	}
```

Then add `FlowGraph: flowGraph,` to the `run := &orch_entity.OrchestrationRun{...}` literal (after `FlowContent: flowContent,`):

```go
	run := &orch_entity.OrchestrationRun{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID,
		FlowID: req.FlowID, FlowContent: flowContent, FlowGraph: flowGraph,
		ProjectID: req.ProjectID, Status: orch_entity.RunRunning,
		AllowedAgentIDs: allowed,
	}
```

- [ ] **Step 7: Fix the two existing library-mode tests (they now also call FlowGraphByID)**

`CreateRun` now calls `FlowGraphByID` whenever `FlowID > 0`. gomock treats an un-EXPECTed call as a failure, so add a `FlowGraphByID` expectation to **both** existing library-mode tests. In `TestCreateRun_LibraryModeSnapshotsFlowContent`, right after its `wf.EXPECT().FlowContentByID(...)` line add:

```go
	wf.EXPECT().FlowGraphByID(gomock.Any(), int64(9)).Return("", nil)
```

In `TestCreateRun_LibraryModeFlowReadError`, right after its `wf.EXPECT().FlowContentByID(...)` line add:

```go
	wf.EXPECT().FlowGraphByID(gomock.Any(), int64(9)).Return("", nil)
```

(The two non-flow tests — `TestCreateRun_BuildsRunRootSessionAndTask`, `TestCreateRun_PersistsAllowedAgentIDs` — pass `FlowContent`/no `FlowID`, so `FlowID > 0` is false and `FlowGraphByID` is not called; leave them unchanged. `TestCreateRun_BuildsRunRootSessionAndTask` passes `FlowContent` with `FlowID` 0, and `TestCreateRun_PersistsAllowedAgentIDs` — confirm it has no `FlowID > 0`; if it does, add the same expectation.)

- [ ] **Step 8: Run tests to verify green**

Run: `go test ./internal/service/orch_svc/ -run TestCreateRun -count=1`
Expected: PASS（新快照测试 + 两既有库模式测试都绿）。

- [ ] **Step 9: Build the app package (adapter wiring compiles)**

Run: `go build ./internal/app/... ./internal/service/orch_svc/...`
Expected: OK。

- [ ] **Step 10: Commit**

```bash
git commit internal/service/orch_svc/deps.go \
  internal/service/orch_svc/mock_orch_svc/mock_deps.go \
  internal/app/orch_adapter.go \
  internal/service/orch_svc/create.go \
  internal/service/orch_svc/create_test.go \
  -m "✨ orchestration: WorkflowReader.FlowGraphByID + CreateRun 快照 flow_graph"
```

---

### Task 3: dispatch 带 `node` 标（工具 schema + 处理器 + Dispatch 落库）

**Files:**
- Modify: `internal/service/orch_svc/dispatch.go`（`Dispatch` 增 `node` 参数 + 写 `NodeRef`）
- Modify: `internal/service/orch_svc/mcp.go`（`handleDispatch` 解析 `node` + 工具 schema 加 `node` 属性）
- Modify: `internal/service/orch_svc/dispatch_test.go`（断言 `NodeRef` 落库）

**Interfaces:**
- Consumes: `orch_entity.Task.NodeRef`（Task 1）。
- Produces: `Dispatch(ctx, parentSessionID, agentName, brief string, isolate bool, node string) (int64, error)`（**末位新增 `node string`**）。

- [ ] **Step 1: Write the failing test**

In `internal/service/orch_svc/dispatch_test.go`, add a test asserting the child task stores `NodeRef`. (Model it on the existing dispatch tests in that file — same mock set: `chat`/`agents`/`runs`/`tasks` via `RegisterDeps`.) Use this test:

```go
func TestDispatch_StoresNodeRef(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	// 注入 no-op enqueue 钩子，避免真实调度器 goroutine 与 ctrl.Finish 竞态(与既有 dispatch 测试一致)。
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 1, RunID: 100}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "FE工程师").Return(&agent_entity.Agent{ID: 3, Name: "FE工程师"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)

	var savedNodeRef string
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		savedNodeRef = tk.NodeRef
		tk.ID = 7
		return nil
	})
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil) // 父转 awaiting-children

	Convey("dispatch 带 node → 子任务落库 NodeRef", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "FE工程师", "做登录页", false, "FE")
		So(err, ShouldBeNil)
		So(savedNodeRef, ShouldEqual, "FE")
	})
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/service/orch_svc/ -run TestDispatch_StoresNodeRef -count=1`
Expected: FAIL — `Dispatch` 目前签名无 `node` 参数(编译错误),或 `savedNodeRef` 为空。

- [ ] **Step 3: Add `node` to `Dispatch`**

In `internal/service/orch_svc/dispatch.go`, change the signature and set `NodeRef` on the child task:

```go
func (s *orchSvc) Dispatch(ctx context.Context, parentSessionID int64, agentName, brief string, isolate bool, node string) (int64, error) {
```

and in the `child := &orch_entity.Task{...}` literal add `NodeRef: node,` (after `Brief: brief,`):

```go
	child := &orch_entity.Task{
		RunID:        parent.RunID,
		AgentID:      target.ID,
		SessionID:    childSession,
		ParentTaskID: parent.ID,
		Kind:         orch_entity.TaskKindDispatch,
		Status:       orch_entity.TaskRunning,
		Brief:        brief,
		NodeRef:      node,
		CallSeq:      int(n) + 1,
	}
```

- [ ] **Step 4: Thread `node` through the MCP handler + tool schema**

In `internal/service/orch_svc/mcp.go` `handleDispatch`, add `Node` to the parsed struct and pass it to `Dispatch`:

```go
	var p struct {
		Agent   string `json:"agent"`
		Brief   string `json:"brief"`
		Isolate bool   `json:"isolate"`
		Node    string `json:"node"`
	}
```

```go
	taskID, err := m.svc.Dispatch(r.Context(), ref.sessionID, p.Agent, p.Brief, p.Isolate, p.Node)
```

Then in `orchToolSchemas()`, add a `node` property to the `dispatch` tool's `properties` map (after the `isolate` line):

```go
					"isolate": map[string]any{"type": "boolean", "description": "true=独立 git worktree 隔离;默认 false 共享工作区"},
					"node":    map[string]any{"type": "string", "description": "可选:本次工作对应流程里的哪个步骤,传该步骤名(如 FE),仅用于进度可视化;不确定就留空,不影响派活"},
```

- [ ] **Step 5: Fix ALL other `Dispatch` call sites (arity change)**

`Dispatch` gained a 6th parameter, so **every existing caller stops compiling** until updated. The existing `dispatch_test.go` tests call `Dispatch(ctx, sessionID, name, brief, isolate)` with **5 args** (e.g. `...Dispatch(context.Background(), 500, "李", "实现登录表单", true)`) — each must gain a trailing `, ""`. Find every call site (zsh-safe grep, no `--include`):

Run: `grep -rn "\.Dispatch(" internal | grep -v "func .*Dispatch"`

For each call that is NOT the `handleDispatch` line already fixed in Step 4, append `, ""` as the new last argument (untagged). This includes:
- all `orch_svc.Default().Dispatch(...)` calls in `internal/service/orch_svc/dispatch_test.go` (there are several),
- any production caller outside `handleDispatch` (if the grep shows one).

- [ ] **Step 5b: Verify no stale 5-arg calls remain**

Run: `go vet ./internal/service/orch_svc/... 2>&1 | grep -i "not enough arguments\|too many arguments" || echo OK`
Expected: `OK`（无 arity 编译错误）。

- [ ] **Step 6: Run tests + build**

Run: `go test ./internal/service/orch_svc/ -run 'TestDispatch' -count=1 && go build ./...`
Expected: PASS / build OK（`TestDispatch_StoresNodeRef` 绿,既有 dispatch 测试因补了 `, ""` 参数仍绿）。

- [ ] **Step 7: Commit**

```bash
git commit internal/service/orch_svc/dispatch.go \
  internal/service/orch_svc/mcp.go \
  internal/service/orch_svc/dispatch_test.go \
  -m "✨ orchestration: dispatch 可选 node 标 → Task.NodeRef(进度可视化)"
```

(If Step 5 required edits to other files, add them to this commit's pathspec.)

---

### Task 4: DTO 暴露 `flowGraph` / `nodeRef` + make generate

**Files:**
- Modify: `internal/app/orch.go`（`RunItemDTO`/`TaskDTO` 加字段 + 映射）
- Create: `internal/app/orch_dto_test.go`（映射断言）
- Regen: `frontend/wailsjs/`（`make generate`,不提交）

**Interfaces:**
- Consumes: `orch_entity.OrchestrationRun.FlowGraph`、`orch_entity.Task.NodeRef`（Task 1）。
- Produces: `RunItemDTO.FlowGraph string json:"flowGraph"`、`TaskDTO.NodeRef string json:"nodeRef"`（前端 `detail.run.flowGraph` / `detail.tasks[].nodeRef`）。

- [ ] **Step 1: Write the failing test**

Create `internal/app/orch_dto_test.go`:

```go
package app

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestToRunItem_MapsFlowGraph(t *testing.T) {
	dto := toRunItem(&orch_entity.OrchestrationRun{ID: 1, FlowGraph: `{"version":1}`})
	assert.Equal(t, `{"version":1}`, dto.FlowGraph)
}

func TestToTaskDTO_MapsNodeRef(t *testing.T) {
	dto := toTaskDTO(&orch_entity.Task{ID: 1, NodeRef: "FE"})
	assert.Equal(t, "FE", dto.NodeRef)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/app/ -run 'TestToRunItem_MapsFlowGraph|TestToTaskDTO_MapsNodeRef' -count=1`
Expected: FAIL — `dto.FlowGraph` / `dto.NodeRef` 字段不存在(编译错误)。

- [ ] **Step 3: Add DTO fields + mapping**

In `internal/app/orch.go`, add to `RunItemDTO` (after `FlowContent`):

```go
	// FlowContent 创建时快照的流程正文。
	FlowContent string `json:"flowContent"`
	// FlowGraph 创建时快照的 graph JSON（overlay 用；空=无流程）。
	FlowGraph string `json:"flowGraph"`
```

Add to `TaskDTO` (after `Refs`):

```go
	// Refs JSON 格式的被引用产物/任务列表。
	Refs string `json:"refs"`
	// NodeRef 该任务对应的流程节点 label（overlay 用；空=未打标）。
	NodeRef string `json:"nodeRef"`
```

In `toRunItem`, add `FlowGraph: r.FlowGraph,` (after `FlowContent: r.FlowContent,`). In `toTaskDTO`, add `NodeRef: t.NodeRef,` (after `Refs: t.Refs,`).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/app/ -run 'TestToRunItem_MapsFlowGraph|TestToTaskDTO_MapsNodeRef' -count=1`
Expected: PASS。

- [ ] **Step 5: Regenerate the Wails bindings**

Run: `make generate`
Expected: 磁盘上 `frontend/wailsjs/go/models.ts` 的 `app.RunItemDTO` 得 `flowGraph: string`、`app.TaskDTO` 得 `nodeRef: string`。验证:`grep -nE "flowGraph|nodeRef" frontend/wailsjs/go/models.ts` 有匹配。**不提交 wailsjs**。

- [ ] **Step 6: Commit（仅 Go 文件；wailsjs 是 gitignore 生成物）**

```bash
git commit internal/app/orch.go internal/app/orch_dto_test.go \
  -m "✨ orchestration: RunItemDTO.flowGraph + TaskDTO.nodeRef 暴露给前端"
```

---

### Task 5: 前端纯函数 `deriveNodeOverlay`

**Files:**
- Create: `frontend/src/components/agentre/orchestration/flow-overlay.ts`
- Test: `frontend/src/components/agentre/orchestration/__tests__/flow-overlay.test.ts`

**Interfaces:**
- Consumes: `FlowGraph` + `parseFlowGraph` from `./flow-graph`（Phase 1）。
- Produces:
  - `type NodeStatus = "pending" | "running" | "done" | "error" | "neutral"`
  - `interface NodeOverlay { status: NodeStatus; count: number }`
  - `interface OverlayTask { nodeRef?: string; status: string }`
  - `deriveNodeOverlay(graph: FlowGraph | string | null | undefined, tasks: OverlayTask[]): Record<string, NodeOverlay>`

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/agentre/orchestration/__tests__/flow-overlay.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { deriveNodeOverlay } from "../flow-overlay";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "Break", kind: "leader" },
    { id: "n2", label: "FE", kind: "task" },
    { id: "n3", label: "BE", kind: "task" },
    { id: "n4", label: "QA", kind: "task" },
  ],
  edges: [],
});

describe("deriveNodeOverlay", () => {
  it("leader 节点恒 neutral, 无计数", () => {
    const o = deriveNodeOverlay(graph, []);
    expect(o.n1).toEqual({ status: "neutral", count: 0 });
  });

  it("无匹配任务的 task 节点 = pending", () => {
    const o = deriveNodeOverlay(graph, []);
    expect(o.n2).toEqual({ status: "pending", count: 0 });
  });

  it("label 匹配(trim+大小写不敏感)聚合任务并计数", () => {
    const o = deriveNodeOverlay(graph, [
      { nodeRef: " fe ", status: "done" },
      { nodeRef: "FE", status: "done" },
    ]);
    expect(o.n2).toEqual({ status: "done", count: 2 });
  });

  it("优先级 error > running > done", () => {
    expect(
      deriveNodeOverlay(graph, [
        { nodeRef: "FE", status: "done" },
        { nodeRef: "FE", status: "error" },
      ]).n2.status,
    ).toBe("error");
    expect(
      deriveNodeOverlay(graph, [
        { nodeRef: "FE", status: "done" },
        { nodeRef: "FE", status: "running" },
      ]).n2.status,
    ).toBe("running");
  });

  it("awaiting-children/awaiting-user/pending/paused 都算 running(非终态)", () => {
    for (const s of ["awaiting-children", "awaiting-user", "pending", "paused"]) {
      expect(deriveNodeOverlay(graph, [{ nodeRef: "FE", status: s }]).n2.status).toBe(
        "running",
      );
    }
  });

  it("全终态(含 canceled-only)→ done", () => {
    expect(
      deriveNodeOverlay(graph, [{ nodeRef: "QA", status: "canceled" }]).n4.status,
    ).toBe("done");
  });

  it("未打标 / 匹配不到节点的任务被忽略", () => {
    const o = deriveNodeOverlay(graph, [
      { status: "running" }, // 无 nodeRef
      { nodeRef: "不存在", status: "running" },
    ]);
    expect(o.n2.status).toBe("pending");
    expect(o.n3.status).toBe("pending");
  });

  it("非法/空 graph → 空对象", () => {
    expect(deriveNodeOverlay("", [])).toEqual({});
    expect(deriveNodeOverlay(null, [])).toEqual({});
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/flow-overlay.test.ts`
Expected: FAIL — `Failed to resolve import "../flow-overlay"`。

- [ ] **Step 3: Write the implementation**

Create `frontend/src/components/agentre/orchestration/flow-overlay.ts`:

```ts
import { parseFlowGraph, type FlowGraph } from "./flow-graph";

export type NodeStatus = "pending" | "running" | "done" | "error" | "neutral";

export interface NodeOverlay {
  status: NodeStatus;
  count: number;
}

// OverlayTask: deriveNodeOverlay 的最小任务输入(与 wails 生成类型解耦, 便于纯单测)。
export interface OverlayTask {
  nodeRef?: string;
  status: string;
}

// 非终态任务状态(点亮为 running)。终态 = done / canceled / error。
const NON_TERMINAL = new Set([
  "pending",
  "running",
  "awaiting-children",
  "awaiting-user",
  "paused",
]);

const norm = (s: string) => s.trim().toLowerCase();

// statusOf: task-kind 节点按匹配任务聚合状态。优先级 error > running > done > pending。
// 全终态(含 canceled-only)统一记 done(已了结)——总函数, 覆盖 spec 的边界。
function statusOf(tasks: OverlayTask[]): NodeStatus {
  if (tasks.length === 0) return "pending";
  if (tasks.some((t) => t.status === "error")) return "error";
  if (tasks.some((t) => NON_TERMINAL.has(t.status))) return "running";
  return "done";
}

// deriveNodeOverlay: 按 label(trim+小写)把带 nodeRef 的任务匹配到图节点, 算出每节点状态+计数。
// leader-kind 节点恒 neutral;未打标 / 匹配不到的任务忽略;非法/空 graph → {}。
export function deriveNodeOverlay(
  graph: FlowGraph | string | null | undefined,
  tasks: OverlayTask[],
): Record<string, NodeOverlay> {
  const g = typeof graph === "string" ? parseFlowGraph(graph) : (graph ?? null);
  const out: Record<string, NodeOverlay> = {};
  if (!g) return out;

  const byLabel = new Map<string, OverlayTask[]>();
  for (const tk of tasks) {
    const ref = tk.nodeRef?.trim();
    if (!ref) continue;
    const k = norm(ref);
    byLabel.set(k, [...(byLabel.get(k) ?? []), tk]);
  }

  for (const node of g.nodes) {
    if (node.kind === "leader") {
      out[node.id] = { status: "neutral", count: 0 };
      continue;
    }
    const matched = byLabel.get(norm(node.label)) ?? [];
    out[node.id] = { status: statusOf(matched), count: matched.length };
  }
  return out;
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/flow-overlay.test.ts`
Expected: PASS（8 passed）。

- [ ] **Step 5: Lint the new files**

Run: `cd frontend && pnpm exec eslint --fix src/components/agentre/orchestration/flow-overlay.ts src/components/agentre/orchestration/__tests__/flow-overlay.test.ts && pnpm exec eslint src/components/agentre/orchestration/flow-overlay.ts src/components/agentre/orchestration/__tests__/flow-overlay.test.ts`
Expected: exit 0。

- [ ] **Step 6: Commit**

```bash
git commit frontend/src/components/agentre/orchestration/flow-overlay.ts \
  frontend/src/components/agentre/orchestration/__tests__/flow-overlay.test.ts \
  -m "✨ orchestration: deriveNodeOverlay 纯函数(按 label 聚合节点状态+计数)"
```

---

### Task 6: `FlowGraphView` 加 `overlay` prop（状态着色 + 计数徽章）

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/flow-graph-view.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx`

**Interfaces:**
- Consumes: `NodeOverlay`, `NodeStatus` from `./flow-overlay`（Task 5）。
- Produces: `FlowGraphView` 新增可选 prop `overlay?: Record<string, NodeOverlay>`。不传 → Phase 1 行为不变。

- [ ] **Step 1: Write the failing test**

Create `frontend/src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { FlowGraphView } from "../flow-graph-view";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "FE", kind: "task" },
    { id: "n2", label: "BE", kind: "task" },
  ],
  edges: [],
});

describe("FlowGraphView overlay", () => {
  it("不传 overlay → 无状态色 class(向后兼容)", () => {
    render(<FlowGraphView graph={graph} />);
    const node = screen.getByTestId("flow-node-n1");
    expect(node.className).not.toContain("border-status-running");
    expect(node.className).not.toContain("border-destructive");
  });

  it("done 节点着绿(status-running token)", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "done", count: 2 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-status-running",
    );
  });

  it("error 节点着红", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "error", count: 1 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-destructive",
    );
  });

  it("count>0 渲染计数徽章", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "running", count: 3 } }}
      />,
    );
    expect(screen.getByTestId("flow-node-n1-count").textContent).toBe("3");
  });

  it("count=0 不渲染徽章", () => {
    render(
      <FlowGraphView
        graph={graph}
        overlay={{ n1: { status: "pending", count: 0 } }}
      />,
    );
    expect(screen.queryByTestId("flow-node-n1-count")).toBeNull();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx`
Expected: FAIL — `overlay` prop 不存在(状态 class / 计数徽章都没有)。

- [ ] **Step 3: Add the overlay prop + rendering**

In `frontend/src/components/agentre/orchestration/flow-graph-view.tsx`:

(a) Extend the imports at the top to include the overlay types:

```tsx
import { cn } from "@/lib/utils";
import {
  type FlowGraph,
  isBounceSource,
  layoutFlowGraph,
  parseFlowGraph,
} from "./flow-graph";
import type { NodeOverlay, NodeStatus } from "./flow-overlay";
```

(b) Add an `overlayClass` helper above the `FlowGraphView` function (near `kindBadge`):

```tsx
// overlayClass: 节点状态 → 卡片边框/背景色(复用 run-status banner 的既有 token)。
function overlayClass(status: NodeStatus): string {
  switch (status) {
    case "done":
      return "border-status-running bg-status-running-bg";
    case "running":
      return "border-status-waiting bg-status-waiting-bg";
    case "error":
      return "border-destructive bg-destructive-soft";
    case "neutral":
      return "opacity-60";
    default: // pending
      return "";
  }
}
```

(c) Change the `FlowGraphView` signature to accept `overlay`:

```tsx
export function FlowGraphView({
  graph,
  className,
  overlay,
}: {
  graph?: string | FlowGraph;
  className?: string;
  overlay?: Record<string, NodeOverlay>;
}) {
```

(d) In the node-card `.map`, apply the overlay class + count badge. Replace the node card block (the `<div ... data-testid={`flow-node-${p.node.id}`}> ... </div>`) with:

```tsx
        const ov = overlay?.[p.node.id];
        return (
          <div
            key={p.node.id}
            className={cn(
              "absolute flex flex-col justify-center rounded-md border border-border bg-card px-2 py-1",
              ov ? overlayClass(ov.status) : undefined,
            )}
            style={{
              left: pos.get(p.node.id)!.x,
              top: pos.get(p.node.id)!.y,
              width: NODE_W,
              height: NODE_H,
            }}
            title={p.node.brief}
            data-testid={`flow-node-${p.node.id}`}
          >
            <span className="truncate text-2xs font-medium text-foreground">
              {p.node.label}
            </span>
            <span className={cn("text-[9px]", badge.cls)}>{badge.label}</span>
            {ov && ov.count > 0 ? (
              <span
                data-testid={`flow-node-${p.node.id}-count`}
                className="absolute -right-1.5 -top-1.5 flex size-4 items-center justify-center rounded-full bg-foreground text-[9px] font-semibold text-background"
              >
                {ov.count}
              </span>
            ) : null}
          </div>
        );
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx`
Expected: PASS（5 passed）。

- [ ] **Step 5: Confirm Phase 1/2 consumers still green (FlowGraphView is reused)**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/flow-graph.test.ts src/components/agentre/orchestration/__tests__/run-new-dialog.test.tsx`
Expected: PASS（既有只读用法不传 overlay,行为不变）。

- [ ] **Step 6: Lint**

Run: `cd frontend && pnpm exec eslint --fix src/components/agentre/orchestration/flow-graph-view.tsx src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx && pnpm exec eslint src/components/agentre/orchestration/flow-graph-view.tsx src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx`
Expected: exit 0。

- [ ] **Step 7: Commit**

```bash
git commit frontend/src/components/agentre/orchestration/flow-graph-view.tsx \
  frontend/src/components/agentre/orchestration/__tests__/flow-graph-view-overlay.test.tsx \
  -m "✨ orchestration: FlowGraphView 加 overlay prop(状态着色 + 计数徽章)"
```

---

### Task 7: `RunFlowOverlay` 组件 + 「Flow」Tab 接入 + i18n

**Files:**
- Create: `frontend/src/components/agentre/orchestration/run-flow-overlay.tsx`
- Modify: `frontend/src/components/agentre/orchestration/toggle-bar.tsx`（view 加 `"flow"` + Flow Tab）
- Modify: `frontend/src/components/agentre/orchestration/index.tsx`（view 状态 + 渲染 overlay）
- Modify: `frontend/src/i18n/locales/en/common.json` + `frontend/src/i18n/locales/zh-CN/common.json`
- Test: `frontend/src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx`
- Test (modify existing): `frontend/src/components/agentre/orchestration/__tests__/toggle-bar.test.tsx`（加 Flow tab 用例 + 本地 i18n mock 补 `viewFlow`）

**Interfaces:**
- Consumes: `deriveNodeOverlay`（Task 5)、`FlowGraphView` overlay prop(Task 6)、`parseFlowGraph`(Phase 1)、生成类型 `app.RunDetailDTO`（Task 4 后 `run.flowGraph`/`tasks[].nodeRef` 就位）。
- Produces: `RunFlowOverlay({ detail })`;`ToggleBar` 的 `view`/`onView` 类型扩为 `"graph" | "feed" | "flow"` + 新增**可选** `showFlow?: boolean` prop(默认 `false` → 不渲染 Flow tab;既有 12 个 `toggle-bar.test.tsx` 渲染不传该 prop,行为不变、不破)。

- [ ] **Step 1: Add i18n keys**

In `frontend/src/i18n/locales/en/common.json`, under `orchestration.header` add `viewFlow`, and under `orchestration` add a `flow` block:

```json
      "viewFlow": "Flow"
```

```json
    "flow": {
      "empty": "This run has no flow to overlay"
    }
```

In `frontend/src/i18n/locales/zh-CN/common.json`, mirror:

```json
      "viewFlow": "流程"
```

```json
    "flow": {
      "empty": "本次编排没有可叠加进度的流程"
    }
```

(Place `viewFlow` as a sibling of the existing `orchestration.header.viewGraph`/`viewFeed`; place the `flow` block as a sibling of the existing `orchestration.graph`/`toggle` blocks.)

- [ ] **Step 2: Write the failing test**

Create `frontend/src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx`:

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import type { app } from "../../../../wailsjs/go/models";
import { RunFlowOverlay } from "../run-flow-overlay";

const graph = JSON.stringify({
  version: 1,
  nodes: [
    { id: "n1", label: "FE", kind: "task" },
    { id: "n2", label: "BE", kind: "task" },
  ],
  edges: [{ from: "n1", to: "n2" }],
});

function detail(over: Partial<app.RunDetailDTO> = {}): app.RunDetailDTO {
  return {
    run: { id: 1, flowGraph: graph },
    tasks: [{ id: 9, nodeRef: "FE", status: "done" }],
    ...over,
  } as unknown as app.RunDetailDTO;
}

describe("RunFlowOverlay", () => {
  it("渲染 flow DAG 节点(来自 run.flowGraph 快照)", () => {
    render(<RunFlowOverlay detail={detail()} />);
    expect(screen.getByTestId("run-flow-overlay")).toBeInTheDocument();
    expect(screen.getByTestId("flow-node-n1")).toBeInTheDocument();
  });

  it("按任务实况给节点着色(FE done → 绿)", () => {
    render(<RunFlowOverlay detail={detail()} />);
    expect(screen.getByTestId("flow-node-n1").className).toContain(
      "border-status-running",
    );
  });

  it("无 flowGraph → 空态", () => {
    render(
      <RunFlowOverlay
        detail={detail({ run: { id: 1, flowGraph: "" } as app.RunItemDTO })}
      />,
    );
    expect(screen.getByTestId("run-flow-empty")).toBeInTheDocument();
  });
});
```

- [ ] **Step 3: Run test to verify it fails**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx`
Expected: FAIL — `Failed to resolve import "../run-flow-overlay"`。

- [ ] **Step 4: Write `RunFlowOverlay`**

Create `frontend/src/components/agentre/orchestration/run-flow-overlay.tsx`:

```tsx
import * as React from "react";
import { useTranslation } from "react-i18next";

import type { app } from "../../../../wailsjs/go/models";
import { parseFlowGraph } from "./flow-graph";
import { FlowGraphView } from "./flow-graph-view";
import { deriveNodeOverlay } from "./flow-overlay";

// RunFlowOverlay: 把当前 Run 的任务实况叠加到快照的 flow DAG 上(只读)。
export function RunFlowOverlay({ detail }: { detail: app.RunDetailDTO }) {
  const { t } = useTranslation();
  const graph = detail.run?.flowGraph ?? "";
  const tasks = detail.tasks ?? [];

  const overlay = React.useMemo(
    () =>
      deriveNodeOverlay(
        graph,
        tasks.map((tk) => ({ nodeRef: tk.nodeRef, status: tk.status })),
      ),
    [graph, tasks],
  );

  if (!parseFlowGraph(graph)) {
    return (
      <div
        data-testid="run-flow-empty"
        className="flex flex-1 items-center justify-center p-8 text-sm text-muted-foreground"
      >
        {t("orchestration.flow.empty")}
      </div>
    );
  }

  return (
    <div data-testid="run-flow-overlay" className="p-5">
      <FlowGraphView graph={graph} overlay={overlay} />
    </div>
  );
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx`
Expected: PASS（3 passed）。

- [ ] **Step 6: Add the Flow tab to `ToggleBar`**

In `frontend/src/components/agentre/orchestration/toggle-bar.tsx`:

(a) Widen the view type + add an **optional** `showFlow` to the props interface (optional + default `false` so the 12 existing `toggle-bar.test.tsx` renders that omit it keep compiling and behaving unchanged):

```tsx
interface ToggleBarProps {
  view: "graph" | "feed" | "flow";
  onView: (v: "graph" | "feed" | "flow") => void;
  stats: ToggleBarStats;
  showFlow?: boolean;
}

export function ToggleBar({
  view,
  onView,
  stats,
  showFlow = false,
}: ToggleBarProps) {
```

(b) Add a `Waypoints` import (lucide) at the top:

```tsx
import { GitFork, GitMerge, List, ListChecks, Users, Waypoints } from "lucide-react";
```

(c) Inside the segmented control `<div ...>`, add the Flow button as the FIRST button (before the graph button), rendered only when `showFlow`:

```tsx
      <div className="flex items-center gap-0.5 rounded-lg bg-secondary p-0.5">
        {showFlow ? (
          <button
            type="button"
            data-testid="toggle-flow"
            onClick={() => onView("flow")}
            className={cn(
              "flex items-center gap-1.5 rounded-md px-3 py-[5px] text-xs",
              view === "flow"
                ? "border border-border bg-card font-semibold text-foreground"
                : "text-muted-foreground",
            )}
          >
            <Waypoints className="size-3 shrink-0" aria-hidden="true" />
            {t("orchestration.header.viewFlow")}
          </button>
        ) : null}
        <button
          type="button"
          data-testid="toggle-graph"
```

- [ ] **Step 6.5: Add a Flow-tab test to the existing `toggle-bar.test.tsx`**

In `frontend/src/components/agentre/orchestration/__tests__/toggle-bar.test.tsx`, add `"orchestration.header.viewFlow": "流程",` to the local i18n mock `map` object (alongside `viewGraph`/`viewFeed`), then append these tests inside the `describe("ToggleBar", ...)` block:

```tsx
  it("showFlow=false(默认)不渲染 Flow tab", () => {
    render(<ToggleBar view="graph" onView={vi.fn()} stats={defaultStats} />);
    expect(screen.queryByTestId("toggle-flow")).toBeNull();
  });

  it("showFlow=true 渲染 Flow tab, 点击调用 onView('flow')", () => {
    const onView = vi.fn();
    render(
      <ToggleBar
        view="graph"
        onView={onView}
        stats={defaultStats}
        showFlow
      />,
    );
    fireEvent.click(screen.getByTestId("toggle-flow"));
    expect(onView).toHaveBeenCalledWith("flow");
  });
```

- [ ] **Step 7: Wire the Flow view into `index.tsx`**

In `frontend/src/components/agentre/orchestration/index.tsx`:

(a) Add the import (near the other view imports):

```tsx
import { RunFlowOverlay } from "./run-flow-overlay";
```

(b) Widen the `view` state:

```tsx
  const [view, setView] = React.useState<"graph" | "feed" | "flow">("graph");
```

(c) Just before the `return (`, derive `hasFlow` + `effView` (so a run without a snapshot never gets stuck on the Flow tab):

```tsx
  const hasFlow = !!detail?.run?.flowGraph;
  const effView = view === "flow" && !hasFlow ? "graph" : view;
```

(d) Pass both to `ToggleBar` (replace the existing `<ToggleBar view={view} onView={setView} stats={toggleStats} />`):

```tsx
            <ToggleBar
              view={effView}
              onView={setView}
              stats={toggleStats}
              showFlow={hasFlow}
            />
```

(e) Replace the content branch (the `{view === "graph" ? <StructureGraph .../> : <ActivityFeed .../>}`) with a three-way switch on `effView`:

```tsx
              {effView === "flow" ? (
                <RunFlowOverlay detail={detail} />
              ) : effView === "graph" ? (
                <StructureGraph
                  detail={detail}
                  onSelectSession={setSelectedSessionId}
                />
              ) : (
                <ActivityFeed detail={detail} />
              )}
```

- [ ] **Step 8: Run the frontend tests + i18n coverage**

Run: `cd frontend && pnpm test -- --run src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx src/components/agentre/orchestration/__tests__/toggle-bar.test.tsx src/components/agentre/orchestration/__tests__/index.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS（overlay 3/3 + toggle-bar 全绿含新 Flow tab 2 例 + index 无回归 + i18n 双 locale 齐备）。

- [ ] **Step 9: Lint the changed files**

Run: `cd frontend && pnpm exec eslint --fix src/components/agentre/orchestration/run-flow-overlay.tsx src/components/agentre/orchestration/toggle-bar.tsx src/components/agentre/orchestration/index.tsx src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx src/components/agentre/orchestration/__tests__/toggle-bar.test.tsx` then re-run the same paths without `--fix` and confirm exit 0.
Expected: exit 0。

- [ ] **Step 10: Commit**

```bash
git commit frontend/src/components/agentre/orchestration/run-flow-overlay.tsx \
  frontend/src/components/agentre/orchestration/toggle-bar.tsx \
  frontend/src/components/agentre/orchestration/index.tsx \
  frontend/src/i18n/locales/en/common.json \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/components/agentre/orchestration/__tests__/run-flow-overlay.test.tsx \
  frontend/src/components/agentre/orchestration/__tests__/toggle-bar.test.tsx \
  -m "✨ orchestration: Run 详情新增 Flow Tab + RunFlowOverlay(进度叠加 DAG)"
```

---

### Task 8: 收尾全量 gate

跑全套后端 + 前端 gate,看真 exit code。

**Files:** 无新增（仅发现回归时回对应任务修复）。

- [ ] **Step 1: 后端全量测试**

Run: `make test-backend > /tmp/p3-backend.txt 2>&1; echo "exit=$?"; tail -20 /tmp/p3-backend.txt`
Expected: `exit=0`。**注意既有非本 feature flake**:`pkg/piagent TestStreamDiagnosticsTruncateLongStderrTail`(timing)——若只此一处失败,`-count=1` 重跑确认,不算回归(记入汇报,不修)。

- [ ] **Step 2: 后端 lint**

Run: `make lint-backend > /tmp/p3-lint.txt 2>&1; echo "exit=$?"; tail -15 /tmp/p3-lint.txt`
Expected: `exit=0`（或仅既有非本 feature 的 issue;新代码零 issue）。

- [ ] **Step 3: 前端 tsc**

Run: `cd frontend && pnpm exec tsc --noEmit > /tmp/p3-tsc.txt 2>&1; echo "exit=$? errors=$(grep -c 'error TS' /tmp/p3-tsc.txt)"`
Expected: `exit=0 errors=0`。

- [ ] **Step 4: 前端 eslint（编排面）**

Run: `cd frontend && pnpm exec eslint src/components/agentre/orchestration; echo "exit=$?"`
Expected: `exit=0`。

- [ ] **Step 5: 前端全量 vitest**

Run: `cd frontend && pnpm test -- --run > /tmp/p3-vitest.txt 2>&1; echo "exit=$?"; grep -E "Test Files|Tests " /tmp/p3-vitest.txt | tail -2; grep -E "FAIL " /tmp/p3-vitest.txt | head`
Expected: `exit=0`,0 failures（新 3 个前端测试文件 + 后端全绿 + 既有无回归）。

- [ ] **Step 6: 汇报**

如实写出各 gate 真实 exit code + 通过数;任一失败不得标记完成——回对应任务修复后重跑。

---

## 交付方式

沿用 Phase 1/2 的 Subagent-Driven:每 Task 一 implementer + 逐任务两段复审(spec + 质量)+ opus 全分支终审,合入 develop/wyz。Task 1/4/5/6 偏机械(迁移/DTO/纯函数/单组件,plan 含完整代码)→ 便宜模型;Task 2/3/7 有集成判断(mock 接线/工具链路/Tab 接入)→ 标准模型;终审用最强模型。
