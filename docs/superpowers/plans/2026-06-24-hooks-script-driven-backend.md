# Hooks 脚本驱动重构 — Plan 1：后端核心 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Hooks 模块从「source/rule/event 三段 + 硬编码 email 连接器」重写为「按解释器声明执行的可调度脚本 Hook」，脚本拉数据→stdout 产出结构化事件→落库，并由调度器按 cron无人值守跑。

**Architecture:** 数据塌成两表 `hooks`（脚本+解释器+调度+env+state）/ `hook_events`（产出日志）。执行经 `internal/pkg/hookexec` 的 `ScriptRunner` 接缝（生产 `osScriptRunner` 按解释器注册表写临时文件后起子进程；测试注入 fake，绝不真起进程）。`hook_svc` 做 CRUD + 试运行 + 调度，泛化现有 email 轮询器骨架。本 Plan 只到「事件落库 + 可被 Wails 绑定调用」；MCP 创作工具与前端是 Plan 2 / Plan 3。

**Tech Stack:** Go 1.26、cago(`github.com/cago-frame/cago`)、gorm + gormigrate、sqlmock、go.uber.org/mock(mockgen)、goconvey、新增 `github.com/robfig/cron/v3`(仅用其 cron 解析器)。

## Global Constraints

- Go 1.26；模块路径 `github.com/agentre-ai/agentre`；工作区内 Go 命令需 `GOWORK=off` 时谨慎（本仓库直接 `make test-backend`）。
- **严格 TDD：Red → Green → Refactor**，先写失败测试并亲眼看它因正确原因失败，再实现。
- **Repository 单测一律 sqlmock**（`testutils.Database(t)`），禁止起真 SQLite；**Service 单测用 mockgen 注入 repo mock，不连库**。
- **迁移只追加到 `migrationList()` 末尾，绝不改既有迁移**；新迁移 ID = **`202606240002`**（`202606240001` 已被 orchestration 占用）；DDL 用原生 SQL。
- `agentre` 未发布：**可硬删旧数据，无需兼容层**——本 Plan 直接 DROP 旧 `hook_sources`/`hook_rules`/`hook_events`。
- **不做无关改动**：diff 只含 Hook 域 + 其测试；不顺手重排 import / 改格式 / 清无关死代码。
- gitmoji commit；`make lint`(golangci-lint v2) 必须过；改完跑 `make mock` 重新生成 repo mock。
- **关键流程必须日志**：`logger.Ctx(ctx)`，消息前缀小写 `package.Method:`，动态值走 `zap.Xxx(...)`。
- 临时性大重写期间，单测以**包级**为准（整模块 `app` 包在 Task 9 前不编译属预期）；Task 10 收口保证 `make test-backend` 全绿。

## 文件结构（决策锁定）

| 文件 | 责任 | 动作 |
| --- | --- | --- |
| `internal/model/entity/hook_entity/hook.go` | `Hook` / `HookEvent` 富模型 + allowlist + `Check` | 重写（删 `HookSource`/`HookRule`） |
| `internal/model/entity/hook_entity/hook_test.go` | 实体校验测试 | 重写 |
| `internal/pkg/code/{code,en,zh_cn}.go` | 新错误码 | 追加少量码 |
| `migrations/202606240002_hooks_script_redesign.go` | DROP 旧 3 表 + CREATE 新 2 表 + 索引 | 新建 |
| `migrations/202606240002_hooks_script_redesign_test.go` | 迁移测试 | 新建 |
| `migrations/migrations.go` | 注册迁移 | 改 1 行 |
| `internal/repository/hook_repo/hook.go` | `Hook` + `HookEvent` 持久化 | 重写 |
| `internal/repository/hook_repo/hook_test.go` | sqlmock 仓储测试 | 重写 |
| `internal/repository/hook_repo/mock_hook_repo/mock_hook.go` | repo mock | `make mock` 重新生成 |
| `internal/pkg/hookexec/runner.go` | `ScriptRunner` 接口 + 解释器注册表 + `Resolve` | 新建 |
| `internal/pkg/hookexec/runner_unix.go` / `runner_windows.go` | `osScriptRunner`（平台分文件，进程组 kill） | 新建 |
| `internal/pkg/hookexec/runner_test.go` | 注册表/解析单测 + 真子进程集成测试 | 新建 |
| `internal/service/hook_svc/types.go` | 请求/响应/投影 DTO | 重写 |
| `internal/service/hook_svc/hook.go` | `HookSvc` 接口 + CRUD | 重写 |
| `internal/service/hook_svc/run.go` | 试运行/立即运行 + stdout 解析 + 去重落库 | 新建 |
| `internal/service/hook_svc/scheduler.go` | `StartScheduler` ticker + due 扫描 + next_run | 新建（替 `email.go`） |
| `internal/service/hook_svc/email.go` | 旧 IMAP 连接器 | 删除 |
| `internal/service/hook_svc/*_test.go` | service 单测（mockgen + fake runner） | 重写 |
| `internal/app/hook.go` | Wails 绑定 | 重写 |
| `internal/app/app.go:82` | 启动调度器（替 `StartEmailPoller`） | 改 1 行 |
| `internal/bootstrap/cago.go:112-114` | 注册新 repo | 改 |
| `go.mod` | 加 `robfig/cron/v3` | 改 |

---

## Task 1: 实体 `hook_entity` 重写（Hook + HookEvent + Check）

**Files:**
- Modify(重写): `internal/model/entity/hook_entity/hook.go`
- Modify(重写): `internal/model/entity/hook_entity/hook_test.go`
- Modify: `internal/pkg/code/code.go`、`internal/pkg/code/en.go`、`internal/pkg/code/zh_cn.go`

**Interfaces:**
- Produces:
  - `hook_entity.Hook{ ID, Name, Interpreter, Command, TriggerType, ScheduleExpr, Timezone, EnvJSON, StateJSON, NextRunAt, Enabled int, LastRunAt, LastStatus, LastError string, LastDurationMs, TotalCount int64, Status int, Createtime, Updatetime int64 }`，方法 `TableName() string`、`IsEnabled() bool`、`Check(ctx) error`
  - `hook_entity.HookEvent{ ID, HookID int64, Title, DedupeKey, PayloadJSON string, ReceivedAt int64, Status int, Createtime, Updatetime int64 }`，方法 `TableName()`、`Check(ctx)`
  - 常量：`TriggerSchedule = "schedule"`；解释器 allowlist `ValidInterpreters`（`ScheduleExpr` 恒为 cron 表达式）
  - 错误码：`code.HookNotFound`、`code.HookNameDuplicated`、`code.HookInvalidInterpreter`、`code.HookInvalidSchedule`（复用既有 `code.HookInvalidConfig`、`code.HookEventNotFound`）

- [ ] **Step 1: 加错误码** — 在 `internal/pkg/code/code.go` 追加（紧邻既有 `Hook*` 码；不删旧码）：

```go
HookNotFound          = 120010
HookNameDuplicated    = 120011
HookInvalidInterpreter = 120012
HookInvalidSchedule   = 120013
```

在 `en.go` / `zh_cn.go` 各追加对应文案：

```go
// en.go
HookNotFound:           "hook not found",
HookNameDuplicated:     "hook name already exists",
HookInvalidInterpreter: "unsupported interpreter",
HookInvalidSchedule:    "invalid schedule expression",
// zh_cn.go
HookNotFound:           "Hook 不存在",
HookNameDuplicated:     "Hook 名称已存在",
HookInvalidInterpreter: "不支持的解释器",
HookInvalidSchedule:    "调度表达式无效",
```

> 实际码值对齐 `code.go` 现有 Hook 段位（读文件确认下一个可用值，勿与现存冲突）。

- [ ] **Step 2: 写失败测试** — 重写 `hook_entity/hook_test.go`：

```go
package hook_entity

import (
	"context"
	"testing"
)

func TestHook_Check(t *testing.T) {
	ctx := context.Background()
	base := func() *Hook {
		return &Hook{Name: "jira", Interpreter: "bash", Command: "echo '{}'",
			TriggerType: TriggerSchedule, ScheduleExpr: "*/5 * * * *", EnvJSON: "[]"}
	}
	if err := base().Check(ctx); err != nil {
		t.Fatalf("valid hook should pass: %v", err)
	}
	cases := map[string]func(*Hook){
		"empty name":        func(h *Hook) { h.Name = "  " },
		"bad interpreter":   func(h *Hook) { h.Interpreter = "ruby" },
		"empty schedule":    func(h *Hook) { h.ScheduleExpr = "" },
		"empty command":     func(h *Hook) { h.Command = "" },
		"bad env json":      func(h *Hook) { h.EnvJSON = "{not array}" },
	}
	for name, mutate := range cases {
		h := base()
		mutate(h)
		if err := h.Check(ctx); err == nil {
			t.Errorf("%s: expected Check error, got nil", name)
		}
	}
}

func TestHookEvent_Check(t *testing.T) {
	ctx := context.Background()
	ok := &HookEvent{HookID: 1, Title: "t", PayloadJSON: "{}"}
	if err := ok.Check(ctx); err != nil {
		t.Fatalf("valid event should pass: %v", err)
	}
	for name, e := range map[string]*HookEvent{
		"no hook":   {HookID: 0, Title: "t", PayloadJSON: "{}"},
		"no title":  {HookID: 1, Title: "", PayloadJSON: "{}"},
		"bad json":  {HookID: 1, Title: "t", PayloadJSON: "{bad"},
	} {
		if err := e.Check(ctx); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
```

- [ ] **Step 3: 运行看失败**

Run: `make test-backend` 或 `go test ./internal/model/entity/hook_entity/...`
Expected: 编译失败 / FAIL（`Hook`、`TriggerSchedule` 等未定义）。

- [ ] **Step 4: 重写实体实现** — 覆盖 `hook_entity/hook.go`：

```go
// Package hook_entity 是脚本驱动 Hook 与其产出事件的富模型。
package hook_entity

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

const TriggerSchedule = "schedule" // 预留 "webhook"

// ValidInterpreters 是允许声明的解释器 allowlist（见 hookexec 注册表）。
var ValidInterpreters = map[string]struct{}{
	"bash": {}, "sh": {}, "node": {}, "python": {}, "pwsh": {}, "powershell": {}, "cmd": {},
}

// Hook 是一段可调度的脚本：拉数据→stdout 产出 {events,state}。
type Hook struct {
	ID             int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name           string `gorm:"column:name;type:text;not null"`
	Interpreter    string `gorm:"column:interpreter;type:text;not null;default:'bash'"`
	Command        string `gorm:"column:command;type:text;not null;default:''"`
	TriggerType    string `gorm:"column:trigger_type;type:text;not null;default:'schedule'"`
	ScheduleExpr   string `gorm:"column:schedule_expr;type:text;not null;default:''"` // cron 表达式
	Timezone       string `gorm:"column:timezone;type:text;not null;default:'Asia/Shanghai'"`
	EnvJSON        string `gorm:"column:env_json;type:text;not null;default:'[]'"`
	StateJSON      string `gorm:"column:state_json;type:text;not null;default:'{}'"`
	NextRunAt      int64  `gorm:"column:next_run_at;type:bigint;not null;default:0"`
	Enabled        int    `gorm:"column:enabled;type:int;not null;default:1"`
	LastRunAt      int64  `gorm:"column:last_run_at;type:bigint;not null;default:0"`
	LastStatus     string `gorm:"column:last_status;type:text;not null;default:''"`
	LastError      string `gorm:"column:last_error;type:text;not null;default:''"`
	LastDurationMs int64  `gorm:"column:last_duration_ms;type:bigint;not null;default:0"`
	TotalCount     int64  `gorm:"column:total_count;type:bigint;not null;default:0"`
	Status         int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime     int64
	Updatetime     int64
}

func (*Hook) TableName() string { return "hooks" }

func (h *Hook) IsEnabled() bool { return h != nil && h.Enabled == 1 }

func (h *Hook) Check(ctx context.Context) error {
	if h == nil {
		return i18n.NewError(ctx, code.HookNotFound)
	}
	if strings.TrimSpace(h.Name) == "" || strings.TrimSpace(h.Command) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, ok := ValidInterpreters[strings.TrimSpace(h.Interpreter)]; !ok {
		return i18n.NewError(ctx, code.HookInvalidInterpreter)
	}
	if h.TriggerType == "" {
		h.TriggerType = TriggerSchedule
	}
	if strings.TrimSpace(h.ScheduleExpr) == "" {
		return i18n.NewError(ctx, code.HookInvalidSchedule)
	}
	if err := validateJSONArray(h.EnvJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	if err := validateJSONObject(h.StateJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	return nil
}

// HookEvent 是脚本产出的一条结构化记录（产出日志）。
type HookEvent struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	HookID      int64  `gorm:"column:hook_id;type:bigint;not null"`
	Title       string `gorm:"column:title;type:text;not null"`
	DedupeKey   string `gorm:"column:dedupe_key;type:text;not null;default:''"`
	PayloadJSON string `gorm:"column:payload_json;type:text;not null;default:'{}'"`
	ReceivedAt  int64  `gorm:"column:received_at;type:bigint;not null;default:0"`
	Status      int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime  int64
	Updatetime  int64
}

func (*HookEvent) TableName() string { return "hook_events" }

func (e *HookEvent) Check(ctx context.Context) error {
	if e == nil {
		return i18n.NewError(ctx, code.HookEventNotFound)
	}
	if e.HookID <= 0 || strings.TrimSpace(e.Title) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if err := validateJSONObject(e.PayloadJSON); err != nil {
		return i18n.NewError(ctx, code.HookInvalidConfig)
	}
	return nil
}

func validateJSONObject(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out map[string]any
	return json.Unmarshal([]byte(raw), &out)
}

func validateJSONArray(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var out []any
	return json.Unmarshal([]byte(raw), &out)
}
```

- [ ] **Step 5: 运行看通过**

Run: `go test ./internal/model/entity/hook_entity/... ./internal/pkg/code/...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/model/entity/hook_entity/ internal/pkg/code/
git commit -m "♻️ hook: 实体重写为脚本驱动 Hook + HookEvent(删 source/rule)"
```

---

## Task 2: 迁移 `202606240002`（DROP 旧 3 表 + CREATE 新 2 表）

**Files:**
- Create: `migrations/202606240002_hooks_script_redesign.go`
- Create: `migrations/202606240002_hooks_script_redesign_test.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾追加 1 行）

**Interfaces:**
- Produces: `migration202606240002() *gormigrate.Migration`；建表 `hooks` / `hook_events` + 唯一部分索引 `ux_hook_events_dedupe`。

- [ ] **Step 1: 写迁移测试**（`..._test.go`，参照同目录现有 `*_test.go` 用真 sqlite 临时库——迁移测试是 sqlmock 规则的合法例外）：

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestMigration202606240002(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := RunMigrations(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// 新表在
	for _, tbl := range []string{"hooks", "hook_events"} {
		if !gdb.Migrator().HasTable(tbl) {
			t.Errorf("expected table %s", tbl)
		}
	}
	// 旧表已删
	for _, tbl := range []string{"hook_sources", "hook_rules"} {
		if gdb.Migrator().HasTable(tbl) {
			t.Errorf("expected %s dropped", tbl)
		}
	}
	// 去重部分索引允许多条空 key、拒绝重复非空 key
	if err := gdb.Exec(`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime) VALUES (1,'a','',' {}',0,1,0,0),(1,'b','','{}',0,1,0,0)`).Error; err != nil {
		t.Fatalf("empty dedupe should be allowed: %v", err)
	}
	gdb.Exec(`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime) VALUES (1,'c','K1','{}',0,1,0,0)`)
	if err := gdb.Exec(`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime) VALUES (1,'d','K1','{}',0,1,0,0)`).Error; err == nil {
		t.Error("duplicate (hook_id,dedupe_key) should violate unique index")
	}
}
```

> 确认 import 的 sqlite 驱动与同目录现有迁移测试一致（读一个现有 `*_test.go` 头部对齐 driver 包路径）。

- [ ] **Step 2: 运行看失败**

Run: `go test ./migrations/ -run TestMigration202606240002`
Expected: 编译失败（`migration202606240002` 未定义）。

- [ ] **Step 3: 写迁移实现** — `202606240002_hooks_script_redesign.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240002 Hooks 脚本驱动重构：删 source/rule/event 三表，建 hooks + hook_events。
func migration202606240002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240002",
		Migrate: func(tx *gorm.DB) error {
			for _, drop := range []string{"hook_events", "hook_rules", "hook_sources"} {
				if err := tx.Exec("DROP TABLE IF EXISTS " + drop).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec(`CREATE TABLE hooks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				interpreter TEXT NOT NULL DEFAULT 'bash',
				command TEXT NOT NULL DEFAULT '',
				trigger_type TEXT NOT NULL DEFAULT 'schedule',
				schedule_expr TEXT NOT NULL DEFAULT '',
				timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
				env_json TEXT NOT NULL DEFAULT '[]',
				state_json TEXT NOT NULL DEFAULT '{}',
				next_run_at INTEGER NOT NULL DEFAULT 0,
				enabled INTEGER NOT NULL DEFAULT 1,
				last_run_at INTEGER NOT NULL DEFAULT 0,
				last_status TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				last_duration_ms INTEGER NOT NULL DEFAULT 0,
				total_count INTEGER NOT NULL DEFAULT 0,
				status INTEGER NOT NULL DEFAULT 1,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX idx_hooks_due ON hooks(enabled, next_run_at)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE hook_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				hook_id INTEGER NOT NULL,
				title TEXT NOT NULL,
				dedupe_key TEXT NOT NULL DEFAULT '',
				payload_json TEXT NOT NULL DEFAULT '{}',
				received_at INTEGER NOT NULL DEFAULT 0,
				status INTEGER NOT NULL DEFAULT 1,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX idx_hook_events_hook ON hook_events(hook_id, received_at)`).Error; err != nil {
				return err
			}
			// 非空 dedupe_key 才去重；空 key 可多条。
			return tx.Exec(`CREATE UNIQUE INDEX ux_hook_events_dedupe ON hook_events(hook_id, dedupe_key) WHERE dedupe_key <> ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS hook_events`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS hooks`).Error
		},
	}
}
```

- [ ] **Step 4: 注册迁移** — `migrations/migrations.go` 的 `migrationList()` 末尾追加：

```go
		migration202606240001(), // 编排能力基座:Run/Task 表 + chat_sessions.run_id + orchestrate 工具种子
		migration202606240002(), // Hooks 脚本驱动重构:删 source/rule/event,建 hooks + hook_events
	}
```

- [ ] **Step 5: 运行看通过**

Run: `go test ./migrations/ -run TestMigration202606240002 -v`
Expected: PASS（新表在、旧表删、去重索引生效）。

- [ ] **Step 6: Commit**

```bash
git add migrations/
git commit -m "✨ hook: 迁移 202606240002 重建 hooks + hook_events,删旧三表"
```

---

## Task 3: `hook_repo` 重写（Hook CRUD + ListDue；HookEvent 去重落库）

**Files:**
- Modify(重写): `internal/repository/hook_repo/hook.go`
- Modify(重写): `internal/repository/hook_repo/hook_test.go`
- Regenerate: `internal/repository/hook_repo/mock_hook_repo/mock_hook.go`（`make mock`）

**Interfaces:**
- Produces:
  - `hook_repo.Hook() HookRepo` / `RegisterHook(impl)` / `NewHook()`，`HookRepo` 接口：
    `Create(ctx,*Hook) error` · `Update(ctx,*Hook) error` · `Find(ctx,id) (*Hook,error)` · `FindByName(ctx,name) (*Hook,error)` · `List(ctx) ([]*Hook,error)` · `ListDue(ctx, now int64) ([]*Hook,error)` · `Delete(ctx,id) error`
  - `hook_repo.HookEvent() HookEventRepo` / `RegisterHookEvent` / `NewHookEvent()`，`HookEventRepo` 接口：
    `Create(ctx,*HookEvent) error` · `FindByDedupeKey(ctx, hookID int64, key string) (*HookEvent,error)` · `ListByHook(ctx, hookID int64, limit int) ([]*HookEvent,error)` · `ListRecent(ctx, limit int) ([]*HookEvent,error)`

- [ ] **Step 1: 写 sqlmock 测试** — 重写 `hook_repo/hook_test.go`（参照本仓库其它 repo 的 `testutils.Database(t)` 用法；下示关键用例，CRUD 同模式补齐）：

```go
package hook_repo_test

import (
	"context"
	"regexp"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/testutils" // 对齐本仓库实际 testutils 包路径
)

func TestHookRepo_ListDue(t *testing.T) {
	db, mock := testutils.Database(t)
	_ = db
	repo := hook_repo.NewHook()
	rows := sqlmock.NewRows([]string{"id", "name", "enabled", "next_run_at", "status"}).
		AddRow(1, "due", 1, 100, 1)
	mock.ExpectQuery(regexp.QuoteMeta(
		`SELECT * FROM "hooks" WHERE enabled = 1 AND next_run_at <= ? AND status = ? ORDER BY next_run_at ASC`)).
		WithArgs(int64(150), 1).
		WillReturnRows(rows)

	got, err := repo.ListDue(context.Background(), 150)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(got) != 1 || got[0].Name != "due" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestHookEventRepo_FindByDedupeKey_NotFound(t *testing.T) {
	_, mock := testutils.Database(t)
	repo := hook_repo.NewHookEvent()
	mock.ExpectQuery(`SELECT \* FROM .hook_events.`).
		WithArgs(int64(7), "K1", 1).
		WillReturnError(gormErrRecordNotFound()) // helper 返回 gorm.ErrRecordNotFound
	got, err := repo.FindByDedupeKey(context.Background(), 7, "K1")
	if err != nil || got != nil {
		t.Fatalf("expected (nil,nil), got (%v,%v)", got, err)
	}
}
```

> 实际 SQL 字符串以 gorm 生成为准——先跑测试看 mock 报「unexpected query」打印的真实 SQL，再回填 `ExpectQuery`（本仓库既有 repo 测试就是这套路）。`testutils` 包路径、`gormErrRecordNotFound` 辅助按仓库现状对齐。

- [ ] **Step 2: 运行看失败**

Run: `go test ./internal/repository/hook_repo/...`
Expected: 编译失败（`NewHook`/`ListDue`/`FindByDedupeKey` 未定义）。

- [ ] **Step 3: 重写 repo 实现** — 覆盖 `hook_repo/hook.go`：

```go
// Package hook_repo 提供脚本 Hook 与产出事件的持久化访问。
package hook_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
)

//go:generate mockgen -source hook.go -destination mock_hook_repo/mock_hook.go

type HookRepo interface {
	Create(ctx context.Context, h *hook_entity.Hook) error
	Update(ctx context.Context, h *hook_entity.Hook) error
	Find(ctx context.Context, id int64) (*hook_entity.Hook, error)
	FindByName(ctx context.Context, name string) (*hook_entity.Hook, error)
	List(ctx context.Context) ([]*hook_entity.Hook, error)
	ListDue(ctx context.Context, now int64) ([]*hook_entity.Hook, error)
	Delete(ctx context.Context, id int64) error
}

type HookEventRepo interface {
	Create(ctx context.Context, e *hook_entity.HookEvent) error
	FindByDedupeKey(ctx context.Context, hookID int64, key string) (*hook_entity.HookEvent, error)
	ListByHook(ctx context.Context, hookID int64, limit int) ([]*hook_entity.HookEvent, error)
	ListRecent(ctx context.Context, limit int) ([]*hook_entity.HookEvent, error)
}

var (
	defaultHook  HookRepo
	defaultEvent HookEventRepo
)

func Hook() HookRepo           { return defaultHook }
func HookEvent() HookEventRepo { return defaultEvent }

func RegisterHook(impl HookRepo)           { defaultHook = impl }
func RegisterHookEvent(impl HookEventRepo) { defaultEvent = impl }

type hookRepo struct{}
type hookEventRepo struct{}

func NewHook() HookRepo           { return &hookRepo{} }
func NewHookEvent() HookEventRepo { return &hookEventRepo{} }

func (r *hookRepo) Create(ctx context.Context, h *hook_entity.Hook) error {
	return db.Ctx(ctx).Create(h).Error
}

func (r *hookRepo) Update(ctx context.Context, h *hook_entity.Hook) error {
	return db.Ctx(ctx).Save(h).Error
}

func (r *hookRepo) Find(ctx context.Context, id int64) (*hook_entity.Hook, error) {
	out := &hook_entity.Hook{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookRepo) FindByName(ctx context.Context, name string) (*hook_entity.Hook, error) {
	out := &hook_entity.Hook{}
	err := db.Ctx(ctx).Where("name = ? AND status = ?", name, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookRepo) List(ctx context.Context) ([]*hook_entity.Hook, error) {
	var rows []*hook_entity.Hook
	if err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookRepo) ListDue(ctx context.Context, now int64) ([]*hook_entity.Hook, error) {
	var rows []*hook_entity.Hook
	if err := db.Ctx(ctx).
		Where("enabled = 1 AND next_run_at <= ? AND status = ?", now, consts.ACTIVE).
		Order("next_run_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&hook_entity.Hook{}).Where("id = ?", id).Update("status", consts.DELETE).Error
}

func (r *hookEventRepo) Create(ctx context.Context, e *hook_entity.HookEvent) error {
	return db.Ctx(ctx).Create(e).Error
}

func (r *hookEventRepo) FindByDedupeKey(ctx context.Context, hookID int64, key string) (*hook_entity.HookEvent, error) {
	out := &hook_entity.HookEvent{}
	err := db.Ctx(ctx).Where("hook_id = ? AND dedupe_key = ? AND status = ?", hookID, key, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookEventRepo) ListByHook(ctx context.Context, hookID int64, limit int) ([]*hook_entity.HookEvent, error) {
	var rows []*hook_entity.HookEvent
	q := db.Ctx(ctx).Where("hook_id = ? AND status = ?", hookID, consts.ACTIVE).Order("received_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookEventRepo) ListRecent(ctx context.Context, limit int) ([]*hook_entity.HookEvent, error) {
	var rows []*hook_entity.HookEvent
	q := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("received_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
```

- [ ] **Step 4: 重新生成 mock + 跑测试**

Run: `make mock && go test ./internal/repository/hook_repo/...`
Expected: PASS（SQL 字符串按真实生成回填后）。

- [ ] **Step 5: Commit**

```bash
git add internal/repository/hook_repo/
git commit -m "♻️ hook: repo 重写为 Hook/HookEvent(ListDue + dedupe 查询)"
```

---

## Task 4: `hookexec` 解释器注册表 + Resolve（纯函数）

**Files:**
- Create: `internal/pkg/hookexec/runner.go`
- Create: `internal/pkg/hookexec/runner_test.go`

**Interfaces:**
- Produces:
  - `hookexec.ScriptRunner` 接口：`Run(ctx, RunSpec) (*RunResult, error)`
  - `hookexec.RunSpec{ Interpreter, Command string; Env map[string]string; Timeout time.Duration; MaxOutputBytes int }`
  - `hookexec.RunResult{ Stdout, Stderr []byte; ExitCode int; Duration time.Duration; TimedOut, Truncated bool }`
  - `hookexec.Resolve(interpreter string) (*Interp, error)`，`Interp{ Bin string; Args []string; Ext string }`（`Bin` 已经过 `exec.LookPath`；Args 是文件前的固定参数如 `-File`）
  - 错误 `hookexec.ErrUnknownInterpreter`、`hookexec.ErrInterpreterNotInstalled`

- [ ] **Step 1: 写测试** — `runner_test.go`：

```go
package hookexec

import (
	"errors"
	"testing"
)

func TestResolve_UnknownInterpreter(t *testing.T) {
	if _, err := Resolve("ruby"); !errors.Is(err, ErrUnknownInterpreter) {
		t.Fatalf("expected ErrUnknownInterpreter, got %v", err)
	}
}

func TestResolve_KnownInterpreterShape(t *testing.T) {
	// sh 在类 Unix CI 一定在；不在则跳过（Windows）。
	in, err := Resolve("sh")
	if errors.Is(err, ErrInterpreterNotInstalled) {
		t.Skip("sh not installed on this platform")
	}
	if err != nil {
		t.Fatalf("Resolve sh: %v", err)
	}
	if in.Bin == "" || in.Ext != ".sh" {
		t.Fatalf("unexpected interp: %+v", in)
	}
}

func TestResolve_PwshArgs(t *testing.T) {
	in, err := Resolve("pwsh")
	if errors.Is(err, ErrInterpreterNotInstalled) {
		t.Skip("pwsh not installed")
	}
	if err != nil {
		t.Fatalf("Resolve pwsh: %v", err)
	}
	if len(in.Args) == 0 || in.Args[len(in.Args)-1] != "-File" {
		t.Fatalf("pwsh should pass -File before script path: %+v", in.Args)
	}
}
```

- [ ] **Step 2: 运行看失败**

Run: `go test ./internal/pkg/hookexec/...`
Expected: 编译失败（`Resolve` 未定义）。

- [ ] **Step 3: 写注册表实现** — `runner.go`：

```go
// Package hookexec 按声明的解释器执行脚本 Hook：写临时文件后起子进程，
// 注入 env、限时限输出。新增解释器 = 往 registry 加一条（OCP）。
package hookexec

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

var (
	ErrUnknownInterpreter       = errors.New("hookexec: unknown interpreter")
	ErrInterpreterNotInstalled  = errors.New("hookexec: interpreter not installed")
)

type ScriptRunner interface {
	Run(ctx context.Context, spec RunSpec) (*RunResult, error)
}

type RunSpec struct {
	Interpreter    string
	Command        string
	Env            map[string]string
	Timeout        time.Duration
	MaxOutputBytes int
}

type RunResult struct {
	Stdout    []byte
	Stderr    []byte
	ExitCode  int
	Duration  time.Duration
	TimedOut  bool
	Truncated bool
}

// Interp 描述一个解释器如何被调用。
type Interp struct {
	Bin  string   // 已 LookPath 解析的绝对/可执行名
	Args []string // 脚本文件路径之前的固定参数
	Ext  string   // 临时脚本文件扩展名
}

type interpDef struct {
	candidates []string // 按序探测的二进制名
	args       []string
	ext        string
}

var registry = map[string]interpDef{
	"bash":       {candidates: []string{"bash"}, ext: ".sh"},
	"sh":         {candidates: []string{"sh"}, ext: ".sh"},
	"node":       {candidates: []string{"node"}, ext: ".mjs"},
	"python":     {candidates: []string{"python3", "python"}, ext: ".py"},
	"pwsh":       {candidates: []string{"pwsh"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1"},
	"powershell": {candidates: []string{"powershell"}, args: []string{"-NoProfile", "-File"}, ext: ".ps1"},
	"cmd":        {candidates: []string{"cmd"}, args: []string{"/c"}, ext: ".bat"},
}

// Resolve 校验解释器并解析其二进制路径。
func Resolve(interpreter string) (*Interp, error) {
	def, ok := registry[interpreter]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownInterpreter, interpreter)
	}
	for _, name := range def.candidates {
		if bin, err := exec.LookPath(name); err == nil {
			return &Interp{Bin: bin, Args: def.args, Ext: def.ext}, nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrInterpreterNotInstalled, interpreter)
}
```

- [ ] **Step 4: 运行看通过**

Run: `go test ./internal/pkg/hookexec/...`
Expected: PASS（pwsh/sh 不在则相应用例 Skip）。

- [ ] **Step 5: Commit**

```bash
git add internal/pkg/hookexec/runner.go internal/pkg/hookexec/runner_test.go
git commit -m "✨ hookexec: 解释器注册表 + Resolve(LookPath 探测)"
```

---

## Task 5: `osScriptRunner`（真子进程执行，平台分文件）

**Files:**
- Create: `internal/pkg/hookexec/exec.go`（跨平台主体）
- Create: `internal/pkg/hookexec/exec_unix.go`（`//go:build !windows`，进程组 kill）
- Create: `internal/pkg/hookexec/exec_windows.go`（`//go:build windows`，taskkill）
- Modify: `internal/pkg/hookexec/runner_test.go`（追加集成测试）

**Interfaces:**
- Consumes: `Resolve`、`RunSpec`、`RunResult`（Task 4）
- Produces: `hookexec.NewOSRunner() ScriptRunner`（生产实现）；内部 `setSysProcAttr(cmd)` / `killGroup(cmd)` 平台钩子。

- [ ] **Step 1: 写集成测试**（追加到 `runner_test.go`，真起子进程，sh 不在则 Skip）：

```go
import (
	"context"
	"strings"
	"time"
)

func TestOSRunner_EchoJSON(t *testing.T) {
	if _, err := Resolve("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	r := NewOSRunner()
	res, err := r.Run(context.Background(), RunSpec{
		Interpreter:    "sh",
		Command:        `printf '{"events":[],"state":{"k":"%s"}}' "$HOOK_STATE"`,
		Env:            map[string]string{"HOOK_STATE": "v1"},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 64 * 1024,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.ExitCode != 0 || !strings.Contains(string(res.Stdout), `"k":"v1"`) {
		t.Fatalf("unexpected result: code=%d out=%s", res.ExitCode, res.Stdout)
	}
}

func TestOSRunner_Timeout(t *testing.T) {
	if _, err := Resolve("sh"); err != nil {
		t.Skip("sh unavailable")
	}
	r := NewOSRunner()
	res, err := r.Run(context.Background(), RunSpec{
		Interpreter: "sh", Command: "sleep 5", Timeout: 200 * time.Millisecond, MaxOutputBytes: 1024,
	})
	if err != nil {
		t.Fatalf("Run returned err (timeout should be in result, not err): %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("expected TimedOut, got %+v", res)
	}
}
```

- [ ] **Step 2: 运行看失败**

Run: `go test ./internal/pkg/hookexec/ -run TestOSRunner`
Expected: 编译失败（`NewOSRunner` 未定义）。

- [ ] **Step 3: 写主体** — `exec.go`：

```go
package hookexec

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type osScriptRunner struct{}

func NewOSRunner() ScriptRunner { return &osScriptRunner{} }

func (osScriptRunner) Run(ctx context.Context, spec RunSpec) (*RunResult, error) {
	in, err := Resolve(spec.Interpreter)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "agentre-hook-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	file := filepath.Join(dir, "hook"+in.Ext)
	if err := os.WriteFile(file, []byte(spec.Command), 0o600); err != nil {
		return nil, err
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := append(append([]string{}, in.Args...), file)
	cmd := exec.CommandContext(runCtx, in.Bin, args...)
	cmd.Env = append(os.Environ(), envSlice(spec.Env)...)
	setSysProcAttr(cmd) // 平台钩子：独立进程组

	limit := spec.MaxOutputBytes
	if limit <= 0 {
		limit = 256 * 1024
	}
	var outBuf, errBuf bytes.Buffer
	outW := &cappedWriter{w: &outBuf, max: limit}
	cmd.Stdout = outW
	cmd.Stderr = &cappedWriter{w: &errBuf, max: limit}

	start := time.Now()
	cmd.Cancel = func() error { return killGroup(cmd) } // 超时/取消时杀整组
	runErr := cmd.Run()
	dur := time.Since(start)

	res := &RunResult{
		Stdout:    outBuf.Bytes(),
		Stderr:    errBuf.Bytes(),
		Duration:  dur,
		Truncated: outW.truncated,
		TimedOut:  errors.Is(runCtx.Err(), context.DeadlineExceeded),
	}
	var exitErr *exec.ExitError
	switch {
	case runErr == nil:
		res.ExitCode = 0
	case errors.As(runErr, &exitErr):
		res.ExitCode = exitErr.ExitCode()
	default:
		res.ExitCode = -1
		if !res.TimedOut {
			return res, runErr // 真正的起进程失败（非退出码/非超时）
		}
	}
	return res, nil
}

func envSlice(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k, v := range m {
		out = append(out, k+"="+v)
	}
	return out
}

// cappedWriter 截断超限输出但继续吞剩余字节避免子进程阻塞。
type cappedWriter struct {
	w         io.Writer
	max       int
	written   int
	truncated bool
}

func (c *cappedWriter) Write(p []byte) (int, error) {
	if c.written >= c.max {
		c.truncated = true
		return len(p), nil
	}
	room := c.max - c.written
	if len(p) > room {
		c.truncated = true
		_, _ = c.w.Write(p[:room])
		c.written = c.max
		return len(p), nil
	}
	n, err := c.w.Write(p)
	c.written += n
	return n, err
}
```

- [ ] **Step 4: 写平台钩子** — `exec_unix.go`：

```go
//go:build !windows

package hookexec

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// 负 PID = 杀整个进程组。
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
```

`exec_windows.go`：

```go
//go:build windows

package hookexec

import (
	"os/exec"
	"strconv"
)

func setSysProcAttr(cmd *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// /T 连子进程树一起杀，/F 强制。
	return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
```

- [ ] **Step 5: 运行看通过**

Run: `go test ./internal/pkg/hookexec/...`
Expected: PASS（Unix 上 echo/timeout 用例跑真子进程通过）。

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/hookexec/
git commit -m "✨ hookexec: osScriptRunner 真子进程执行(限时/限输出/进程组 kill,平台分文件)"
```

---

## Task 6: `hook_svc` 骨架 + CRUD

**Files:**
- Modify(重写): `internal/service/hook_svc/types.go`
- Modify(重写): `internal/service/hook_svc/hook.go`
- Create: `internal/service/hook_svc/hook_test.go`
- Delete: `internal/service/hook_svc/email.go`（及其 `email`/IMAP 相关测试文件）

**Interfaces:**
- Consumes: `hook_repo.Hook()`（Task 3）、`hook_entity.Hook`（Task 1）
- Produces:
  - `hook_svc.Hook() HookSvc` / 包级 `defaultHook`；`HookSvc` 接口：
    `Load(ctx,*LoadHooksRequest) (*LoadHooksResponse,error)` · `CreateHook(ctx,*CreateHookRequest) (*HookItem,error)` · `UpdateHook(ctx,*UpdateHookRequest) (*HookItem,error)` · `DeleteHook(ctx,id int64) error` · `ToggleHook(ctx,id int64,enabled bool) (*HookItem,error)` · `RunHook(ctx,*RunHookRequest) (*RunHookResult,error)`（Task 7）· `StartScheduler(ctx) context.CancelFunc`（Task 8）
  - DTO：`HookItem`（env 中 secret 值脱敏成 `********`）、`EnvVar{ Key, Value string; Secret bool }`、`CreateHookRequest`/`UpdateHookRequest`
  - 内部：`hookSvc{ now func() int64; runner hookexec.ScriptRunner; ... }`，`maskedSecret = "********"`

- [ ] **Step 1: 写 service 测试**（mockgen 注入 repo，不连库）— `hook_test.go`：

```go
package hook_svc

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo/mock_hook_repo"
)

func TestCreateHook_RejectsDuplicateName(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)

	mh.EXPECT().FindByName(gomock.Any(), "jira").
		Return(&hook_entity.Hook{ID: 9, Name: "jira"}, nil)

	svc := &hookSvc{now: func() int64 { return 1000 }}
	_, err := svc.CreateHook(context.Background(), &CreateHookRequest{
		Name: "jira", Interpreter: "bash", Command: "echo '{}'",
		ScheduleExpr: "*/5 * * * *",
	})
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestCreateHook_PersistsAndMasksSecrets(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)

	mh.EXPECT().FindByName(gomock.Any(), "jira").Return(nil, nil)
	mh.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error { h.ID = 1; return nil })

	svc := &hookSvc{now: func() int64 { return 1000 }}
	item, err := svc.CreateHook(context.Background(), &CreateHookRequest{
		Name: "jira", Interpreter: "bash", Command: "echo '{}'",
		ScheduleExpr: "*/5 * * * *",
		Env: []EnvVar{{Key: "TOKEN", Value: "supersecret", Secret: true}},
	})
	if err != nil {
		t.Fatalf("CreateHook: %v", err)
	}
	if item.Env[0].Value != maskedSecret {
		t.Fatalf("secret should be masked in projection, got %q", item.Env[0].Value)
	}
}
```

- [ ] **Step 2: 运行看失败**

Run: `go test ./internal/service/hook_svc/ -run TestCreateHook`
Expected: 编译失败（`hookSvc`/`CreateHookRequest`/`EnvVar` 未定义）。

- [ ] **Step 3: 写 DTO** — 重写 `types.go`：

```go
// Package hook_svc 暴露脚本 Hook 的 CRUD / 试运行 / 调度服务契约给 Wails 绑定层。
package hook_svc

type EnvVar struct {
	Key    string `json:"key"`
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type HookItem struct {
	ID             int64    `json:"id"`
	Name           string   `json:"name"`
	Interpreter    string   `json:"interpreter"`
	Command        string   `json:"command"`
	ScheduleExpr   string   `json:"scheduleExpr"`
	Timezone       string   `json:"timezone"`
	Env            []EnvVar `json:"env"`
	Enabled        bool     `json:"enabled"`
	NextRunAt      int64    `json:"nextRunAt"`
	LastRunAt      int64    `json:"lastRunAt"`
	LastStatus     string   `json:"lastStatus"`
	LastError      string   `json:"lastError"`
	LastDurationMs int64    `json:"lastDurationMs"`
	TotalCount     int64    `json:"totalCount"`
	Createtime     int64    `json:"createtime"`
	Updatetime     int64    `json:"updatetime"`
}

type HookEventItem struct {
	ID          int64  `json:"id"`
	HookID      int64  `json:"hookId"`
	Title       string `json:"title"`
	DedupeKey   string `json:"dedupeKey"`
	PayloadJSON string `json:"payloadJson"`
	ReceivedAt  int64  `json:"receivedAt"`
	Createtime  int64  `json:"createtime"`
}

type LoadHooksRequest struct {
	HookID int64 `json:"hookId"`
	Limit  int   `json:"limit"`
}

type LoadHooksResponse struct {
	Hooks  []*HookItem      `json:"hooks"`
	Events []*HookEventItem `json:"events"`
}

type CreateHookRequest struct {
	Name         string   `json:"name" binding:"required"`
	Interpreter  string   `json:"interpreter" binding:"required"`
	Command      string   `json:"command"`
	ScheduleExpr string   `json:"scheduleExpr"`
	Timezone     string   `json:"timezone"`
	Env          []EnvVar `json:"env"`
	Enabled      bool     `json:"enabled"`
}

type UpdateHookRequest struct {
	ID int64 `json:"id" binding:"required"`
	CreateHookRequest
}

type RunHookRequest struct {
	ID     int64 `json:"id" binding:"required"`
	DryRun bool  `json:"dryRun"`
}

type RunHookResult struct {
	ExitCode     int              `json:"exitCode"`
	DurationMs   int64            `json:"durationMs"`
	TimedOut     bool             `json:"timedOut"`
	Stdout       string           `json:"stdout"`
	Stderr       string           `json:"stderr"`
	ParseError   string           `json:"parseError"`
	Events       []*HookEventItem `json:"events"`       // 解析出的事件（dry-run 不落库）
	NewCount     int              `json:"newCount"`     // 去重后将/已入库数
	DupCount     int              `json:"dupCount"`
	Persisted    bool             `json:"persisted"`
}
```

- [ ] **Step 4: 写 CRUD 实现** — 重写 `hook.go`（删除原 source/rule/email 全部方法；CRUD + 投影 + 密钥保留）：

```go
package hook_svc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/hookexec"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
)

const (
	defaultEventLimit = 80
	maskedSecret      = "********"
)

type HookSvc interface {
	Load(ctx context.Context, req *LoadHooksRequest) (*LoadHooksResponse, error)
	CreateHook(ctx context.Context, req *CreateHookRequest) (*HookItem, error)
	UpdateHook(ctx context.Context, req *UpdateHookRequest) (*HookItem, error)
	DeleteHook(ctx context.Context, id int64) error
	ToggleHook(ctx context.Context, id int64, enabled bool) (*HookItem, error)
	RunHook(ctx context.Context, req *RunHookRequest) (*RunHookResult, error)
	StartScheduler(ctx context.Context) context.CancelFunc
}

type hookSvc struct {
	now    func() int64
	runner hookexec.ScriptRunner
	sched  schedulerState // Task 8
}

var defaultHook HookSvc = newHookSvc()

func newHookSvc() *hookSvc {
	return &hookSvc{now: func() int64 { return time.Now().Unix() }, runner: hookexec.NewOSRunner()}
}

func Hook() HookSvc { return defaultHook }

func (s *hookSvc) Load(ctx context.Context, req *LoadHooksRequest) (*LoadHooksResponse, error) {
	if req == nil {
		req = &LoadHooksRequest{}
	}
	limit := req.Limit
	if limit <= 0 {
		limit = defaultEventLimit
	}
	hooks, err := hook_repo.Hook().List(ctx)
	if err != nil {
		return nil, err
	}
	var events []*hook_entity.HookEvent
	if req.HookID > 0 {
		events, err = hook_repo.HookEvent().ListByHook(ctx, req.HookID, limit)
	} else {
		events, err = hook_repo.HookEvent().ListRecent(ctx, limit)
	}
	if err != nil {
		return nil, err
	}
	items := make([]*HookItem, 0, len(hooks))
	for _, h := range hooks {
		items = append(items, toHookItem(h))
	}
	evItems := make([]*HookEventItem, 0, len(events))
	for _, e := range events {
		evItems = append(evItems, toEventItem(e))
	}
	return &LoadHooksResponse{Hooks: items, Events: evItems}, nil
}

func (s *hookSvc) CreateHook(ctx context.Context, req *CreateHookRequest) (*HookItem, error) {
	if req == nil {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	dup, err := hook_repo.Hook().FindByName(ctx, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	if dup != nil {
		return nil, i18n.NewError(ctx, code.HookNameDuplicated)
	}
	now := s.now()
	h := &hook_entity.Hook{
		Name:         strings.TrimSpace(req.Name),
		Interpreter:  strings.TrimSpace(req.Interpreter),
		Command:      req.Command,
		TriggerType:  hook_entity.TriggerSchedule,
		ScheduleExpr: strings.TrimSpace(req.ScheduleExpr),
		Timezone:     orDefault(req.Timezone, "Asia/Shanghai"),
		EnvJSON:      marshalEnv(req.Env),
		StateJSON:    "{}",
		Enabled:      boolInt(req.Enabled),
		NextRunAt:    now, // 首个 tick 即到期
		Status:       consts.ACTIVE,
		Createtime:   now,
		Updatetime:   now,
	}
	if err := h.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.Hook().Create(ctx, h); err != nil {
		return nil, err
	}
	return toHookItem(h), nil
}

func (s *hookSvc) UpdateHook(ctx context.Context, req *UpdateHookRequest) (*HookItem, error) {
	if req == nil || req.ID <= 0 {
		return nil, i18n.NewError(ctx, code.InvalidParameter)
	}
	h, err := s.require(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	newName := strings.TrimSpace(req.Name)
	if newName != h.Name {
		dup, err := hook_repo.Hook().FindByName(ctx, newName)
		if err != nil {
			return nil, err
		}
		if dup != nil && dup.ID != h.ID {
			return nil, i18n.NewError(ctx, code.HookNameDuplicated)
		}
	}
	h.Name = newName
	h.Interpreter = strings.TrimSpace(req.Interpreter)
	h.Command = req.Command
	h.ScheduleExpr = strings.TrimSpace(req.ScheduleExpr)
	h.Timezone = orDefault(req.Timezone, "Asia/Shanghai")
	h.EnvJSON = marshalEnv(preserveSecrets(req.Env, parseEnv(h.EnvJSON)))
	h.Enabled = boolInt(req.Enabled)
	h.Updatetime = s.now()
	if err := h.Check(ctx); err != nil {
		return nil, err
	}
	if err := hook_repo.Hook().Update(ctx, h); err != nil {
		return nil, err
	}
	return toHookItem(h), nil
}

func (s *hookSvc) DeleteHook(ctx context.Context, id int64) error {
	if id <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if _, err := s.require(ctx, id); err != nil {
		return err
	}
	return hook_repo.Hook().Delete(ctx, id)
}

func (s *hookSvc) ToggleHook(ctx context.Context, id int64, enabled bool) (*HookItem, error) {
	h, err := s.require(ctx, id)
	if err != nil {
		return nil, err
	}
	h.Enabled = boolInt(enabled)
	h.Updatetime = s.now()
	if err := hook_repo.Hook().Update(ctx, h); err != nil {
		return nil, err
	}
	return toHookItem(h), nil
}

func (s *hookSvc) require(ctx context.Context, id int64) (*hook_entity.Hook, error) {
	h, err := hook_repo.Hook().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if h == nil {
		return nil, i18n.NewError(ctx, code.HookNotFound)
	}
	return h, nil
}

// ---- 投影 / env 编解码 / 密钥保留 ----

func toHookItem(h *hook_entity.Hook) *HookItem {
	if h == nil {
		return nil
	}
	env := parseEnv(h.EnvJSON)
	for i := range env {
		if env[i].Secret && strings.TrimSpace(env[i].Value) != "" {
			env[i].Value = maskedSecret
		}
	}
	return &HookItem{
		ID: h.ID, Name: h.Name, Interpreter: h.Interpreter, Command: h.Command,
		ScheduleExpr: h.ScheduleExpr, Timezone: h.Timezone,
		Env: env, Enabled: h.IsEnabled(), NextRunAt: h.NextRunAt, LastRunAt: h.LastRunAt,
		LastStatus: h.LastStatus, LastError: h.LastError, LastDurationMs: h.LastDurationMs,
		TotalCount: h.TotalCount, Createtime: h.Createtime, Updatetime: h.Updatetime,
	}
}

func toEventItem(e *hook_entity.HookEvent) *HookEventItem {
	if e == nil {
		return nil
	}
	return &HookEventItem{
		ID: e.ID, HookID: e.HookID, Title: e.Title, DedupeKey: e.DedupeKey,
		PayloadJSON: e.PayloadJSON, ReceivedAt: e.ReceivedAt, Createtime: e.Createtime,
	}
}

func parseEnv(raw string) []EnvVar {
	var out []EnvVar
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &out); err != nil {
		return []EnvVar{}
	}
	return out
}

func marshalEnv(env []EnvVar) string {
	if env == nil {
		env = []EnvVar{}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

// preserveSecrets：更新时若 secret 值是掩码或空，则保留旧值。
func preserveSecrets(next, current []EnvVar) []EnvVar {
	old := map[string]string{}
	for _, e := range current {
		if e.Secret {
			old[e.Key] = e.Value
		}
	}
	for i := range next {
		if next[i].Secret {
			v := strings.TrimSpace(next[i].Value)
			if v == "" || v == maskedSecret {
				next[i].Value = old[next[i].Key]
			}
		}
	}
	return next
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
```

> `schedulerState` 类型与 `RunHook`/`StartScheduler` 方法在 Task 7/8 补上；本 Task 先放空壳 `type schedulerState struct{}` 让包编译（或把这两方法的空实现暂置 `panic("todo")` 并在 Task 7/8 替换——推荐前者：`schedulerState` 空结构体 + `RunHook`/`StartScheduler` 留到下个 Task，本 Task 暂时从接口里去掉这两个方法，Task 7/8 再加回）。**实现者注意**：为保持每个 Task 包可编译，本 Task 的 `HookSvc` 接口先**不含** `RunHook`/`StartScheduler`，Task 7/8 各自加回接口方法 + 实现。

- [ ] **Step 5: 删除旧 email 连接器**

```bash
git rm internal/service/hook_svc/email.go
# 若存在 email 相关测试文件一并删除
```

- [ ] **Step 6: 运行看通过**

Run: `go test ./internal/service/hook_svc/ -run TestCreateHook`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/service/hook_svc/
git commit -m "♻️ hook_svc: 重写为脚本 Hook CRUD(密钥脱敏/保留),删 email 连接器"
```

---

## Task 7: `hook_svc.RunHook`（试运行 + 立即运行：解析 stdout、去重落库、回写 state）

**Files:**
- Create: `internal/service/hook_svc/run.go`
- Modify: `internal/service/hook_svc/hook.go`（`HookSvc` 接口加回 `RunHook`）
- Create: `internal/service/hook_svc/run_test.go`

**Interfaces:**
- Consumes: `hookexec.ScriptRunner`（注入到 `hookSvc.runner`）、`hook_repo.Hook()` / `hook_repo.HookEvent()`
- Produces: `(*hookSvc).RunHook(ctx, *RunHookRequest) (*RunHookResult, error)`；内部 `scriptOutput{ Events []scriptEvent; State json.RawMessage }`、`scriptEvent{ Title string; DedupeKey string; Payload json.RawMessage }`、`executeHook(ctx, *Hook, dryRun bool) (*RunHookResult, error)`（Task 8 复用）

- [ ] **Step 1: 写测试**（fake runner，不起真进程）— `run_test.go`：

```go
package hook_svc

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/hookexec"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo/mock_hook_repo"
)

type fakeRunner struct {
	res *hookexec.RunResult
	err error
}

func (f fakeRunner) Run(_ context.Context, _ hookexec.RunSpec) (*hookexec.RunResult, error) {
	return f.res, f.err
}

func TestRunHook_DryRunParsesButDoesNotPersist(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)
	// dry-run 不应触碰 event repo（没有 EXPECT 即代表「不可被调用」）
	hook_repo.RegisterHookEvent(mock_hook_repo.NewMockHookEventRepo(ctrl))

	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
	}, nil)

	svc := &hookSvc{
		now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{
			ExitCode: 0, Duration: 10 * time.Millisecond,
			Stdout: []byte(`{"events":[{"title":"t","dedupeKey":"K1","payload":{"a":1}}],"state":{"c":2}}`),
		}},
	}
	out, err := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: true})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if out.Persisted || len(out.Events) != 1 || out.NewCount != 1 {
		t.Fatalf("dry-run unexpected: %+v", out)
	}
}

func TestRunHook_RealPersistsDedupAndState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	me := mock_hook_repo.NewMockHookEventRepo(ctrl)
	hook_repo.RegisterHook(mh)
	hook_repo.RegisterHookEvent(me)

	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
		ScheduleExpr: "*/5 * * * *",
	}, nil)
	// 第一条新事件 → 查重未命中 → 落库；hook 状态回写
	me.EXPECT().FindByDedupeKey(gomock.Any(), int64(1), "K1").Return(nil, nil)
	me.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	mh.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error {
			if h.LastStatus != "ok" || h.TotalCount != 1 {
				t.Errorf("hook not updated correctly: %+v", h)
			}
			return nil
		})

	svc := &hookSvc{
		now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{ExitCode: 0,
			Stdout: []byte(`{"events":[{"title":"t","dedupeKey":"K1"}],"state":{"c":2}}`)}},
	}
	out, err := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: false})
	if err != nil || !out.Persisted || out.NewCount != 1 {
		t.Fatalf("real run unexpected: out=%+v err=%v", out, err)
	}
}

func TestRunHook_NonZeroExitMarksFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)
	hook_repo.RegisterHookEvent(mock_hook_repo.NewMockHookEventRepo(ctrl))
	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
		ScheduleExpr: "*/5 * * * *"}, nil)
	mh.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error {
			if h.LastStatus != "failed" {
				t.Errorf("expected failed, got %q", h.LastStatus)
			}
			return nil
		})
	svc := &hookSvc{now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{ExitCode: 1, Stderr: []byte("boom")}}}
	out, _ := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: false})
	if out.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %+v", out)
	}
}
```

- [ ] **Step 2: `HookSvc` 接口加回 `RunHook`** — 在 `hook.go` 接口里补 `RunHook(ctx, *RunHookRequest) (*RunHookResult, error)`。

- [ ] **Step 3: 运行看失败**

Run: `go test ./internal/service/hook_svc/ -run TestRunHook`
Expected: 编译失败（`RunHook` 未实现）。

- [ ] **Step 4: 写实现** — `run.go`：

```go
package hook_svc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/hookexec"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
)

const (
	runTimeout     = 30 * time.Second
	runMaxOutBytes = 256 * 1024
)

type scriptOutput struct {
	Events []scriptEvent   `json:"events"`
	State  json.RawMessage `json:"state"`
}

type scriptEvent struct {
	Title     string          `json:"title"`
	DedupeKey string          `json:"dedupeKey"`
	Payload   json.RawMessage `json:"payload"`
}

func (s *hookSvc) RunHook(ctx context.Context, req *RunHookRequest) (*RunHookResult, error) {
	if req == nil || req.ID <= 0 {
		return nil, errInvalid(ctx)
	}
	h, err := s.require(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	return s.executeHook(ctx, h, req.DryRun)
}

// executeHook 跑一次脚本；dryRun=true 时不落库不改 state（调度器以 dryRun=false 复用）。
func (s *hookSvc) executeHook(ctx context.Context, h *hook_entity.Hook, dryRun bool) (*RunHookResult, error) {
	spec := hookexec.RunSpec{
		Interpreter:    h.Interpreter,
		Command:        h.Command,
		Env:            buildEnv(h),
		Timeout:        runTimeout,
		MaxOutputBytes: runMaxOutBytes,
	}
	res, runErr := s.runner.Run(ctx, spec)
	out := &RunHookResult{}
	if res != nil {
		out.ExitCode = res.ExitCode
		out.DurationMs = res.Duration.Milliseconds()
		out.TimedOut = res.TimedOut
		out.Stdout = string(res.Stdout)
		out.Stderr = string(res.Stderr)
	}

	failed := runErr != nil || res == nil || res.ExitCode != 0 || res.TimedOut
	var parsed scriptOutput
	if !failed {
		if perr := json.Unmarshal(res.Stdout, &parsed); perr != nil {
			failed = true
			out.ParseError = perr.Error()
		}
	}

	now := s.now()
	if failed {
		if !dryRun {
			s.finishRun(ctx, h, now, "failed", failureMessage(out, runErr), res, 0)
		}
		logger.Ctx(ctx).Warn("hook_svc.executeHook: run failed",
			zap.Int64("hook_id", h.ID), zap.Int("exit", out.ExitCode), zap.Bool("timeout", out.TimedOut))
		return out, nil
	}

	// 成功：解析事件 → (非 dry-run) 去重落库。
	for _, ev := range parsed.Events {
		title := strings.TrimSpace(ev.Title)
		if title == "" {
			continue
		}
		item := &HookEventItem{HookID: h.ID, Title: title, DedupeKey: ev.DedupeKey,
			PayloadJSON: rawOrEmpty(ev.Payload), ReceivedAt: now}
		if ev.DedupeKey != "" {
			existing, err := hook_repo.HookEvent().FindByDedupeKey(ctx, h.ID, ev.DedupeKey)
			if err != nil {
				return out, err
			}
			if existing != nil {
				out.DupCount++
				continue
			}
		}
		out.Events = append(out.Events, item)
		out.NewCount++
		if !dryRun {
			row := &hook_entity.HookEvent{
				HookID: h.ID, Title: title, DedupeKey: ev.DedupeKey,
				PayloadJSON: item.PayloadJSON, ReceivedAt: now,
				Status: consts.ACTIVE, Createtime: now, Updatetime: now,
			}
			if err := row.Check(ctx); err != nil {
				return out, err
			}
			if err := hook_repo.HookEvent().Create(ctx, row); err != nil {
				return out, err
			}
		}
	}

	if !dryRun {
		newState := rawOrEmpty(parsed.State)
		if strings.TrimSpace(newState) == "" {
			newState = h.StateJSON
		}
		h.StateJSON = newState
		s.finishRun(ctx, h, now, "ok", "", res, out.NewCount)
		out.Persisted = true
	}
	return out, nil
}

// finishRun 回写 last_run_* / state / total_count / next_run_at（next_run 计算见 Task 8）。
func (s *hookSvc) finishRun(ctx context.Context, h *hook_entity.Hook, now int64, status, errMsg string, res *hookexec.RunResult, added int) {
	h.LastRunAt = now
	h.LastStatus = status
	h.LastError = errMsg
	if res != nil {
		h.LastDurationMs = res.Duration.Milliseconds()
	}
	h.TotalCount += int64(added)
	h.NextRunAt = s.computeNextRun(h, now) // Task 8 定义
	h.Updatetime = now
	if err := hook_repo.Hook().Update(ctx, h); err != nil {
		logger.Ctx(ctx).Error("hook_svc.finishRun: update hook failed", zap.Int64("hook_id", h.ID), zap.Error(err))
	}
}

func buildEnv(h *hook_entity.Hook) map[string]string {
	env := map[string]string{
		"HOOK_STATE": orEmptyObject(h.StateJSON),
		"HOOK_NAME":  h.Name,
	}
	for _, e := range parseEnv(h.EnvJSON) {
		if strings.TrimSpace(e.Key) != "" {
			env[e.Key] = e.Value
		}
	}
	return env
}

func rawOrEmpty(r json.RawMessage) string {
	if len(r) == 0 {
		return "{}"
	}
	return string(r)
}

func orEmptyObject(s string) string {
	if strings.TrimSpace(s) == "" {
		return "{}"
	}
	return s
}

func failureMessage(out *RunHookResult, runErr error) string {
	switch {
	case out.TimedOut:
		return "execution timed out"
	case out.ParseError != "":
		return "stdout not valid JSON: " + out.ParseError
	case runErr != nil:
		return runErr.Error()
	default:
		return strings.TrimSpace(out.Stderr)
	}
}
```

> `errInvalid(ctx)` = `i18n.NewError(ctx, code.InvalidParameter)`，若 `hook.go` 未导出该 helper 就直接内联。

- [ ] **Step 5: 运行看通过**

Run: `go test ./internal/service/hook_svc/ -run TestRunHook`
Expected: PASS（注意：`computeNextRun` 需 Task 8；本 Task 可先在 `run.go` 放一个临时 `func (s *hookSvc) computeNextRun(*hook_entity.Hook, int64) int64 { return 0 }` 占位，Task 8 替换为真实现并补测试——占位返回 0 不影响本 Task 断言）。

- [ ] **Step 6: Commit**

```bash
git add internal/service/hook_svc/run.go internal/service/hook_svc/run_test.go internal/service/hook_svc/hook.go
git commit -m "✨ hook_svc: RunHook 试运行/立即运行(解析 stdout + 去重落库 + 回写 state)"
```

---

## Task 8: 调度器 `StartScheduler` + `computeNextRun`（cron）

**Files:**
- Create: `internal/service/hook_svc/scheduler.go`
- Create: `internal/service/hook_svc/scheduler_test.go`
- Modify: `internal/service/hook_svc/hook.go`（接口加回 `StartScheduler`；`schedulerState` 真定义）
- Modify: `go.mod` / `go.sum`（`go get github.com/robfig/cron/v3`）

**Interfaces:**
- Consumes: `hook_repo.Hook().ListDue`、`(*hookSvc).executeHook`（Task 7）
- Produces: `(*hookSvc).StartScheduler(ctx) context.CancelFunc`、`(*hookSvc).computeNextRun(*Hook, now int64) int64`；包级 `hook_svc.StartScheduler(ctx)`（供 app 调用）

- [ ] **Step 1: 加依赖**

Run: `go get github.com/robfig/cron/v3@v3.0.1 && go mod tidy`

- [ ] **Step 2: 写测试** — `scheduler_test.go`：

```go
package hook_svc

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
)

func TestComputeNextRun_Cron(t *testing.T) {
	s := &hookSvc{}
	// 每天 0 点；从 now=0(1970-01-01T00:00:00Z UTC) 起下一次应为 86400。
	h := &hook_entity.Hook{ScheduleExpr: "0 0 * * *", Timezone: "UTC"}
	if got := s.computeNextRun(h, 0); got != 86400 {
		t.Fatalf("cron next = %d, want 86400", got)
	}
}

func TestComputeNextRun_BadCronFallback(t *testing.T) {
	s := &hookSvc{}
	h := &hook_entity.Hook{ScheduleExpr: "garbage", Timezone: "UTC"}
	if got := s.computeNextRun(h, 1000); got <= 1000 {
		t.Fatalf("bad cron should fall back to a future time, got %d", got)
	}
}
```

- [ ] **Step 3: 运行看失败**

Run: `go test ./internal/service/hook_svc/ -run TestComputeNextRun`
Expected: FAIL（占位 `computeNextRun` 返回 0）。

- [ ] **Step 4: 写实现** — `scheduler.go`（并删掉 Task 7 的占位 `computeNextRun`）：

```go
package hook_svc

import (
	"context"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
)

const (
	schedulerTick   = 15 * time.Second
	maxConcurrent   = 4
	fallbackInterval = time.Hour
)

type schedulerState struct {
	mu           sync.Mutex
	cancel       context.CancelFunc
	inflight     map[int64]struct{}
}

// 包级入口：供 internal/app 启动。
func StartScheduler(ctx context.Context) context.CancelFunc { return Hook().StartScheduler(ctx) }

func (s *hookSvc) StartScheduler(parent context.Context) context.CancelFunc {
	ctx, cancel := context.WithCancel(parent)
	s.sched.mu.Lock()
	if s.sched.cancel != nil {
		s.sched.cancel()
	}
	s.sched.cancel = cancel
	if s.sched.inflight == nil {
		s.sched.inflight = map[int64]struct{}{}
	}
	s.sched.mu.Unlock()

	go func() {
		sem := make(chan struct{}, maxConcurrent)
		ticker := time.NewTicker(schedulerTick)
		defer ticker.Stop()
		s.tick(ctx, sem)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.tick(ctx, sem)
			}
		}
	}()
	return func() {
		cancel()
		s.sched.mu.Lock()
		s.sched.cancel = nil
		s.sched.mu.Unlock()
	}
}

func (s *hookSvc) tick(ctx context.Context, sem chan struct{}) {
	if hook_repo.Hook() == nil {
		return
	}
	due, err := hook_repo.Hook().ListDue(ctx, s.now())
	if err != nil {
		logger.Ctx(ctx).Warn("hook_svc.tick: list due", zap.Error(err))
		return
	}
	for _, h := range due {
		if ctx.Err() != nil {
			return
		}
		if !s.claim(h.ID) {
			continue // 仍在跑,不重叠
		}
		hook := h
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			s.release(hook.ID)
			return
		}
		go func() {
			defer func() { <-sem; s.release(hook.ID) }()
			if _, err := s.executeHook(ctx, hook, false); err != nil {
				logger.Ctx(ctx).Warn("hook_svc.tick: execute", zap.Int64("hook_id", hook.ID), zap.Error(err))
			}
		}()
	}
}

func (s *hookSvc) claim(id int64) bool {
	s.sched.mu.Lock()
	defer s.sched.mu.Unlock()
	if _, ok := s.sched.inflight[id]; ok {
		return false
	}
	s.sched.inflight[id] = struct{}{}
	return true
}

func (s *hookSvc) release(id int64) {
	s.sched.mu.Lock()
	delete(s.sched.inflight, id)
	s.sched.mu.Unlock()
}

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func (s *hookSvc) computeNextRun(h *hook_entity.Hook, now int64) int64 {
	sched, err := cronParser.Parse(h.ScheduleExpr)
	if err != nil {
		logger.Ctx(context.Background()).Warn("hook_svc.computeNextRun: bad cron", zap.String("expr", h.ScheduleExpr))
		return now + int64(fallbackInterval.Seconds())
	}
	loc, lerr := time.LoadLocation(orDefault(h.Timezone, "UTC"))
	if lerr != nil {
		loc = time.UTC
	}
	return sched.Next(time.Unix(now, 0).In(loc)).Unix()
}
```

- [ ] **Step 5: 接口补 `StartScheduler`** — `hook.go` 的 `HookSvc` 接口加回 `StartScheduler(ctx context.Context) context.CancelFunc`。

- [ ] **Step 6: 运行看通过**

Run: `go test ./internal/service/hook_svc/...`
Expected: PASS（含 Task 6/7 全部）。

- [ ] **Step 7: Commit**

```bash
git add internal/service/hook_svc/scheduler.go internal/service/hook_svc/scheduler_test.go internal/service/hook_svc/hook.go go.mod go.sum
git commit -m "✨ hook_svc: 调度器(ticker+ListDue+不重叠) + computeNextRun(cron)"
```

---

## Task 9: Wails 绑定 + 启动调度器

**Files:**
- Modify(重写): `internal/app/hook.go`
- Modify: `internal/app/app.go:82`（`StartEmailPoller` → `StartScheduler`）

**Interfaces:**
- Consumes: `hook_svc.Hook()` 全部方法；`hook_svc.StartScheduler`
- Produces: `App` 方法 `LoadHooks` / `CreateHook` / `UpdateHook` / `DeleteHook` / `ToggleHook` / `RunHook`

- [ ] **Step 1: 重写绑定** — `internal/app/hook.go`：

```go
package app

import (
	"github.com/agentre-ai/agentre/internal/service/hook_svc"
)

// LoadHooks 返回脚本 Hook 列表与产出事件日志。
func (a *App) LoadHooks(req *hook_svc.LoadHooksRequest) (*hook_svc.LoadHooksResponse, error) {
	return hook_svc.Hook().Load(a.ctx, req)
}

// CreateHook 新建脚本 Hook。
func (a *App) CreateHook(req *hook_svc.CreateHookRequest) (*hook_svc.HookItem, error) {
	return hook_svc.Hook().CreateHook(a.ctx, req)
}

// UpdateHook 更新脚本 Hook。
func (a *App) UpdateHook(req *hook_svc.UpdateHookRequest) (*hook_svc.HookItem, error) {
	return hook_svc.Hook().UpdateHook(a.ctx, req)
}

// DeleteHook 软删除脚本 Hook。
func (a *App) DeleteHook(id int64) error {
	return hook_svc.Hook().DeleteHook(a.ctx, id)
}

// ToggleHook 启用/停用脚本 Hook。
func (a *App) ToggleHook(id int64, enabled bool) (*hook_svc.HookItem, error) {
	return hook_svc.Hook().ToggleHook(a.ctx, id, enabled)
}

// RunHook 立即执行一次(dryRun=true 为试运行,不落库)。
func (a *App) RunHook(req *hook_svc.RunHookRequest) (*hook_svc.RunHookResult, error) {
	return hook_svc.Hook().RunHook(a.ctx, req)
}
```

- [ ] **Step 2: 换启动调用** — `internal/app/app.go:82`：

```go
	a.hookPollerCancel = hook_svc.StartScheduler(ctx)
```

（变量名 `hookPollerCancel` 保留即可，或顺手按需改名——但**不**做无关重命名以外的改动。）

- [ ] **Step 3: 编译**

Run: `go build ./internal/app/...`
Expected: 通过。

- [ ] **Step 4: Commit**

```bash
git add internal/app/hook.go internal/app/app.go
git commit -m "♻️ app: Hook 绑定改为脚本 Hook CRUD/RunHook + 启动调度器"
```

---

## Task 10: bootstrap 接线 + 全量收口

**Files:**
- Modify: `internal/bootstrap/cago.go:112-114`（注册新 repo）

**Interfaces:**
- Consumes: `hook_repo.RegisterHook` / `RegisterHookEvent`、`hook_repo.NewHook` / `NewHookEvent`

- [ ] **Step 1: 改 repo 注册** — `internal/bootstrap/cago.go` 把原三行换成：

```go
	hook_repo.RegisterHook(hook_repo.NewHook())
	hook_repo.RegisterHookEvent(hook_repo.NewHookEvent())
```

- [ ] **Step 2: 全量编译 + lint + 测试**

Run:
```bash
make mock
make lint
make test-backend
```
Expected: 全绿。**任何对旧 `HookSource`/`HookRule`/`SyncHookEmailSource` 等符号的残留引用都在此暴露**（如 daemon/remotefs、cc_usage、chat 等若引用 hook 旧符号需顺该引用修正——仅修编译必需，不扩散）。

- [ ] **Step 3: 自查 grep 残留**

Run: `grep -rn "HookSource\|HookRule\|SyncHookEmailSource\|StartEmailPoller\|hook_entity.SourceKind" --include="*.go" internal/ migrations/`
Expected: 无输出（除非有意保留的历史注释）。

- [ ] **Step 4: Commit**

```bash
git add internal/bootstrap/cago.go
git commit -m "✅ hook: bootstrap 注册脚本 Hook repo + 全量后端测试绿"
```

---

## Self-Review（已对照 spec §1–§12）

- **§3 数据模型** → Task 1（实体）+ Task 2（迁移，两表 + 部分唯一索引）。✓
- **§4 契约（env 进 / stdout {events,state} 出）** → Task 7 `buildEnv` + `scriptOutput` 解析。✓
- **§5 执行 + 解释器注册表 + 跨平台** → Task 4/5（`hookexec`，平台分文件，`LookPath` 探测）。✓
- **§6 调度器** → Task 8（ticker + ListDue + 不重叠 + computeNextRun cron + 注入 now）。✓
- **§8.1 Wails 绑定** → Task 9。✓
- **§9 文件地图 / §10 测试计划** → 各 Task 的 sqlmock(repo) / fake-runner(svc) / 真子进程(hookexec) 分层覆盖。✓
- **§11 不在本期**：dispatch、webhook、remote、运行历史表、密钥加密 —— 本 Plan 均未触及。✓
- **MCP 创作工具(§7)** 与 **前端(§8.2)** → 不在本 Plan，分别为 Plan 2 / Plan 3。✓
- **开放问题**：`hook_run` 审批粒度与 allowlist 广度属 Plan 2（MCP 门控）范畴；本 Plan allowlist 已全量落地于 `hookexec` 注册表。
- **类型一致性**：`HookSvc` 接口在 Task 6 暂不含 `RunHook`/`StartScheduler`，Task 7/8 显式加回（Step 已注明）；`computeNextRun` Task 7 占位、Task 8 实现并删占位——已在步骤中点明，避免悬空引用。
