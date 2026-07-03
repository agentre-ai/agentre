# 编排 Harness · 切片 E — 可参与 agent 生效 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「新建编排」弹窗挑的可参与团队（`allowedAgentIds`）真正生效——持久化到 Run，`agent_list` 只列可参与集，`dispatch`/`ask` 拒绝集外 agent。

**Architecture:** `OrchestrationRun` 新增 `allowed_agent_ids`（JSON `[]int64`，空=全部）列 + 充血方法 `AllowedSet()/IsAgentAllowed()`；`CreateRun` 落库；`Dispatch`/`Ask` 解析目标后用实体方法校验；`agent_list` 经新 service 方法 `ListAllowedAgents` 过滤。纯后端改动（前端已把 `allowedAgentIds` 传到位）。

**Tech Stack:** Go 1.26、cago（`db.Ctx`/gormigrate）、gorm、goconvey + `go.uber.org/mock`（service 单测）、`go-sqlmock`（repo 单测）。

## Global Constraints

- **严格 TDD：先写失败测试 → 跑挂 → 最小实现 → 跑绿 → 提交。**
- **service 单测走 mock（`RegisterDeps` 注入），不连库；repo 单测走 `testutils.Database(t)` + sqlmock。**
- **迁移只追加到 `migrationList()` 末尾，禁止改已有迁移；DDL 用原生 SQL。**
- **不改前端**（`allowedAgentIds` 已从弹窗传到 `RunCreate`）。
- **空/`[]` = 不限制**（向后兼容既有 Run，无此数据即全员可参与）。
- **Leader 恒允许**（`IsAgentAllowed` 内置 `agentID == leaderID`），与「Leader 与 allowedAgentIds 相互独立」的既有决策一致。
- **提交用 pathspec**（`develop/wyz` 有并发会话共享 index）：`git commit <files> -m ...`，绝不裸 `git commit`。
- gitmoji 提交；golangci-lint v2。

---

## File Structure

- `internal/model/entity/orch_entity/run.go` — 加字段 `AllowedAgentIDs` + 方法 `AllowedSet`/`IsAgentAllowed`。
- `internal/model/entity/orch_entity/run_test.go`（已存在）— 追加实体方法单测。
- `migrations/202607030001_orch_run_allowed_agents.go`（新建）— ADD COLUMN 迁移。
- `migrations/migrations.go` — `migrationList()` 末尾注册新迁移。
- `internal/service/orch_svc/orch.go` — 加 `errAgentNotAllowed`。
- `internal/service/orch_svc/create.go` — `CreateRun` marshal 落库 `AllowedAgentIDs`。
- `internal/service/orch_svc/create_test.go`（已存在）— 追加落库断言。
- `internal/service/orch_svc/dispatch.go` — 用实体方法在既有 run 加载点校验。
- `internal/service/orch_svc/dispatch_test.go`（已存在）— 追加拒绝用例。
- `internal/service/orch_svc/ask.go` — 加 run 加载 + 校验；`resolveOrCreateAgentSession` 改收 `projectID`；删 `runProjectID`。
- `internal/service/orch_svc/ask_test.go`（已存在）— 5 个复用会话用例补 runs mock + 追加拒绝用例。
- `internal/service/orch_svc/query.go` — 加 `ListAllowedAgents(ctx, sessionID)`。
- `internal/service/orch_svc/query_test.go`（已存在）— 追加过滤单测。
- `internal/service/orch_svc/mcp.go` — `handleAgentList` 改签名带 `ref`，走 `ListAllowedAgents`。

---

## Task 1: 实体 — allowed_agent_ids 方法

**Files:**
- Modify: `internal/model/entity/orch_entity/run.go`
- Test: `internal/model/entity/orch_entity/run_test.go`

**Interfaces:**
- Produces: `OrchestrationRun.AllowedAgentIDs string`；`(*OrchestrationRun).AllowedSet() map[int64]bool`；`(*OrchestrationRun).IsAgentAllowed(agentID, leaderID int64) bool`。

- [ ] **Step 1: 写失败测试**（追加到 `run_test.go` 末尾）

```go
func TestOrchestrationRun_IsAgentAllowed(t *testing.T) {
	t.Run("空集合=全部允许", func(t *testing.T) {
		r := &OrchestrationRun{AllowedAgentIDs: ""}
		assert.True(t, r.IsAgentAllowed(9, 2))
		r2 := &OrchestrationRun{AllowedAgentIDs: "[]"}
		assert.True(t, r2.IsAgentAllowed(9, 2))
	})
	t.Run("集合内允许、集合外拒绝", func(t *testing.T) {
		r := &OrchestrationRun{AllowedAgentIDs: "[3,4]"}
		assert.True(t, r.IsAgentAllowed(3, 2))
		assert.False(t, r.IsAgentAllowed(9, 2))
	})
	t.Run("Leader 恒允许(即便不在集合)", func(t *testing.T) {
		r := &OrchestrationRun{AllowedAgentIDs: "[3,4]"}
		assert.True(t, r.IsAgentAllowed(2, 2))
	})
	t.Run("非法 JSON → 不限制", func(t *testing.T) {
		r := &OrchestrationRun{AllowedAgentIDs: "not-json"}
		assert.True(t, r.IsAgentAllowed(9, 2))
	})
}
```

> `run_test.go` 已在 `package orch_entity` 内，需确认顶部 import 含 `"testing"` 与 `"github.com/stretchr/testify/assert"`（`task_test.go` 同目录已用 testify，一般已具备；缺则补）。

- [ ] **Step 2: 跑测试确认失败**

Run: `cd /Users/codfrm/Code/agentre/agentre && GOWORK=off go test ./internal/model/entity/orch_entity/ -run TestOrchestrationRun_IsAgentAllowed -v`
Expected: 编译失败（`AllowedAgentIDs`/`IsAgentAllowed` 未定义）。

- [ ] **Step 3: 加字段与方法**

`run.go` 顶部 import 改为：

```go
import (
	"encoding/json"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
)
```

结构体 `OrchestrationRun` 在 `ProjectID` 行后加一列：

```go
	ProjectID       int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	AllowedAgentIDs string `gorm:"column:allowed_agent_ids;type:text;not null;default:''"` // JSON []int64；空=全部可参与
```

文件末尾（`var _ = consts.ACTIVE` 之前）加方法：

```go
// AllowedSet 解析 allowed_agent_ids JSON 为集合；空/非法/无有效 id → nil（不限制）。
func (r *OrchestrationRun) AllowedSet() map[int64]bool {
	s := strings.TrimSpace(r.AllowedAgentIDs)
	if s == "" || s == "[]" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(s), &ids); err != nil {
		return nil
	}
	set := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			set[id] = true
		}
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

// IsAgentAllowed 目标是否可参与：集合空=全部允许；否则须在集合内或为 Leader。
func (r *OrchestrationRun) IsAgentAllowed(agentID, leaderID int64) bool {
	set := r.AllowedSet()
	if len(set) == 0 {
		return true
	}
	return set[agentID] || agentID == leaderID
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/model/entity/orch_entity/ -run TestOrchestrationRun_IsAgentAllowed -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/model/entity/orch_entity/run.go internal/model/entity/orch_entity/run_test.go \
  -m "✨ orchestration: OrchestrationRun 加 allowed_agent_ids 充血方法(空=全部/Leader 恒允许)"
```

---

## Task 2: 迁移 — orchestration_runs 加 allowed_agent_ids 列

**Files:**
- Create: `migrations/202607030001_orch_run_allowed_agents.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾）

**Interfaces:**
- Produces: DB 列 `orchestration_runs.allowed_agent_ids TEXT NOT NULL DEFAULT ''`。

> 排号确认：实现时 `ls migrations/` 看当日是否已有 `2026070300xx` 迁移（`develop/wyz` 并发风险，见 memory `project_migration_squash_group_features`）；若冲突改用下一个可用序号，本 plan 后续引用同步更新。

- [ ] **Step 1: 新建迁移文件**

`migrations/202607030001_orch_run_allowed_agents.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607030001 编排可参与范围：orchestration_runs.allowed_agent_ids（JSON []int64，空=全部）。
func migration202607030001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607030001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orchestration_runs ADD COLUMN allowed_agent_ids TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orchestration_runs DROP COLUMN allowed_agent_ids`).Error
		},
	}
}
```

- [ ] **Step 2: 注册到 migrationList**

`migrations/migrations.go` 的 `migrationList()` 里，`migration202606240001(),` 那一行**之后**追加：

```go
		migration202607030001(), // 编排可参与范围:orchestration_runs.allowed_agent_ids
```

- [ ] **Step 3: 构建 + 迁移执行验证**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./internal/bootstrap/ -run Cago -v`
Expected: 构建通过；bootstrap 迁移测试跑通（真实迁移链含新列，无 dup column）。
> 若无 `bootstrap` 迁移测试命中，退而跑 `make test-backend | tail -5` 看 exit 0（迁移在应用启动路径执行）。

- [ ] **Step 4: 提交**

```bash
git commit migrations/202607030001_orch_run_allowed_agents.go migrations/migrations.go \
  -m "✨ orchestration: 迁移新增 orchestration_runs.allowed_agent_ids 列"
```

---

## Task 3: CreateRun 落库 allowedAgentIDs

**Files:**
- Modify: `internal/service/orch_svc/create.go`
- Test: `internal/service/orch_svc/create_test.go`

**Interfaces:**
- Consumes: `CreateRunRequest.AllowedAgentIDs []int64`（已存在，`create.go:19`）；`OrchestrationRun.AllowedAgentIDs`（Task 1）。
- Produces: `run.AllowedAgentIDs` = 去重剔零后的 JSON（空切片 → `""`）。

- [ ] **Step 1: 写失败测试**（追加到 `create_test.go`）

```go
func TestCreateRun_PersistsAllowedAgentIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "L"}, nil)
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.AllowedAgentIDs, ShouldEqual, "[3,4]") // 去重剔零后 JSON
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(500), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error { tk.ID = 9; return nil })
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("CreateRun 把 allowedAgentIds 去重剔零后落库", t, func() {
		_, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "g", LeaderAgentID: 2, AllowedAgentIDs: []int64{3, 4, 3, 0},
		})
		So(err, ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestCreateRun_PersistsAllowedAgentIDs -v`
Expected: FAIL（`r.AllowedAgentIDs` 为空，`ShouldEqual "[3,4]"` 不满足）。

- [ ] **Step 3: 实现落库**

`create.go` 顶部 import 加 `"encoding/json"`。在构造 `run := &orch_entity.OrchestrationRun{...}` **之前**加去重逻辑，并把结果写进结构体：

```go
	allowed := marshalAllowedAgentIDs(req.AllowedAgentIDs)

	run := &orch_entity.OrchestrationRun{
		Goal: req.Goal, LeaderAgentID: req.LeaderAgentID,
		FlowID: req.FlowID, FlowContent: req.FlowContent,
		ProjectID: req.ProjectID, Status: orch_entity.RunRunning,
		AllowedAgentIDs: allowed,
	}
```

文件末尾加 helper：

```go
// marshalAllowedAgentIDs 去重 + 剔 0 后 JSON 化；空切片 → ""（表示不限制）。
func marshalAllowedAgentIDs(ids []int64) string {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id == 0 || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) == 0 {
		return ""
	}
	b, _ := json.Marshal(out)
	return string(b)
}
```

- [ ] **Step 4: 跑测试确认通过（含既有 CreateRun 测试不回归）**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestCreateRun -v`
Expected: 新用例与 `TestCreateRun_BuildsRunRootSessionAndTask` 均 PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/create.go internal/service/orch_svc/create_test.go \
  -m "✨ orchestration: CreateRun 持久化可参与 agent(去重剔零 JSON)"
```

---

## Task 4: Dispatch 拒绝集外 agent

**Files:**
- Modify: `internal/service/orch_svc/orch.go`（加 `errAgentNotAllowed`）、`internal/service/orch_svc/dispatch.go`
- Test: `internal/service/orch_svc/dispatch_test.go`

**Interfaces:**
- Consumes: `run.IsAgentAllowed`（Task 1）。
- Produces: `errAgentNotAllowed`（`orch.go`，`errors.New("orch: target agent not in allowed set")`）。

> 校验放在**既有 run 加载点**（替换 `runProjectID` 调用，位于 `CountByRunAgent` 之后）→ 现有 Dispatch 用例零改动（`TestDispatch_SpawnsChildSessionAndTask`/`_EnsureOrchSessionError` 已 mock 一次 `runs.Find`；`_NilParent`/`_NilAgent`/`_CountByRunAgentError` 在到达该点前已返回）。

- [ ] **Step 1: 写失败测试**（追加到 `dispatch_test.go`）

```go
func TestDispatch_RejectsAgentOutsideAllowedSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "外人").Return(&agent_entity.Agent{ID: 9, Name: "外人"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(9)).Return(int64(0), nil)
	// 可参与集 {3,4}、Leader=2；目标 9 不在集内且非 Leader → 拒绝，不建会话/任务。
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2, AllowedAgentIDs: "[3,4]"}, nil)

	Convey("dispatch 集外 agent → errAgentNotAllowed", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "外人", "brief", false)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in allowed set")
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestDispatch_RejectsAgentOutsideAllowedSet -v`
Expected: FAIL（当前不校验，会继续走 `EnsureOrchSession`（未 mock）→ 报 unexpected call 或不返回预期错误）。

- [ ] **Step 3: 实现校验**

`orch.go` 的错误块（`errAgentNotFound` 那组，约 `:25-27`）加一行：

```go
	errAgentNotAllowed = errors.New("orch: target agent not in allowed set")
```

`dispatch.go` 把 `runProjectID` 块（约 `:36-39`）替换为：

```go
	run, err := s.runs.Find(ctx, parent.RunID)
	if err != nil {
		return 0, err
	}
	if run != nil && !run.IsAgentAllowed(target.ID, run.LeaderAgentID) {
		return 0, errAgentNotAllowed
	}
	var projectID int64
	if run != nil {
		projectID = run.ProjectID
	}
```

（`runProjectID` 保留——`ask.go` 仍用，Task 5 再删。）

- [ ] **Step 4: 跑测试确认通过（含既有 Dispatch 用例不回归）**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestDispatch -v`
Expected: 新用例 + 5 个既有 `TestDispatch_*` 全 PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/orch.go internal/service/orch_svc/dispatch.go internal/service/orch_svc/dispatch_test.go \
  -m "✨ orchestration: dispatch 拒绝可参与范围外 agent"
```

---

## Task 5: Ask 拒绝集外 agent（+ resolve 收敛为单次 run 加载）

**Files:**
- Modify: `internal/service/orch_svc/ask.go`
- Test: `internal/service/orch_svc/ask_test.go`

**Interfaces:**
- Consumes: `run.IsAgentAllowed`（Task 1）、`errAgentNotAllowed`（Task 4）。
- Produces: `resolveOrCreateAgentSession(ctx, runID, projectID, agentID int64, title string) (int64, error)`（签名新增 `projectID`）；删除 `runProjectID`。

> Ask 复用会话路径今天不加载 run，故新增校验必然引入一次 `runs.Find`。5 个传 `nil` runs 的既有用例需补 runs mock（见 Step 3B）。

- [ ] **Step 1: 写失败测试**（追加到 `ask_test.go`）

```go
func TestAsk_RejectsAgentOutsideAllowedSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "外人").Return(&agent_entity.Agent{ID: 9, Name: "外人"}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2, AllowedAgentIDs: "[3,4]"}, nil)

	Convey("ask 集外 agent → errAgentNotAllowed(不注入不建会话)", t, func() {
		_, err := orch_svc.Default().Ask(context.Background(), 500, "外人", "q?")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in allowed set")
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestAsk_RejectsAgentOutsideAllowedSet -v`
Expected: FAIL（当前不校验，走 `resolveOrCreateAgentSession`（`ListByRun` 等未 mock）→ 非预期错误/panic）。

- [ ] **Step 3A: 实现 Ask 校验 + resolve 收敛**

`ask.go` 在 `FindByName` 的 nil 校验（`:28-31`）**之后**、`resolveOrCreateAgentSession` 调用（`:32`）**之前**插入：

```go
	run, err := s.runs.Find(ctx, from.RunID)
	if err != nil {
		return "", err
	}
	if run != nil && !run.IsAgentAllowed(target.ID, run.LeaderAgentID) {
		return "", errAgentNotAllowed
	}
	var projectID int64
	if run != nil {
		projectID = run.ProjectID
	}
	toSession, err := s.resolveOrCreateAgentSession(ctx, from.RunID, projectID, target.ID, question)
```

（即把原 `:32` 那行 `toSession, err := s.resolveOrCreateAgentSession(ctx, from.RunID, target.ID, question)` 一并替换掉。）

改 `resolveOrCreateAgentSession` 签名与新建分支（`:131-155`），收 `projectID` 参数、删内部 `runProjectID`：

```go
func (s *orchSvc) resolveOrCreateAgentSession(ctx context.Context, runID, projectID, agentID int64, title string) (int64, error) {
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
			return t.SessionID, nil
		}
		fallback = t.SessionID
	}
	if fallback != 0 {
		return fallback, nil
	}
	return s.chat.EnsureOrchSession(ctx, EnsureOrchSessionInput{AgentID: agentID, RunID: runID, Title: title, ProjectID: projectID})
}
```

删除 `dispatch.go` 里现已无人调用的 `runProjectID`（`:94-104`）。

- [ ] **Step 3B: 给 5 个复用会话用例补 runs mock**

以下 `ask_test.go` 用例当前 `RegisterDeps(chat, agents, nil, tasks, nil, nil)`（复用活会话/历史会话，不曾用 runs）。逐个改：把 `nil`（第 3 个参数）换成新建的 `runs := mock_orch_repo.NewMockRunRepo(ctrl)`，并加一条 `runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100}, nil)`（空 allowed 集 → 放行，行为不变）：

- `TestAsk_InjectLiveSessionThenReplyResolves`（约 `:46`）
- `TestAsk_BusyTargetSteersIntoCurrentTurn`（约 `:135`）
- `TestAsk_EscapesXMLInQuestionAndAskerName`（约 `:173`）
- `TestAsk_EmitsAskAndReplyEvents`（约 `:223`）
- 约 `:251` 处 `RegisterDeps(chat, agents, nil, tasks, nil, nil)` 的用例（`ListByRun`/reply-foreign 一类）

`TestAsk_CreatesSessionWithQuestionTitle`（约 `:88`，已有 `runs.EXPECT().Find(...100).Return({ProjectID:55})`）：**保持一条 Find**（现在这条同时供校验 + projectID），无需加第二条；确认其返回的 run `AllowedAgentIDs` 为空即放行。

> `mock_orch_repo` 该文件已 import（`TestAsk_CreatesSessionWithQuestionTitle` 在用），无需加 import。

- [ ] **Step 4: 跑测试确认通过（Ask 全绿）**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestAsk -v`
Expected: 新用例 + 全部既有 `TestAsk_*` PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/ask.go internal/service/orch_svc/dispatch.go internal/service/orch_svc/ask_test.go \
  -m "✨ orchestration: ask 拒绝可参与范围外 agent(resolve 收敛单次 run 加载)"
```

---

## Task 6: agent_list 只列可参与集

**Files:**
- Modify: `internal/service/orch_svc/query.go`（加 `ListAllowedAgents`）、`internal/service/orch_svc/mcp.go`（`handleAgentList` 带 `ref`）
- Test: `internal/service/orch_svc/query_test.go`

**Interfaces:**
- Consumes: `AgentLookup.List`、`TaskRepo.FindBySession`、`RunRepo.Find`、`run.AllowedSet`（Task 1）。
- Produces: `(*orchSvc).ListAllowedAgents(ctx context.Context, sessionID int64) ([]*agent_entity.Agent, error)`。

- [ ] **Step 1: 写失败测试**（追加到 `query_test.go`；确认其 import 含 `agent_entity`/`mock_orch_repo`/`mock_orch_svc`，缺则补）

```go
func TestListAllowedAgents_FiltersToAllowedSetPlusLeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 2, Name: "L"}, {ID: 3, Name: "A"}, {ID: 4, Name: "B"}, {ID: 9, Name: "X"},
	}, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{RunID: 100}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2, AllowedAgentIDs: "[3,4]"}, nil)

	Convey("agent_list 只回 allowed∪{leader}", t, func() {
		got, err := orch_svc.Default().ListAllowedAgents(context.Background(), 500)
		So(err, ShouldBeNil)
		ids := []int64{}
		for _, a := range got {
			ids = append(ids, a.ID)
		}
		So(ids, ShouldResemble, []int64{2, 3, 4}) // 保 List 顺序，排除 9
	})
}

func TestListAllowedAgents_EmptySetReturnsAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{{ID: 3}, {ID: 9}}, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{RunID: 100}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, AllowedAgentIDs: ""}, nil)

	Convey("空集合 → 回全部", t, func() {
		got, err := orch_svc.Default().ListAllowedAgents(context.Background(), 500)
		So(err, ShouldBeNil)
		So(len(got), ShouldEqual, 2)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestListAllowedAgents -v`
Expected: 编译失败（`ListAllowedAgents` 未定义）。

- [ ] **Step 3: 实现服务方法 + 接线 handler**

`query.go` 顶部 import 确认含 `"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"`（缺则补），加方法：

```go
// ListAllowedAgents 返回调用者所在 Run 的可参与 agent（allowed∪{leader}；集合空/定位不到 Run → 全部）。
func (s *orchSvc) ListAllowedAgents(ctx context.Context, sessionID int64) ([]*agent_entity.Agent, error) {
	all, err := s.agents.List(ctx)
	if err != nil {
		return nil, err
	}
	tk, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil || tk == nil {
		return all, nil
	}
	run, err := s.runs.Find(ctx, tk.RunID)
	if err != nil || run == nil {
		return all, nil
	}
	set := run.AllowedSet()
	if len(set) == 0 {
		return all, nil
	}
	out := make([]*agent_entity.Agent, 0, len(all))
	for _, a := range all {
		if set[a.ID] || a.ID == run.LeaderAgentID {
			out = append(out, a)
		}
	}
	return out, nil
}
```

`mcp.go`：`dispatchTool` 的 `agent_list` 分支（`:143-144`）改为传 `ref`：

```go
	case "agent_list":
		m.handleAgentList(w, r, id, ref)
```

`handleAgentList`（`:275`）改签名并换数据源：

```go
func (m *orchMCP) handleAgentList(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef) {
	list, err := m.svc.ListAllowedAgents(r.Context(), ref.sessionID)
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
```

（`agent_list` 工具 schema 描述 `orchToolSchemas()` `:293` 可同步微调为「列出你可调度的 agent（受本次可参与范围约束）」——非必须，但更准确。）

- [ ] **Step 4: 跑测试确认通过 + mcp_test 不回归**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run 'TestListAllowedAgents|TestMCP|AgentList' -v`
Expected: 新用例 PASS；既有 `mcp_test.go` 里 agent_list 相关用例若断言「返回全部」需同步（该 Run 无 allowed 数据时仍返回全部 → 一般不回归；若回归按空集合语义修断言）。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/query.go internal/service/orch_svc/mcp.go internal/service/orch_svc/query_test.go \
  -m "✨ orchestration: agent_list 只列本次可参与 agent(allowed∪leader)"
```

---

## 收尾验证（全部任务后）

- [ ] `make test-backend | tail -5` — 看真 exit code（focused 测试漏跨包 sqlmock/goroutine flake）。
- [ ] `make lint | tail -5` — golangci-lint v2（vitest 不查 Go 类型/格式）。
- [ ] 手验（可选）：新建一个限定 2 个 agent 的编排，让 Leader `agent_list` → 应只见 2 个；`dispatch` 集外名 → 收到「not in allowed set」错误。

## Self-Review（对照 spec §E）

- E.1 数据模型：Task 1（实体字段+方法）+ Task 2（迁移）✓
- E.2 CreateRun 落库：Task 3 ✓
- E.3 强制点：`agent_list` 过滤 Task 6 ✓；`Dispatch` 拒绝 Task 4 ✓；`Ask` 拒绝 Task 5 ✓；`reply`/`send` 不按名解析 → 无需改（spec 已述）✓
- 空=全部、Leader 恒允许：Task 1 `AllowedSet`/`IsAgentAllowed` + 各消费点 ✓
- `subagent_svc` 不在范围：本 plan 未触及 ✓
- 类型一致性：`AllowedAgentIDs string`（实体）↔ `[]int64`（请求，marshal 于 Task 3）↔ `IsAgentAllowed(agentID, leaderID)` 调用点（Task 4/5）↔ `ListAllowedAgents(ctx, sessionID)`（Task 6 定义 + mcp 调用）一致 ✓
- 占位符扫描：无 TBD/TODO；测试代码均给全 ✓
