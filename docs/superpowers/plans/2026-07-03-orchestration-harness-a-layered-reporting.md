# 编排 Harness · 切片 A — 完成/报错回报分层 + 按需读取 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 子任务完成/报错默认只给 Leader 注入**轻量 XML 通知**;显式 `finish`/`report` 才内联小结;全文由 Leader 用 `read(task_id)` 按需拉取。

**Architecture:** 拆分 `orch_tasks` 的 `Result`(完整正文,始终落库供 `read`)与 `Summary`(显式小结,仅 finish/report 写)。`watchCompletion`/`markTaskError` 经统一 XML 信封注入父会话:有 `Summary`→`<task_report>` 内联、否则→`<task_done>` ping、崩溃→`<task_error>` ping。新增 `report`(中途主动小结)与 `read`(settled 拉全文)两个 MCP 工具,并更新编排 guidance。

**Tech Stack:** Go 1.26、cago(db.Ctx/gormigrate)、gorm、MCP-over-HTTP(orchMCP)、goconvey + go.uber.org/mock、go-sqlmock、`html.EscapeString`(信封转义,复用 ask.go 惯例)。

## Global Constraints

- **严格 TDD:先失败测试 → 跑挂 → 最小实现 → 跑绿 → 提交。** service 单测走 mock 不连库;repo/迁移走 sqlmock/bootstrap。
- **测试跑 `GOWORK=off go test ...`**(workspace 根 go.work 会扫 frontend 包)。
- **迁移只追加到 `migrationList()` 末尾**,native SQL DDL。最新既有迁移 = `202607030001`,本片用 `202607030002`(**实现时核对当日最新 ID 再排号**,develop/wyz 并发风险)。
- **触发规则(spec §A.2):** `child.Summary != ""`(agent 主动 finish/report)→ 内联 `<task_report final="true">`;完成但无小结 → `<task_done>` ping(首行摘要 + read 提示);技术崩溃 → `<task_error>` ping。三者都仍走 `SendAndForget` 续 Leader 轮。
- **`read` 本片只做 settled 分支**(返回已终态任务的 `Summary`+`Result`,来自 DB);running/peek 分支属切片 B,不在本片。
- **XML 信封内容/属性一律 `html.EscapeString` 转义**;`task_id`/`agent`/`call_seq`/`final` 为数字/布尔,无需转义。
- **提交用 pathspec**(共享分支 develop/wyz;`git commit <files>`,禁 `git add -A`/裸 commit);gitmoji。
- **不改前端**(本片纯后端 + agent 面向 XML/guidance,均为动态内容不入 i18n)。

---

## File Structure

- `internal/model/entity/orch_entity/task.go` — 加 `Summary` 字段。
- `migrations/202607030002_orch_task_summary.go`(新建) + `migrations/migrations.go` — summary 列迁移 + 注册。
- `internal/service/orch_svc/envelope.go`(新建) + `envelope_test.go`(新建) — XML 信封纯函数 + `firstLine`。
- `internal/service/orch_svc/finish.go` — 写 `Summary`(而非 `Result`)。
- `internal/service/orch_svc/complete.go` — watcher 分流 + `injectToParent` 重构 + `markTaskError` 走 task_error。
- `internal/service/orch_svc/complete_test.go` / `finish_test.go` — 改写受影响用例。
- `internal/service/orch_svc/report.go`(新建) + `report_test.go`(新建) — `Report` 服务方法(中途主动小结)。
- `internal/service/orch_svc/read.go`(新建) + `read_test.go`(新建) — `ReadTask`(settled 拉全文)。
- `internal/service/orch_svc/mcp.go` — dispatchTool 加 `report`/`read` case + handler + `orchToolSchemas` 两条 schema。
- `internal/service/orch_svc/turn.go` / `turn_test.go` — `orchGuidance` 增补新模型说明。

---

## Task 1: orch_tasks.summary 字段 + 迁移

**Files:**
- Modify: `internal/model/entity/orch_entity/task.go`
- Create: `migrations/202607030002_orch_task_summary.go`
- Modify: `migrations/migrations.go`

**Interfaces:**
- Produces: `Task.Summary string`(gorm 列 `summary`);DB 列 `orch_tasks.summary TEXT NOT NULL DEFAULT ''`。

> 排号确认:`ls migrations/` 看当日最新 ID;若已有 `202607030002` 则顺延,后续引用同步更新。

- [ ] **Step 1: 加实体字段**

`task.go` 的 `Task` 结构体,在 `Result` 行后加:

```go
	Result       string `gorm:"column:result;type:text;not null;default:''"` // agent 自报语义报告(完整正文,供 read)
	Summary      string `gorm:"column:summary;type:text;not null;default:''"` // 显式小结(仅 finish/report 写;非空=主动汇报)
```

- [ ] **Step 2: 新建迁移**

`migrations/202607030002_orch_task_summary.go`:

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607030002 回报分层:orch_tasks.summary(显式小结,空=无主动汇报)。
func migration202607030002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607030002",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orch_tasks ADD COLUMN summary TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orch_tasks DROP COLUMN summary`).Error
		},
	}
}
```

- [ ] **Step 3: 注册**

`migrations/migrations.go` `migrationList()` 里 `migration202607030001(),` **之后**追加:

```go
		migration202607030002(), // 回报分层:orch_tasks.summary
```

- [ ] **Step 4: 构建 + 迁移验证**

Run: `GOWORK=off go build ./... && GOWORK=off go test ./internal/bootstrap/ -run Cago -v`
Expected: 构建通过;bootstrap 迁移链跑通(新列无 dup)。若无 bootstrap 命中,退 `make test-backend 2>&1 | tail -5` 看 exit 0。

- [ ] **Step 5: 提交**

```bash
git commit internal/model/entity/orch_entity/task.go migrations/202607030002_orch_task_summary.go migrations/migrations.go \
  -m "✨ orchestration: orch_tasks 加 summary 列(回报分层:显式小结与完整正文分离)"
```

---

## Task 2: XML 信封纯函数

**Files:**
- Create: `internal/service/orch_svc/envelope.go`
- Create: `internal/service/orch_svc/envelope_test.go`

**Interfaces:**
- Produces:
  - `taskDoneMsg(taskID, agentID int64, callSeq int, excerpt string) string`
  - `taskReportMsg(taskID, agentID int64, callSeq int, summary string, final bool) string`
  - `taskErrorMsg(taskID, agentID int64, reason string) string`
  - `firstLine(s string, maxRunes int) string`

- [ ] **Step 1: 写失败测试**

`envelope_test.go`(`package orch_svc`,包内测试,直接调私有函数):

```go
package orch_svc

import (
	"strings"
	"testing"
)

func TestTaskDoneMsg_ShapeAndEscape(t *testing.T) {
	got := taskDoneMsg(11, 3, 2, `实现完成 <ok> & "done"`)
	if !strings.HasPrefix(got, `<task_done task_id="11" agent="3" call_seq="2">`) {
		t.Fatalf("bad prefix: %s", got)
	}
	if !strings.Contains(got, `read(task_id=11)`) {
		t.Fatalf("missing read hint: %s", got)
	}
	if strings.Contains(got, "<ok>") || !strings.Contains(got, "&lt;ok&gt;") {
		t.Fatalf("excerpt not escaped: %s", got)
	}
	if !strings.HasSuffix(got, `</task_done>`) {
		t.Fatalf("bad suffix: %s", got)
	}
}

func TestTaskReportMsg_FinalFlagAndEscape(t *testing.T) {
	fin := taskReportMsg(11, 3, 2, "已完成", true)
	if !strings.Contains(fin, `final="true"`) || !strings.Contains(fin, "已完成") {
		t.Fatalf("bad final report: %s", fin)
	}
	interim := taskReportMsg(11, 3, 2, `中途 <x>`, false)
	if !strings.Contains(interim, `final="false"`) || !strings.Contains(interim, "&lt;x&gt;") {
		t.Fatalf("bad interim report: %s", interim)
	}
}

func TestTaskErrorMsg_ReasonEscaped(t *testing.T) {
	got := taskErrorMsg(12, 4, `崩溃 <boom>`)
	if !strings.Contains(got, `task_id="12" agent="4"`) || !strings.Contains(got, `reason="崩溃 &lt;boom&gt;"`) {
		t.Fatalf("bad error msg: %s", got)
	}
	if !strings.Contains(got, `read(task_id=12)`) {
		t.Fatalf("missing read hint: %s", got)
	}
}

func TestFirstLine_TruncatesAndStripsNewline(t *testing.T) {
	if got := firstLine("  第一行\n第二行  ", 100); got != "第一行" {
		t.Fatalf("newline strip: %q", got)
	}
	if got := firstLine("abcdef", 3); got != "abc…" {
		t.Fatalf("truncate: %q", got)
	}
	if got := firstLine("abc", 3); got != "abc" {
		t.Fatalf("exact: %q", got)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run 'TestTaskDoneMsg|TestTaskReportMsg|TestTaskErrorMsg|TestFirstLine' -v`
Expected: 编译失败(函数未定义)。

- [ ] **Step 3: 实现信封**

`envelope.go`:

```go
package orch_svc

import (
	"fmt"
	"html"
	"strings"
)

// taskDoneMsg 子任务完成、无显式小结 → 轻量通知(首行摘要 + read 提示)。
func taskDoneMsg(taskID, agentID int64, callSeq int, excerpt string) string {
	return fmt.Sprintf(
		`<task_done task_id="%d" agent="%d" call_seq="%d">%s(read(task_id=%d) 看全文)</task_done>`,
		taskID, agentID, callSeq, html.EscapeString(excerpt), taskID,
	)
}

// taskReportMsg 子任务主动小结(finish→final:true / report→final:false)→ 内联回报。
func taskReportMsg(taskID, agentID int64, callSeq int, summary string, final bool) string {
	return fmt.Sprintf(
		`<task_report task_id="%d" agent="%d" call_seq="%d" final="%t">%s</task_report>`,
		taskID, agentID, callSeq, final, html.EscapeString(summary),
	)
}

// taskErrorMsg 子任务技术崩溃 → 轻量通知(read 看详情)。
func taskErrorMsg(taskID, agentID int64, reason string) string {
	return fmt.Sprintf(
		`<task_error task_id="%d" agent="%d" reason="%s">(read(task_id=%d) 看详情;决定重试/换 agent/放弃该分支)</task_error>`,
		taskID, agentID, html.EscapeString(reason), taskID,
	)
}

// firstLine 取首行并按 rune 截断到 maxRunes(超出补 …),用作 task_done 摘要。
func firstLine(s string, maxRunes int) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes]) + "…"
	}
	return s
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run 'TestTaskDoneMsg|TestTaskReportMsg|TestTaskErrorMsg|TestFirstLine' -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/envelope.go internal/service/orch_svc/envelope_test.go \
  -m "✨ orchestration: 回报 XML 信封纯函数(task_done/task_report/task_error + firstLine)"
```

---

## Task 3: 回报分层(finish→Summary + watcher 分流)

**Files:**
- Modify: `internal/service/orch_svc/finish.go`
- Modify: `internal/service/orch_svc/complete.go`
- Modify: `internal/service/orch_svc/complete_test.go`
- Modify: `internal/service/orch_svc/finish_test.go`

**Interfaces:**
- Consumes: `taskDoneMsg`/`taskReportMsg`/`taskErrorMsg`/`firstLine`(Task 2)、`Task.Summary`(Task 1)。
- Produces: `reportToParent` 改签名为 `(ctx, parentTaskID int64, child *orch_entity.Task)`;新私有 `injectToParent(ctx, parentTaskID int64, msg string)`。

> 核心行为变更(spec §A.2):`finish` 写 `Summary`;watcher idle 始终把 `FinalAssistantText` 落 `Result`(供 read),再按 `Summary` 是否非空分流 inline/ping;error 走 `task_error` ping。producer(finish)+consumer(watcher)必须同任务落地,否则中间态回报错乱。

- [ ] **Step 1: 改写失败测试(先改测试断言到新行为)**

在 `finish_test.go`:
- `TestFinish_RootCollapsesRun`:把 `So(tk.Result, ShouldEqual, "全部完成,已交付")` 改为 `So(tk.Summary, ShouldEqual, "全部完成,已交付")`。
- `TestFinish_NonRootRecordsResultNoReport`:把 `capturedResult = tk.Result` 改为 `capturedSummary = tk.Summary`(声明改名),断言 `So(capturedSummary, ShouldEqual, "子任务完成小结")`。

在 `complete_test.go`:
- `TestWatchCompletion_ReportsToParentAndMarksDone`(无 finish 小结路径):`tasks.Find(11)` 返回的 fresh 任务 `Result: "", Summary: ""`;断言注入的是 `<task_done` ping 且含首行摘要 —— 把 `So(capturedSendMsg, ShouldContainSubstring, "登录表单已实现")` 保留(首行摘要含它),并**新增** `So(capturedSendMsg, ShouldContainSubstring, "<task_done")` 与 `So(capturedSendMsg, ShouldContainSubstring, "read(task_id=11)")`。
- `TestWatchCompletion_PrefersFinishSummary` → 重命名语义为「有 Summary 时内联 task_report」:把 `tasks.Find(13)` 返回的 fresh 改为 `Summary: finishSummary`(不再用 `Result: finishSummary`);断言 `So(capturedSendMsg, ShouldContainSubstring, "<task_report")`、`So(capturedSendMsg, ShouldContainSubstring, "final=\"true\"")`、`So(capturedSendMsg, ShouldContainSubstring, finishSummary)`。`capturedResult` 改断言 `Result` 为 FinalAssistantText(`"末条 assistant 正文(不应被采用)"`)—— 即新模型下 Result 始终落末条正文、Summary 才是回报正文;把该测试里 `ShouldNotContainSubstring, "不应被采用"` 改为断言 `capturedResult ShouldContainSubstring "末条 assistant 正文"`(正文落 Result 供 read),回报体断言改为含 `finishSummary`。
- `TestWatchCompletion_TechnicalErrorEscalates`:把 `So(capturedSendMsg, ShouldContainSubstring, "技术中断")` 改为 `So(capturedSendMsg, ShouldContainSubstring, "<task_error")` + `So(capturedSendMsg, ShouldContainSubstring, "运行时崩溃")`(reason)。
- `TestWatchCompletion_ParentFlipEmitsRunUpdated`:`tasks.Find(11)` fresh 保持 `Result:"", Summary:""`;该测试只断言 emit 次数,回报体不变,无需改断言(SendAndForget 仍 `gomock.Any()`)。

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run 'TestFinish|TestWatchCompletion' -v`
Expected: 上述改写用例 FAIL(现实现写 Result 而非 Summary、注入的是纯文本而非 XML)。

- [ ] **Step 3: 实现 finish 写 Summary**

`finish.go`:把 `tk.Result = summary`(:19)改为:

```go
	tk.Status = orch_entity.TaskDone
	tk.Summary = summary
```

并把 :41 附近注释「watcher 的 idle 分支会优先读到该 Result 作为回报正文」改为「watcher 的 idle 分支据 Summary 决定内联小结」。

- [ ] **Step 4: 实现 watcher 分流 + injectToParent**

`complete.go`:

idle 分支(:31-45)改为:

```go
		case "idle":
			// 完整正文始终落 Result(供 read);Summary 由显式 finish 写,决定内联 vs ping。
			result, _ := s.chat.FinalAssistantText(ctx, task.SessionID)
			task.Status = orch_entity.TaskDone
			task.Result = result
			if fresh, _ := s.tasks.Find(ctx, task.ID); fresh != nil && fresh.Summary != "" {
				task.Summary = fresh.Summary
			}
			if err := s.tasks.Update(ctx, task); err != nil {
				logger.Ctx(ctx).Error("orch.watchCompletion: 写子任务终态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
			}
			s.emitRunUpdated(ctx, task.RunID)
			s.reportToParent(ctx, task.ParentTaskID, task)
			return
```

把 `reportToParent`(:57-79)重构为按 Summary 分流 + 抽出 `injectToParent`:

```go
// reportToParent 子任务完成回报:有显式小结 → 内联 task_report;否则 → task_done 轻量通知。
func (s *orchSvc) reportToParent(ctx context.Context, parentTaskID int64, child *orch_entity.Task) {
	var msg string
	if child.Summary != "" {
		msg = taskReportMsg(child.ID, child.AgentID, child.CallSeq, child.Summary, true)
	} else {
		msg = taskDoneMsg(child.ID, child.AgentID, child.CallSeq, firstLine(child.Result, 120))
	}
	s.injectToParent(ctx, parentTaskID, msg)
}

// injectToParent 把一条消息注入父会话续轮,并在无未决子任务时把父翻回 running。
func (s *orchSvc) injectToParent(ctx context.Context, parentTaskID int64, msg string) {
	if parentTaskID == 0 {
		return // 根任务无父:根的收口只认 Leader 显式 finish
	}
	parent, err := s.tasks.Find(ctx, parentTaskID)
	if err != nil || parent == nil {
		return
	}
	if s.allChildrenSettled(ctx, parent) && parent.Status == orch_entity.TaskAwaitingChildren {
		parent.Status = orch_entity.TaskRunning
		if err := s.tasks.Update(ctx, parent); err != nil {
			logger.Ctx(ctx).Error("orch.injectToParent: 父任务翻回 running 失败(可被对账纠正)", zap.Int64("task", parent.ID), zap.Error(err))
		}
		s.emitRunUpdated(ctx, parent.RunID)
	}
	if err := s.chat.SendAndForget(ctx, parent.SessionID, msg); err != nil {
		logger.Ctx(ctx).Error("orch.injectToParent: 续轮注入失败", zap.Int64("parent", parent.SessionID), zap.Error(err))
	}
}
```

把 `markTaskError`(:82-89)改为走 task_error 信封:

```go
// markTaskError 技术崩溃:标 error,把崩溃当 task_error 轻量通知上抛父会话(与 done 同一续轮路)。
func (s *orchSvc) markTaskError(ctx context.Context, task *orch_entity.Task, reason string) {
	task.Status = orch_entity.TaskError
	task.Result = reason
	if err := s.tasks.Update(ctx, task); err != nil {
		logger.Ctx(ctx).Error("orch.markTaskError: 写子任务 error 态失败(可被对账纠正)", zap.Int64("task", task.ID), zap.String("status", task.Status), zap.Error(err))
	}
	s.injectToParent(ctx, task.ParentTaskID, taskErrorMsg(task.ID, task.AgentID, reason))
}
```

- [ ] **Step 5: 跑测试确认通过(含全 orch_svc 包)**

Run: `GOWORK=off go test ./internal/service/orch_svc/ 2>&1 | tail -6`
Expected: 全包 PASS(改写的 Finish/WatchCompletion 用例 + 其余均绿)。

- [ ] **Step 6: 提交**

```bash
git commit internal/service/orch_svc/finish.go internal/service/orch_svc/complete.go internal/service/orch_svc/complete_test.go internal/service/orch_svc/finish_test.go \
  -m "✨ orchestration: 完成/报错回报分层(finish 写 Summary + watcher 按 Summary 分流 XML 信封)"
```

---

## Task 4: report 工具(中途主动小结)

**Files:**
- Create: `internal/service/orch_svc/report.go`
- Create: `internal/service/orch_svc/report_test.go`
- Modify: `internal/service/orch_svc/mcp.go`

**Interfaces:**
- Consumes: `taskReportMsg`(Task 2)、`injectToParent`(Task 3)。
- Produces: `(*orchSvc).Report(ctx context.Context, sessionID int64, note string) error`;MCP 工具 `report{note}`。

- [ ] **Step 1: 写失败测试**

`report_test.go`(`package orch_svc_test`):

```go
package orch_svc_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestReport_InjectsInterimReportToParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	// 调用者子任务(有父 9)。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentTaskID: 9, CallSeq: 2, Status: orch_entity.TaskRunning}, nil)
	// injectToParent 取父 + 判定 settled(此处父仍 running,不翻转)。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 11, ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning},
	}, nil).AnyTimes()

	var msg string
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error {
		msg = m
		return nil
	})

	Convey("report 中途向父注入 task_report(final=false),不改状态", t, func() {
		err := orch_svc.Default().Report(context.Background(), 600, "进度:表单已搭好,正在接接口")
		So(err, ShouldBeNil)
		So(strings.Contains(msg, `<task_report`), ShouldBeTrue)
		So(strings.Contains(msg, `final="false"`), ShouldBeTrue)
		So(strings.Contains(msg, "正在接接口"), ShouldBeTrue)
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestReport_InjectsInterimReportToParent -v`
Expected: 编译失败(`Report` 未定义)。

- [ ] **Step 3: 实现 Report + MCP 接线**

`report.go`:

```go
package orch_svc

import "context"

// Report 子任务运行中主动向父推一条中途小结(final=false),不改状态、不收口。
// 不写 Task.Summary(Summary 只留给终态 finish,避免污染完成分流)。
func (s *orchSvc) Report(ctx context.Context, sessionID int64, note string) error {
	tk, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	if tk == nil {
		return errRunNotActive
	}
	s.injectToParent(ctx, tk.ParentTaskID, taskReportMsg(tk.ID, tk.AgentID, tk.CallSeq, note, false))
	return nil
}
```

`mcp.go` dispatchTool(`finish` case 之后)加:

```go
	case "report":
		m.handleReport(w, r, id, ref, args)
```

在 `handleFinish` 之后加 handler:

```go
func (m *orchMCP) handleReport(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
	var p struct {
		Note string `json:"note"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		writeRPCError(w, id, -32700, "parse error: "+err.Error())
		return
	}
	if p.Note == "" {
		writeRPCError(w, id, -32602, "note is required")
		return
	}
	if err := m.svc.Report(r.Context(), ref.sessionID, p.Note); err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult("已汇报"))
}
```

`orchToolSchemas()` 数组里(`finish` 之后)加:

```go
		map[string]any{
			"name":        "report",
			"description": "运行中向派发你的上级主动汇报一条中途进展/中间结论(不收口、不改状态)。默认完成时上级只收到通知,主动 report/finish 才把内容内联给它。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"note"},
				"properties": map[string]any{
					"note": map[string]any{"type": "string"},
				},
			},
		},
```

- [ ] **Step 4: 跑测试确认通过 + 构建**

Run: `GOWORK=off go build ./internal/service/orch_svc/ && GOWORK=off go test ./internal/service/orch_svc/ -run TestReport_InjectsInterimReportToParent -v`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/report.go internal/service/orch_svc/report_test.go internal/service/orch_svc/mcp.go \
  -m "✨ orchestration: report 工具(子任务中途主动小结 task_report final=false)"
```

---

## Task 5: read 工具(settled 拉全文)

**Files:**
- Create: `internal/service/orch_svc/read.go`
- Create: `internal/service/orch_svc/read_test.go`
- Modify: `internal/service/orch_svc/mcp.go`

**Interfaces:**
- Consumes: `TaskRepo.FindBySession`/`Find`(定位调用者 Run + 目标任务)。
- Produces: `(*orchSvc).ReadTask(ctx context.Context, sessionID, taskID int64) (string, error)`;MCP 工具 `read{task_id}`。

> 本片只做 settled 分支:返回目标任务的 `Summary`(有则)+ `Result`(完整正文)。目标任务须与调用者同 Run(否则 `errForeignTask`)。running/peek 属切片 B。

- [ ] **Step 1: 写失败测试**

`read_test.go`(`package orch_svc_test`):

```go
package orch_svc_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestReadTask_ReturnsSettledResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, Status: orch_entity.TaskDone, Summary: "小结", Result: "完整正文见 src/x.go"}, nil)

	Convey("read 返回 settled 任务的 Summary + 完整 Result", t, func() {
		out, err := orch_svc.Default().ReadTask(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(strings.Contains(out, "完整正文见 src/x.go"), ShouldBeTrue)
		So(strings.Contains(out, "小结"), ShouldBeTrue)
	})
}

func TestReadTask_RejectsForeignRunTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(77)).Return(
		&orch_entity.Task{ID: 77, RunID: 999, Status: orch_entity.TaskDone}, nil) // 别的 Run

	Convey("read 跨 Run 任务 → 拒绝", t, func() {
		_, err := orch_svc.Default().ReadTask(context.Background(), 500, 77)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in this run")
	})
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestReadTask -v`
Expected: 编译失败(`ReadTask` 未定义)。

- [ ] **Step 3: 实现 ReadTask + 错误 + MCP 接线**

`orch.go` 错误块加:

```go
	errForeignTask = errors.New("orch: task not in this run")
```

`read.go`:

```go
package orch_svc

import (
	"context"
	"fmt"
	"strings"
)

// ReadTask 拉取本 Run 内某任务的最终输出(settled 分支):Summary(有则)+ 完整 Result。
// 目标任务须与调用者同 Run。running/peek 属切片 B。
func (s *orchSvc) ReadTask(ctx context.Context, sessionID, taskID int64) (string, error) {
	caller, err := s.tasks.FindBySession(ctx, sessionID)
	if err != nil {
		return "", err
	}
	if caller == nil {
		return "", errRunNotActive
	}
	tk, err := s.tasks.Find(ctx, taskID)
	if err != nil {
		return "", err
	}
	if tk == nil || tk.RunID != caller.RunID {
		return "", errForeignTask
	}
	var b strings.Builder
	fmt.Fprintf(&b, "task #%d · agent#%d · %s", tk.ID, tk.AgentID, tk.Status)
	if tk.Summary != "" {
		fmt.Fprintf(&b, "\n【小结】%s", tk.Summary)
	}
	if tk.Result != "" {
		fmt.Fprintf(&b, "\n【输出】%s", tk.Result)
	}
	return b.String(), nil
}
```

`mcp.go` dispatchTool 加(`report` case 之后):

```go
	case "read":
		m.handleRead(w, r, id, ref, args)
```

handler(`handleReport` 之后):

```go
func (m *orchMCP) handleRead(w http.ResponseWriter, r *http.Request, id json.RawMessage, ref orchRef, args json.RawMessage) {
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
	out, err := m.svc.ReadTask(r.Context(), ref.sessionID, p.TaskID)
	if err != nil {
		writeRPCError(w, id, -32000, err.Error())
		return
	}
	writeRPCResult(w, id, textResult(out))
}
```

`orchToolSchemas()` 加(`report` 之后):

```go
		map[string]any{
			"name":        "read",
			"description": "读取你派发/同 Run 内某任务的输出(默认完成只发通知,用它按需拉全文)。传通知里给出的 task_id。",
			"inputSchema": map[string]any{
				"type":     "object",
				"required": []string{"task_id"},
				"properties": map[string]any{
					"task_id": map[string]any{"type": "integer"},
				},
			},
		},
```

- [ ] **Step 4: 跑测试确认通过 + 构建**

Run: `GOWORK=off go build ./internal/service/orch_svc/ && GOWORK=off go test ./internal/service/orch_svc/ -run TestReadTask -v`
Expected: 两用例 PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/read.go internal/service/orch_svc/read_test.go internal/service/orch_svc/orch.go internal/service/orch_svc/mcp.go \
  -m "✨ orchestration: read 工具(按需拉 settled 任务全文,跨 Run 拒绝)"
```

---

## Task 6: guidance 更新(教会 agent 新模型)

**Files:**
- Modify: `internal/service/orch_svc/turn.go`
- Modify: `internal/service/orch_svc/turn_test.go`

**Interfaces:**
- Consumes: —(仅改 `orchGuidance` 常量文本 + BuildTurnExtras 已有注入)。

- [ ] **Step 1: 写失败测试**

`turn_test.go` 追加(若已有 guidance 断言测试则在其内补断言;否则新增):

```go
func TestBuildTurnExtras_GuidanceMentionsReadAndReport(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, agents, nil, tasks, nil, nil)

	a := &agent_entity.Agent{ID: 3, ToolsJSON: `[{"key":"orchestrate","enabled":true}]`}
	// 非根任务(避免触发 flow 注入分支);FindBySession 返回带父任务。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, ParentTaskID: 9, SessionID: 600}, nil).AnyTimes()

	Convey("guidance 提到 read / report 新模型", t, func() {
		_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 600, 0)
		So(ok, ShouldBeTrue)
		So(strings.Contains(suffix, "read(task_id"), ShouldBeTrue)
		So(strings.Contains(suffix, "report"), ShouldBeTrue)
	})
}
```

> 该测试需 import:`context`、`strings`、`testing`、goconvey、gomock、`agent_entity`、`mock_orch_repo`、`mock_orch_svc`、`orch_svc`。确认 `turn_test.go` 顶部具备(缺则补)。

- [ ] **Step 2: 跑测试确认失败**

Run: `GOWORK=off go test ./internal/service/orch_svc/ -run TestBuildTurnExtras_GuidanceMentionsReadAndReport -v`
Expected: FAIL(现 guidance 无 read/report)。

- [ ] **Step 3: 更新 guidance**

`turn.go` 的 `orchGuidance` 常量改为(在原文基础上补新模型):

```go
const orchGuidance = `你被授予编排能力(dispatch/ask/send/finish/report/read + agent_list)。模型:` +
	`一切结果都会回到你、由你决定下一步;` +
	`并行 dispatch 子任务,审核/测试/合并也是 dispatch,返工用 send,补信息用 ask,收口用 finish。` +
	`子任务完成/报错默认只给你一条轻量通知(task_done/task_error),要看输出用 read(task_id=…)按需拉全文;` +
	`子任务想主动汇报中途进展用 report、收口小结用 finish,才会把内容内联给你。` +
	`agent_list 即你本次可调度的全集。无次数/时长/成本上限——自己判断何时收口或换策略。用户可能随时插话。`
```

- [ ] **Step 4: 跑测试确认通过 + 全包回归**

Run: `GOWORK=off go test ./internal/service/orch_svc/ 2>&1 | tail -5`
Expected: 新用例 + 全包 PASS。

- [ ] **Step 5: 提交**

```bash
git commit internal/service/orch_svc/turn.go internal/service/orch_svc/turn_test.go \
  -m "✨ orchestration: guidance 增补回报分层 + read/report 新模型"
```

---

## 收尾验证(全部任务后)

- [ ] `make test-backend | tail -8` — 看真 exit;my 包(orch_svc/orch_entity/migrations)须全绿。
      **已知**:`pkg/piagent` `TestStreamDiagnostics*` 为**预存在、与本片无关**的失败(切片 E 已记录),别归因本片。
- [ ] `make lint | tail -8` — golangci-lint v2;`gofmt -l` 对新文件须空。
- [ ] 手验(可选):新建编排让子任务完成 → Leader 侧应收到 `<task_done>` 通知而非整段正文;Leader `read(task_id)` 能拉到全文;子任务 `finish(summary)` → Leader 收到 `<task_report final="true">`。

## Self-Review(对照 spec §A)

- A.1 数据模型(Result 全文 / Summary 显式小结拆列):Task 1 ✓
- A.2 触发分流(finish/report→内联 report;完成→done ping;报错→error ping;都续 Leader 轮):Task 3 ✓;信封 Task 2 ✓
- A.3 report 工具(中途主动、不写 Summary、不改状态):Task 4 ✓
- 按需读取 read(settled 分支;running/peek 属 B):Task 5 ✓
- 统一 XML 信封 + 转义:Task 2(builder)+ Task 3/4/5(接入)✓
- guidance 更新:Task 6 ✓
- 类型一致性:`Task.Summary`(T1)↔ finish 写 Summary(T3)↔ watcher 读 Summary 分流(T3)↔ `taskReportMsg(...,final bool)`(T2)被 T3/T4 调用签名一致;`reportToParent(ctx,parentTaskID,child*)` / `injectToParent(ctx,parentTaskID,msg)`(T3)被 T4 `Report` 复用 ✓
- 占位符扫描:无 TBD;测试代码均给全 ✓

## 非目标(本片)

- 不做 `read` 的 running/peek 分支(需 chat_svc「取最新 assistant 文本」原语 → 切片 B)。
- 不做 status/cancel(切片 B)、软检测(切片 D)。
- 不动前端;不改 ask/reply/send 语义与 `<peer_ask>`。
