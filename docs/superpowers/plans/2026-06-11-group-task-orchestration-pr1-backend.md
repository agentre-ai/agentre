# 群任务卡编排 PR1(后端任务域)实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 落地 spec `docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md` 的后端任务域:`group_tasks`/`workflows` 表、任务卡三个 MCP tool、任务事件即消息的投递联动、系统提示强化(SOP 注入+任务快照+`.agentre/handoff` 约定)、`group_invite` 池放宽。

**Architecture:** 任务事件复用现有群消息管线(per-group `ingestMu` 串行化 seq/task_no → `persistMessage` → `enqueueDeliveries` → `kick`),不开第二条投递通道。任务卡是 `group_entity` 域内的充血实体;`workflows` 是新独立域(剧本库)。MCP 层只做参数解析与回调路由,鉴权/业务全在 svc。

**Tech Stack:** Go 1.26 / GORM+gormigrate(原生 SQL DDL) / goconvey(entity+svc 测试) / sqlmock+testutils.Database(repo 测试) / mockgen(go.uber.org/mock)。

**执行前置:** 在隔离 worktree 执行(superpowers:using-git-worktrees)。注意本仓 worktree 坑:Go 命令需 `GOWORK=off`;`go test ./...` 会扫 frontend/node_modules,一律用 `make test-backend` 或精确包路径。

**与 spec 的两处刻意偏差**(实施更优,Task 17 回写 spec):
1. 实体放 `group_entity` 包(spec 写 `group_task_entity`)——任务卡属于 group 域,与 GroupMember/GroupMessage 同包,符合「一个域一套包」。
2. 回指列叫 `parent_task_no`(spec 写 `parent_task_id`)——MCP 工具与 UI 都用群内编号 #N 寻址,存 task_no 避免 id/编号两套语义。

---

### Task 1: Migration 202606110001(group_tasks + workflows + 加列)

**Files:**
- Create: `migrations/202606110001_group_tasks_workflows.go`
- Create: `migrations/202606110001_group_tasks_workflows_test.go`
- Modify: `migrations/migrations.go:36`(列表末尾追加)

- [x] **Step 1: 写失败的迁移测试**

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606110001GroupTasksWorkflows(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 前置:群聊基线(groups/group_messages 表)。
	require.NoError(t, migration202606030001().Migrate(gdb))
	require.NoError(t, migration202606110001().Migrate(gdb))

	// group_tasks 表可写入且 (group_id, task_no) 唯一。
	require.NoError(t, gdb.Exec(`INSERT INTO group_tasks
		(group_id, task_no, title, brief, creator_member_id, assignee_member_id, status)
		VALUES (1, 1, '重构设置页', '按新稿', 100, 101, 'open')`).Error)
	err = gdb.Exec(`INSERT INTO group_tasks
		(group_id, task_no, title, creator_member_id, assignee_member_id, status)
		VALUES (1, 1, '重复编号', 100, 101, 'open')`).Error
	require.Error(t, err, "同群同 task_no 必须被唯一索引拒绝")

	// group_messages 两列默认值。
	require.NoError(t, gdb.Exec(`INSERT INTO group_messages (group_id, seq, content) VALUES (1, 1, 'x')`).Error)
	var msg struct {
		TaskID    int64
		TaskEvent string
	}
	require.NoError(t, gdb.Table("group_messages").Select("task_id, task_event").
		Where("group_id = 1").Scan(&msg).Error)
	require.Equal(t, int64(0), msg.TaskID)
	require.Equal(t, "", msg.TaskEvent)

	// workflows 表 + groups.workflow_id。
	require.NoError(t, gdb.Exec(`INSERT INTO workflows (name, content, status) VALUES ('产品开发流程', '# 流程', 1)`).Error)
	require.NoError(t, gdb.Exec(`INSERT INTO groups (title, host_agent_id, workflow_id) VALUES ('g', 1, 1)`).Error)
	var wf int64
	require.NoError(t, gdb.Table("groups").Select("workflow_id").Where("title = 'g'").Scan(&wf).Error)
	require.Equal(t, int64(1), wf)

	// Rollback 干净。
	require.NoError(t, migration202606110001().Rollback(gdb))
	require.Error(t, gdb.Exec(`SELECT 1 FROM group_tasks`).Error)
	require.Error(t, gdb.Exec(`SELECT 1 FROM workflows`).Error)
}
```

> import 的 sqlite 驱动以现有 `202605220010_projects_sort_order_test.go` 顶部 import 为准照抄(glebarez 或 gorm.io/driver/sqlite,看现有文件)。

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./migrations/ -run TestMigration202606110001 -v`
Expected: FAIL,`undefined: migration202606110001`

- [x] **Step 3: 写迁移实现**

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606110001 群任务卡编排基线:group_tasks / workflows 两张新表 +
// group_messages.task_id/task_event + groups.workflow_id。
// 同一特性的 schema 合并为一个迁移(先例:202606030001 群聊基线)。
func migration202606110001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606110001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	task_no INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	brief TEXT NOT NULL DEFAULT '',
	creator_member_id INTEGER NOT NULL DEFAULT 0,
	assignee_member_id INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'open',
	result TEXT NOT NULL DEFAULT '',
	parent_task_no INTEGER NOT NULL DEFAULT 0,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_tasks_group ON group_tasks(group_id, status, task_no)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_group_tasks_group_no ON group_tasks(group_id, task_no)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages ADD COLUMN task_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages ADD COLUMN task_event TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS workflows (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE groups ADD COLUMN workflow_id INTEGER NOT NULL DEFAULT 0`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE groups DROP COLUMN workflow_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS workflows`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages DROP COLUMN task_event`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages DROP COLUMN task_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_group_tasks_group_no`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_group_tasks_group`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS group_tasks`).Error
		},
	}
}
```

在 `migrations/migrations.go` 的 `migrationList()` 末尾(`migration202606100001()` 之后)追加:

```go
		migration202606110001(), // group_tasks + workflows + task message columns
```

- [x] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./migrations/ -run TestMigration202606110001 -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add migrations/
git commit -m "✨ migration: group_tasks/workflows 表 + group_messages task 列 + groups.workflow_id"
```

---

### Task 2: GroupTask 充血实体

**Files:**
- Create: `internal/model/entity/group_entity/task.go`
- Create: `internal/model/entity/group_entity/task_test.go`

- [x] **Step 1: 写失败的实体测试**(goconvey,风格对齐 `group_test.go`)

```go
package group_entity_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
)

func validTask() *group_entity.GroupTask {
	return &group_entity.GroupTask{
		GroupID: 1, TaskNo: 1, Title: "重构设置页",
		CreatorMemberID: 100, AssigneeMemberID: 101,
		Status: group_entity.TaskStatusOpen,
	}
}

func TestGroupTask_Check(t *testing.T) {
	ctx := context.Background()
	Convey("Check 校验必填与状态枚举", t, func() {
		So(validTask().Check(ctx), ShouldBeNil)

		Convey("nil receiver", func() {
			var t0 *group_entity.GroupTask
			So(t0.Check(ctx), ShouldNotBeNil)
		})
		Convey("blank title", func() {
			x := validTask()
			x.Title = "  "
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("missing assignee", func() {
			x := validTask()
			x.AssigneeMemberID = 0
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("missing creator", func() {
			x := validTask()
			x.CreatorMemberID = 0
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("bad status", func() {
			x := validTask()
			x.Status = "doing"
			So(x.Check(ctx), ShouldNotBeNil)
		})
	})
}

func TestGroupTask_Permissions(t *testing.T) {
	Convey("IsOpen / CanComplete / CanCancel", t, func() {
		x := validTask()
		So(x.IsOpen(), ShouldBeTrue)
		So(x.CanComplete(101), ShouldBeTrue)  // assignee
		So(x.CanComplete(100), ShouldBeFalse) // creator 不是执行人
		So(x.CanCancel(100, false), ShouldBeTrue) // creator
		So(x.CanCancel(999, true), ShouldBeTrue)  // 主持人
		So(x.CanCancel(999, false), ShouldBeFalse)

		x.Status = group_entity.TaskStatusDone
		So(x.IsOpen(), ShouldBeFalse)
		So(x.CanComplete(101), ShouldBeFalse) // 关单后不可再操作
		So(x.CanCancel(100, true), ShouldBeFalse)
	})
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/model/entity/group_entity/ -run 'TestGroupTask' -v`
Expected: FAIL,`undefined: group_entity.GroupTask`

- [x] **Step 3: 写实体实现**

```go
package group_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// 任务卡状态机刻意小:open → done / canceled(spec §3.1)。
const (
	TaskStatusOpen      = "open"
	TaskStatusDone      = "done"
	TaskStatusCanceled = "canceled"
)

// GroupTask 群内一张任务卡(派活-交付的结构化痕迹)。
type GroupTask struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID          int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	TaskNo           int    `gorm:"column:task_no;type:int;not null;default:0"`
	Title            string `gorm:"column:title;type:text;not null;default:''"`
	Brief            string `gorm:"column:brief;type:text;not null;default:''"`
	CreatorMemberID  int64  `gorm:"column:creator_member_id;type:bigint;not null;default:0"`
	AssigneeMemberID int64  `gorm:"column:assignee_member_id;type:bigint;not null;default:0"`
	Status           string `gorm:"column:status;type:text;not null;default:'open'"`
	Result           string `gorm:"column:result;type:text;not null;default:''"`
	ParentTaskNo     int    `gorm:"column:parent_task_no;type:int;not null;default:0"`
	Createtime       int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime       int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*GroupTask) TableName() string { return "group_tasks" }

// Check 字段校验(单实体规则)。
func (t *GroupTask) Check(ctx context.Context) error {
	if t == nil {
		return i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if t.GroupID <= 0 || strings.TrimSpace(t.Title) == "" ||
		t.CreatorMemberID <= 0 || t.AssigneeMemberID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	switch t.Status {
	case TaskStatusOpen, TaskStatusDone, TaskStatusCanceled:
		return nil
	default:
		return i18n.NewError(ctx, code.InvalidParameter)
	}
}

func (t *GroupTask) IsOpen() bool { return t != nil && t.Status == TaskStatusOpen }

// CanComplete 仅执行人可交付,且卡必须仍 open。
func (t *GroupTask) CanComplete(memberID int64) bool {
	return t.IsOpen() && t.AssigneeMemberID == memberID
}

// CanCancel 仅建卡人或主持人可取消,且卡必须仍 open。
func (t *GroupTask) CanCancel(memberID int64, isHost bool) bool {
	return t.IsOpen() && (t.CreatorMemberID == memberID || isHost)
}
```

> 此时 `code.GroupTaskNotFound` 还不存在 → 顺手在本任务加错误码(下一 Step),实体与错误码同 commit。

- [x] **Step 4: 加错误码**

`internal/pkg/code/code.go` 的 Group 段(19000~19999,`GroupInviteForbidden` 之后)追加:

```go
	GroupTaskNotFound       // 任务不存在
	GroupTaskForbidden      // 无权操作该任务
	GroupTaskClosed         // 任务已关闭
	GroupTaskResultRequired // 交付说明不能为空
```

`internal/pkg/code/zh_cn.go` 的 Group 段追加:

```go
	GroupTaskNotFound:       "任务不存在",
	GroupTaskForbidden:      "无权操作该任务",
	GroupTaskClosed:         "任务已关闭",
	GroupTaskResultRequired: "交付说明(result)不能为空",
```

`internal/pkg/code/en.go` 的 Group 段追加(对齐该文件既有英文风格):

```go
	GroupTaskNotFound:       "task not found",
	GroupTaskForbidden:      "not allowed to operate this task",
	GroupTaskClosed:         "task already closed",
	GroupTaskResultRequired: "result is required",
```

- [x] **Step 5: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/model/entity/group_entity/ -run 'TestGroupTask' -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/model/entity/group_entity/task.go internal/model/entity/group_entity/task_test.go internal/pkg/code/
git commit -m "✨ group: GroupTask 充血实体(open→done/canceled + 权限判定)+ 任务错误码"
```

---

### Task 3: Workflow 充血实体(新域)

**Files:**
- Create: `internal/model/entity/workflow_entity/workflow.go`
- Create: `internal/model/entity/workflow_entity/workflow_test.go`

- [x] **Step 1: 写失败的测试**

```go
package workflow_entity_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
)

func TestWorkflow_Check(t *testing.T) {
	ctx := context.Background()
	Convey("Check 校验 nil / 空名 / 合法", t, func() {
		var w0 *workflow_entity.Workflow
		So(w0.Check(ctx), ShouldNotBeNil)
		So((&workflow_entity.Workflow{Name: "  "}).Check(ctx), ShouldNotBeNil)
		So((&workflow_entity.Workflow{Name: "产品开发流程"}).Check(ctx), ShouldBeNil)
	})
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/model/entity/workflow_entity/ -v`
Expected: FAIL(包不存在)

- [x] **Step 3: 写实现**

```go
// Package workflow_entity 维护流程(剧本库)的充血实体。流程是写给群主持人读的
// 自由 Markdown SOP,与部门/项目正交(spec §3.2/§6.1)。
package workflow_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// Workflow 一条流程(SOP 剧本)。
type Workflow struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name       string `gorm:"column:name;type:text;not null;default:''"`
	Content    string `gorm:"column:content;type:text;not null;default:''"`
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Workflow) TableName() string { return "workflows" }

func (w *Workflow) IsActive() bool { return w != nil && w.Status == consts.ACTIVE }

// Check 字段校验。
func (w *Workflow) Check(ctx context.Context) error {
	if w == nil {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if strings.TrimSpace(w.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
```

- [x] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/model/entity/workflow_entity/ -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/model/entity/workflow_entity/
git commit -m "✨ workflow: 流程(剧本库)充血实体"
```

---

### Task 4: GroupMessage/Group 实体加字段 + 事件载荷字段

**Files:**
- Modify: `internal/model/entity/group_entity/message.go`(结构体 + 常量)
- Modify: `internal/model/entity/group_entity/group.go:22-35`(Group 加 WorkflowID)
- Modify: `internal/service/group_svc/events.go:27-38`(groupMessageEvent + toGroupMessageEvent)

- [x] **Step 1: message.go 加任务事件常量与字段**

在 `SenderKindSystem` 常量块之后追加:

```go
// 任务事件消息(task_event != "" 的 group_message 前端渲染为任务卡气泡,spec §3.3)。
const (
	TaskEventCreated   = "created"
	TaskEventCompleted = "completed"
	TaskEventCanceled = "canceled"
)
```

`GroupMessage` 结构体在 `SourceMessageID` 之后、`Createtime` 之前插入:

```go
	TaskID             int64  `gorm:"column:task_id;type:bigint;not null;default:0"`
	TaskEvent          string `gorm:"column:task_event;type:text;not null;default:''"`
```

- [x] **Step 2: group.go 的 Group 结构体加列**(`Pinned` 之后):

```go
	WorkflowID   int64  `gorm:"column:workflow_id;type:bigint;not null;default:0"`
```

- [x] **Step 3: events.go 载荷加字段**

`groupMessageEvent` 结构体(`Createtime` 之前)插入:

```go
	TaskID             int64   `json:"taskID"`
	TaskEvent          string  `json:"taskEvent"`
```

`toGroupMessageEvent` 对应补两行映射:

```go
		TaskID:             m.TaskID,
		TaskEvent:          m.TaskEvent,
```

- [x] **Step 4: 编译 + 全量回归**

Run: `GOWORK=off go build ./... && GOWORK=off go test -race ./internal/service/group_svc/ ./internal/model/entity/group_entity/`
Expected: PASS(纯加字段,零行为变化)

- [x] **Step 5: Commit**

```bash
git add internal/model/entity/group_entity/ internal/service/group_svc/events.go
git commit -m "✨ group: 消息实体 task_id/task_event + Group.workflow_id + 事件载荷透传"
```

---

### Task 5: group_repo 任务仓储(sqlmock 测试)

**Files:**
- Create: `internal/repository/group_repo/task.go`
- Create: `internal/repository/group_repo/task_test.go`
- Modify: `internal/bootstrap/cago.go:110` 附近(装配)

- [x] **Step 1: 写失败的 sqlmock 测试**(风格对齐 `group_test.go:263-289`)

```go
package group_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/testutils"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
)

func TestGroupTaskRepo_NextTaskNo(t *testing.T) {
	t.Run("有任务时返回 max+1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT COALESCE\(MAX\(task_no\), 0\) FROM .group_tasks. WHERE group_id = \?`).
			WithArgs(int64(5)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(3))
		n, err := group_repo.NewTask().NextTaskNo(ctx, 5)
		require.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("无任务返回 1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT COALESCE\(MAX\(task_no\), 0\) FROM .group_tasks. WHERE group_id = \?`).
			WithArgs(int64(9)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
		n, err := group_repo.NewTask().NextTaskNo(ctx, 9)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupTaskRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .group_tasks.`).
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()

	task := &group_entity.GroupTask{GroupID: 5, TaskNo: 1, Title: "t",
		CreatorMemberID: 1, AssigneeMemberID: 2, Status: group_entity.TaskStatusOpen}
	require.NoError(t, group_repo.NewTask().Create(ctx, task))
	assert.NotZero(t, task.Createtime, "Create 必须补 createtime/updatetime")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupTaskRepo_FindByGroupAndNo(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .group_tasks. WHERE group_id = \? AND task_no = \?`).
		WithArgs(int64(5), 3, sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "task_no", "status"}).
			AddRow(7, 5, 3, "open"))
	got, err := group_repo.NewTask().FindByGroupAndNo(ctx, 5, 3)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(7), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupTaskRepo_ListByGroup(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .group_tasks. WHERE group_id = \? ORDER BY task_no ASC`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_no"}).AddRow(1, 1).AddRow(2, 2))
	rows, err := group_repo.NewTask().ListByGroup(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

> sqlmock 正则按现有 `group_test.go` 的实际期望微调(如 Find 带 LIMIT 参数);先照写,跑红后按真实 SQL 修正则——修的是测试的正则,不是实现。

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/repository/group_repo/ -run TestGroupTaskRepo -v`
Expected: FAIL,`undefined: group_repo.NewTask`

- [x] **Step 3: 写仓储实现**

`internal/repository/group_repo/task.go`:

```go
package group_repo

import (
	"context"
	"time"

	"github.com/cago-frame/cago/database/db"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
)

//go:generate mockgen -source task.go -destination mock_group_repo/mock_task.go

// GroupTaskRepo 群任务卡仓储。
type GroupTaskRepo interface {
	Create(ctx context.Context, t *group_entity.GroupTask) error
	Update(ctx context.Context, t *group_entity.GroupTask) error
	FindByGroupAndNo(ctx context.Context, groupID int64, taskNo int) (*group_entity.GroupTask, error)
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupTask, error)
	NextTaskNo(ctx context.Context, groupID int64) (int, error)
}

var defaultTask GroupTaskRepo

func Task() GroupTaskRepo                 { return defaultTask }
func RegisterTask(impl GroupTaskRepo)     { defaultTask = impl }
func NewTask() GroupTaskRepo              { return &taskRepo{} }

type taskRepo struct{}

func (r *taskRepo) Create(ctx context.Context, t *group_entity.GroupTask) error {
	now := time.Now().UnixMilli()
	if t.Createtime == 0 {
		t.Createtime = now
	}
	t.Updatetime = now
	return db.Ctx(ctx).Create(t).Error
}

func (r *taskRepo) Update(ctx context.Context, t *group_entity.GroupTask) error {
	t.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(t).Error
}

func (r *taskRepo) FindByGroupAndNo(ctx context.Context, groupID int64, taskNo int) (*group_entity.GroupTask, error) {
	var t group_entity.GroupTask
	err := db.Ctx(ctx).Where("group_id = ? AND task_no = ?", groupID, taskNo).First(&t).Error
	if db.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *taskRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupTask, error) {
	var rows []*group_entity.GroupTask
	err := db.Ctx(ctx).Where("group_id = ?", groupID).Order("task_no ASC").Find(&rows).Error
	return rows, err
}

func (r *taskRepo) NextTaskNo(ctx context.Context, groupID int64) (int, error) {
	var maxNo int
	err := db.Ctx(ctx).Model(&group_entity.GroupTask{}).
		Where("group_id = ?", groupID).
		Select("COALESCE(MAX(task_no), 0)").Scan(&maxNo).Error
	if err != nil {
		return 0, err
	}
	return maxNo + 1, nil
}
```

> `db.IsNotFound` 的写法以 `group_repo/group.go` 里现有 Find 的 not-found 处理为准照抄(可能是 `errors.Is(err, gorm.ErrRecordNotFound)`)。

- [x] **Step 4: 装配 + 生成 mock**

`internal/bootstrap/cago.go`(`group_repo.RegisterMessage(...)` 之后)追加:

```go
	group_repo.RegisterTask(group_repo.NewTask())
```

Run: `make mock`
Expected: 生成 `internal/repository/group_repo/mock_group_repo/mock_task.go`

- [x] **Step 5: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/repository/group_repo/ -run TestGroupTaskRepo -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/repository/group_repo/ internal/bootstrap/cago.go
git commit -m "✨ group: GroupTaskRepo 仓储(NextTaskNo/FindByGroupAndNo)+ 装配 + mock"
```

---

### Task 6: workflow_repo 仓储

**Files:**
- Create: `internal/repository/workflow_repo/workflow.go`
- Create: `internal/repository/workflow_repo/workflow_test.go`
- Modify: `internal/bootstrap/cago.go`(装配)

- [x] **Step 1: 写失败的 sqlmock 测试**

```go
package workflow_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/pkg/testutils"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
)

func TestWorkflowRepo_CreateAndFind(t *testing.T) {
	t.Run("Create 补时间戳", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO .workflows.`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		w := &workflow_entity.Workflow{Name: "产品开发流程", Content: "# 流程", Status: 1}
		require.NoError(t, workflow_repo.NewWorkflow().Create(ctx, w))
		assert.NotZero(t, w.Createtime)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("Find 不存在返回 nil,nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT \* FROM .workflows. WHERE id = \?`).
			WithArgs(int64(99), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		got, err := workflow_repo.NewWorkflow().Find(ctx, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
	})
}

func TestWorkflowRepo_List(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .workflows. WHERE status = \? ORDER BY updatetime DESC`).
		WithArgs(1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a").AddRow(2, "b"))
	rows, err := workflow_repo.NewWorkflow().List(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
}

func TestWorkflowRepo_Delete(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE .workflows. SET .status.=`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, workflow_repo.NewWorkflow().Delete(ctx, 1))
	assert.NoError(t, mock.ExpectationsWereMet())
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/repository/workflow_repo/ -v`
Expected: FAIL(包不存在)

- [x] **Step 3: 写实现**

```go
// Package workflow_repo 流程(剧本库)仓储。
package workflow_repo

import (
	"context"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
)

//go:generate mockgen -source workflow.go -destination mock_workflow_repo/mock_workflow.go

// WorkflowRepo 流程仓储。
type WorkflowRepo interface {
	Create(ctx context.Context, w *workflow_entity.Workflow) error
	Update(ctx context.Context, w *workflow_entity.Workflow) error
	Find(ctx context.Context, id int64) (*workflow_entity.Workflow, error)
	List(ctx context.Context) ([]*workflow_entity.Workflow, error)
	Delete(ctx context.Context, id int64) error
}

var defaultWorkflow WorkflowRepo

func Workflow() WorkflowRepo             { return defaultWorkflow }
func RegisterWorkflow(impl WorkflowRepo) { defaultWorkflow = impl }
func NewWorkflow() WorkflowRepo          { return &workflowRepo{} }

type workflowRepo struct{}

func (r *workflowRepo) Create(ctx context.Context, w *workflow_entity.Workflow) error {
	now := time.Now().UnixMilli()
	if w.Createtime == 0 {
		w.Createtime = now
	}
	w.Updatetime = now
	return db.Ctx(ctx).Create(w).Error
}

func (r *workflowRepo) Update(ctx context.Context, w *workflow_entity.Workflow) error {
	w.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(w).Error
}

func (r *workflowRepo) Find(ctx context.Context, id int64) (*workflow_entity.Workflow, error) {
	var w workflow_entity.Workflow
	err := db.Ctx(ctx).Where("id = ?", id).First(&w).Error
	if db.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *workflowRepo) List(ctx context.Context) ([]*workflow_entity.Workflow, error) {
	var rows []*workflow_entity.Workflow
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("updatetime DESC").Find(&rows).Error
	return rows, err
}

// Delete 软删(status=DELETE),已绑定该流程的群按「不绑定」处理(注入时查不到即跳过)。
func (r *workflowRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&workflow_entity.Workflow{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": consts.DELETE, "updatetime": time.Now().UnixMilli()}).Error
}
```

> not-found 判定与 sqlmock 正则同 Task 5 的备注:以现有 repo 实际写法为准。

- [x] **Step 4: 装配 + mock**

`internal/bootstrap/cago.go` 的 repo 注册区追加:

```go
	workflow_repo.RegisterWorkflow(workflow_repo.NewWorkflow())
```

Run: `make mock && GOWORK=off go test -race ./internal/repository/workflow_repo/ -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/repository/workflow_repo/ internal/bootstrap/cago.go
git commit -m "✨ workflow: WorkflowRepo 仓储(CRUD+软删)+ 装配 + mock"
```

---

### Task 7: persistMessage 扩签名(携带 task_id/task_event)

**Files:**
- Modify: `internal/service/group_svc/group.go:464-485`(persistMessage)
- Modify: 全部调用点(grep `persistMessage(` — ingest.go / SendGroupMessage / HandleInvite / AddGroupMember 等)

- [x] **Step 1: 改签名与落库**

```go
func (s *groupSvc) persistMessage(ctx context.Context, g *group_entity.Group, kind string, senderMemberID int64, content string, recipients []int64, toUser bool, sourceMsgID int64, taskID int64, taskEvent string) (*group_entity.GroupMessage, error) {
	seq, err := group_repo.Message().NextSeq(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	m := &group_entity.GroupMessage{
		GroupID:         g.ID,
		Seq:             seq,
		SenderKind:      kind,
		SenderMemberID:  senderMemberID,
		ToUser:          toUser,
		Content:         content,
		SourceMessageID: sourceMsgID,
		TaskID:          taskID,
		TaskEvent:       taskEvent,
		Createtime:      s.now(),
	}
	m.SetRecipients(recipients)
	if err := group_repo.Message().Create(ctx, m); err != nil {
		return nil, err
	}
	s.emitter.Emit(ctx, groupEventName(g.ID), map[string]any{"kind": "message", "message": toGroupMessageEvent(m)})
	return m, nil
}
```

- [x] **Step 2: 更新全部既有调用点**

Run: `grep -rn "persistMessage(" internal/service/group_svc/ --include="*.go" | grep -v _test`

每个既有调用尾部追加 `, 0, ""`(普通消息无任务语义)。例如 ingest.go:

```go
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, sender.ID, body, recipientIDs, toUser, 0, 0, ""); err != nil {
```

- [x] **Step 3: 编译 + 全量回归**

Run: `GOWORK=off go build ./... && GOWORK=off go test -race ./internal/service/group_svc/`
Expected: PASS(行为不变,纯透传)

- [x] **Step 4: Commit**

```bash
git add internal/service/group_svc/
git commit -m "♻️ group: persistMessage 携带 task_id/task_event(任务事件即消息的管线座)"
```

---

### Task 8: svc HandleTaskCreate(建卡即派活)

**Files:**
- Create: `internal/service/group_svc/task.go`
- Create: `internal/service/group_svc/task_test.go`
- Modify: `internal/service/group_svc/group.go:45-67`(GroupSvc 接口加方法)

- [x] **Step 1: 写失败的测试**(范式对齐 `send_test.go:19-61`;mock 全部 repo + gateway)

```go
package group_svc_test

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

// registerTaskMocks 注册任务域测试共用的 mock 仓储,返回各 mock。
func registerTaskMocks(t *testing.T, ctrl *gomock.Controller) (*mock_group_repo.MockGroupRepo, *mock_group_repo.MockGroupMemberRepo, *mock_group_repo.MockGroupMessageRepo, *mock_group_repo.MockGroupTaskRepo) {
	groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
	memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
	msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
	taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
	group_repo.RegisterGroup(groupRepo)
	group_repo.RegisterMember(memberRepo)
	group_repo.RegisterMessage(msgRepo)
	group_repo.RegisterTask(taskRepo)
	return groupRepo, memberRepo, msgRepo, taskRepo
}

func TestHandleTaskCreate(t *testing.T) {
	Convey("主持人建卡 → 落卡+落 created 消息+投递给执行人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		taskRepo.EXPECT().NextTaskNo(gomock.Any(), int64(5)).Return(1, nil)
		taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.TaskNo, ShouldEqual, 1)
				So(x.CreatorMemberID, ShouldEqual, 100)
				So(x.AssigneeMemberID, ShouldEqual, 101)
				So(x.Status, ShouldEqual, group_entity.TaskStatusOpen)
				x.ID = 77
				return nil
			})
		// 任务快照注入会在 launch 路径再读一次任务列表 → 容忍。
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCreated)
				So(m.TaskID, ShouldEqual, 77)
				So(m.Recipients(), ShouldResemble, []int64{101})
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindAgent)
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		// 执行人被 kick 起轮 → 容忍调度副作用。
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		task, err := svc.HandleTaskCreate(ctx, 100, "前端工程师", "重构设置页", "按新稿来", 0)
		So(err, ShouldBeNil)
		So(task.TaskNo, ShouldEqual, 1)
	})

	Convey("assignee 不在群 → GroupMemberNotFound", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, _ := registerTaskMocks(t, ctrl)

		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host}, nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		_, err := svc.HandleTaskCreate(ctx, 100, "不存在的人", "t", "b", 0)
		So(err, ShouldNotBeNil)
	})
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run TestHandleTaskCreate -v`
Expected: FAIL,`svc.HandleTaskCreate undefined`

- [x] **Step 3: 写实现**

`internal/service/group_svc/group.go` 的 `GroupSvc` 接口(`HandleInvite` 之后)追加:

```go
	// HandleTaskCreate / HandleTaskComplete / HandleTaskCancel 是任务卡三个 MCP tool 的服务端入口。
	HandleTaskCreate(ctx context.Context, callerMemberID int64, assigneeName, title, brief string, parentTaskNo int) (*group_entity.GroupTask, error)
	HandleTaskComplete(ctx context.Context, callerMemberID int64, taskNo int, result string) (*group_entity.GroupTask, error)
	HandleTaskCancel(ctx context.Context, callerMemberID int64, taskNo int, reason string) (*group_entity.GroupTask, error)
```

新建 `internal/service/group_svc/task.go`:

```go
package group_svc

import (
	"context"
	"fmt"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"
	"go.uber.org/zap"

	"github.com/cago-frame/cago/pkg/logger"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
)

// HandleTaskCreate 是 group_task_create MCP tool 的服务端入口:建卡即派活。
// 落卡 + 落一条 task_event=created 的群消息投递给执行人(复用消息管线触发其轮次)。
func (s *groupSvc) HandleTaskCreate(ctx context.Context, callerMemberID int64, assigneeName, title, brief string, parentTaskNo int) (*group_entity.GroupTask, error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil || !caller.IsActive() {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	// 与 IngestAgentMessage 共用 per-group 锁:task_no 与 seq 同纪律,不重号(spec §5)。
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	assignee := s.memberByName(ctx, members, assigneeName)
	if assignee == nil {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	no, err := group_repo.Task().NextTaskNo(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	t := &group_entity.GroupTask{
		GroupID: g.ID, TaskNo: no,
		Title: strings.TrimSpace(title), Brief: strings.TrimSpace(brief),
		CreatorMemberID: caller.ID, AssigneeMemberID: assignee.ID,
		Status: group_entity.TaskStatusOpen, ParentTaskNo: parentTaskNo,
		Createtime: s.now(), Updatetime: s.now(),
	}
	if err := t.Check(ctx); err != nil {
		return nil, err
	}
	if err := group_repo.Task().Create(ctx, t); err != nil {
		return nil, err
	}
	content := fmt.Sprintf("任务 #%d：%s\n%s", no, t.Title, t.Brief)
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, []int64{assignee.ID}, false, 0, t.ID, group_entity.TaskEventCreated); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskCreate: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, []int64{assignee.ID}, content, s.names(ctx, caller.AgentID), caller.ID)
	s.kick(ctx, g.ID)
	logger.Ctx(ctx).Info("group_svc.HandleTaskCreate: created",
		zap.Int64("groupId", g.ID), zap.Int("taskNo", no), zap.Int64("assignee", assignee.ID))
	return t, nil
}

// memberByName 在群成员里按显示名找 active 成员(找不到返回 nil)。
func (s *groupSvc) memberByName(ctx context.Context, members []*group_entity.GroupMember, name string) *group_entity.GroupMember {
	for _, m := range members {
		if m.IsActive() && s.names(ctx, m.AgentID) == name {
			return m
		}
	}
	return nil
}

// bumpRound 任务事件与成员发言同样计轮(spec §5)。
func (s *groupSvc) bumpRound(ctx context.Context, g *group_entity.Group) {
	g.RoundCount++
	if err := group_repo.Group().Update(ctx, g); err != nil {
		logger.Ctx(ctx).Warn("group_svc.bumpRound: update failed", zap.Int64("groupId", g.ID), zap.Error(err))
	}
}
```

`internal/service/group_svc/events.go` 追加:

```go
// GroupTaskEvent 推给前端的任务事件载荷;json 形状须与 app.GroupTaskItem 一致。
type GroupTaskEvent struct {
	ID               int64  `json:"id"`
	TaskNo           int    `json:"taskNo"`
	Title            string `json:"title"`
	Brief            string `json:"brief"`
	CreatorMemberID  int64  `json:"creatorMemberID"`
	AssigneeMemberID int64  `json:"assigneeMemberID"`
	Status           string `json:"status"`
	Result           string `json:"result"`
	ParentTaskNo     int    `json:"parentTaskNo"`
	Createtime       int64  `json:"createtime"`
	Updatetime       int64  `json:"updatetime"`
}

func toGroupTaskEvent(t *group_entity.GroupTask) GroupTaskEvent {
	return GroupTaskEvent{
		ID: t.ID, TaskNo: t.TaskNo, Title: t.Title, Brief: t.Brief,
		CreatorMemberID: t.CreatorMemberID, AssigneeMemberID: t.AssigneeMemberID,
		Status: t.Status, Result: t.Result, ParentTaskNo: t.ParentTaskNo,
		Createtime: t.Createtime, Updatetime: t.Updatetime,
	}
}
```

`task.go` 里再加:

```go
// emitTaskUpdated 推任务状态变化(前端任务 tab + 历史卡片状态回写,spec §8)。
func (s *groupSvc) emitTaskUpdated(ctx context.Context, t *group_entity.GroupTask) {
	s.emitter.Emit(ctx, groupEventName(t.GroupID), map[string]any{"kind": "task_updated", "task": toGroupTaskEvent(t)})
}
```

- [x] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run TestHandleTaskCreate -v`
Expected: PASS

> 若既有测试因 `group_repo.Task()` 未注册而 panic:在对应测试加 `taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl); group_repo.RegisterTask(taskRepo); taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()`(Task 11 的提示注入会读它)。本任务此问题尚未出现(注入在 Task 11 才接线),先记住这个模式。

- [x] **Step 5: Commit**

```bash
git add internal/service/group_svc/
git commit -m "✨ group: HandleTaskCreate 建卡即派活(task_no 锁内分配+created 消息进管线)"
```

---

### Task 9: svc HandleTaskComplete(交付回建卡人)

**Files:**
- Modify: `internal/service/group_svc/task.go`
- Modify: `internal/service/group_svc/task_test.go`

- [x] **Step 1: 写失败的测试**

```go
func TestHandleTaskComplete(t *testing.T) {
	Convey("执行人交付 → 卡置 done + completed 消息投回建卡人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(dev, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.Status, ShouldEqual, group_entity.TaskStatusDone)
				So(x.Result, ShouldEqual, "改了 12 个文件,测试通过")
				return nil
			})
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCompleted)
				So(m.Recipients(), ShouldResemble, []int64{100}) // 回建卡人
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		done, err := svc.HandleTaskComplete(ctx, 101, 1, "改了 12 个文件,测试通过")
		So(err, ShouldBeNil)
		So(done.Status, ShouldEqual, group_entity.TaskStatusDone)
	})

	Convey("非执行人 complete → GroupTaskForbidden;空 result → GroupTaskResultRequired;关单再 complete → GroupTaskClosed", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil).AnyTimes()
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()

		Convey("空 result 在查库前就拒绝", func() {
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 1, "   ")
			So(err, ShouldNotBeNil)
		})
		Convey("非执行人", func() {
			open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 1, "我替他交了")
			So(err, ShouldNotBeNil)
		})
		Convey("已关单", func() {
			closed := &group_entity.GroupTask{ID: 78, GroupID: 5, TaskNo: 2, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 100, Status: group_entity.TaskStatusDone}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 2).Return(closed, nil)
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 2, "再交一次")
			So(err, ShouldNotBeNil)
		})
	})
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run TestHandleTaskComplete -v`
Expected: FAIL,方法未定义

- [x] **Step 3: 写实现**(task.go 追加)

```go
// HandleTaskComplete 是 group_task_complete MCP tool 的服务端入口:仅执行人可交付,
// result 必填(软验收门)。completed 消息投回建卡人;建卡人已离群走 applyFallback 既有链。
func (s *groupSvc) HandleTaskComplete(ctx context.Context, callerMemberID int64, taskNo int, result string) (*group_entity.GroupTask, error) {
	result = strings.TrimSpace(result)
	if result == "" {
		return nil, i18n.NewError(ctx, code.GroupTaskResultRequired)
	}
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil || !caller.IsActive() {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	t, err := group_repo.Task().FindByGroupAndNo(ctx, g.ID, taskNo)
	if err != nil || t == nil {
		return nil, i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if !t.IsOpen() {
		return nil, i18n.NewError(ctx, code.GroupTaskClosed)
	}
	if !t.CanComplete(caller.ID) {
		return nil, i18n.NewError(ctx, code.GroupTaskForbidden)
	}
	t.Status = group_entity.TaskStatusDone
	t.Result = result
	t.Updatetime = s.now()
	if err := group_repo.Task().Update(ctx, t); err != nil {
		return nil, err
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	// 默认投回建卡人;建卡人已离群 → 复用 applyFallback 既有回退链(来源/最近发言者/用户)。
	recipients := []int64{}
	toUser := false
	if creator := activeMemberByID(members, t.CreatorMemberID); creator != nil {
		recipients = []int64{creator.ID}
	} else {
		recipients, toUser = s.applyFallback(ctx, g, caller, members, nil, false)
	}
	content := fmt.Sprintf("任务 #%d 已完成\n%s", t.TaskNo, result)
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, recipients, toUser, 0, t.ID, group_entity.TaskEventCompleted); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskComplete: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, recipients, content, s.names(ctx, caller.AgentID), caller.ID)
	s.kick(ctx, g.ID)
	logger.Ctx(ctx).Info("group_svc.HandleTaskComplete: done",
		zap.Int64("groupId", g.ID), zap.Int("taskNo", taskNo))
	return t, nil
}

// activeMemberByID 在成员列表里找 active 成员(找不到/已离群返回 nil)。
func activeMemberByID(members []*group_entity.GroupMember, id int64) *group_entity.GroupMember {
	for _, m := range members {
		if m.ID == id && m.IsActive() {
			return m
		}
	}
	return nil
}
```

- [x] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run TestHandleTaskComplete -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/service/group_svc/
git commit -m "✨ group: HandleTaskComplete(仅执行人+result 必填软门+交付回建卡人)"
```

---

### Task 10: svc HandleTaskCancel + 成员离群级联取消

**Files:**
- Modify: `internal/service/group_svc/task.go`
- Modify: `internal/service/group_svc/group.go:391-413`(RemoveGroupMember)
- Modify: `internal/service/group_svc/task_test.go`

- [x] **Step 1: 写失败的测试**

```go
func TestHandleTaskCancel(t *testing.T) {
	Convey("建卡人取消 → 卡置 canceled + canceled 消息投给执行人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
				return nil
			})
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
				So(m.Recipients(), ShouldResemble, []int64{101}) // 通知执行人
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).Return(&chat_svc.SendResponse{}, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		got, err := svc.HandleTaskCancel(ctx, 100, 1, "需求变了")
		So(err, ShouldBeNil)
		So(got.Status, ShouldEqual, group_entity.TaskStatusCanceled)
	})

	Convey("无关成员取消 → GroupTaskForbidden", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, taskRepo := registerTaskMocks(t, ctrl)

		other := &group_entity.GroupMember{ID: 102, GroupID: 5, AgentID: 3, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(102)).Return(other, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
		open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleTaskCancel(ctx, 102, 1, "我看不顺眼")
		So(err, ShouldNotBeNil)
	})
}

func TestRemoveGroupMember_CancelsOpenTasks(t *testing.T) {
	Convey("成员离群 → 其名下 open 任务级联取消 + 落 system 消息", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(dev, nil)
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		gw.EXPECT().DeleteSession(gomock.Any(), int64(12)).Return(nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil).AnyTimes()

		mine := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		others := &group_entity.GroupTask{ID: 78, GroupID: 5, TaskNo: 2, Title: "x",
			CreatorMemberID: 100, AssigneeMemberID: 100, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupTask{mine, others}, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.ID, ShouldEqual, 77) // 只取消该成员名下的
				So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
				return nil
			})
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindSystem)
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
				return nil
			})

		svc := group_svc.NewForTest(gw)
		So(svc.RemoveGroupMember(ctx, 101), ShouldBeNil)
	})
}
```

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run 'TestHandleTaskCancel|TestRemoveGroupMember_CancelsOpenTasks' -v`
Expected: FAIL

- [x] **Step 3: 写实现**

task.go 追加:

```go
// HandleTaskCancel 是 group_task_cancel MCP tool 的服务端入口:仅建卡人或主持人。
func (s *groupSvc) HandleTaskCancel(ctx context.Context, callerMemberID int64, taskNo int, reason string) (*group_entity.GroupTask, error) {
	caller, err := group_repo.Member().Find(ctx, callerMemberID)
	if err != nil || caller == nil || !caller.IsActive() {
		return nil, i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	mu := s.ingestMu(caller.GroupID)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, caller.GroupID)
	if err != nil || g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	t, err := group_repo.Task().FindByGroupAndNo(ctx, g.ID, taskNo)
	if err != nil || t == nil {
		return nil, i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if !t.IsOpen() {
		return nil, i18n.NewError(ctx, code.GroupTaskClosed)
	}
	if !t.CanCancel(caller.ID, caller.IsHost()) {
		return nil, i18n.NewError(ctx, code.GroupTaskForbidden)
	}
	t.Status = group_entity.TaskStatusCanceled
	t.Updatetime = s.now()
	if err := group_repo.Task().Update(ctx, t); err != nil {
		return nil, err
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return nil, err
	}
	// 通知执行人与建卡人(去掉调用者自己、去重、过滤已离群)。
	notify := []int64{}
	for _, id := range []int64{t.AssigneeMemberID, t.CreatorMemberID} {
		if id == caller.ID {
			continue
		}
		if m := activeMemberByID(members, id); m != nil && !containsID(notify, id) {
			notify = append(notify, id)
		}
	}
	content := fmt.Sprintf("任务 #%d 已取消：%s", t.TaskNo, strings.TrimSpace(reason))
	s.bumpRound(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, caller.ID, content, notify, false, 0, t.ID, group_entity.TaskEventCanceled); err != nil {
		logger.Ctx(ctx).Warn("group_svc.HandleTaskCancel: persist failed", zap.Error(err))
	}
	s.emitTaskUpdated(ctx, t)
	s.enqueueDeliveries(g.ID, notify, content, s.names(ctx, caller.AgentID), caller.ID)
	s.kick(ctx, g.ID)
	return t, nil
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

// cancelTasksOfLeftMember 成员离群级联:其名下 open 任务全部取消 + 落 system 消息(spec §5)。
// 在 ingestMu 内调用(persistMessage 需要 seq 纪律)。
func (s *groupSvc) cancelTasksOfLeftMember(ctx context.Context, groupID, memberID int64) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil || g == nil {
		return
	}
	tasks, err := group_repo.Task().ListByGroup(ctx, groupID)
	if err != nil {
		return
	}
	for _, t := range tasks {
		if !t.IsOpen() || t.AssigneeMemberID != memberID {
			continue
		}
		t.Status = group_entity.TaskStatusCanceled
		t.Updatetime = s.now()
		if err := group_repo.Task().Update(ctx, t); err != nil {
			logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: update failed",
				zap.Int64("taskId", t.ID), zap.Error(err))
			continue
		}
		content := fmt.Sprintf("任务 #%d 已取消(执行成员离群)", t.TaskNo)
		if _, err := s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, content, nil, false, 0, t.ID, group_entity.TaskEventCanceled); err != nil {
			logger.Ctx(ctx).Warn("group_svc.cancelTasksOfLeftMember: persist failed", zap.Error(err))
		}
		s.emitTaskUpdated(ctx, t)
	}
}
```

`RemoveGroupMember`(group.go)在 `group_repo.Member().Update(ctx, m)` 成功之后、DeleteSession 之前插入:

```go
	// 离群级联:其名下 open 任务全部取消(spec §5)。与消息管线共用 per-group 锁。
	func() {
		mu := s.ingestMu(m.GroupID)
		mu.Lock()
		defer mu.Unlock()
		s.cancelTasksOfLeftMember(ctx, m.GroupID, m.ID)
	}()
```

- [x] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run 'TestHandleTaskCancel|TestRemoveGroupMember' -v`
Expected: PASS。若既有 RemoveGroupMember 测试红了:按 Step 1 的模式给它注册 task mock(`ListByGroup` 返回空)。

- [x] **Step 5: Commit**

```bash
git add internal/service/group_svc/
git commit -m "✨ group: HandleTaskCancel + 成员离群级联取消名下任务"
```

---

### Task 11: MCP 三工具(schema+路由)+ buildGroupMCP + 系统提示强化

**Files:**
- Modify: `internal/service/group_svc/mcp.go`(Arguments 字段、schema、路由、回调)
- Modify: `internal/service/group_svc/group.go:523-561`(buildGroupMCP + buildGroupSystemPrompt)
- Modify: `internal/service/group_svc/mcp_test.go`、`group_test.go`(提示断言)

- [x] **Step 1: 写失败的测试**

mcp 路由测试(对齐现有 mcp_test.go 的 httptest 风格;若现有测试用别的模式,以现有为准改写):

```go
func TestMCP_TaskTools(t *testing.T) {
	Convey("group_task_create/complete/cancel 路由到回调", t, func() {
		// 构造 svc 并替换任务回调为桩,POST tools/call 断言回调参数与响应文本。
		// 参考现有 group_send 路由测试的构造方式(token 用 MintToken 签)。
		// 断言点:
		// 1. tools/list 包含 5 个工具名。
		// 2. group_task_create(assignee/title/brief/parentTaskId) → 回调收到一致参数,响应含 "task #1 created"。
		// 3. group_task_complete(taskId=1, result) → 响应含 "task #1 completed"。
		// 4. group_task_cancel(taskId=1, reason) → 响应含 "task #1 canceled"。
		// 5. 回调返回 error → JSON-RPC error(-32000)。
	})
}
```

提示词测试(group_test.go 或新 prompt_test.go;buildGroupSystemPrompt 未导出 → 经现有该函数的测试入口,如有,沿用;没有则通过 NewForTestWithNames 暴露的 launch 路径捕获 SendRequest.SystemPromptSuffix 断言):

```go
func TestGroupSystemPrompt_TasksAndWorkflow(t *testing.T) {
	Convey("主持人提示含任务动作环+SOP+未完成任务快照;成员提示含交付纪律", t, func() {
		// 安排:g.WorkflowID=3;workflow_repo mock Find(3) 返回 content "# 产品开发流程…";
		// task repo ListByGroup 返回一张 open 卡(#2 → 前端工程师)。
		// 经 SendGroupMessage 触发 launchDelivery,拦截 gw.Send 的 req.SystemPromptSuffix:
		// So(suffix, ShouldContainSubstring, "group_task_create")
		// So(suffix, ShouldContainSubstring, "产品开发流程")        // SOP 注入(仅主持人)
		// So(suffix, ShouldContainSubstring, "#2")                  // 任务快照
		// So(suffix, ShouldContainSubstring, ".agentre/handoff")    // 交付物约定
	})
}
```

> 两个测试骨架在 Step 1 写成可编译、断言完整的真实测试(参数构造照抄现有同类测试),此处省略的是与现有测试雷同的搭建样板,**不是**省略断言。

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run 'TestMCP_TaskTools|TestGroupSystemPrompt_TasksAndWorkflow' -v`
Expected: FAIL

- [x] **Step 3: mcp.go 实现**

Arguments 结构体(`ServeHTTP` 内)追加字段:

```go
				Assignee     string `json:"assignee"`
				Title        string `json:"title"`
				Brief        string `json:"brief"`
				ParentTaskID int    `json:"parentTaskId"`
				TaskID       int    `json:"taskId"`
				Result       string `json:"result"`
```

`groupMCP` 结构体追加回调(返回 task_no/err,保持 mcp 层与实体解耦):

```go
	taskCreate   func(ctx context.Context, memberID int64, assignee, title, brief string, parentTaskNo int) (int, error)
	taskComplete func(ctx context.Context, memberID int64, taskNo int, result string) error
	taskCancel   func(ctx context.Context, memberID int64, taskNo int, reason string) error
```

`newGroupSvc` 绑定(`s.mcp.invite = s.HandleInvite` 之后):

```go
	s.mcp.taskCreate = func(ctx context.Context, memberID int64, assignee, title, brief string, parentTaskNo int) (int, error) {
		t, err := s.HandleTaskCreate(ctx, memberID, assignee, title, brief, parentTaskNo)
		if err != nil {
			return 0, err
		}
		return t.TaskNo, nil
	}
	s.mcp.taskComplete = func(ctx context.Context, memberID int64, taskNo int, result string) error {
		_, err := s.HandleTaskComplete(ctx, memberID, taskNo, result)
		return err
	}
	s.mcp.taskCancel = func(ctx context.Context, memberID int64, taskNo int, reason string) error {
		_, err := s.HandleTaskCancel(ctx, memberID, taskNo, reason)
		return err
	}
```

三个 schema 函数:

```go
func groupTaskCreateToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_create",
		"description": "建一张任务卡并派给某成员(建卡即派活,对方会立即收到)。跨成员派活/交接一律用任务卡而不是裸 group_send。brief 写清楚要做什么+验收标准;验证类任务用 parentTaskId 回指被验证的任务编号。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"assignee", "title", "brief"},
			"properties": map[string]any{
				"assignee":     map[string]any{"type": "string", "description": "执行成员的显示名"},
				"title":        map[string]any{"type": "string", "description": "任务短标题"},
				"brief":        map[string]any{"type": "string", "description": "任务说明,含验收标准;交付物路径建议 .agentre/handoff/<群ID>/task-<编号>-<slug>.md"},
				"parentTaskId": map[string]any{"type": "integer", "description": "回指的任务编号(#N,可选,验证类任务用)"},
			},
		},
	}
}

func groupTaskCompleteToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_complete",
		"description": "交付你名下的任务。result 必填:写清改动/产出了什么、自测/验证情况——这是交付物,会投回建卡人。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"taskId", "result"},
			"properties": map[string]any{
				"taskId": map[string]any{"type": "integer", "description": "任务编号(#N)"},
				"result": map[string]any{"type": "string", "description": "交付说明(改动文件、自测结论、产物路径)"},
			},
		},
	}
}

func groupTaskCancelToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_task_cancel",
		"description": "取消一张未完成的任务卡(仅建卡人或主持人)。打回返工不要用取消,新建一张任务卡。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"taskId", "reason"},
			"properties": map[string]any{
				"taskId": map[string]any{"type": "integer", "description": "任务编号(#N)"},
				"reason": map[string]any{"type": "string", "description": "取消原因"},
			},
		},
	}
}
```

tools/list 改为:

```go
		writeRPCResult(w, rpc.ID, map[string]any{"tools": []any{
			groupSendToolSchema(), groupInviteToolSchema(),
			groupTaskCreateToolSchema(), groupTaskCompleteToolSchema(), groupTaskCancelToolSchema(),
		}})
```

tools/call 的 switch 加三个 case(`group_invite` case 之后):

```go
	case "group_task_create":
		if h.taskCreate == nil {
			writeRPCError(w, rpc.ID, -32000, "task create not wired")
			return
		}
		no, err := h.taskCreate(r.Context(), ref.memberID, rpc.Params.Arguments.Assignee,
			rpc.Params.Arguments.Title, rpc.Params.Arguments.Brief, rpc.Params.Arguments.ParentTaskID)
		if err != nil {
			writeRPCError(w, rpc.ID, -32000, err.Error())
			return
		}
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
			"text": fmt.Sprintf("task #%d created", no)}}})
	case "group_task_complete":
		if h.taskComplete == nil {
			writeRPCError(w, rpc.ID, -32000, "task complete not wired")
			return
		}
		if err := h.taskComplete(r.Context(), ref.memberID, rpc.Params.Arguments.TaskID, rpc.Params.Arguments.Result); err != nil {
			writeRPCError(w, rpc.ID, -32000, err.Error())
			return
		}
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
			"text": fmt.Sprintf("task #%d completed", rpc.Params.Arguments.TaskID)}}})
	case "group_task_cancel":
		if h.taskCancel == nil {
			writeRPCError(w, rpc.ID, -32000, "task cancel not wired")
			return
		}
		if err := h.taskCancel(r.Context(), ref.memberID, rpc.Params.Arguments.TaskID, rpc.Params.Arguments.Reason); err != nil {
			writeRPCError(w, rpc.ID, -32000, err.Error())
			return
		}
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text",
			"text": fmt.Sprintf("task #%d canceled", rpc.Params.Arguments.TaskID)}}})
```

- [x] **Step 4: buildGroupMCP 工具放行 + 系统提示强化**

`buildGroupMCP` 的 tools 改为(所有成员可用任务三件套):

```go
	tools := []string{"group_send", "group_task_create", "group_task_complete", "group_task_cancel"}
	if m.IsHost() {
		tools = append(tools, "group_invite")
	}
```

`buildGroupSystemPrompt` 整体替换为(完整新文本;workflow/任务快照在仓储查不到时静默跳过):

```go
// buildGroupSystemPrompt 拼接注入到成员 turn 的群聊 system prompt 后缀
// (角色 + roster + tool 用法 + 任务协作纪律 + SOP + 未完成任务快照 + 交付物约定)。
func (s *groupSvc) buildGroupSystemPrompt(g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	bg := context.Background()
	var b strings.Builder
	role := "成员"
	if me.IsHost() {
		role = "主持人"
	}
	fmt.Fprintf(&b, "\n\n## 群聊「%s」\n你是本群的%s。", g.Title, role)
	b.WriteString("\n当前成员：")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s（%s）", s.names(bg, m.AgentID), m.Role)
	}
	b.WriteString("\n\n你只会收到 @ 到你的消息。要发言请调用 `group_send` 工具：body=正文，mentions=收件成员显示名数组（@用户 = 回复人类）。一个回合可多次调用、可分别对不同人发不同内容。**不调用 group_send 的内容不会进群**。")
	b.WriteString("\n回复路由：消息抬头「(来自 X)」标明来源，回复时默认 mentions 该来源——来源是用户时用 mentions:[\"用户\"]。除非任务确实需要协作，不要主动 @ 其他成员；任务完成直接向来源汇报，不要转发给主持人或其他成员。")

	b.WriteString("\n\n### 任务协作")
	b.WriteString("\n跨成员派活/交接一律用任务卡：`group_task_create`(assignee=成员显示名, title, brief 含验收标准, parentTaskId 可选回指) 建卡即派活；做完自己的任务调用 `group_task_complete`(taskId, result)——result 必须写清改动/产出与自测情况。`group_task_cancel`(taskId, reason) 仅建卡人或主持人可调；打回返工=新建任务卡。")
	b.WriteString("\n过程交付物（PRD 草稿/设计说明/测试报告等不进版本库的交接物）写到 `.agentre/handoff/" + strconv.FormatInt(g.ID, 10) + "/task-<编号>-<slug>.md`（自行 mkdir -p；首次写入前把 `.agentre/` 追加进 `.git/info/exclude`）；正式产物（代码、要进 repo 的文档）照常放仓库正常位置。brief/result 引用文件路径，不要贴全文。")

	if me.IsHost() {
		b.WriteString("\n\n### 主持人编排")
		b.WriteString("\n标准动作环：理解需求 → 拆解 → group_task_create 派活（可能改同一片代码的任务串行派）→ 收到 completed 后派验证任务（测试/审查可并行，parentTaskId 回指被验证任务）→ 全部通过后 group_send @用户 汇总；发现问题 → 新任务卡打回。")
		if g.WorkflowID > 0 {
			if repo := workflow_repo.Workflow(); repo != nil {
				if wf, err := repo.Find(bg, g.WorkflowID); err == nil && wf.IsActive() && strings.TrimSpace(wf.Content) != "" {
					b.WriteString("\n\n### 团队协作流程（按此编排，用户首条消息可临时覆盖）\n" + strings.TrimSpace(wf.Content))
				}
			}
		}
		b.WriteString("\n作为主持人，调用 `group_invite` 工具邀请 Agent 进群（可跨部门）：agentNames 填显示名数组（或 agentIds 填 id），reason 写明跨部门理由。")
		if roster := s.recruitableRoster(bg, g, members); roster != "" {
			b.WriteString("\n可招募：" + roster)
		}
	} else {
		b.WriteString("\n\n收到任务后：在项目目录执行 → 自测 → group_task_complete 交付；需要协作可自行 group_task_create 或 group_send。")
	}

	if snapshot := s.openTaskSnapshot(bg, g, members, me); snapshot != "" {
		b.WriteString("\n\n### 未完成任务\n" + snapshot)
	}

	b.WriteString("\n若你要修改文件且可能与他人并发，请先 `git worktree add` 在自己的工作树里作业。")
	return b.String()
}

// openTaskSnapshot 渲染未完成任务快照:主持人看全群,成员只看与自己相关的(spec §4)。
func (s *groupSvc) openTaskSnapshot(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	repo := group_repo.Task()
	if repo == nil {
		return ""
	}
	tasks, err := repo.ListByGroup(ctx, g.ID)
	if err != nil {
		return ""
	}
	nameOf := func(memberID int64) string {
		for _, m := range members {
			if m.ID == memberID {
				return s.names(ctx, m.AgentID)
			}
		}
		return "?"
	}
	var lines []string
	for _, t := range tasks {
		if !t.IsOpen() {
			continue
		}
		if !me.IsHost() && t.AssigneeMemberID != me.ID && t.CreatorMemberID != me.ID {
			continue
		}
		line := fmt.Sprintf("#%d %s → %s", t.TaskNo, t.Title, nameOf(t.AssigneeMemberID))
		if t.ParentTaskNo > 0 {
			line += fmt.Sprintf("（验证 #%d）", t.ParentTaskNo)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
```

> import 需补 `strconv` 与 `workflow_repo`。`openTaskSnapshot` 与 workflow 注入对仓储做 nil 防御——`NewForTest` 体系下未注册的 repo 是 nil,这是测试构造的既有约定,不是掩盖生产 bug(生产在 bootstrap 全量装配)。

- [x] **Step 5: 跑测试 + 修既有红灯**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -v 2>&1 | tail -30`
Expected: 新测试 PASS;若既有 launch 路径测试因 task 仓储未注册而红,按 Task 8 Step 4 的模式补注册。

- [x] **Step 6: Commit**

```bash
git add internal/service/group_svc/
git commit -m "✨ group: 任务三 MCP tool(schema+路由)+ 提示强化(动作环/SOP 注入/任务快照/.agentre 约定)"
```

---

### Task 12: group_invite 招募池放宽(部门池 → 全部可用 agent)

**Files:**
- Modify: `internal/service/group_svc/group.go:226`(HandleInvite 的 pool)与 `group.go:563-588`(recruitableRoster)
- Modify: `internal/service/group_svc/mcp.go`(group_invite schema 描述)
- Modify: `internal/service/group_svc/group_test.go:434-478`(TestGroupSvc_HandleInvite)

- [x] **Step 1: 先改测试锁定新行为**

`TestGroupSvc_HandleInvite` 中:

```go
		// 旧: agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(42)).Return(...)
		// 新: 招募池=全部 active agent(spec §7 修订 06-03 部门池决策)。
		agentRepo.EXPECT().List(gomock.Any()).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Bob", Status: consts.ACTIVE}}, nil)
```

再补一个跨部门用例(同一测试函数内新 Convey 分支):被邀请 agent 的 `DepartmentID` 与群不同 → 仍可入群。

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run TestGroupSvc_HandleInvite -v`
Expected: FAIL(实现仍调 ListByDepartment,mock 未满足)

- [x] **Step 3: 改实现**

`HandleInvite` 中:

```go
	// 招募池=全部 active 且后端支持群聊的 agent(部门仅组织单位,跨部门招人直接成立,spec §7)。
	pool, err := agent_repo.Agent().List(ctx)
```

`recruitableRoster` 中删除 `if g.DepartmentID == 0 { return "" }` 早退,并:

```go
	pool, err := agent_repo.Agent().List(ctx)
```

`groupInviteToolSchema` 描述改为:

```go
		"description": "把可用的 Agent 拉进当前群聊(可跨部门;优先同部门,跨部门请在 reason 说明理由)。只有主持人可调用。agentNames 或 agentIds 二选一。",
```

- [x] **Step 4: 跑全量回归**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ 2>&1 | tail -10`
Expected: PASS(其它引用 ListByDepartment 的测试如有红,同样换 List mock)

- [x] **Step 5: Commit**

```bash
git add internal/service/group_svc/
git commit -m "♻️ group: group_invite 招募池放宽到全部可用 agent(跨部门协作,修订 06-03 部门池决策)"
```

---

### Task 13: CreateGroup.WorkflowID + LoadGroup.Tasks + app 绑定

**Files:**
- Modify: `internal/service/group_svc/types.go:7-25`
- Modify: `internal/service/group_svc/group.go`(CreateGroup / LoadGroup)
- Modify: `internal/app/group.go:49-55, 72-84, 98-104`
- Modify: `internal/service/group_svc/group_test.go`

- [x] **Step 1: 写失败的测试**(group_test.go 内现有 CreateGroup/LoadGroup 测试旁追加)

```go
func TestCreateGroup_PersistsWorkflowID(t *testing.T) {
	Convey("建群带 WorkflowID → 落到 groups.workflow_id", t, func() {
		// 照抄现有 TestCreateGroup 的 mock 搭建,断言 groupRepo.Create 收到的
		// g.WorkflowID == 3;请求加 WorkflowID: 3。
	})
}

func TestLoadGroup_IncludesTasks(t *testing.T) {
	Convey("LoadGroup 返回 Tasks", t, func() {
		// 照抄现有 LoadGroup 测试搭建;taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
		//   Return([]*group_entity.GroupTask{{ID: 77, TaskNo: 1}}, nil)
		// So(detail.Tasks, ShouldHaveLength, 1)
	})
}
```

(同 Task 11 备注:Step 1 实写为完整可编译测试,搭建样板照抄现有同名测试。)

- [x] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run 'TestCreateGroup_PersistsWorkflowID|TestLoadGroup_IncludesTasks' -v`
Expected: FAIL(字段不存在)

- [x] **Step 3: 改实现**

`types.go`:

```go
type CreateGroupRequest struct {
	Title        string
	HostAgentID  int64
	DepartmentID int64
	ProjectID    int64
	// WorkflowID 可选绑定的协作流程(剧本库),0=不绑定;主持人每轮注入最新内容。
	WorkflowID int64
	MemberAgentIDs []int64
}
```

`GroupDetail` 追加:

```go
	// Tasks 全部任务卡(任务 tab 与历史卡片状态回写的数据源)。
	Tasks []*group_entity.GroupTask
```

`CreateGroup` 构造 `g` 时加 `WorkflowID: req.WorkflowID,`。

`LoadGroup` 在 msgs 之后:

```go
	var tasks []*group_entity.GroupTask
	if repo := group_repo.Task(); repo != nil {
		if rows, err := repo.ListByGroup(ctx, id); err == nil {
			tasks = rows
		}
	}
```

并在返回的 `GroupDetail` 加 `Tasks: tasks,`。

`internal/app/group.go`:

```go
type GroupCreateRequest struct {
	Title          string  `json:"title"`
	HostAgentID    int64   `json:"hostAgentID"`
	DepartmentID   int64   `json:"departmentID"`
	ProjectID      int64   `json:"projectID"`
	WorkflowID     int64   `json:"workflowID"`
	MemberAgentIDs []int64 `json:"memberAgentIDs"`
}
```

`GroupCreate` 透传 `WorkflowID: req.WorkflowID`。新增条目类型与映射:

```go
// GroupTaskItem 任务卡条目;json 形状与 group_svc.GroupTaskEvent 一致(live 事件与 Load 同构)。
type GroupTaskItem struct {
	ID               int64  `json:"id"`
	TaskNo           int    `json:"taskNo"`
	Title            string `json:"title"`
	Brief            string `json:"brief"`
	CreatorMemberID  int64  `json:"creatorMemberID"`
	AssigneeMemberID int64  `json:"assigneeMemberID"`
	Status           string `json:"status"`
	Result           string `json:"result"`
	ParentTaskNo     int    `json:"parentTaskNo"`
	Createtime       int64  `json:"createtime"`
	Updatetime       int64  `json:"updatetime"`
}
```

`GroupDetailResponse` 加 `Tasks []*GroupTaskItem \`json:"tasks"\``;`toGroupDetail` 加循环:

```go
	tasks := make([]*GroupTaskItem, 0, len(d.Tasks))
	for _, t := range d.Tasks {
		tasks = append(tasks, &GroupTaskItem{ID: t.ID, TaskNo: t.TaskNo, Title: t.Title, Brief: t.Brief,
			CreatorMemberID: t.CreatorMemberID, AssigneeMemberID: t.AssigneeMemberID,
			Status: t.Status, Result: t.Result, ParentTaskNo: t.ParentTaskNo,
			Createtime: t.Createtime, Updatetime: t.Updatetime})
	}
```

并在返回值加 `Tasks: tasks`。GroupMessageItem 同步加 `TaskID int64 \`json:"taskID"\`` 与 `TaskEvent string \`json:"taskEvent"\``,`toGroupDetail` 的消息映射补两字段。

- [x] **Step 4: 跑测试确认通过 + 编译 app 层**

Run: `GOWORK=off go test -race ./internal/service/group_svc/ -run 'TestCreateGroup|TestLoadGroup' -v && GOWORK=off go build ./internal/app/`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/service/group_svc/ internal/app/group.go
git commit -m "✨ group: 建群可绑定 workflow + LoadGroup 返回任务卡 + app 绑定透传"
```

---

### Task 14: 收尾(全量回归 + lint + spec 回写)

**Files:**
- Modify: `docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md`(两处偏差回写)

- [x] **Step 1: 全量后端回归 + lint**

Run: `make mock && make test-backend && make lint`
Expected: 全绿。lint 红了就修(常见:未用 import、注释格式)。

- [x] **Step 2: spec 回写两处实现偏差**

§3.1 的 `parent_task_id` 改为 `parent_task_no`(注明存群内编号);充血实体包名 `group_task_entity` 改为 `group_entity`。§4 工具参数表补 `taskId=任务编号(#N)` 的语义说明。

- [x] **Step 3: Commit**

```bash
git add docs/superpowers/specs/2026-06-11-group-task-orchestration-design.md
git commit -m "📝 spec: 回写 PR1 实现偏差(parent_task_no/实体包名/taskId 语义)"
```

---

## 验收清单(对照 spec)

- [x] §3.1/3.2/3.3 schema 全落(Task 1)
- [x] §3.1 实体语义:open→done/canceled、CanComplete/CanCancel(Task 2)
- [x] §4 三个 MCP tool + 鉴权 + 错误码(Task 2/8/9/10/11)
- [x] §4 任务快照注入(不做 list tool)(Task 11)
- [x] §5 created/completed/canceled 消息进既有管线、task_no 锁内分配、计轮(Task 7/8/9/10)
- [x] §5 成员离群级联取消(Task 10)
- [x] §6/6.1/6.2 提示强化:动作环/成员纪律/SOP 每轮注入/.agentre/handoff(Task 11)
- [x] §7 invite 池放宽 + schema 文案(Task 12)
- [x] 建群绑定 workflow_id、LoadGroup 带 Tasks、事件 task_updated(Task 8/13)
- [x] §12 不变量:软门不挡路(任务校验只挡 task 工具自身,不阻塞 group_send/调度)——代码审查确认,无需额外代码

**PR1 不含**(后续 PR):前端任务卡气泡/任务 tab(PR2)、流程库管理 UI 与建群下拉前端(PR3)、e2e seam(PR4)、`group_create`(PR5)。
