# Issue 看板核心 Implementation Plan（Plan 1 / 2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 issue 模块重构成以看板为主导——4 固定阶段列 + 拖拽跨列改 stage + 列内拖拽排序（持久化 position），看板成为 `/issues` 默认视图，List 视图保留为次要切换。

**Architecture:** 沿用 cago 单向分层 `app → service → repository → entity`。`issues` 表新增 `stage/position/assignee_agent_id/session_id` 四列（本计划只用前两列；后两列为 Plan 2 派发预留，随迁移一次性建好避免二次 ALTER）。`stage` 成为主生命周期字段，`state`(open/closed) 由 entity 在 `SetStage` 时同步为派生镜像（`closed` ⇔ `stage=done`）。列内排序用 `position REAL` 分数排序（midpoint 插入）。前端用已装依赖 `@dnd-kit/core` + `@dnd-kit/sortable` 做跨列 + 列内拖拽，乐观更新后调 `IssueMove`。

**Tech Stack:** Go 1.26 + gorm + gormigrate；sqlmock（repo 单测）+ go.uber.org/mock（svc 单测）；React 19 + TS + Vite + Tailwind v4 + shadcn + @dnd-kit + react-i18next + Vitest。

## Global Constraints

- 迁移 append 到 `migrations/migrations.go` 的 `migrationList()` 末尾（当前最新 `202606250002()`），**禁改既有迁移**；DDL 用原生 SQL；回填在同一迁移内。
- repo 单测一律 `testutils.Database(t)` + sqlmock，禁起真库；svc 单测用 mockgen repo mock + `RegisterXxx` 注入，不接 DB。
- service 只依赖 repository 接口（DIP）；`internal/app` 绑定层只 parse → `svc.Method` → return，不塞业务。
- 关键流程 `logger.Ctx(ctx)`，message 前缀 `issue_svc.Method:` 小写，动态值走 `zap.Xxx`。
- 新可见前端文案一律 `react-i18next` `t(...)`，`frontend/src/i18n/locales/{zh-CN,en}/common.json` 同步；表单控件用 shadcn `@/components/ui/*`，禁原生 `<select>`；不翻译 agent/会话/仓库等动态内容。
- gitmoji commit；diff 只含 producer + 测试，无 drive-by refactor / 格式化 / 导入重排。
- 共享分支 `develop/wyz` 有并发会话：提交一律带 pathspec（`git commit <files...>`），禁裸 `git commit`。
- `stage` 枚举：`todo` / `doing` / `review` / `done`。`POSITION_STEP = 65536`。

---

## Task 1: 迁移 —— issues 表新增 stage/position/assignee_agent_id/session_id + 回填

**Files:**
- Create: `migrations/202607020001_issue_kanban.go`
- Create: `migrations/202607020001_issue_kanban_test.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾追加一行）

**Interfaces:**
- Produces: `issues` 表含列 `stage TEXT`、`position REAL`、`assignee_agent_id INTEGER`、`session_id INTEGER`；回填 `stage`（open→todo / closed→done）、`position=createtime`。

- [ ] **Step 1: 写迁移文件**

`migrations/202607020001_issue_kanban.go`:

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607020001 issue 看板:新增 stage(4 阶段主生命周期)/position(列内分数排序)/
// assignee_agent_id/session_id(Plan 2 派发预留)。回填 stage 由 state 派生、position 取 createtime。
func migration202607020001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607020001",
		Migrate: func(tx *gorm.DB) error {
			stmts := []string{
				`ALTER TABLE issues ADD COLUMN stage TEXT NOT NULL DEFAULT 'todo'`,
				`ALTER TABLE issues ADD COLUMN position REAL NOT NULL DEFAULT 0`,
				`ALTER TABLE issues ADD COLUMN assignee_agent_id INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE issues ADD COLUMN session_id INTEGER NOT NULL DEFAULT 0`,
				`UPDATE issues SET stage = CASE WHEN state = 'closed' THEN 'done' ELSE 'todo' END`,
				`UPDATE issues SET position = createtime`,
				`CREATE INDEX IF NOT EXISTS idx_issues_board ON issues (status, stage, position)`,
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
				`DROP INDEX IF EXISTS idx_issues_board`,
				`ALTER TABLE issues DROP COLUMN session_id`,
				`ALTER TABLE issues DROP COLUMN assignee_agent_id`,
				`ALTER TABLE issues DROP COLUMN position`,
				`ALTER TABLE issues DROP COLUMN stage`,
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

- [ ] **Step 2: 在 migrationList() 末尾登记**

在 `migrations/migrations.go` 中，`migration202606250002(),` 那一行之后追加：

```go
		migration202607020001(), // issue 看板:stage/position/assignee_agent_id/session_id + 回填
```

- [ ] **Step 3: 写迁移测试（验证列存在 + 回填）**

`migrations/202607020001_issue_kanban_test.go`（迁移测试是允许接真库的例外之一）:

```go
package migrations

import (
	"testing"

	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMigration202607020001(t *testing.T) {
	db := testutils.NewMigrationDB(t, migrationList()) // 见既有迁移 *_test.go 的建库/跑迁移 helper 用法
	// 插入一条 closed + 一条 open 的旧 issue，验证回填后的 stage/position。
	require.NoError(t, db.Exec(`INSERT INTO issues (id, title, state, status, createtime) VALUES (1,'a','closed',1,1000),(2,'b','open',1,2000)`).Error)

	type row struct {
		Stage    string
		Position float64
	}
	var r1, r2 row
	require.NoError(t, db.Raw(`SELECT stage, position FROM issues WHERE id = 1`).Scan(&r1).Error)
	require.NoError(t, db.Raw(`SELECT stage, position FROM issues WHERE id = 2`).Scan(&r2).Error)
	assert.Equal(t, "done", r1.Stage)
	assert.Equal(t, "todo", r2.Stage)
	assert.Equal(t, float64(1000), r1.Position)
}
```

> 注：`testutils.NewMigrationDB` 是占位名 —— 打开 `migrations/` 下任一现有 `*_test.go`，照抄它建库并执行 `migrationList()` 的方式（`bootstrap/cago_test.go` 亦是允许接真库的例外）。若现有迁移无独立测试，则本步骤改为在 `bootstrap/cago_test.go` 的迁移路径里断言列存在。

- [ ] **Step 4: 跑迁移测试**

Run: `go test ./migrations/ -run TestMigration202607020001 -v`
Expected: PASS（stage/position 回填正确）。

- [ ] **Step 5: Commit**

```bash
git add migrations/202607020001_issue_kanban.go migrations/202607020001_issue_kanban_test.go migrations/migrations.go
git commit -m "🗃️ issue: 迁移新增 stage/position/assignee/session 列 + 回填 (202607020001)"
```

---

## Task 2: 实体 —— Issue 增 stage/position 字段 + SetStage + Check 校验

**Files:**
- Modify: `internal/model/entity/issue_entity/issue.go`
- Modify: `internal/model/entity/issue_entity/issue_test.go`

**Interfaces:**
- Consumes: 迁移列（Task 1）。
- Produces:
  - 常量 `StageTodo="todo"`、`StageDoing="doing"`、`StageReview="review"`、`StageDone="done"`；`func IsKnownStage(s string) bool`。
  - 字段 `Stage string`、`Position float64`、`AssigneeAgentID int64`、`SessionID int64`（gorm 列名 `stage/position/assignee_agent_id/session_id`）。
  - 方法 `func (i *Issue) SetStage(stage string, now int64)`：置 `Stage`；`done` → `Close(now)`；离开 `done`（原为 done、现非 done）→ `Reopen()`。
  - `Check` 增加 `stage ∈ known` 校验（空 `stage` 视为 `todo` 合法，见 Step 3）。

- [ ] **Step 1: 写失败测试**

在 `internal/model/entity/issue_entity/issue_test.go` 追加：

```go
func TestIssueSetStage_DoneClosesAndReopens(t *testing.T) {
	i := &issue_entity.Issue{State: issue_entity.StateOpen, Stage: issue_entity.StageTodo}

	i.SetStage(issue_entity.StageDone, 1234)
	assert.Equal(t, issue_entity.StageDone, i.Stage)
	assert.Equal(t, issue_entity.StateClosed, i.State)
	assert.Equal(t, int64(1234), i.ClosedAt)

	i.SetStage(issue_entity.StageDoing, 5678)
	assert.Equal(t, issue_entity.StageDoing, i.Stage)
	assert.Equal(t, issue_entity.StateOpen, i.State)
	assert.Equal(t, int64(0), i.ClosedAt)
}

func TestIssueCheck_RejectsUnknownStage(t *testing.T) {
	i := &issue_entity.Issue{Title: "x", State: issue_entity.StateOpen, Stage: "bogus"}
	assert.Error(t, i.Check(context.Background()))
}

func TestIssueCheck_EmptyStageDefaultsValid(t *testing.T) {
	i := &issue_entity.Issue{Title: "x", State: issue_entity.StateOpen, Stage: ""}
	assert.NoError(t, i.Check(context.Background()))
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/model/entity/issue_entity/ -run TestIssueSetStage -v`
Expected: FAIL（`StageTodo` / `SetStage` undefined）。

- [ ] **Step 3: 实现**

在 `internal/model/entity/issue_entity/issue.go` 的 const 块补充阶段常量：

```go
const (
	StageTodo   = "todo"
	StageDoing  = "doing"
	StageReview = "review"
	StageDone   = "done"
)

// IsKnownStage 空串视为 todo 合法（迁移默认 / 旧行兜底）。
func IsKnownStage(s string) bool {
	switch s {
	case "", StageTodo, StageDoing, StageReview, StageDone:
		return true
	default:
		return false
	}
}
```

在 `Issue` struct 追加字段（放在 `AgentStatus` 之后、`Source` 之前，保持列可读顺序）：

```go
	Stage           string  `gorm:"column:stage;type:text;not null;default:'todo'"`
	Position        float64 `gorm:"column:position;type:real;not null;default:0"`
	AssigneeAgentID int64   `gorm:"column:assignee_agent_id;type:bigint;not null;default:0"`
	SessionID       int64   `gorm:"column:session_id;type:bigint;not null;default:0"`
```

追加方法：

```go
// SetStage 置阶段并同步 state：done=关闭，离开 done=重开。
func (i *Issue) SetStage(stage string, now int64) {
	wasDone := i.Stage == StageDone
	i.Stage = stage
	if stage == StageDone {
		i.Close(now)
		return
	}
	if wasDone {
		i.Reopen()
	}
}
```

在 `Check` 里、`state` 校验之后追加：

```go
	if !IsKnownStage(i.Stage) {
		return i18n.NewError(ctx, code.IssueInvalidState)
	}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/model/entity/issue_entity/ -v`
Expected: PASS（全部实体测试）。

- [ ] **Step 5: Commit**

```bash
git add internal/model/entity/issue_entity/issue.go internal/model/entity/issue_entity/issue_test.go
git commit -m "✨ issue: 实体新增 stage/position + SetStage 同步 state"
```

---

## Task 3: 仓储 —— List 支持 stage 过滤/position 排序 + StageCounts + Update 覆盖新列

**Files:**
- Modify: `internal/repository/issue_repo/issue.go`
- Modify: `internal/repository/issue_repo/issue_test.go`
- Regenerate: `internal/repository/issue_repo/mock_issue_repo/mock_issue.go`（`make mock`）

**Interfaces:**
- Consumes: 实体字段（Task 2）。
- Produces:
  - `ListFilter` 增字段 `Stage string`（"" = 不筛选）。
  - `List`：`Sort == "position"` 时 `ORDER BY stage, position ASC, id ASC`；否则 `updatetime DESC, id DESC`。`Stage != ""` 时加 `stage = ?` 过滤。
  - `IssueRepo` 新方法 `StageCounts(ctx, filter ListFilter) (map[string]int64, error)`。
  - `Update` 的 map 增 `stage / position / assignee_agent_id / session_id`。

- [ ] **Step 1: 写失败测试**

在 `internal/repository/issue_repo/issue_test.go` 追加：

```go
func TestIssueList_PositionSort(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `issues` WHERE status = \\? AND stage = \\? ORDER BY stage, position ASC, id ASC").
		WithArgs(consts.ACTIVE, issue_entity.StageDoing).
		WillReturnRows(sqlmock.NewRows([]string{"id", "stage", "position"}).
			AddRow(int64(3), issue_entity.StageDoing, 10.0).
			AddRow(int64(4), issue_entity.StageDoing, 20.0))

	rows, err := repo.List(ctx, issue_repo.ListFilter{Stage: issue_entity.StageDoing, Sort: "position"})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, int64(3), rows[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueStageCounts(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT stage, count\\(\\*\\) as cnt FROM `issues` WHERE status = \\? GROUP BY `stage`").
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"stage", "cnt"}).
			AddRow(issue_entity.StageTodo, int64(2)).
			AddRow(issue_entity.StageDone, int64(5)))

	got, err := repo.StageCounts(ctx, issue_repo.ListFilter{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), got[issue_entity.StageTodo])
	assert.Equal(t, int64(5), got[issue_entity.StageDone])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueUpdate_WritesStagePosition(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `issues` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.Update(ctx, &issue_entity.Issue{ID: 7, Stage: issue_entity.StageDoing, Position: 12.5})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/repository/issue_repo/ -run 'TestIssueList_PositionSort|TestIssueStageCounts' -v`
Expected: FAIL（`StageCounts` undefined / 排序 SQL 不匹配）。

- [ ] **Step 3: 实现**

在 `ListFilter` 增字段：

```go
	Stage string // "" = 不筛选
```

`List` 改为：

```go
func (r *issueRepo) List(ctx context.Context, filter ListFilter) ([]*issue_entity.Issue, error) {
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).Where("status = ?", consts.ACTIVE)
	if filter.State != "" {
		q = q.Where("state = ?", filter.State)
	}
	if filter.Stage != "" {
		q = q.Where("stage = ?", filter.Stage)
	}
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if len(filter.LabelIDs) > 0 {
		sub := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
			Select("issue_id").Where("label_id IN ?", filter.LabelIDs)
		q = q.Where("id IN (?)", sub)
	}
	order := "updatetime DESC, id DESC"
	if filter.Sort == "position" {
		order = "stage, position ASC, id ASC"
	}
	var rows []*issue_entity.Issue
	err := q.Order(order).Find(&rows).Error
	return rows, err
}
```

`Update` 的 map 追加四列：

```go
			"stage":             i.Stage,
			"position":          i.Position,
			"assignee_agent_id": i.AssigneeAgentID,
			"session_id":        i.SessionID,
```

新增 `StageCounts`（`CountByState` 之后）：

```go
func (r *issueRepo) StageCounts(ctx context.Context, filter ListFilter) (map[string]int64, error) {
	type agg struct {
		Stage string
		Cnt   int64
	}
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Select("stage, count(*) as cnt").
		Where("status = ?", consts.ACTIVE)
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if len(filter.LabelIDs) > 0 {
		sub := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
			Select("issue_id").Where("label_id IN ?", filter.LabelIDs)
		q = q.Where("id IN (?)", sub)
	}
	var rows []agg
	if err := q.Group("stage").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]int64, 4)
	for _, row := range rows {
		out[row.Stage] = row.Cnt
	}
	return out, nil
}
```

在 `IssueRepo` 接口内加声明：

```go
	StageCounts(ctx context.Context, filter ListFilter) (map[string]int64, error)
```

- [ ] **Step 4: 重生成 mock 并跑测试**

Run: `make mock && go test ./internal/repository/issue_repo/ -v`
Expected: PASS（含新增三测试；mock 含 `StageCounts`）。

- [ ] **Step 5: Commit**

```bash
git add internal/repository/issue_repo/issue.go internal/repository/issue_repo/issue_test.go internal/repository/issue_repo/mock_issue_repo/mock_issue.go
git commit -m "✨ issue: 仓储支持 stage 过滤/position 排序 + StageCounts"
```

---

## Task 4: 服务 —— Create 默认 stage/position + Move 排序 + List 返回 stageCounts

**Files:**
- Modify: `internal/service/issue_svc/issue.go`
- Modify: `internal/service/issue_svc/types.go`
- Modify: `internal/service/issue_svc/issue_test.go`

**Interfaces:**
- Consumes: `issue_repo.List(sort="position")`、`issue_repo.StageCounts`（Task 3）；`entity.SetStage`（Task 2）。
- Produces:
  - `CreateIssueRequest` 增 `Stage string`（""→todo）。
  - `MoveIssueRequest{ ID int64; Stage string; AfterID int64 }`（`AfterID=0`→列顶）。
  - `ListIssuesResponse` 增 `StageCounts map[string]int64`。
  - `IssueSvc` 增 `Move(ctx, *MoveIssueRequest) (*IssueDetail, error)`。
  - 常量 `positionStep = 65536.0`（包内私有）。

- [ ] **Step 1: 写失败测试**

在 `internal/service/issue_svc/issue_test.go` 追加：

```go
func TestIssueSvcCreate_DefaultsStageAndAppendsPosition(t *testing.T) {
	ctx, mi, _, mil, svc := setupIssueSvc(t)
	// 该 stage 已有末位 position=100 → 新卡 position=100+step。
	mi.EXPECT().List(ctx, issue_repo.ListFilter{Stage: issue_entity.StageTodo, Sort: "position"}).
		Return([]*issue_entity.Issue{{ID: 1, Stage: issue_entity.StageTodo, Position: 100}}, nil)
	mi.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, issue_entity.StageTodo, i.Stage)
		assert.Equal(t, float64(100+65536), i.Position)
		i.ID = 9
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(9), []int64(nil)).Return(nil)

	got, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo"})
	require.NoError(t, err)
	assert.Equal(t, int64(9), got.Issue.ID)
}

func TestIssueSvcMove_MidpointBetweenNeighbors(t *testing.T) {
	ctx, mi, _, mil, svc := setupIssueSvc(t)
	moving := &issue_entity.Issue{ID: 5, Stage: issue_entity.StageTodo, Position: 5, State: issue_entity.StateOpen}
	mi.EXPECT().Find(ctx, int64(5)).Return(moving, nil)
	// 目标列 doing 顺序：[id=3 pos=10, id=4 pos=20]；AfterID=3 → 落在 3 与 4 之间 → 15。
	mi.EXPECT().List(ctx, issue_repo.ListFilter{Stage: issue_entity.StageDoing, Sort: "position"}).
		Return([]*issue_entity.Issue{
			{ID: 3, Stage: issue_entity.StageDoing, Position: 10},
			{ID: 4, Stage: issue_entity.StageDoing, Position: 20},
		}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, issue_entity.StageDoing, i.Stage)
		assert.Equal(t, float64(15), i.Position)
		return nil
	})
	mil.EXPECT().ListByIssue(ctx, int64(5)).Return(nil, nil)
	ml := issue_repo.Label()
	_ = ml

	_, err := svc.Move(ctx, &issue_svc.MoveIssueRequest{ID: 5, Stage: issue_entity.StageDoing, AfterID: 3})
	require.NoError(t, err)
}

func TestIssueSvcMove_TopOfColumn(t *testing.T) {
	ctx, mi, _, mil, svc := setupIssueSvc(t)
	moving := &issue_entity.Issue{ID: 5, Stage: issue_entity.StageTodo, Position: 5, State: issue_entity.StateOpen}
	mi.EXPECT().Find(ctx, int64(5)).Return(moving, nil)
	mi.EXPECT().List(ctx, issue_repo.ListFilter{Stage: issue_entity.StageReview, Sort: "position"}).
		Return([]*issue_entity.Issue{{ID: 8, Stage: issue_entity.StageReview, Position: 40}}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, float64(40-65536), i.Position) // AfterID=0 → 顶部 = 首元素 - step
		return nil
	})
	mil.EXPECT().ListByIssue(ctx, int64(5)).Return(nil, nil)

	_, err := svc.Move(ctx, &issue_svc.MoveIssueRequest{ID: 5, Stage: issue_entity.StageReview, AfterID: 0})
	require.NoError(t, err)
}
```

> 注：`Move` 结尾走 `hydrate`，因此期望 `IssueLabel().ListByIssue` + `Label().ListByIDs`。若某测试 `ListByIssue` 返回 nil，则 `Label().ListByIDs` 不会被调用（`hydrate` 中 `ListByIDs(nil)` 仍会调用——按 `resolveLabels` 惯例 mock 一个 `ml.EXPECT().ListByIDs(ctx, []int64(nil)).Return(nil,nil)`；若跑测报未预期调用，据实补 mock）。

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/service/issue_svc/ -run 'TestIssueSvcMove|DefaultsStage' -v`
Expected: FAIL（`Move` / `MoveIssueRequest` undefined）。

- [ ] **Step 3: 实现 types**

在 `internal/service/issue_svc/types.go`：`CreateIssueRequest` 增 `Stage string`；追加 `MoveIssueRequest`；`ListIssuesResponse` 增 `StageCounts`：

```go
type CreateIssueRequest struct {
	ProjectID int64
	Title     string
	Body      string
	Stage     string // "" = todo
	LabelIDs  []int64
}

type MoveIssueRequest struct {
	ID      int64
	Stage   string
	AfterID int64 // 0 = 落在目标列顶部
}
```

`ListIssuesResponse` 增字段：

```go
	StageCounts map[string]int64
```

- [ ] **Step 4: 实现 svc**

在 `internal/service/issue_svc/issue.go`：

顶部包级常量：

```go
const positionStep = 65536.0
```

接口加声明：

```go
	Move(ctx context.Context, req *MoveIssueRequest) (*IssueDetail, error)
```

`Create` 内，构造 `issue` 时 `State/AgentStatus` 之后加 stage，并在 `issue.Check` 之后、`resolveLabels` 之前算 position：

```go
	stage := req.Stage
	if stage == "" {
		stage = issue_entity.StageTodo
	}
	issue := &issue_entity.Issue{
		ProjectID:   req.ProjectID,
		Title:       strings.TrimSpace(req.Title),
		Body:        req.Body,
		State:       issue_entity.StateOpen,
		Stage:       stage,
		AgentStatus: issue_entity.AgentStatusIdle,
		Source:      issue_entity.SourceManual,
		Status:      consts.ACTIVE,
		Createtime:  now,
		Updatetime:  now,
	}
	if stage == issue_entity.StageDone {
		issue.SetStage(issue_entity.StageDone, now)
	}
	if err := issue.Check(ctx); err != nil {
		return nil, err
	}
	pos, err := s.appendPosition(ctx, stage)
	if err != nil {
		return nil, err
	}
	issue.Position = pos
```

新增私有方法（放在 `hydrate` 附近）：

```go
// appendPosition 返回目标 stage 末位之后的 position。
func (s *issueSvc) appendPosition(ctx context.Context, stage string) (float64, error) {
	rows, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{Stage: stage, Sort: "position"})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return positionStep, nil
	}
	return rows[len(rows)-1].Position + positionStep, nil
}

// Move 改 stage + 计算列内 position（AfterID 之后 / 顶部）。
func (s *issueSvc) Move(ctx context.Context, req *MoveIssueRequest) (*IssueDetail, error) {
	if !issue_entity.IsKnownStage(req.Stage) || req.Stage == "" {
		return nil, i18n.NewError(ctx, code.IssueInvalidState)
	}
	issue, err := issue_repo.Issue().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	siblings, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{Stage: req.Stage, Sort: "position"})
	if err != nil {
		return nil, err
	}
	pos := computePosition(siblings, req.ID, req.AfterID)
	issue.SetStage(req.Stage, s.now())
	issue.Position = pos
	logger.Ctx(ctx).Info("issue_svc.Move: reposition",
		zap.Int64("issueId", req.ID), zap.String("stage", req.Stage), zap.Float64("position", pos))
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	return s.hydrate(ctx, issue)
}

// computePosition：在 siblings（按 position 升序，可能含自身）中，
// 把卡片放到 afterID 之后。afterID=0 → 顶部。落点相邻两卡取中点；顶/底外扩 step。
func computePosition(siblings []*issue_entity.Issue, movingID, afterID int64) float64 {
	// 过滤掉自身，得到目标列的稳定序列。
	seq := make([]*issue_entity.Issue, 0, len(siblings))
	for _, it := range siblings {
		if it.ID != movingID {
			seq = append(seq, it)
		}
	}
	if len(seq) == 0 {
		return positionStep
	}
	if afterID == 0 {
		return seq[0].Position - positionStep
	}
	for idx, it := range seq {
		if it.ID != afterID {
			continue
		}
		if idx == len(seq)-1 {
			return it.Position + positionStep
		}
		return (it.Position + seq[idx+1].Position) / 2
	}
	// afterID 不在目标列（异常）→ 落底。
	return seq[len(seq)-1].Position + positionStep
}
```

`List` 结尾把 stageCounts 塞进响应：

```go
	stageCounts, err := issue_repo.Issue().StageCounts(ctx, issue_repo.ListFilter{
		ProjectID: req.ProjectID, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return &ListIssuesResponse{Issues: details, OpenCount: open, ClosedCount: closed, StageCounts: stageCounts}, nil
```

- [ ] **Step 5: 跑测试确认通过**

Run: `go test ./internal/service/issue_svc/ -v`
Expected: PASS（含 Create 默认 stage/position、Move 中点/顶部；据 Step 1 注释补齐 hydrate 的 label mock）。

- [ ] **Step 6: Commit**

```bash
git add internal/service/issue_svc/issue.go internal/service/issue_svc/types.go internal/service/issue_svc/issue_test.go
git commit -m "✨ issue: 服务支持 Create 默认 stage/position + Move 列内排序 + stageCounts"
```

---

## Task 5: 绑定 —— IssueItem 增 stage/position + IssueMove + stageCounts + 刷新 wailsjs

**Files:**
- Modify: `internal/app/issue.go`
- Modify: `internal/app/issue_test.go`
- Regenerate: `frontend/wailsjs/`（`make generate`）

**Interfaces:**
- Consumes: `issue_svc.Move` / `ListIssuesResponse.StageCounts`（Task 4）。
- Produces:
  - `IssueItem` 增 `Stage/Position/AssigneeAgentID/SessionID`（json: `stage/position/assigneeAgentID/sessionID`）。
  - `IssueListResponse` 增 `StageCounts map[string]int64` json:`stageCounts`。
  - `IssueCreateRequest` 增 `Stage string` json:`stage`。
  - `IssueMoveRequest{ ID int64; Stage string; AfterID int64 }`；`App.IssueMove(req) (*IssueItem, error)`。

- [ ] **Step 1: 写失败测试**

在 `internal/app/issue_test.go` 追加（绑定层测试通常 mock svc；照抄该文件既有风格。若既有测试仅编译级冒烟，则加最小断言）：

```go
func TestToIssueItem_MapsStagePosition(t *testing.T) {
	d := &issue_svc.IssueDetail{Issue: &issue_entity.Issue{
		ID: 1, Stage: issue_entity.StageDoing, Position: 12.5, AssigneeAgentID: 3, SessionID: 4,
	}}
	item := toIssueItem(d)
	assert.Equal(t, "doing", item.Stage)
	assert.Equal(t, 12.5, item.Position)
	assert.Equal(t, int64(3), item.AssigneeAgentID)
	assert.Equal(t, int64(4), item.SessionID)
}
```

（`toIssueItem` 为包内函数，测试放在 `package app`。若既有 `issue_test.go` 是 `package app_test`，则把此测试单列一个 `package app` 文件 `internal/app/issue_maps_test.go`。）

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/app/ -run TestToIssueItem_MapsStagePosition -v`
Expected: FAIL（`item.Stage` 不存在）。

- [ ] **Step 3: 实现**

`IssueItem` 追加字段（`AgentStatus` 后）：

```go
	Stage           string `json:"stage"`
	Position        float64 `json:"position"`
	AssigneeAgentID int64  `json:"assigneeAgentID"`
	SessionID       int64  `json:"sessionID"`
```

`toIssueItem` 内补映射：

```go
		Stage:           d.Issue.Stage,
		Position:        d.Issue.Position,
		AssigneeAgentID: d.Issue.AssigneeAgentID,
		SessionID:       d.Issue.SessionID,
```

`IssueListResponse` 增 `StageCounts map[string]int64 \`json:"stageCounts"\``；`IssueList` 组装时透传 `resp.StageCounts`。

`IssueCreateRequest` 增 `Stage string \`json:"stage"\``；`IssueCreate` 透传到 `issue_svc.CreateIssueRequest{... Stage: req.Stage ...}`。

追加 Move DTO + 方法：

```go
type IssueMoveRequest struct {
	ID      int64  `json:"id"`
	Stage   string `json:"stage"`
	AfterID int64  `json:"afterID"`
}

// IssueMove 拖拽:改 stage + 列内 position。
func (a *App) IssueMove(req *IssueMoveRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().Move(a.ctx, &issue_svc.MoveIssueRequest{
		ID: req.ID, Stage: req.Stage, AfterID: req.AfterID,
	})
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}
```

- [ ] **Step 4: 跑测试 + 刷新 binding**

Run: `go test ./internal/app/ -run TestToIssueItem_MapsStagePosition -v && make generate`
Expected: PASS；`frontend/wailsjs/go/app/App.d.ts` 出现 `IssueMove`，`models.ts` 的 `IssueItem` 含 `stage/position`。

- [ ] **Step 5: Commit**

```bash
git add internal/app/issue.go internal/app/issue_test.go internal/app/issue_maps_test.go frontend/wailsjs
git commit -m "✨ issue: 绑定新增 IssueMove + IssueItem stage/position + stageCounts"
```

---

## Task 6: 前端数据 hook —— use-issues 走 position 排序 + stageCounts + moveIssue

**Files:**
- Modify: `frontend/src/hooks/use-issues.ts`
- Create: `frontend/src/hooks/use-issues.test.ts`（若已存在则追加）

**Interfaces:**
- Consumes: `IssueList`（sort 支持 "position"）、`IssueMove`（Task 5 生成的 binding）。
- Produces: `useIssues(filter)` 返回新增 `stageCounts: Record<string, number>` 和 `moveIssue(id, stage, afterID)`；`filter` 增 `sort?: "position" | "updated"`（默认 board 用 "position"）。

- [ ] **Step 1: 写失败测试**

`frontend/src/hooks/use-issues.test.ts`（per-file `vi.mock` wailsjs，见既有 hook 测试风格）：

```ts
import { renderHook, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  IssueList: vi.fn().mockResolvedValue({
    issues: [{ id: 1, stage: "doing", position: 10, labels: [] }],
    openCount: 1,
    closedCount: 0,
    stageCounts: { doing: 1 },
  }),
  IssueListLabels: vi.fn().mockResolvedValue([]),
  IssueMove: vi.fn().mockResolvedValue({ id: 1, stage: "review", position: 5, labels: [] }),
}));

import { IssueList, IssueMove } from "../../wailsjs/go/app/App";
import { useIssues } from "./use-issues";

describe("useIssues", () => {
  it("board 默认按 position 拉取并暴露 stageCounts", async () => {
    const { result } = renderHook(() =>
      useIssues({ state: "", projectID: 0, labelIDs: [], sort: "position" }),
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(IssueList).toHaveBeenCalledWith(
      expect.objectContaining({ sort: "position" }),
    );
    expect(result.current.stageCounts.doing).toBe(1);
  });

  it("moveIssue 调 IssueMove", async () => {
    const { result } = renderHook(() =>
      useIssues({ state: "", projectID: 0, labelIDs: [], sort: "position" }),
    );
    await waitFor(() => expect(result.current.loading).toBe(false));
    await result.current.moveIssue(1, "review", 0);
    expect(IssueMove).toHaveBeenCalledWith({ id: 1, stage: "review", afterID: 0 });
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/hooks/use-issues.test.ts`
Expected: FAIL（`stageCounts` / `moveIssue` undefined）。

- [ ] **Step 3: 实现**

改 `frontend/src/hooks/use-issues.ts`：

```ts
import { useCallback, useEffect, useState } from "react";

import { IssueList, IssueListLabels, IssueMove } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type IssueFilter = {
  state: string;
  projectID: number;
  labelIDs: number[];
  sort?: "position" | "updated";
};

export function useIssues(filter: IssueFilter) {
  const [issues, setIssues] = useState<app.IssueItem[]>([]);
  const [labels, setLabels] = useState<app.LabelItem[]>([]);
  const [openCount, setOpenCount] = useState(0);
  const [closedCount, setClosedCount] = useState(0);
  const [stageCounts, setStageCounts] = useState<Record<string, number>>({});
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { state, projectID } = filter;
  const sort = filter.sort ?? "updated";
  const labelKey = filter.labelIDs.join(",");

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const labelIDs = labelKey ? labelKey.split(",").map(Number) : [];
      const [resp, labelList] = await Promise.all([
        IssueList({ state, projectID, labelIDs, sort }),
        IssueListLabels(),
      ]);
      setIssues(resp?.issues ?? []);
      setOpenCount(resp?.openCount ?? 0);
      setClosedCount(resp?.closedCount ?? 0);
      setStageCounts(resp?.stageCounts ?? {});
      setLabels(labelList ?? []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [state, projectID, sort, labelKey]);

  const moveIssue = useCallback(
    async (id: number, stage: string, afterID: number) => {
      await IssueMove({ id, stage, afterID });
    },
    [],
  );

  useEffect(() => {
    void reload();
  }, [reload]);

  return {
    issues, labels, openCount, closedCount, stageCounts,
    loading, error, reload, moveIssue,
  };
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/hooks/use-issues.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/use-issues.ts frontend/src/hooks/use-issues.test.ts
git commit -m "✨ issue(fe): use-issues 支持 position 排序 + stageCounts + moveIssue"
```

---

## Task 7: 前端看板 —— 拖拽组件 board-dnd（阶段定义 + 纯排序函数）

**Files:**
- Create: `frontend/src/components/agentre/kanban/stages.ts`
- Create: `frontend/src/components/agentre/kanban/reorder.ts`
- Create: `frontend/src/components/agentre/kanban/__tests__/reorder.test.ts`

**Interfaces:**
- Produces:
  - `stages.ts`：`STAGES: { id: "todo"|"doing"|"review"|"done"; labelKey: string; icon: string; accent: string }[]`（accent 用 tailwind 语义 class 名或 token key）。
  - `reorder.ts`：`groupByStage(issues): Record<string, app.IssueItem[]>`（各 stage 内按 position 升序）；`afterIdForDrop(list, overIndex): number`（给定放置索引，返回其前一张卡 id，0=顶部）。纯函数，无 React 依赖，便于单测（对齐 chat-tabs `tab-cycle.ts` 纯函数拆分做法）。

- [ ] **Step 1: 写失败测试**

`frontend/src/components/agentre/kanban/__tests__/reorder.test.ts`:

```ts
import { describe, expect, it } from "vitest";

import { afterIdForDrop, groupByStage } from "../reorder";

const mk = (id: number, stage: string, position: number) =>
  ({ id, stage, position, labels: [] }) as any;

describe("groupByStage", () => {
  it("按 stage 分组且列内按 position 升序", () => {
    const g = groupByStage([mk(2, "todo", 20), mk(1, "todo", 10), mk(3, "done", 5)]);
    expect(g.todo.map((i) => i.id)).toEqual([1, 2]);
    expect(g.done.map((i) => i.id)).toEqual([3]);
    expect(g.doing).toEqual([]);
  });
});

describe("afterIdForDrop", () => {
  const list = [mk(1, "todo", 10), mk(2, "todo", 20)];
  it("放到索引 0 → afterID=0（顶部）", () => {
    expect(afterIdForDrop(list, 0)).toBe(0);
  });
  it("放到索引 1 → afterID=前一张卡 id", () => {
    expect(afterIdForDrop(list, 1)).toBe(1);
  });
  it("放到末尾 → afterID=最后一张卡 id", () => {
    expect(afterIdForDrop(list, 2)).toBe(2);
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/kanban/__tests__/reorder.test.ts`
Expected: FAIL（模块不存在）。

- [ ] **Step 3: 实现**

`frontend/src/components/agentre/kanban/stages.ts`:

```ts
export type StageId = "todo" | "doing" | "review" | "done";

export const STAGES: {
  id: StageId;
  labelKey: string;
  icon: string;
  accent: string;
}[] = [
  { id: "todo", labelKey: "issues.stages.todo", icon: "circle", accent: "text-status-idle" },
  { id: "doing", labelKey: "issues.stages.doing", icon: "circle-dot", accent: "text-primary-text" },
  { id: "review", labelKey: "issues.stages.review", icon: "circle-dashed", accent: "text-status-waiting" },
  { id: "done", labelKey: "issues.stages.done", icon: "circle-check-big", accent: "text-status-running" },
];
```

`frontend/src/components/agentre/kanban/reorder.ts`:

```ts
import type { app } from "../../../../wailsjs/go/models";
import { STAGES, type StageId } from "./stages";

export function groupByStage(
  issues: app.IssueItem[],
): Record<StageId, app.IssueItem[]> {
  const out = { todo: [], doing: [], review: [], done: [] } as Record<
    StageId,
    app.IssueItem[]
  >;
  for (const it of issues) {
    const stage = (it.stage || "todo") as StageId;
    (out[stage] ?? out.todo).push(it);
  }
  for (const s of STAGES) {
    out[s.id].sort((a, b) => a.position - b.position);
  }
  return out;
}

// afterIdForDrop：给定卡片将插入的目标索引 overIndex（0..len），
// 返回其前一张卡的 id（0 = 落在列顶）。
export function afterIdForDrop(
  list: app.IssueItem[],
  overIndex: number,
): number {
  if (overIndex <= 0) return 0;
  const prev = list[Math.min(overIndex, list.length) - 1];
  return prev ? prev.id : 0;
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `cd frontend && pnpm test -- src/components/agentre/kanban/__tests__/reorder.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/kanban/stages.ts frontend/src/components/agentre/kanban/reorder.ts frontend/src/components/agentre/kanban/__tests__/reorder.test.ts
git commit -m "✨ issue(fe): 看板阶段定义 + 分组/落位纯函数"
```

---

## Task 8: 前端看板 —— IssuesBoard 组件（dnd-kit 跨列 + 列内拖拽 + 乐观更新）

**Files:**
- Create: `frontend/src/components/agentre/kanban/issues-board.tsx`
- Modify: `frontend/src/components/agentre/issues-page.tsx`（默认视图改 board、接 `moveIssue`、渲染 `IssuesBoard`）
- Create: `frontend/src/components/agentre/kanban/__tests__/issues-board.test.tsx`

**Interfaces:**
- Consumes: `groupByStage` / `afterIdForDrop` / `STAGES`（Task 7）；`useIssues().moveIssue`（Task 6）。
- Produces: `IssuesBoard({ issues, stageCounts, onEdit, onMove })`，`onMove(id, stage, afterID)`；拖拽用 `@dnd-kit/core` `DndContext`（PointerSensor + KeyboardSensor）+ 每列 `SortableContext`（`verticalListSortingStrategy`）+ 卡片 `useSortable`。跨列/列内落点均计算 `(stage, afterID)`，先本地乐观重排再 `await onMove`，失败交由父组件 `reload` 回滚。

- [ ] **Step 1: 写失败测试**

`frontend/src/components/agentre/kanban/__tests__/issues-board.test.tsx`（渲染断言 4 列 + 计数；拖拽交互难在 jsdom 精确模拟，改为测「渲染分组」+「onMove 通过导出的 handler 触发」；dnd 端到端在 e2e 覆盖）：

```tsx
import { render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import "../../../../i18n"; // 初始化 i18n（照抄其他组件测试的初始化方式）
import { IssuesBoard } from "../issues-board";

const mk = (id: number, stage: string, position: number, title: string) =>
  ({ id, stage, position, title, labels: [], agentStatus: "idle" }) as any;

describe("IssuesBoard", () => {
  it("按 stage 渲染 4 列并显示计数", () => {
    render(
      <IssuesBoard
        issues={[mk(1, "todo", 10, "甲"), mk(2, "doing", 10, "乙")]}
        stageCounts={{ todo: 1, doing: 1, review: 0, done: 0 }}
        onEdit={vi.fn()}
        onMove={vi.fn()}
      />,
    );
    expect(screen.getByText("甲")).toBeInTheDocument();
    expect(screen.getByText("乙")).toBeInTheDocument();
    // 4 个阶段列标题都在（i18n 中文）
    expect(screen.getByText("待办")).toBeInTheDocument();
    expect(screen.getByText("已完成")).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/kanban/__tests__/issues-board.test.tsx`
Expected: FAIL（`issues-board` 模块不存在）。

- [ ] **Step 3: 实现 IssuesBoard**

`frontend/src/components/agentre/kanban/issues-board.tsx`（结构参照 `chat-tabs/tab-strip.tsx` 的 dnd-kit 用法：`DndContext` + `SortableContext` + `useSortable`；卡片视觉照 Pencil `Issues — Kanban` 帧）：

```tsx
import {
  DndContext,
  KeyboardSensor,
  PointerSensor,
  closestCorners,
  useSensor,
  useSensors,
  type DragEndEvent,
} from "@dnd-kit/core";
import {
  SortableContext,
  sortableKeyboardCoordinates,
  useSortable,
  verticalListSortingStrategy,
} from "@dnd-kit/sortable";
import { CSS } from "@dnd-kit/utilities";
import { Circle, CircleCheckBig, CircleDashed, CircleDot } from "lucide-react";
import * as React from "react";
import { useTranslation } from "react-i18next";

import type { app } from "../../../../wailsjs/go/models";
import { IssueLabels } from "../issue-labels"; // 复用现有标签渲染；若在 issues-page.tsx 内联则抽出为共享组件
import { afterIdForDrop, groupByStage } from "./reorder";
import { STAGES, type StageId } from "./stages";

const STAGE_ICON: Record<StageId, React.ComponentType<{ className?: string }>> = {
  todo: Circle,
  doing: CircleDot,
  review: CircleDashed,
  done: CircleCheckBig,
};

export function IssuesBoard({
  issues,
  stageCounts,
  onEdit,
  onMove,
}: {
  issues: app.IssueItem[];
  stageCounts: Record<string, number>;
  onEdit: (issue: app.IssueItem) => void;
  onMove: (id: number, stage: StageId, afterID: number) => Promise<void> | void;
}) {
  const { t } = useTranslation();
  // 本地乐观镜像：拖拽后先改本地，再 await onMove。
  const [local, setLocal] = React.useState(issues);
  React.useEffect(() => setLocal(issues), [issues]);
  const grouped = React.useMemo(() => groupByStage(local), [local]);

  const sensors = useSensors(
    useSensor(PointerSensor, { activationConstraint: { distance: 4 } }),
    useSensor(KeyboardSensor, { coordinateGetter: sortableKeyboardCoordinates }),
  );

  const onDragEnd = (e: DragEndEvent) => {
    const activeId = Number(e.active.id);
    const over = e.over;
    if (!over) return;
    // over 可能是卡片 id 或列容器 id（"col:doing"）。
    const overId = String(over.id);
    const targetStage: StageId = overId.startsWith("col:")
      ? (overId.slice(4) as StageId)
      : ((local.find((i) => i.id === Number(overId))?.stage || "todo") as StageId);
    const targetList = grouped[targetStage].filter((i) => i.id !== activeId);
    const overIndex = overId.startsWith("col:")
      ? targetList.length
      : targetList.findIndex((i) => i.id === Number(overId));
    const afterID = afterIdForDrop(targetList, Math.max(overIndex, 0));
    // 乐观：本地更新 stage（position 由后端权威，reload 时校正）。
    setLocal((prev) =>
      prev.map((i) => (i.id === activeId ? { ...i, stage: targetStage } : i)),
    );
    void onMove(activeId, targetStage, afterID);
  };

  return (
    <DndContext sensors={sensors} collisionDetection={closestCorners} onDragEnd={onDragEnd}>
      <section
        aria-label={t("issues.board.aria")}
        className="min-h-0 flex-1 overflow-auto bg-sidebar px-5 py-3.5"
      >
        <div className="flex min-w-max items-start gap-3">
          {STAGES.map((stage) => {
            const Icon = STAGE_ICON[stage.id];
            const items = grouped[stage.id];
            return (
              <section key={stage.id} id={`col:${stage.id}`} className="flex w-[300px] shrink-0 flex-col gap-2.5">
                <header className="flex items-center gap-2 px-1">
                  <Icon className={`size-3.5 ${stage.accent}`} aria-hidden />
                  <h2 className="text-[13px] font-semibold">{t(stage.labelKey)}</h2>
                  <span className="font-mono text-2xs font-semibold text-muted-foreground">
                    {stageCounts[stage.id] ?? items.length}
                  </span>
                </header>
                <SortableContext items={items.map((i) => i.id)} strategy={verticalListSortingStrategy}>
                  <div className="flex flex-col gap-2" data-stage={stage.id}>
                    {items.map((issue) => (
                      <BoardCard key={issue.id} issue={issue} onEdit={onEdit} />
                    ))}
                    {items.length === 0 ? (
                      <p className="rounded-md border border-dashed border-border px-3 py-6 text-center text-2xs text-subtle-foreground">
                        {t("issues.board.emptyColumn")}
                      </p>
                    ) : null}
                  </div>
                </SortableContext>
              </section>
            );
          })}
        </div>
      </section>
    </DndContext>
  );
}

function BoardCard({
  issue,
  onEdit,
}: {
  issue: app.IssueItem;
  onEdit: (issue: app.IssueItem) => void;
}) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } =
    useSortable({ id: issue.id });
  const done = issue.stage === "done";
  return (
    <button
      ref={setNodeRef}
      type="button"
      onClick={() => onEdit(issue)}
      {...attributes}
      {...listeners}
      style={{ transform: CSS.Translate.toString(transform), transition }}
      className={`flex flex-col gap-2 rounded-md border bg-card px-3 py-2.5 text-left ${
        isDragging ? "border-primary shadow-lg opacity-90" : "border-border shadow-xs"
      } ${done ? "opacity-80" : ""}`}
    >
      <span className="font-mono text-2xs font-semibold text-primary-text">#{issue.id}</span>
      <h3 className="line-clamp-2 text-xs font-semibold leading-normal">{issue.title}</h3>
      <IssueLabels labels={issue.labels} />
    </button>
  );
}
```

> 注：`IssueLabels` 若目前内联在 `issues-page.tsx`，本 Task 顺带把它抽到 `frontend/src/components/agentre/kanban/issue-labels.tsx` 供两处复用（属本任务范围内的 in-scope 抽取）。`text-2xs` / `text-primary-text` / `bg-sidebar` 等类名沿用现有 board 代码里已用的（`issues-page.tsx` 现有 board 已用 `bg-sidebar` / `text-2xs`）。

- [ ] **Step 4: 接入 issues-page.tsx（board 默认 + onMove）**

改 `frontend/src/components/agentre/issues-page.tsx`：
- `const [view, setView] = React.useState<IssueView>("board");`（默认改 board）
- board 分支下的 filter 用 `sort: "position"`：`useIssues({ state: effectiveState, projectID, labelIDs, sort: view === "board" ? "position" : "updated" })`。
- 取出 `stageCounts, moveIssue`。
- 用新组件替换旧 `IssuesBoard`：
  ```tsx
  ) : view === "board" ? (
    <IssuesBoard
      issues={issues}
      stageCounts={stageCounts}
      onEdit={openEdit}
      onMove={async (id, stage, afterID) => {
        try {
          await moveIssue(id, stage, afterID);
        } finally {
          void reload();
        }
      }}
    />
  ) : (
  ```
- 删除文件底部旧的 `function IssuesBoard(...)`（被 `kanban/issues-board.tsx` 取代），并 `import { IssuesBoard } from "./kanban/issues-board";`。删除随之无引用的 `Columns3` 等旧 import 由 lint 提示后清理（属本任务范围内）。

- [ ] **Step 5: 跑组件测试 + 类型检查**

Run: `cd frontend && pnpm test -- src/components/agentre/kanban/__tests__/issues-board.test.tsx`
Expected: PASS（4 列 + 卡片渲染）。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/kanban/ frontend/src/components/agentre/issues-page.tsx
git commit -m "✨ issue(fe): 看板 IssuesBoard(dnd-kit 跨列+列内拖拽) 设为默认视图"
```

---

## Task 9: i18n —— 阶段/看板文案 + 新建表单 stage 字段 + 清理旧列键

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Modify: `frontend/src/components/agentre/issue-new-dialog.tsx`（表单加 `阶段` shadcn Select）
- Modify: `frontend/src/components/agentre/__tests__/issues-page.test.tsx`（若断言到旧列文案则更新）

**Interfaces:**
- Consumes: `STAGES[].labelKey`（Task 7）；`issue_svc` 的 `CreateIssueRequest.Stage`（Task 4/5）。
- Produces: i18n key `issues.stages.{todo,doing,review,done}`、`issues.board.aria`、`issues.board.emptyColumn`、`issues.form.stage`；删除被取代的 `issues.columns.backlog` / `issues.columns.closed`。

- [ ] **Step 1: 加 i18n key（zh-CN + en）**

`zh-CN/common.json` 的 `issues` 节点内新增（并删除旧 `columns.backlog/closed`）：

```json
"stages": { "todo": "待办", "doing": "进行中", "review": "待审阅", "done": "已完成" },
"board": { "aria": "Issue 看板", "emptyColumn": "拖拽到此处" },
"form": { "stage": "阶段" }
```

`en/common.json` 对应：

```json
"stages": { "todo": "Todo", "doing": "In Progress", "review": "Review", "done": "Done" },
"board": { "aria": "Issue board", "emptyColumn": "Drop here" },
"form": { "stage": "Stage" }
```

（若 `issues` 下已有 `board`/`form` 对象，则合并键而非覆盖对象。）

- [ ] **Step 2: 表单加 stage 字段**

在 `issue-new-dialog.tsx`：加 `const [stage, setStage] = React.useState<string>("todo");`，`editing` 变化时 `setStage(editing?.stage ?? "todo")`；在项目 Select 旁加一个 shadcn `Select`（`SelectTrigger/Content/Item`），选项来自 `STAGES`（`t(s.labelKey)`）；提交时 `IssueCreate({ ..., stage })`（编辑走 `IssueUpdate` 时 stage 通过拖拽维护，可不在表单改；若产品要在编辑弹窗改 stage 则一并传，本计划仅创建时传 stage）。

- [ ] **Step 3: 跑 i18n + 相关组件测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts src/components/agentre/__tests__/issues-page.test.tsx`
Expected: PASS（静态 key 覆盖、中英对齐；issues-page 若断言旧列文案已更新为新阶段文案）。

- [ ] **Step 4: Commit**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json frontend/src/components/agentre/issue-new-dialog.tsx frontend/src/components/agentre/__tests__/issues-page.test.tsx
git commit -m "🌐 issue(fe): 看板阶段/表单 stage i18n + 清理旧列键"
```

---

## Task 10: 收尾验证（全量 gate）

**Files:** 无（仅验证）

- [ ] **Step 1: 后端全量 + race**

Run: `make test-backend`
Expected: PASS（看真 exit code，勿被 `| tail` 吞退出码）。

- [ ] **Step 2: 前端全量 vitest**

Run: `cd frontend && pnpm test`
Expected: PASS（含 i18n / issues-page / board / hook）。

- [ ] **Step 3: lint（含 tsc/eslint/i18next-no-literal-string/gofmt）**

Run: `make lint`
Expected: PASS（无硬编码中文 JSX、无未用 import、类型无误）。

- [ ] **Step 4: 手动冒烟（可选，dev）**

Run: `make dev`，打开 `/issues`：默认看板、4 列、拖卡跨列/列内、刷新后顺序保留、切列表视图仍工作、新建带阶段。

- [ ] **Step 5: 无独立提交**（各 Task 已提交；此任务仅确认绿）。

---

## Self-Review（对照 spec 的覆盖检查）

- **决策 1/2（手动阶段 / 固定 4 列）**：Task 2（stage 常量）+ Task 7（STAGES）+ Task 8（4 列渲染）。✅
- **决策 3（已完成即终态 / state 同步）**：Task 2 `SetStage`。✅
- **决策 4（List 保留）**：Task 8 Step 4 保留 view 切换。✅
- **决策 5（position 拖拽持久化）**：Task 1 列 + Task 3 排序 + Task 4 `Move`/`computePosition` + Task 8 拖拽。✅
- **决策 6（指派/派发）**：**不在本计划** —— Plan 2 覆盖（`assignee_agent_id`/`session_id` 列已在 Task 1 建好）。⏭️
- **数据模型（4 列 + 回填 + 索引）**：Task 1。✅
- **后端分层（entity/repo/svc/binding）**：Task 2–5。✅
- **前端（board 默认 + dnd + 空态 + list 保留 + i18n）**：Task 6–9。✅
- **TDD/sqlmock/mockgen/i18n.test/全量 gate**：各 Task Red→Green + Task 10。✅

**占位扫描**：`Move` 的 label mock 在 Task 4 Step 1 注释里给了兜底说明（据实补）；迁移测试 helper 名 `NewMigrationDB` 标注为占位、要求照抄现有迁移测试——执行者需先看一个现有 `migrations/*_test.go`。其余步骤均含真实代码。

**类型一致性**：`MoveIssueRequest{ID,Stage,AfterID}` 在 svc(Task4)/binding(Task5)/hook(Task6)/board(Task8) 一致；`stageCounts` 贯穿 repo→svc→binding→hook→board 命名一致；`StageId` 联合类型在 stages/reorder/board 一致。

---

## Plan 2 预告（不在本计划）

指派 + 派发 Agent：`Assign`/`Dispatch`/`Stop` svc + `SessionStatusObserver` 回写接缝（chat_svc 不反向依赖 issue_svc）+ 卡片 agent 头像/实时徽标/点卡开会话 + 表单「创建并派发」+ 远端验证。前置侦查现有 session 状态更新链路后另写 `2026-07-02-issue-dispatch.md`。
