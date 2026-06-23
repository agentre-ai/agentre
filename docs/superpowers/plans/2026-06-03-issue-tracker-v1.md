# Issue Tracker v1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the fully-mocked `issues-page.tsx` with a real local issue tracker — a persisted `issues`/`labels`/`issue_labels` data layer with manual CRUD, wired to the List + Board UI.

**Architecture:** Standard cago vertical slice mirroring the `project` domain: `issue_entity` (充血 entity) → `issue_repo` (interface + Register/accessor) → `issue_svc` (interface + Default) → `internal/app/issue.go` (thin Wails binding with camelCase DTOs) → regenerated `frontend/wailsjs` → React hook + dialog + page rewrite. Board columns are derived client-side (open→backlog, closed→closed); dispatch/webhook/assignee/comments are explicitly out of scope.

**Tech Stack:** Go 1.26, Wails v2, gorm + gormigrate (glebarez sqlite), cago framework, mockgen (go.uber.org/mock) + sqlmock + goconvey, React 19 + TS + Vite + Vitest (happy-dom) + react-i18next + shadcn ui.

**Reference spec:** `docs/superpowers/specs/2026-06-03-issue-tracker-v1-design.md`. Create-dialog mockup: `Issues — New Issue` frame in `agentry.pen`.

---

## File Structure

**Backend (new):**
- `migrations/202605220011_issues.go` — DDL for 3 tables + seed 10 labels
- `migrations/202605220011_issues_test.go` — migration test (real in-memory sqlite)
- `internal/model/entity/issue_entity/issue.go` — `Issue` entity
- `internal/model/entity/issue_entity/label.go` — `Label` + `IssueLabel`
- `internal/model/entity/issue_entity/issue_test.go`, `label_test.go`
- `internal/repository/issue_repo/issue.go` — `IssueRepo`
- `internal/repository/issue_repo/label.go` — `LabelRepo` + `IssueLabelRepo`
- `internal/repository/issue_repo/issue_test.go`, `label_test.go`
- `internal/repository/issue_repo/mock_issue_repo/*` — generated
- `internal/service/issue_svc/issue.go`, `types.go`, `issue_test.go`
- `internal/app/issue.go` — Wails binding + DTOs + `issue_test.go`

**Backend (modify):**
- `internal/pkg/code/code.go`, `zh_cn.go`, `en.go` — Issue 18200 block
- `migrations/migrations.go` — append `migration202605220011()`
- `internal/bootstrap/cago.go` — register issue repos + svc

**Frontend (new):**
- `frontend/src/components/agentre/issue-tones.ts` — shared `labelToneClassNames`
- `frontend/src/hooks/use-issues.ts` + `use-issues.test.ts`
- `frontend/src/components/agentre/issue-new-dialog.tsx` + `issue-new-dialog.test.tsx`
- `frontend/src/components/agentre/__tests__/issues-page.test.tsx`

**Frontend (modify):**
- `frontend/src/components/agentre/issues-page.tsx` — full rewrite
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — remove `issues.samples.*` + `summary.list/board`; add new keys
- `frontend/src/__tests__/mocks/wailsApp.ts` — default mocks for new bindings

---

## Phase 1 — Backend data layer

### Task 1: Error codes (Issue block)

**Files:**
- Modify: `internal/pkg/code/code.go`
- Modify: `internal/pkg/code/zh_cn.go`
- Modify: `internal/pkg/code/en.go`

> No unit test — these are declarative constants + message maps, verified by `go build`. The gap `18200–20299` is free (Project is 18000, Server starts at 20300).

- [ ] **Step 1: Add the Issue code block to `code.go`**

Append a new `const` block (place it after the Project Location block):

```go
// Issue 18200~18999
const (
	IssueNotFound          = iota + 18200 // issue 不存在
	IssueTitleRequired                    // issue 标题不能为空
	IssueInvalidState                     // issue 状态非法
	IssueLabelNameRequired                // 标签名不能为空
	IssueLabelInvalidTone                 // 标签色调非法
	IssueLabelNotFound                    // 引用的标签不存在
)
```

- [ ] **Step 2: Add zh messages to `zh_cn.go`** (inside the `zhCN` map literal)

```go
	IssueNotFound:          "issue 不存在",
	IssueTitleRequired:     "issue 标题不能为空",
	IssueInvalidState:      "issue 状态非法",
	IssueLabelNameRequired: "标签名不能为空",
	IssueLabelInvalidTone:  "标签色调非法",
	IssueLabelNotFound:     "引用的标签不存在",
```

- [ ] **Step 3: Add en messages to `en.go`** (inside the `enUS` map literal)

```go
	IssueNotFound:          "Issue not found",
	IssueTitleRequired:     "Issue title is required",
	IssueInvalidState:      "Invalid issue state",
	IssueLabelNameRequired: "Label name is required",
	IssueLabelInvalidTone:  "Invalid label tone",
	IssueLabelNotFound:     "Referenced label not found",
```

- [ ] **Step 4: Verify build**

Run: `go build ./internal/pkg/code/...`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/code/
git commit -m "✨ issue: 分配 Issue 错误码段 (18200)"
```

---

### Task 2: Migration + seed

**Files:**
- Create: `migrations/202605220011_issues.go`
- Create: `migrations/202605220011_issues_test.go`
- Modify: `migrations/migrations.go`

- [ ] **Step 1: Write the failing migration test** — `migrations/202605220011_issues_test.go`

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202605220011CreatesIssueTablesAndSeedsLabels(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, migration202605220011().Migrate(gdb))

	require.NoError(t, gdb.Exec(`INSERT INTO issues (id, title) VALUES (1, 'demo')`).Error)
	require.NoError(t, gdb.Exec(`INSERT INTO issue_labels (issue_id, label_id) VALUES (1, 1)`).Error)

	var count int64
	require.NoError(t, gdb.Table("labels").Where("status = 1").Count(&count).Error)
	require.Equal(t, int64(10), count)

	var tone string
	require.NoError(t, gdb.Table("labels").Select("tone").Where("name = ?", "bug").Scan(&tone).Error)
	require.Equal(t, "bug", tone)

	// 幂等：重跑不重复 seed
	require.NoError(t, migration202605220011().Migrate(gdb))
	require.NoError(t, gdb.Table("labels").Where("status = 1").Count(&count).Error)
	require.Equal(t, int64(10), count)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./migrations/ -run TestMigration202605220011 -v`
Expected: FAIL — `undefined: migration202605220011`.

- [ ] **Step 3: Write the migration** — `migrations/202605220011_issues.go`

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220011 建 issues / labels / issue_labels 三张表，并 seed 10 个内置标签。
func migration202605220011() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220011",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	agent_status TEXT NOT NULL DEFAULT 'idle',
	source TEXT NOT NULL DEFAULT 'manual',
	closed_at INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_state ON issues(status, state, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_project ON issues(project_id, status)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS labels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	tone TEXT NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_labels_name_active ON labels(name) WHERE status = 1`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS issue_labels (
	issue_id INTEGER NOT NULL,
	label_id INTEGER NOT NULL,
	PRIMARY KEY (issue_id, label_id)
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issue_labels_label ON issue_labels(label_id)`).Error; err != nil {
				return err
			}

			return tx.Exec(`INSERT INTO labels (name, tone, sort_order, status, createtime, updatetime)
SELECT name, name, sort_order, 1,
	CAST(strftime('%s','now') AS INTEGER) * 1000,
	CAST(strftime('%s','now') AS INTEGER) * 1000
FROM (
	SELECT 'auth' AS name, 1 AS sort_order
	UNION ALL SELECT 'bug', 2
	UNION ALL SELECT 'critical', 3
	UNION ALL SELECT 'docs', 4
	UNION ALL SELECT 'feature', 5
	UNION ALL SELECT 'hook', 6
	UNION ALL SELECT 'ops', 7
	UNION ALL SELECT 'perf', 8
	UNION ALL SELECT 'refactor', 9
	UNION ALL SELECT 'ui', 10
) seed
WHERE NOT EXISTS (SELECT 1 FROM labels WHERE labels.name = seed.name AND labels.status = 1)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS issue_labels`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS labels`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS issues`).Error
		},
	}
}
```

- [ ] **Step 4: Register it** — in `migrations/migrations.go`, append to the END of `migrationList()`:

```go
		migration202605220010(), // projects.sort_order
		migration202605220011(), // issues + labels + issue_labels + label seed
	}
}
```

- [ ] **Step 5: Run the migration test + the bootstrap migration suite**

Run: `go test ./migrations/ -run TestMigration202605220011 -v`
Expected: PASS.
Run: `go test ./internal/bootstrap/...`
Expected: PASS (all migrations still apply cleanly on a real DB).

- [ ] **Step 6: Commit**

```bash
git add migrations/202605220011_issues.go migrations/202605220011_issues_test.go migrations/migrations.go
git commit -m "🗃️ issue: 新增 issues/labels/issue_labels 迁移 (202605220011) + seed 10 标签"
```

---

### Task 3: Entities

**Files:**
- Create: `internal/model/entity/issue_entity/issue.go`
- Create: `internal/model/entity/issue_entity/label.go`
- Create: `internal/model/entity/issue_entity/issue_test.go`
- Create: `internal/model/entity/issue_entity/label_test.go`

- [ ] **Step 1: Write failing entity tests** — `issue_test.go`

```go
package issue_entity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
)

func TestIssueCheck(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, (&issue_entity.Issue{Title: "ok", State: issue_entity.StateOpen}).Check(ctx))

	assert.Error(t, (&issue_entity.Issue{Title: "  ", State: issue_entity.StateOpen}).Check(ctx))
	assert.Error(t, (&issue_entity.Issue{Title: "ok", State: "weird"}).Check(ctx))
}

func TestIssueCloseReopen(t *testing.T) {
	i := &issue_entity.Issue{Title: "x", State: issue_entity.StateOpen}
	i.Close(1234)
	assert.Equal(t, issue_entity.StateClosed, i.State)
	assert.Equal(t, int64(1234), i.ClosedAt)
	assert.True(t, i.IsClosed())

	i.Reopen()
	assert.Equal(t, issue_entity.StateOpen, i.State)
	assert.Equal(t, int64(0), i.ClosedAt)
	assert.True(t, i.IsOpen())
}
```

- [ ] **Step 2: Write failing label test** — `label_test.go`

```go
package issue_entity_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
)

func TestLabelCheck(t *testing.T) {
	ctx := context.Background()
	require.NoError(t, (&issue_entity.Label{Name: "bug", Tone: "bug"}).Check(ctx))
	assert.Error(t, (&issue_entity.Label{Name: "", Tone: "bug"}).Check(ctx))
	assert.Error(t, (&issue_entity.Label{Name: "x", Tone: "rainbow"}).Check(ctx))
}
```

- [ ] **Step 3: Run to verify they fail**

Run: `go test ./internal/model/entity/issue_entity/...`
Expected: FAIL — package `issue_entity` does not exist.

- [ ] **Step 4: Write `issue.go`**

```go
// Package issue_entity 维护 Issue 的充血实体。
package issue_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

const (
	StateOpen   = "open"
	StateClosed = "closed"

	AgentStatusIdle = "idle"

	SourceManual = "manual"
)

// Issue 一条 issue 记录。
type Issue struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID   int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	Title       string `gorm:"column:title;type:text;not null"`
	Body        string `gorm:"column:body;type:text;not null;default:''"`
	State       string `gorm:"column:state;type:text;not null;default:'open'"`
	AgentStatus string `gorm:"column:agent_status;type:text;not null;default:'idle'"`
	Source      string `gorm:"column:source;type:text;not null;default:'manual'"`
	ClosedAt    int64  `gorm:"column:closed_at;type:bigint;not null;default:0"`
	Status      int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime  int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime  int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Issue) TableName() string { return "issues" }

func (i *Issue) IsActive() bool { return i != nil && i.Status == consts.ACTIVE }
func (i *Issue) IsOpen() bool   { return i != nil && i.State == StateOpen }
func (i *Issue) IsClosed() bool { return i != nil && i.State == StateClosed }

// Close 关闭 issue：置 state=closed 并记录关闭时间（unix ms）。
func (i *Issue) Close(now int64) {
	i.State = StateClosed
	i.ClosedAt = now
}

// Reopen 重新打开 issue：置 state=open 并清空关闭时间。
func (i *Issue) Reopen() {
	i.State = StateOpen
	i.ClosedAt = 0
}

// Check 校验必填字段与枚举合法性。
func (i *Issue) Check(ctx context.Context) error {
	if i == nil {
		return i18n.NewError(ctx, code.IssueNotFound)
	}
	if strings.TrimSpace(i.Title) == "" {
		return i18n.NewError(ctx, code.IssueTitleRequired)
	}
	if i.State != StateOpen && i.State != StateClosed {
		return i18n.NewError(ctx, code.IssueInvalidState)
	}
	if i.ProjectID < 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
```

- [ ] **Step 5: Write `label.go`**

```go
package issue_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// allowedTones 与前端 labelToneClassNames 的 key 一致；颜色由前端设计系统统一管理。
var allowedTones = map[string]struct{}{
	"auth": {}, "bug": {}, "critical": {}, "docs": {}, "feature": {},
	"hook": {}, "ops": {}, "perf": {}, "refactor": {}, "ui": {},
}

// Label issue 标签目录项。
type Label struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name       string `gorm:"column:name;type:text;not null"`
	Tone       string `gorm:"column:tone;type:text;not null;default:''"`
	SortOrder  int    `gorm:"column:sort_order;type:int;not null;default:0"`
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Label) TableName() string { return "labels" }

func (l *Label) IsActive() bool { return l != nil && l.Status == consts.ACTIVE }

// Check 校验标签名与色调。
func (l *Label) Check(ctx context.Context) error {
	if l == nil || strings.TrimSpace(l.Name) == "" {
		return i18n.NewError(ctx, code.IssueLabelNameRequired)
	}
	if _, ok := allowedTones[l.Tone]; !ok {
		return i18n.NewError(ctx, code.IssueLabelInvalidTone)
	}
	return nil
}

// IssueLabel issue ↔ label 多对多关联。
type IssueLabel struct {
	IssueID int64 `gorm:"column:issue_id;primaryKey"`
	LabelID int64 `gorm:"column:label_id;primaryKey"`
}

func (*IssueLabel) TableName() string { return "issue_labels" }
```

- [ ] **Step 6: Run to verify pass**

Run: `go test ./internal/model/entity/issue_entity/...`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add internal/model/entity/issue_entity/
git commit -m "✨ issue: 新增 Issue/Label/IssueLabel 充血实体"
```

---

### Task 4: IssueRepo

**Files:**
- Create: `internal/repository/issue_repo/issue.go`
- Create: `internal/repository/issue_repo/issue_test.go`

> Repo tests use `testutils.Database(t)` + sqlmock and call `NewIssue()` directly (no Register). `testutils.Database(t)` returns `(ctx, _, mock)`.

- [ ] **Step 1: Write failing test** — `issue_test.go`

```go
package issue_repo_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

func setupIssueRepo(t *testing.T) (context.Context, sqlmock.Sqlmock, issue_repo.IssueRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, issue_repo.NewIssue()
}

func TestIssueCreate(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `issues`").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()

	err := repo.Create(ctx, &issue_entity.Issue{
		Title: "demo", State: issue_entity.StateOpen, Status: consts.ACTIVE,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueDeleteSoft(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `issues` SET").
		WithArgs(consts.DELETE, sqlmock.AnyArg(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Delete(ctx, 7))
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/repository/issue_repo/...`
Expected: FAIL — package does not exist.

- [ ] **Step 3: Write `issue.go`**

```go
// Package issue_repo 提供 Issue / Label 的持久化访问。
package issue_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
)

//go:generate mockgen -source issue.go -destination mock_issue_repo/mock_issue.go

// ListFilter List 查询过滤条件。
type ListFilter struct {
	State     string  // "" = 不筛选；open / closed
	ProjectID int64   // 0 = 不筛选
	LabelIDs  []int64 // 非空 = 仅含这些 label 的 issue
	Sort      string  // 预留；当前恒按 updatetime DESC
}

// IssueRepo Issue 仓储接口。
type IssueRepo interface {
	Create(ctx context.Context, i *issue_entity.Issue) error
	Update(ctx context.Context, i *issue_entity.Issue) error
	Find(ctx context.Context, id int64) (*issue_entity.Issue, error)
	List(ctx context.Context, filter ListFilter) ([]*issue_entity.Issue, error)
	CountByState(ctx context.Context, projectID int64) (open int64, closed int64, err error)
	Delete(ctx context.Context, id int64) error
}

var defaultIssue IssueRepo

func Issue() IssueRepo             { return defaultIssue }
func RegisterIssue(impl IssueRepo) { defaultIssue = impl }
func NewIssue() IssueRepo          { return &issueRepo{} }

type issueRepo struct{}

func (r *issueRepo) Create(ctx context.Context, i *issue_entity.Issue) error {
	now := time.Now().UnixMilli()
	if i.Createtime == 0 {
		i.Createtime = now
	}
	i.Updatetime = now
	return db.Ctx(ctx).Create(i).Error
}

func (r *issueRepo) Update(ctx context.Context, i *issue_entity.Issue) error {
	i.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Where("id = ? AND status = ?", i.ID, consts.ACTIVE).
		Updates(map[string]any{
			"project_id":   i.ProjectID,
			"title":        i.Title,
			"body":         i.Body,
			"state":        i.State,
			"agent_status": i.AgentStatus,
			"closed_at":    i.ClosedAt,
			"updatetime":   i.Updatetime,
		}).Error
}

func (r *issueRepo) Find(ctx context.Context, id int64) (*issue_entity.Issue, error) {
	out := &issue_entity.Issue{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *issueRepo) List(ctx context.Context, filter ListFilter) ([]*issue_entity.Issue, error) {
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).Where("status = ?", consts.ACTIVE)
	if filter.State != "" {
		q = q.Where("state = ?", filter.State)
	}
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if len(filter.LabelIDs) > 0 {
		sub := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
			Select("issue_id").Where("label_id IN ?", filter.LabelIDs)
		q = q.Where("id IN (?)", sub)
	}
	var rows []*issue_entity.Issue
	err := q.Order("updatetime DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *issueRepo) CountByState(ctx context.Context, projectID int64) (int64, int64, error) {
	type agg struct {
		State string
		Cnt   int64
	}
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Select("state, count(*) as cnt").
		Where("status = ?", consts.ACTIVE)
	if projectID > 0 {
		q = q.Where("project_id = ?", projectID)
	}
	var rows []agg
	if err := q.Group("state").Scan(&rows).Error; err != nil {
		return 0, 0, err
	}
	var open, closed int64
	for _, row := range rows {
		switch row.State {
		case issue_entity.StateOpen:
			open = row.Cnt
		case issue_entity.StateClosed:
			closed = row.Cnt
		}
	}
	return open, closed, nil
}

func (r *issueRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     consts.DELETE,
			"updatetime": time.Now().UnixMilli(),
		}).Error
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/repository/issue_repo/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/issue_repo/issue.go internal/repository/issue_repo/issue_test.go
git commit -m "✨ issue: 新增 IssueRepo (sqlmock 单测)"
```

---

### Task 5: LabelRepo + IssueLabelRepo

**Files:**
- Create: `internal/repository/issue_repo/label.go`
- Create: `internal/repository/issue_repo/label_test.go`

- [ ] **Step 1: Write failing test** — `label_test.go`

```go
package issue_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

func TestLabelList(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewLabel()
	mock.ExpectQuery("SELECT \\* FROM `labels`").
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "tone"}).
			AddRow(int64(2), "bug", "bug"))

	got, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bug", got[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueLabelSetLabels(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewIssueLabel()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `issue_labels`").
		WithArgs(int64(5)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("INSERT INTO `issue_labels`").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	require.NoError(t, repo.SetLabels(ctx, 5, []int64{1, 2}))
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/repository/issue_repo/... -run 'TestLabel|TestIssueLabel'`
Expected: FAIL — `NewLabel`/`NewIssueLabel` undefined.

- [ ] **Step 3: Write `label.go`**

```go
package issue_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
)

//go:generate mockgen -source label.go -destination mock_issue_repo/mock_label.go

// LabelRepo 标签目录仓储。
type LabelRepo interface {
	Find(ctx context.Context, id int64) (*issue_entity.Label, error)
	List(ctx context.Context) ([]*issue_entity.Label, error)
	ListByIDs(ctx context.Context, ids []int64) ([]*issue_entity.Label, error)
}

var defaultLabel LabelRepo

func Label() LabelRepo             { return defaultLabel }
func RegisterLabel(impl LabelRepo) { defaultLabel = impl }
func NewLabel() LabelRepo          { return &labelRepo{} }

type labelRepo struct{}

func (r *labelRepo) Find(ctx context.Context, id int64) (*issue_entity.Label, error) {
	out := &issue_entity.Label{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *labelRepo) List(ctx context.Context) ([]*issue_entity.Label, error) {
	var rows []*issue_entity.Label
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (r *labelRepo) ListByIDs(ctx context.Context, ids []int64) ([]*issue_entity.Label, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*issue_entity.Label
	err := db.Ctx(ctx).Where("id IN ? AND status = ?", ids, consts.ACTIVE).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

// IssueLabelRepo issue ↔ label 关联仓储。
type IssueLabelRepo interface {
	SetLabels(ctx context.Context, issueID int64, labelIDs []int64) error
	ListByIssue(ctx context.Context, issueID int64) ([]int64, error)
	ListByIssues(ctx context.Context, issueIDs []int64) (map[int64][]int64, error)
}

var defaultIssueLabel IssueLabelRepo

func IssueLabel() IssueLabelRepo             { return defaultIssueLabel }
func RegisterIssueLabel(impl IssueLabelRepo) { defaultIssueLabel = impl }
func NewIssueLabel() IssueLabelRepo          { return &issueLabelRepo{} }

type issueLabelRepo struct{}

// SetLabels 用一次事务覆盖 issue 的全部标签关联。
func (r *issueLabelRepo) SetLabels(ctx context.Context, issueID int64, labelIDs []int64) error {
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("issue_id = ?", issueID).
			Delete(&issue_entity.IssueLabel{}).Error; err != nil {
			return err
		}
		if len(labelIDs) == 0 {
			return nil
		}
		rows := make([]issue_entity.IssueLabel, 0, len(labelIDs))
		for _, id := range labelIDs {
			rows = append(rows, issue_entity.IssueLabel{IssueID: issueID, LabelID: id})
		}
		return tx.Create(&rows).Error
	})
}

func (r *issueLabelRepo) ListByIssue(ctx context.Context, issueID int64) ([]int64, error) {
	var ids []int64
	err := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
		Where("issue_id = ?", issueID).Pluck("label_id", &ids).Error
	return ids, err
}

func (r *issueLabelRepo) ListByIssues(ctx context.Context, issueIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	var rows []issue_entity.IssueLabel
	if err := db.Ctx(ctx).Where("issue_id IN ?", issueIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.IssueID] = append(out[row.IssueID], row.LabelID)
	}
	return out, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/repository/issue_repo/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/repository/issue_repo/label.go internal/repository/issue_repo/label_test.go
git commit -m "✨ issue: 新增 LabelRepo + IssueLabelRepo"
```

---

### Task 6: Generate repo mocks

**Files:**
- Create (generated): `internal/repository/issue_repo/mock_issue_repo/mock_issue.go`, `mock_label.go`

- [ ] **Step 1: Generate**

Run: `make mock`
Expected: creates `internal/repository/issue_repo/mock_issue_repo/` with `MockIssueRepo`, `MockLabelRepo`, `MockIssueLabelRepo`.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/repository/issue_repo/...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add internal/repository/issue_repo/mock_issue_repo/
git commit -m "🤖 issue: 生成 issue_repo mocks"
```

---

### Task 7: IssueSvc

**Files:**
- Create: `internal/service/issue_svc/types.go`
- Create: `internal/service/issue_svc/issue.go`
- Create: `internal/service/issue_svc/issue_test.go`

> Service tests use mockgen + `RegisterXxx(mock)` + `New()`, no DB.

- [ ] **Step 1: Write `types.go`** (plain structs, no json tags)

```go
package issue_svc

import "github.com/agentre-ai/agentre/internal/model/entity/issue_entity"

type CreateIssueRequest struct {
	ProjectID int64
	Title     string
	Body      string
	LabelIDs  []int64
}

type UpdateIssueRequest struct {
	ID        int64
	ProjectID int64
	Title     string
	Body      string
	LabelIDs  []int64
}

type ListIssuesRequest struct {
	State     string
	ProjectID int64
	LabelIDs  []int64
	Sort      string
}

// IssueDetail issue + 已水合标签。
type IssueDetail struct {
	Issue  *issue_entity.Issue
	Labels []*issue_entity.Label
}

// ListIssuesResponse 列表结果 + open/closed 计数。
type ListIssuesResponse struct {
	Issues      []*IssueDetail
	OpenCount   int64
	ClosedCount int64
}
```

- [ ] **Step 2: Write failing test** — `issue_test.go`

```go
package issue_svc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo/mock_issue_repo"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

func setupIssueSvc(t *testing.T) (
	context.Context,
	*mock_issue_repo.MockIssueRepo,
	*mock_issue_repo.MockLabelRepo,
	*mock_issue_repo.MockIssueLabelRepo,
	issue_svc.IssueSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mi := mock_issue_repo.NewMockIssueRepo(ctrl)
	ml := mock_issue_repo.NewMockLabelRepo(ctrl)
	mil := mock_issue_repo.NewMockIssueLabelRepo(ctrl)
	issue_repo.RegisterIssue(mi)
	issue_repo.RegisterLabel(ml)
	issue_repo.RegisterIssueLabel(mil)
	return context.Background(), mi, ml, mil, issue_svc.New()
}

func TestIssueSvcCreate_Happy(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)
	mi.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		i.ID = 9
		assert.Equal(t, issue_entity.StateOpen, i.State)
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(9), []int64{2}).Return(nil)

	got, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo", LabelIDs: []int64{2}})
	require.NoError(t, err)
	assert.Equal(t, int64(9), got.Issue.ID)
	require.Len(t, got.Labels, 1)
}

func TestIssueSvcCreate_EmptyTitleRejected(t *testing.T) {
	ctx, _, _, _, svc := setupIssueSvc(t)
	_, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "   "})
	assert.Error(t, err) // 校验在 repo.Create 之前拦截，无 mock 调用
}

func TestIssueSvcSetState_Close(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(3)).Return(&issue_entity.Issue{ID: 3, State: issue_entity.StateOpen}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, issue_entity.StateClosed, i.State)
		assert.NotZero(t, i.ClosedAt)
		return nil
	})
	mil.EXPECT().ListByIssue(ctx, int64(3)).Return(nil, nil)
	ml.EXPECT().ListByIDs(ctx, gomock.Nil()).Return(nil, nil)

	got, err := svc.SetState(ctx, 3, issue_entity.StateClosed)
	require.NoError(t, err)
	assert.True(t, got.Issue.IsClosed())
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./internal/service/issue_svc/...`
Expected: FAIL — package does not exist.

- [ ] **Step 4: Write `issue.go`**

```go
package issue_svc

import (
	"context"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

// IssueSvc Issue 模块应用服务。
type IssueSvc interface {
	Create(ctx context.Context, req *CreateIssueRequest) (*IssueDetail, error)
	Update(ctx context.Context, req *UpdateIssueRequest) (*IssueDetail, error)
	SetState(ctx context.Context, id int64, state string) (*IssueDetail, error)
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*IssueDetail, error)
	List(ctx context.Context, req *ListIssuesRequest) (*ListIssuesResponse, error)
	ListLabels(ctx context.Context) ([]*issue_entity.Label, error)
}

type issueSvc struct {
	now func() int64
}

var defaultIssue IssueSvc = &issueSvc{now: func() int64 { return time.Now().UnixMilli() }}

func Default() IssueSvc       { return defaultIssue }
func SetDefault(svc IssueSvc) { defaultIssue = svc }
func New() IssueSvc {
	return &issueSvc{now: func() int64 { return time.Now().UnixMilli() }}
}

func (s *issueSvc) Create(ctx context.Context, req *CreateIssueRequest) (*IssueDetail, error) {
	now := s.now()
	issue := &issue_entity.Issue{
		ProjectID:   req.ProjectID,
		Title:       strings.TrimSpace(req.Title),
		Body:        req.Body,
		State:       issue_entity.StateOpen,
		AgentStatus: issue_entity.AgentStatusIdle,
		Source:      issue_entity.SourceManual,
		Status:      consts.ACTIVE,
		Createtime:  now,
		Updatetime:  now,
	}
	if err := issue.Check(ctx); err != nil {
		return nil, err
	}
	labels, err := s.resolveLabels(ctx, req.LabelIDs)
	if err != nil {
		return nil, err
	}
	if err := issue_repo.Issue().Create(ctx, issue); err != nil {
		return nil, err
	}
	if err := issue_repo.IssueLabel().SetLabels(ctx, issue.ID, req.LabelIDs); err != nil {
		logger.Ctx(ctx).Warn("issue_svc.Create: set labels failed",
			zap.Int64("issueId", issue.ID), zap.Error(err))
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}

func (s *issueSvc) Update(ctx context.Context, req *UpdateIssueRequest) (*IssueDetail, error) {
	issue, err := issue_repo.Issue().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	issue.ProjectID = req.ProjectID
	issue.Title = strings.TrimSpace(req.Title)
	issue.Body = req.Body
	if err := issue.Check(ctx); err != nil {
		return nil, err
	}
	labels, err := s.resolveLabels(ctx, req.LabelIDs)
	if err != nil {
		return nil, err
	}
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	if err := issue_repo.IssueLabel().SetLabels(ctx, issue.ID, req.LabelIDs); err != nil {
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}

func (s *issueSvc) SetState(ctx context.Context, id int64, state string) (*IssueDetail, error) {
	if state != issue_entity.StateOpen && state != issue_entity.StateClosed {
		return nil, i18n.NewError(ctx, code.IssueInvalidState)
	}
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	if state == issue_entity.StateClosed {
		issue.Close(s.now())
	} else {
		issue.Reopen()
	}
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	return s.hydrate(ctx, issue)
}

func (s *issueSvc) Delete(ctx context.Context, id int64) error {
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return err
	}
	if issue == nil {
		return i18n.NewError(ctx, code.IssueNotFound)
	}
	return issue_repo.Issue().Delete(ctx, id)
}

func (s *issueSvc) Get(ctx context.Context, id int64) (*IssueDetail, error) {
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	return s.hydrate(ctx, issue)
}

func (s *issueSvc) List(ctx context.Context, req *ListIssuesRequest) (*ListIssuesResponse, error) {
	issues, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{
		State:     req.State,
		ProjectID: req.ProjectID,
		LabelIDs:  req.LabelIDs,
		Sort:      req.Sort,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(issues))
	for _, it := range issues {
		ids = append(ids, it.ID)
	}
	labelMap, err := issue_repo.IssueLabel().ListByIssues(ctx, ids)
	if err != nil {
		return nil, err
	}
	allLabels, err := issue_repo.Label().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*issue_entity.Label{}
	for _, l := range allLabels {
		byID[l.ID] = l
	}
	details := make([]*IssueDetail, 0, len(issues))
	for _, it := range issues {
		labels := make([]*issue_entity.Label, 0)
		for _, lid := range labelMap[it.ID] {
			if l := byID[lid]; l != nil {
				labels = append(labels, l)
			}
		}
		details = append(details, &IssueDetail{Issue: it, Labels: labels})
	}
	open, closed, err := issue_repo.Issue().CountByState(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	return &ListIssuesResponse{Issues: details, OpenCount: open, ClosedCount: closed}, nil
}

func (s *issueSvc) ListLabels(ctx context.Context) ([]*issue_entity.Label, error) {
	return issue_repo.Label().List(ctx)
}

func (s *issueSvc) resolveLabels(ctx context.Context, ids []int64) ([]*issue_entity.Label, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	labels, err := issue_repo.Label().ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(labels) != len(ids) {
		return nil, i18n.NewError(ctx, code.IssueLabelNotFound)
	}
	return labels, nil
}

func (s *issueSvc) hydrate(ctx context.Context, issue *issue_entity.Issue) (*IssueDetail, error) {
	ids, err := issue_repo.IssueLabel().ListByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	labels, err := issue_repo.Label().ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}
```

- [ ] **Step 5: Run to verify pass**

Run: `go test ./internal/service/issue_svc/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/service/issue_svc/
git commit -m "✨ issue: 新增 IssueSvc (mockgen 单测)"
```

---

### Task 8: Wails binding + DTOs

**Files:**
- Create: `internal/app/issue.go`
- Create: `internal/app/issue_test.go`

- [ ] **Step 1: Write failing mapper test** — `internal/app/issue_test.go`

```go
package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

func TestToIssueItem(t *testing.T) {
	item := toIssueItem(&issue_svc.IssueDetail{
		Issue:  &issue_entity.Issue{ID: 4, Title: "t", State: "open", AgentStatus: "idle"},
		Labels: []*issue_entity.Label{{ID: 1, Name: "bug", Tone: "bug"}},
	})
	require.NotNil(t, item)
	assert.Equal(t, int64(4), item.ID)
	assert.Equal(t, "open", item.State)
	require.Len(t, item.Labels, 1)
	assert.Equal(t, "bug", item.Labels[0].Tone)
}

func TestToIssueItem_NoLabels(t *testing.T) {
	item := toIssueItem(&issue_svc.IssueDetail{Issue: &issue_entity.Issue{ID: 1, Title: "x", State: "open"}})
	assert.NotNil(t, item.Labels) // 非 nil 空切片，便于前端
	assert.Len(t, item.Labels, 0)
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/app/ -run TestToIssueItem`
Expected: FAIL — `toIssueItem` undefined.

- [ ] **Step 3: Write `internal/app/issue.go`**

```go
package app

import (
	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

// IssueItem issue 摘要（含标签），列表 / 看板 / 详情共用。
type IssueItem struct {
	ID          int64        `json:"id"`
	ProjectID   int64        `json:"projectID"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	State       string       `json:"state"`
	AgentStatus string       `json:"agentStatus"`
	Source      string       `json:"source"`
	ClosedAt    int64        `json:"closedAt"`
	Createtime  int64        `json:"createtime"`
	Updatetime  int64        `json:"updatetime"`
	Labels      []*LabelItem `json:"labels"`
}

// LabelItem 标签 DTO。
type LabelItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Tone string `json:"tone"`
}

type IssueListRequest struct {
	State     string  `json:"state"`
	ProjectID int64   `json:"projectID"`
	LabelIDs  []int64 `json:"labelIDs"`
	Sort      string  `json:"sort"`
}

type IssueListResponse struct {
	Issues      []*IssueItem `json:"issues"`
	OpenCount   int64        `json:"openCount"`
	ClosedCount int64        `json:"closedCount"`
}

type IssueCreateRequest struct {
	ProjectID int64   `json:"projectID"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	LabelIDs  []int64 `json:"labelIDs"`
}

type IssueUpdateRequest struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"projectID"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	LabelIDs  []int64 `json:"labelIDs"`
}

type IssueSetStateRequest struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

func toLabelItem(l *issue_entity.Label) *LabelItem {
	return &LabelItem{ID: l.ID, Name: l.Name, Tone: l.Tone}
}

func toIssueItem(d *issue_svc.IssueDetail) *IssueItem {
	labels := make([]*LabelItem, 0, len(d.Labels))
	for _, l := range d.Labels {
		labels = append(labels, toLabelItem(l))
	}
	return &IssueItem{
		ID:          d.Issue.ID,
		ProjectID:   d.Issue.ProjectID,
		Title:       d.Issue.Title,
		Body:        d.Issue.Body,
		State:       d.Issue.State,
		AgentStatus: d.Issue.AgentStatus,
		Source:      d.Issue.Source,
		ClosedAt:    d.Issue.ClosedAt,
		Createtime:  d.Issue.Createtime,
		Updatetime:  d.Issue.Updatetime,
		Labels:      labels,
	}
}

// IssueList 列出 issue。
func (a *App) IssueList(req *IssueListRequest) (*IssueListResponse, error) {
	resp, err := issue_svc.Default().List(a.ctx, &issue_svc.ListIssuesRequest{
		State: req.State, ProjectID: req.ProjectID, LabelIDs: req.LabelIDs, Sort: req.Sort,
	})
	if err != nil {
		return nil, err
	}
	items := make([]*IssueItem, 0, len(resp.Issues))
	for _, d := range resp.Issues {
		items = append(items, toIssueItem(d))
	}
	return &IssueListResponse{Issues: items, OpenCount: resp.OpenCount, ClosedCount: resp.ClosedCount}, nil
}

// IssueGet 取单条 issue。
func (a *App) IssueGet(id int64) (*IssueItem, error) {
	d, err := issue_svc.Default().Get(a.ctx, id)
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueCreate 创建 issue。
func (a *App) IssueCreate(req *IssueCreateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().Create(a.ctx, &issue_svc.CreateIssueRequest{
		ProjectID: req.ProjectID, Title: req.Title, Body: req.Body, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueUpdate 更新 issue。
func (a *App) IssueUpdate(req *IssueUpdateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().Update(a.ctx, &issue_svc.UpdateIssueRequest{
		ID: req.ID, ProjectID: req.ProjectID, Title: req.Title, Body: req.Body, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueSetState 关闭 / 重新打开 issue。
func (a *App) IssueSetState(req *IssueSetStateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().SetState(a.ctx, req.ID, req.State)
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueDelete 软删 issue。
func (a *App) IssueDelete(id int64) error {
	return issue_svc.Default().Delete(a.ctx, id)
}

// IssueListLabels 列出全部标签。
func (a *App) IssueListLabels() ([]*LabelItem, error) {
	labels, err := issue_svc.Default().ListLabels(a.ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*LabelItem, 0, len(labels))
	for _, l := range labels {
		items = append(items, toLabelItem(l))
	}
	return items, nil
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/app/ -run TestToIssueItem`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/app/issue.go internal/app/issue_test.go
git commit -m "✨ issue: 新增 Wails 绑定 internal/app/issue.go"
```

---

### Task 9: Bootstrap wiring + regenerate bindings

**Files:**
- Modify: `internal/bootstrap/cago.go`

- [ ] **Step 1: Add imports** to the import block in `internal/bootstrap/cago.go`

```go
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
```

- [ ] **Step 2: Register repos + svc** — in `Init(...)`, after the project wiring lines:

```go
	issue_repo.RegisterIssue(issue_repo.NewIssue())
	issue_repo.RegisterLabel(issue_repo.NewLabel())
	issue_repo.RegisterIssueLabel(issue_repo.NewIssueLabel())
	issue_svc.SetDefault(issue_svc.New())
```

- [ ] **Step 3: Verify backend builds + full backend tests pass**

Run: `go build ./...`
Run: `make test-backend`
Expected: PASS.

- [ ] **Step 4: Regenerate Wails bindings**

Run: `make generate`
Expected: `frontend/wailsjs/go/app/App.d.ts` now declares `IssueList/IssueGet/IssueCreate/IssueUpdate/IssueSetState/IssueDelete/IssueListLabels`, and `frontend/wailsjs/go/models.ts` declares `app.IssueItem`, `app.LabelItem`, `app.IssueListResponse`, etc.

> `frontend/wailsjs/` is gitignored — do NOT commit it. It is regenerated by `make dev`/`make generate`/`make build`.

- [ ] **Step 5: Commit**

```bash
git add internal/bootstrap/cago.go
git commit -m "🔌 issue: bootstrap 装配 issue repo/svc"
```

---

## Phase 2 — Frontend

### Task 10: Shared tones module + mock defaults

**Files:**
- Create: `frontend/src/components/agentre/issue-tones.ts`
- Modify: `frontend/src/__tests__/mocks/wailsApp.ts`

- [ ] **Step 1: Create `issue-tones.ts`** (lifted verbatim from the current `issues-page.tsx` map so the page rewrite + dialog can share it)

```ts
export type IssueLabelTone =
  | "auth"
  | "bug"
  | "critical"
  | "docs"
  | "feature"
  | "hook"
  | "ops"
  | "perf"
  | "refactor"
  | "ui";

export const labelToneClassNames: Record<IssueLabelTone, string> = {
  auth: "bg-agent-1/10 text-agent-1",
  bug: "bg-destructive-soft text-destructive",
  critical: "bg-destructive text-destructive-foreground",
  docs: "bg-secondary text-muted-foreground",
  feature: "bg-status-running-bg text-status-running",
  hook: "bg-primary-soft text-primary-text",
  ops: "bg-secondary text-muted-foreground",
  perf: "bg-status-waiting-bg text-status-waiting",
  refactor: "bg-primary-soft text-primary-text",
  ui: "bg-agent-2/10 text-agent-2",
};

export function toneClass(tone: string): string {
  return labelToneClassNames[tone as IssueLabelTone] ?? "bg-secondary text-muted-foreground";
}
```

- [ ] **Step 2: Add default mocks for the new bindings** — open `frontend/src/__tests__/mocks/wailsApp.ts`, find an existing entry (e.g. `ProjectListTree`) to copy its helper style, and add entries for the issue bindings with these defaults:

```ts
  IssueList: <existing-helper>("IssueList", { issues: [], openCount: 0, closedCount: 0 }),
  IssueListLabels: <existing-helper>("IssueListLabels", []),
  IssueGet: <existing-helper>("IssueGet", { id: 0, title: "", state: "open", labels: [] }),
  IssueCreate: <existing-helper>("IssueCreate", { id: 0, title: "", state: "open", labels: [] }),
  IssueUpdate: <existing-helper>("IssueUpdate", { id: 0, title: "", state: "open", labels: [] }),
  IssueSetState: <existing-helper>("IssueSetState", { id: 0, title: "", state: "open", labels: [] }),
  IssueDelete: <existing-helper>("IssueDelete", undefined),
```

Replace `<existing-helper>` with the actual factory used in that file (per the existing entries — the file's own pattern is authoritative). This keeps unrelated tests (e.g. `App.test.tsx`) from hitting `undefined` if they ever mount the page.

- [ ] **Step 3: Verify tones module typechecks**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS (no new literal `t()` keys yet; module is type-only).

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/agentre/issue-tones.ts frontend/src/__tests__/mocks/wailsApp.ts
git commit -m "✨ issue(fe): 抽出 label tone 映射 + wails mock 默认值"
```

---

### Task 11: `use-issues` hook

**Files:**
- Create: `frontend/src/hooks/use-issues.ts`
- Create: `frontend/src/hooks/use-issues.test.ts`

- [ ] **Step 1: Write failing test** — `use-issues.test.ts`

```ts
import { renderHook, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

vi.mock("../../wailsjs/go/app/App", () => ({
  IssueList: vi.fn(),
  IssueListLabels: vi.fn(),
}));

import { IssueList, IssueListLabels } from "../../wailsjs/go/app/App";
import { useIssues } from "./use-issues";

const issueList = IssueList as ReturnType<typeof vi.fn>;
const issueListLabels = IssueListLabels as ReturnType<typeof vi.fn>;

describe("useIssues", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    issueList.mockResolvedValue({
      issues: [{ id: 1, title: "demo", state: "open", agentStatus: "idle", labels: [] }],
      openCount: 1,
      closedCount: 0,
    });
    issueListLabels.mockResolvedValue([{ id: 1, name: "bug", tone: "bug" }]);
  });

  it("loads issues, labels and counts on mount", async () => {
    const { result } = renderHook(() => useIssues({ state: "open", projectID: 0, labelIDs: [] }));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.issues).toHaveLength(1);
    expect(result.current.openCount).toBe(1);
    expect(result.current.labels[0].name).toBe("bug");
    expect(issueList).toHaveBeenCalledWith(expect.objectContaining({ state: "open", projectID: 0 }));
  });

  it("captures errors as a string", async () => {
    issueList.mockRejectedValue(new Error("boom"));
    const { result } = renderHook(() => useIssues({ state: "open", projectID: 0, labelIDs: [] }));
    await waitFor(() => expect(result.current.error).toBe("boom"));
  });
});
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd frontend && pnpm test -- src/hooks/use-issues.test.ts`
Expected: FAIL — cannot resolve `./use-issues`.

- [ ] **Step 3: Write `use-issues.ts`**

```ts
import { useCallback, useEffect, useState } from "react";

import { IssueList, IssueListLabels } from "../../wailsjs/go/app/App";
import type { app } from "../../wailsjs/go/models";

export type IssueFilter = {
  state: string; // "" = all (board); "open" / "closed" (list tabs)
  projectID: number;
  labelIDs: number[];
};

export function useIssues(filter: IssueFilter) {
  const [issues, setIssues] = useState<app.IssueItem[]>([]);
  const [labels, setLabels] = useState<app.LabelItem[]>([]);
  const [openCount, setOpenCount] = useState(0);
  const [closedCount, setClosedCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const { state, projectID } = filter;
  const labelKey = filter.labelIDs.join(",");

  const reload = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const labelIDs = labelKey ? labelKey.split(",").map(Number) : [];
      const [resp, labelList] = await Promise.all([
        IssueList({ state, projectID, labelIDs, sort: "updated" }),
        IssueListLabels(),
      ]);
      setIssues(resp?.issues ?? []);
      setOpenCount(resp?.openCount ?? 0);
      setClosedCount(resp?.closedCount ?? 0);
      setLabels(labelList ?? []);
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : String(e));
    } finally {
      setLoading(false);
    }
  }, [state, projectID, labelKey]);

  useEffect(() => {
    void reload();
  }, [reload]);

  return { issues, labels, openCount, closedCount, loading, error, reload };
}
```

- [ ] **Step 4: Run to verify pass**

Run: `cd frontend && pnpm test -- src/hooks/use-issues.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/hooks/use-issues.ts frontend/src/hooks/use-issues.test.ts
git commit -m "✨ issue(fe): 新增 use-issues hook"
```

---

### Task 12: Create/Edit dialog

**Files:**
- Create: `frontend/src/components/agentre/issue-new-dialog.tsx`
- Create: `frontend/src/components/agentre/issue-new-dialog.test.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`, `frontend/src/i18n/locales/en/common.json`

> The dialog handles BOTH create and edit (an optional `editing` prop). Mirrors `project-new-dialog.tsx`. Labels are toggled via pressable chips (no `command.tsx` combobox exists).

- [ ] **Step 1: Add i18n keys** to BOTH locale files under the existing `issues` object. Keys must be byte-identical in structure across locales.

zh-CN (`issues.dialog`):
```json
"dialog": {
  "title": "新建 Issue",
  "editTitle": "编辑 Issue",
  "titleLabel": "标题",
  "bodyLabel": "描述",
  "projectLabel": "项目",
  "noProject": "未归属",
  "labelsLabel": "标签",
  "create": "创建 Issue",
  "save": "保存"
}
```
en (`issues.dialog`):
```json
"dialog": {
  "title": "New Issue",
  "editTitle": "Edit Issue",
  "titleLabel": "Title",
  "bodyLabel": "Description",
  "projectLabel": "Project",
  "noProject": "No project",
  "labelsLabel": "Labels",
  "create": "Create issue",
  "save": "Save"
}
```

- [ ] **Step 2: Write failing test** — `issue-new-dialog.test.tsx`

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  IssueCreate: vi.fn(),
  IssueUpdate: vi.fn(),
}));
vi.mock("../../../wailsjs/go/app/App", () => appMocks);

import { IssueNewDialog } from "./issue-new-dialog";

const labels = [{ id: 1, name: "bug", tone: "bug" }];
const projects = [{ id: 5, name: "Agentre" }];

describe("IssueNewDialog", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.IssueCreate.mockResolvedValue({ id: 9 });
  });

  it("creates an issue with the typed title", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onCreated = vi.fn();
    render(
      <IssueNewDialog open onOpenChange={() => {}} projects={projects} labels={labels} onSaved={onCreated} />,
    );
    await user.type(screen.getByRole("textbox", { name: /Title/i }), "fix OAuth");
    await user.click(screen.getByRole("button", { name: /Create issue/i }));
    await waitFor(() =>
      expect(appMocks.IssueCreate).toHaveBeenCalledWith(
        expect.objectContaining({ title: "fix OAuth", projectID: 0, labelIDs: [] }),
      ),
    );
    expect(onCreated).toHaveBeenCalled();
  });

  it("disables submit when the title is empty", async () => {
    render(
      <IssueNewDialog open onOpenChange={() => {}} projects={projects} labels={labels} onSaved={() => {}} />,
    );
    expect(screen.getByRole("button", { name: /Create issue/i })).toBeDisabled();
  });
});
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/issue-new-dialog.test.tsx`
Expected: FAIL — cannot resolve `./issue-new-dialog`.

- [ ] **Step 4: Write `issue-new-dialog.tsx`**

```tsx
import * as React from "react";
import { Loader2 } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogBody,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

import { toneClass } from "./issue-tones";
import type { ProjectFlat } from "@/hooks/use-project-list";
import { IssueCreate, IssueUpdate } from "../../../wailsjs/go/app/App";
import type { app } from "../../../wailsjs/go/models";

export type IssueNewDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  projects: ProjectFlat[];
  labels: app.LabelItem[];
  editing?: app.IssueItem | null;
  onSaved: () => void;
};

function IssueNewDialog({ open, onOpenChange, projects, labels, editing, onSaved }: IssueNewDialogProps) {
  const { t } = useTranslation();
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState("");
  const [projectID, setProjectID] = React.useState(0);
  const [labelIDs, setLabelIDs] = React.useState<number[]>([]);
  const [submitError, setSubmitError] = React.useState<string | null>(null);
  const [submitting, setSubmitting] = React.useState(false);

  React.useEffect(() => {
    if (!open) {
      return;
    }
    setTitle(editing?.title ?? "");
    setBody(editing?.body ?? "");
    setProjectID(editing?.projectID ?? 0);
    setLabelIDs((editing?.labels ?? []).map((l) => l.id));
    setSubmitError(null);
  }, [open, editing]);

  const toggleLabel = (id: number) => {
    setLabelIDs((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]));
  };

  const canSubmit = title.trim().length > 0 && !submitting;

  const handleSubmit = async () => {
    setSubmitError(null);
    setSubmitting(true);
    try {
      if (editing) {
        await IssueUpdate({ id: editing.id, projectID, title: title.trim(), body: body.trim(), labelIDs });
      } else {
        await IssueCreate({ projectID, title: title.trim(), body: body.trim(), labelIDs });
      }
      onSaved();
      onOpenChange(false);
    } catch (err) {
      setSubmitError(String(err));
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-[520px]">
        <DialogHeader>
          <DialogTitle>{editing ? t("issues.dialog.editTitle") : t("issues.dialog.title")}</DialogTitle>
        </DialogHeader>
        <DialogBody className="flex flex-col gap-3.5">
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">
              {t("issues.dialog.titleLabel")}
              <span className="ml-0.5 text-destructive">*</span>
            </span>
            <Input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              className="h-9 text-xs"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">{t("issues.dialog.bodyLabel")}</span>
            <Textarea
              value={body}
              onChange={(e) => setBody(e.target.value)}
              className="min-h-[88px] text-xs"
            />
          </label>
          <label className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">{t("issues.dialog.projectLabel")}</span>
            <Select value={String(projectID)} onValueChange={(v) => setProjectID(Number(v))}>
              <SelectTrigger className="h-9 text-xs">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="0">{t("issues.dialog.noProject")}</SelectItem>
                {projects.map((p) => (
                  <SelectItem key={p.id} value={String(p.id)}>
                    {p.name}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
          <div className="flex flex-col gap-1.5 text-xs">
            <span className="font-medium text-foreground">{t("issues.dialog.labelsLabel")}</span>
            <div className="flex flex-wrap gap-1.5">
              {labels.map((l) => {
                const selected = labelIDs.includes(l.id);
                return (
                  <button
                    type="button"
                    key={l.id}
                    aria-pressed={selected}
                    onClick={() => toggleLabel(l.id)}
                    className={cn(
                      "cursor-pointer rounded-full border px-2 py-px font-mono text-2xs font-semibold transition-colors",
                      selected
                        ? cn(toneClass(l.tone), "border-transparent")
                        : "border-border text-muted-foreground hover:bg-accent",
                    )}
                  >
                    {l.name}
                  </button>
                );
              })}
            </div>
          </div>
          {submitError ? (
            <div className="rounded-md border border-destructive bg-destructive-soft px-3 py-2 text-2xs text-destructive">
              {submitError}
            </div>
          ) : null}
        </DialogBody>
        <DialogFooter>
          <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button type="button" disabled={!canSubmit} onClick={() => void handleSubmit()}>
            {submitting ? <Loader2 className="size-3.5 animate-spin" aria-hidden="true" /> : null}
            {editing ? t("issues.dialog.save") : t("issues.dialog.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export { IssueNewDialog };
```

> If `<Input>` doesn't expose an accessible name via the wrapping `<label>` in tests, add `aria-label={t("issues.dialog.titleLabel")}` to the title `<Input>` so `getByRole("textbox", { name: /Title/i })` resolves. Verify in Step 5; add if needed.

- [ ] **Step 5: Run to verify pass + i18n parity**

Run: `cd frontend && pnpm test -- src/components/agentre/issue-new-dialog.test.tsx`
Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: both PASS.

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/issue-new-dialog.tsx frontend/src/components/agentre/issue-new-dialog.test.tsx frontend/src/i18n/locales/
git commit -m "✨ issue(fe): 新建/编辑 Issue 弹窗"
```

---

### Task 13: Rewrite `issues-page.tsx`

**Files:**
- Modify: `frontend/src/components/agentre/issues-page.tsx` (full rewrite)
- Create: `frontend/src/components/agentre/__tests__/issues-page.test.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

> Behavior: List + Board from real data; Open/Closed tabs (list view) with live counts; label filter; row actions (close/reopen/delete); New button + click-to-edit open the dialog; empty + loading states. Board renders only backlog(open)/closed(closed) columns. The `agentStatus` icon is always `idle` in v1.

- [ ] **Step 1: Update i18n keys** in BOTH locale files.

Remove (dead after rewrite): the entire `issues.samples` object, `issues.summary.list`, `issues.summary.board`.

Add `issues.summary.counts`, `issues.empty`, `issues.state`, and extend `issues.actions`:

zh-CN:
```json
"summary": { "counts": "{{open}} 个 Open · {{closed}} 个 Closed" },
"empty": { "title": "还没有 Issue", "desc": "新建一个 Issue 开始追踪工作项" },
"state": { "loading": "加载中…", "error": "加载失败" },
"actions": {
  "dispatchToAgent": "派发给 Agent",
  "filter": "筛选",
  "newIssue": "新建 Issue",
  "redispatch": "重新派发",
  "close": "关闭",
  "reopen": "重新打开",
  "edit": "编辑",
  "delete": "删除"
}
```
en:
```json
"summary": { "counts": "{{open}} open · {{closed}} closed" },
"empty": { "title": "No issues yet", "desc": "Create an issue to start tracking work" },
"state": { "loading": "Loading…", "error": "Failed to load" },
"actions": {
  "dispatchToAgent": "Dispatch to agent",
  "filter": "Filter",
  "newIssue": "New issue",
  "redispatch": "Re-dispatch",
  "close": "Close",
  "reopen": "Reopen",
  "edit": "Edit",
  "delete": "Delete"
}
```

> Keep `issues.actions.dispatchToAgent`/`redispatch`, `issues.board.*`, `issues.columns.running/waiting`, `issues.status.unassigned` — they are retained for the upcoming dispatch increment (and parity must hold).

- [ ] **Step 2: Write failing test** — `__tests__/issues-page.test.tsx`

```tsx
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

const appMocks = vi.hoisted(() => ({
  IssueList: vi.fn(),
  IssueListLabels: vi.fn(),
  IssueSetState: vi.fn(),
  IssueDelete: vi.fn(),
  IssueCreate: vi.fn(),
  IssueUpdate: vi.fn(),
  ProjectListTree: vi.fn(),
}));
vi.mock("../../../../wailsjs/go/app/App", () => appMocks);

import { IssuesPage } from "../issues-page";

describe("IssuesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    appMocks.ProjectListTree.mockResolvedValue([]);
    appMocks.IssueListLabels.mockResolvedValue([{ id: 1, name: "bug", tone: "bug" }]);
    appMocks.IssueList.mockResolvedValue({
      issues: [
        { id: 142, title: "fix OAuth state loss", state: "open", agentStatus: "idle", labels: [{ id: 1, name: "bug", tone: "bug" }] },
      ],
      openCount: 1,
      closedCount: 0,
    });
  });

  it("renders issues from the binding", async () => {
    render(<IssuesPage />);
    expect(await screen.findByText("fix OAuth state loss")).toBeInTheDocument();
    expect(screen.getByText("#142")).toBeInTheDocument();
  });

  it("shows the empty state when there are no issues", async () => {
    appMocks.IssueList.mockResolvedValue({ issues: [], openCount: 0, closedCount: 0 });
    render(<IssuesPage />);
    expect(await screen.findByText("No issues yet")).toBeInTheDocument();
  });

  it("switching to the Closed tab refetches with state=closed", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(<IssuesPage />);
    await screen.findByText("fix OAuth state loss");
    await user.click(screen.getByRole("button", { name: /Closed/i }));
    await waitFor(() =>
      expect(appMocks.IssueList).toHaveBeenLastCalledWith(expect.objectContaining({ state: "closed" })),
    );
  });
});
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/issues-page.test.tsx`
Expected: FAIL (current page renders mock rows, not `#142` from the binding / no empty state).

- [ ] **Step 4: Rewrite `issues-page.tsx`** with the full file below

```tsx
import * as React from "react";
import {
  Circle,
  CircleAlert,
  CircleCheck,
  CircleDot,
  Columns3,
  List,
  MoreHorizontal,
  Plus,
  SlidersHorizontal,
} from "lucide-react";
import type { LucideIcon } from "lucide-react";
import { useTranslation } from "react-i18next";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/popover";
import { cn } from "@/lib/utils";
import { useIssues } from "@/hooks/use-issues";
import { useProjectList } from "@/hooks/use-project-list";

import { IssueNewDialog } from "./issue-new-dialog";
import { toneClass } from "./issue-tones";
import {
  IssueDelete,
  IssueSetState,
} from "../../../wailsjs/go/app/App";
import type { app } from "../../../wailsjs/go/models";

type IssueView = "list" | "board";

const statusIconMeta: Record<string, { className: string; icon: LucideIcon }> = {
  error: { icon: CircleAlert, className: "text-status-error" },
  idle: { icon: Circle, className: "text-muted-foreground" },
  running: { icon: CircleDot, className: "text-status-running" },
  waiting: { icon: CircleDot, className: "text-status-waiting" },
};

function statusIcon(agentStatus: string) {
  return statusIconMeta[agentStatus] ?? statusIconMeta.idle;
}

function IssuesPage() {
  const { t } = useTranslation();
  const [view, setView] = React.useState<IssueView>("list");
  const [tab, setTab] = React.useState<"open" | "closed">("open");
  const [labelIDs, setLabelIDs] = React.useState<number[]>([]);
  const [dialogOpen, setDialogOpen] = React.useState(false);
  const [editing, setEditing] = React.useState<app.IssueItem | null>(null);

  const effectiveState = view === "board" ? "" : tab;
  const { issues, labels, openCount, closedCount, loading, error, reload } = useIssues({
    state: effectiveState,
    projectID: 0,
    labelIDs,
  });
  const { projects } = useProjectList();

  const summary = t("issues.summary.counts", { open: openCount, closed: closedCount });

  const openCreate = () => {
    setEditing(null);
    setDialogOpen(true);
  };
  const openEdit = (issue: app.IssueItem) => {
    setEditing(issue);
    setDialogOpen(true);
  };
  const setState = async (issue: app.IssueItem, state: string) => {
    await IssueSetState({ id: issue.id, state });
    void reload();
  };
  const remove = async (issue: app.IssueItem) => {
    await IssueDelete(issue.id);
    void reload();
  };

  return (
    <main
      className="flex min-h-0 min-w-0 flex-1 flex-col bg-background"
      data-slot="issues-page"
    >
      <IssuesHeader
        view={view}
        summary={summary}
        onViewChange={setView}
        onNewIssue={openCreate}
      />
      <IssueFilterBar
        view={view}
        tab={tab}
        onTabChange={setTab}
        openCount={openCount}
        closedCount={closedCount}
        labels={labels}
        selectedLabelIDs={labelIDs}
        onToggleLabel={(id) =>
          setLabelIDs((ids) => (ids.includes(id) ? ids.filter((x) => x !== id) : [...ids, id]))
        }
      />
      {loading && issues.length === 0 ? (
        <CenterNote text={t("issues.state.loading")} />
      ) : error ? (
        <CenterNote text={t("issues.state.error")} />
      ) : issues.length === 0 ? (
        <IssuesEmpty onNewIssue={openCreate} />
      ) : view === "list" ? (
        <IssuesList
          issues={issues}
          onEdit={openEdit}
          onSetState={setState}
          onDelete={remove}
        />
      ) : (
        <IssuesBoard issues={issues} onEdit={openEdit} />
      )}
      <IssueNewDialog
        open={dialogOpen}
        onOpenChange={setDialogOpen}
        projects={projects}
        labels={labels}
        editing={editing}
        onSaved={reload}
      />
    </main>
  );
}

function CenterNote({ text }: { text: string }) {
  return (
    <div className="flex min-h-0 flex-1 items-center justify-center text-xs text-muted-foreground">
      {text}
    </div>
  );
}

function IssuesEmpty({ onNewIssue }: { onNewIssue: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 px-6 text-center">
      <h2 className="text-sm font-semibold">{t("issues.empty.title")}</h2>
      <p className="max-w-sm text-xs text-muted-foreground">{t("issues.empty.desc")}</p>
      <Button type="button" size="sm" onClick={onNewIssue}>
        <Plus data-icon="inline-start" aria-hidden="true" />
        {t("issues.actions.newIssue")}
      </Button>
    </div>
  );
}

type IssuesHeaderProps = {
  onViewChange: (view: IssueView) => void;
  onNewIssue: () => void;
  summary: string;
  view: IssueView;
};

function IssuesHeader({ onViewChange, onNewIssue, summary, view }: IssuesHeaderProps) {
  const { t } = useTranslation();
  return (
    <header className="flex min-h-[60px] shrink-0 flex-wrap items-center gap-3 border-b border-border bg-background px-5 py-3 lg:h-[60px] lg:flex-nowrap lg:py-0">
      <div className="flex min-w-0 flex-col gap-0.5">
        <h1 className="truncate text-base font-semibold tracking-normal">{t("issues.title")}</h1>
        <p className="truncate text-2xs text-muted-foreground">{summary}</p>
      </div>
      <div className="min-w-0 flex-1" />
      <div
        className="flex h-[30px] shrink-0 items-center gap-0.5 rounded-md border border-border bg-secondary p-0.5"
        aria-label={t("issues.view.aria")}
      >
        <IssueViewButton active={view === "list"} icon={List} label={t("issues.view.list")} onClick={() => onViewChange("list")} />
        <IssueViewButton active={view === "board"} icon={Columns3} label={t("issues.view.board")} onClick={() => onViewChange("board")} />
      </div>
      <Button type="button" size="sm" className="h-[30px]" onClick={onNewIssue}>
        <Plus data-icon="inline-start" aria-hidden="true" />
        {t("issues.actions.newIssue")}
      </Button>
    </header>
  );
}

type IssueViewButtonProps = {
  active: boolean;
  icon: LucideIcon;
  label: string;
  onClick: () => void;
};

function IssueViewButton({ active, icon: Icon, label, onClick }: IssueViewButtonProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      className={cn(
        "inline-flex h-full cursor-pointer items-center justify-center gap-1.5 rounded-sm px-2.5 text-xs font-medium outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/40",
        active
          ? "border border-border bg-card text-foreground shadow-xs"
          : "text-muted-foreground hover:text-foreground",
      )}
      onClick={onClick}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
    </button>
  );
}

type IssueFilterBarProps = {
  view: IssueView;
  tab: "open" | "closed";
  onTabChange: (tab: "open" | "closed") => void;
  openCount: number;
  closedCount: number;
  labels: app.LabelItem[];
  selectedLabelIDs: number[];
  onToggleLabel: (id: number) => void;
};

function IssueFilterBar({
  view,
  tab,
  onTabChange,
  openCount,
  closedCount,
  labels,
  selectedLabelIDs,
  onToggleLabel,
}: IssueFilterBarProps) {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-12 shrink-0 items-center gap-2 overflow-x-auto border-b border-border bg-sidebar px-5 py-2">
      {view === "list" ? (
        <div className="flex shrink-0 items-center gap-0.5">
          <FilterTab active={tab === "open"} icon={CircleDot} label={t("issues.filters.open")} count={openCount} onClick={() => onTabChange("open")} />
          <FilterTab active={tab === "closed"} icon={CircleCheck} label={t("issues.filters.closed")} count={closedCount} onClick={() => onTabChange("closed")} />
        </div>
      ) : null}
      <div className="min-w-0 flex-1" />
      <LabelFilter labels={labels} selectedLabelIDs={selectedLabelIDs} onToggle={onToggleLabel} />
    </div>
  );
}

type FilterTabProps = {
  active: boolean;
  count: number;
  icon: LucideIcon;
  label: string;
  onClick: () => void;
};

function FilterTab({ active, count, icon: Icon, label, onClick }: FilterTabProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      onClick={onClick}
      className={cn(
        "inline-flex cursor-pointer items-center gap-1.5 rounded-md px-2.5 py-1.5 text-sm font-medium outline-none transition-colors focus-visible:ring-[3px] focus-visible:ring-ring/40",
        active
          ? "border border-primary bg-primary-soft text-primary-text"
          : "text-muted-foreground hover:bg-accent hover:text-foreground",
      )}
    >
      <Icon className="size-3.5" aria-hidden="true" />
      {label}
      <span className="font-mono text-2xs text-subtle-foreground">{count}</span>
    </button>
  );
}

function LabelFilter({
  labels,
  selectedLabelIDs,
  onToggle,
}: {
  labels: app.LabelItem[];
  selectedLabelIDs: number[];
  onToggle: (id: number) => void;
}) {
  const { t } = useTranslation();
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button type="button" variant="outline" size="sm" className="h-7">
          <SlidersHorizontal data-icon="inline-start" aria-hidden="true" />
          {t("issues.filters.label")}
          {selectedLabelIDs.length > 0 ? (
            <span className="ml-1 font-mono text-2xs">{selectedLabelIDs.length}</span>
          ) : null}
        </Button>
      </PopoverTrigger>
      <PopoverContent align="end" className="w-48">
        <div className="flex flex-wrap gap-1.5">
          {labels.map((l) => {
            const selected = selectedLabelIDs.includes(l.id);
            return (
              <button
                type="button"
                key={l.id}
                aria-pressed={selected}
                onClick={() => onToggle(l.id)}
                className={cn(
                  "cursor-pointer rounded-full border px-2 py-px font-mono text-2xs font-semibold transition-colors",
                  selected ? cn(toneClass(l.tone), "border-transparent") : "border-border text-muted-foreground hover:bg-accent",
                )}
              >
                {l.name}
              </button>
            );
          })}
        </div>
      </PopoverContent>
    </Popover>
  );
}

function IssueLabels({ labels }: { labels: app.LabelItem[] }) {
  if (!labels || labels.length === 0) {
    return null;
  }
  return (
    <span className="flex shrink-0 flex-wrap items-center gap-1.5">
      {labels.map((label) => (
        <Badge
          variant="secondary"
          className={cn("rounded-full border-0 px-2 py-px font-mono text-2xs font-semibold", toneClass(label.tone))}
          key={label.id}
        >
          {label.name}
        </Badge>
      ))}
    </span>
  );
}

type RowActionsProps = {
  issue: app.IssueItem;
  onEdit: (issue: app.IssueItem) => void;
  onSetState: (issue: app.IssueItem, state: string) => void;
  onDelete: (issue: app.IssueItem) => void;
};

function RowActions({ issue, onEdit, onSetState, onDelete }: RowActionsProps) {
  const { t } = useTranslation();
  const isOpen = issue.state === "open";
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button type="button" variant="ghost" size="icon-xs" aria-label={t("issues.actions.filter")}>
          <MoreHorizontal data-icon="only" aria-hidden="true" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuItem onSelect={() => onEdit(issue)}>{t("issues.actions.edit")}</DropdownMenuItem>
        <DropdownMenuItem onSelect={() => onSetState(issue, isOpen ? "closed" : "open")}>
          {isOpen ? t("issues.actions.close") : t("issues.actions.reopen")}
        </DropdownMenuItem>
        <DropdownMenuSeparator />
        <DropdownMenuItem variant="destructive" onSelect={() => onDelete(issue)}>
          {t("issues.actions.delete")}
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

function IssuesList({
  issues,
  onEdit,
  onSetState,
  onDelete,
}: {
  issues: app.IssueItem[];
  onEdit: (issue: app.IssueItem) => void;
  onSetState: (issue: app.IssueItem, state: string) => void;
  onDelete: (issue: app.IssueItem) => void;
}) {
  const { t } = useTranslation();
  return (
    <section aria-label={t("issues.list.aria")} className="min-h-0 flex-1 overflow-auto bg-background">
      <div role="list" className="min-w-[760px]">
        {issues.map((issue) => {
          const Icon = statusIcon(issue.agentStatus).icon;
          return (
            <article
              role="listitem"
              key={issue.id}
              className="flex min-h-[68px] items-center gap-3.5 border-b border-border px-5 py-3.5 transition-colors hover:bg-accent/40"
            >
              <Icon className={cn("size-4 shrink-0", statusIcon(issue.agentStatus).className)} aria-hidden="true" />
              <button
                type="button"
                onClick={() => onEdit(issue)}
                className="flex min-w-0 flex-1 cursor-pointer flex-col gap-1.5 text-left outline-none"
              >
                <div className="flex min-w-0 flex-wrap items-center gap-2">
                  <span className="truncate text-sm font-semibold">{issue.title}</span>
                  <IssueLabels labels={issue.labels} />
                </div>
                <div className="flex min-w-0 flex-wrap items-center gap-1.5 font-mono text-2xs">
                  <span className="font-medium text-primary-text">#{issue.id}</span>
                  <span className="truncate text-muted-foreground">
                    · {new Date(issue.updatetime).toLocaleDateString()}
                  </span>
                </div>
              </button>
              <RowActions issue={issue} onEdit={onEdit} onSetState={onSetState} onDelete={onDelete} />
            </article>
          );
        })}
      </div>
    </section>
  );
}

function IssuesBoard({
  issues,
  onEdit,
}: {
  issues: app.IssueItem[];
  onEdit: (issue: app.IssueItem) => void;
}) {
  const { t } = useTranslation();
  const backlog = issues.filter((i) => i.state === "open");
  const closed = issues.filter((i) => i.state === "closed");
  const columns = [
    { id: "backlog", title: t("issues.columns.backlog"), items: backlog },
    { id: "closed", title: t("issues.columns.closed"), items: closed },
  ];
  return (
    <section aria-label={t("issues.board.aria")} className="min-h-0 flex-1 overflow-auto bg-sidebar px-5 py-4">
      <div className="flex min-w-max items-start gap-4">
        {columns.map((column) => (
          <section key={column.id} className="flex w-80 shrink-0 flex-col gap-2 rounded-lg border border-border bg-card p-2.5">
            <div className="flex items-center gap-2 border-b border-border px-1.5 pb-2">
              <h2 className="text-xs font-semibold">{column.title}</h2>
              <span className="inline-flex min-w-6 items-center justify-center rounded-full bg-secondary px-1.5 py-px font-mono text-2xs font-semibold text-muted-foreground">
                {column.items.length}
              </span>
            </div>
            <div className="flex flex-col gap-2">
              {column.items.map((issue) => (
                <button
                  type="button"
                  key={issue.id}
                  onClick={() => onEdit(issue)}
                  className="flex cursor-pointer flex-col gap-2 rounded-md border border-border bg-background px-3 py-2.5 text-left shadow-xs"
                >
                  <span className="font-mono text-2xs font-semibold text-primary-text">#{issue.id}</span>
                  <h3 className="line-clamp-2 text-xs font-semibold leading-normal">{issue.title}</h3>
                  <IssueLabels labels={issue.labels} />
                </button>
              ))}
            </div>
          </section>
        ))}
      </div>
    </section>
  );
}

export { IssuesPage };
```

> Verify the imported shadcn primitives exist with these member names: `dropdown-menu.tsx` must export `DropdownMenu/Trigger/Content/Item/Separator` and `DropdownMenuItem` must accept a `variant="destructive"` prop (if not, drop the prop and use `className="text-destructive"`); `popover.tsx` must export `Popover/PopoverTrigger/PopoverContent`. If `Button` lacks `size="icon-xs"`, use `size="icon"`. Adjust imports to match the actual exports during Step 5.

- [ ] **Step 5: Run to verify pass**

Run: `cd frontend && pnpm test -- src/components/agentre/__tests__/issues-page.test.tsx`
Expected: PASS. Fix any primitive prop mismatches flagged above.

- [ ] **Step 6: Run i18n test**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS (no leftover literal `t("issues.samples...")`; zh/en parity holds).

- [ ] **Step 7: Commit**

```bash
git add frontend/src/components/agentre/issues-page.tsx frontend/src/components/agentre/__tests__/issues-page.test.tsx frontend/src/i18n/locales/
git commit -m "✨ issue(fe): issues 页接真实数据 (list/board/筛选/CRUD)"
```

---

### Task 14: Full verification

**Files:** none (verification only)

- [ ] **Step 1: Backend race tests**

Run: `make test-backend`
Expected: PASS.

- [ ] **Step 2: Frontend tests**

Run: `cd frontend && pnpm test`
Expected: PASS (all suites incl. i18n parity + use-issues + dialog + issues-page).

- [ ] **Step 3: Lint**

Run: `make lint`
Expected: PASS (golangci-lint v2 + ESLint incl. `i18next/no-literal-string`).

- [ ] **Step 4: Manual smoke (optional but recommended)**

Run: `make dev`, navigate to `/issues`. Confirm: list loads (empty state on a fresh DB), "新建 Issue" opens the dialog, creating an issue makes it appear, Open/Closed tabs + counts work, label filter narrows results, board shows backlog/closed columns, row menu closes/reopens/deletes.

- [ ] **Step 5: Final commit (if any lint fixups)**

```bash
git add -A
git commit -m "✅ issue: v1 数据层 + CRUD 接真实数据，全量测试通过"
```

---

## Self-Review

**Spec coverage:** migration+seed (Task 2) ✓; entities w/ state/agent_status/source/closed_at (Task 3) ✓; IssueRepo + Label/IssueLabel repos (Tasks 4–5) ✓; IssueSvc create/update/setstate/delete/get/list/listlabels w/ open/closed counts (Task 7) ✓; thin Wails binding + camelCase DTOs (Task 8) ✓; bootstrap wiring + generate (Task 9) ✓; hook (Task 11) ✓; create+edit dialog (Task 12) ✓; list+board+Open/Closed+label filter+row CRUD+empty/loading (Task 13) ✓; i18n remove samples / add keys / parity (Tasks 12–13) ✓; deferred items (dispatch/assignee/comments/webhook/label-mgmt/author+agent filters/DnD/per-project numbers) NOT implemented ✓.

**Placeholder scan:** Two intentional "read the file and mirror its pattern" steps remain — `wailsApp.ts` mock helper (Task 10 Step 2) and shadcn primitive member/prop names (Task 13 Step 4 note). These are codebase-specific facts the executor must confirm against the actual file; default values + fallbacks are specified for each. No TODO/TBD logic gaps.

**Type consistency:** Go: `issue_svc.IssueDetail{Issue,Labels}`, `ListIssuesResponse{Issues,OpenCount,ClosedCount}`, `ListFilter{State,ProjectID,LabelIDs,Sort}`, `CountByState(ctx,projectID)→(open,closed,err)` — used identically across repo/svc/binding. Entity consts `StateOpen/StateClosed/AgentStatusIdle/SourceManual` used in migration defaults, entity, svc. TS: `app.IssueItem`/`app.LabelItem`/`app.IssueListResponse` (generated camelCase) consumed in hook/dialog/page; `IssueNewDialog` prop is `onSaved` (matches page + test). Hook returns `{issues,labels,openCount,closedCount,loading,error,reload}` (matches page destructure).

## Out of scope (later increments)
Dispatch→session (running/waiting/error columns, redispatch, agent_status driving); assignees; comments/detail timeline; webhook-created issues (`source`!=manual); label management UI; author + assigned-agent filters; board drag-and-drop; per-project issue numbers.

