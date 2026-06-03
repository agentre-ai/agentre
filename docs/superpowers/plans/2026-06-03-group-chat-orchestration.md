# 群聊 Agent 编排 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在 agentre 桌面端新增「群聊 agent 编排」——一个协调者牵头、可动态招募成员、成员/用户通过 `@` 寻址收发、并行 fan-out 自动推进的多 agent 协作房间；**agent 经注入的 `group_send` MCP tool 发言**，能力门控 `CapMCPTools`（MVP 仅 claudecode）。

**Architecture:** 新增自包含 domain `group_*`（entity/repo/svc/app 绑定 + 前端面板），作为纯应用层编排器，**架在 `chat_svc` 之上**（走窄接口 accessor）。成员 = 真实的 `chat_sessions` 行（带 `group_id`，从默认列表隐藏），复用 history / steering / 工具权限 / capability gating。**发消息机制 = `group_send` MCP tool**：成员 turn 内调 tool（结构化 `mentions[]`）→ gateway `/mcp/group` handler → `group_svc.IngestAgentMessage` 路由；私有叙述不进群。`ObserveTurn` 退居**生命周期观察**（释放调度槽 + quiesce），不再解析 turn 文本。seam：`ObserveTurn` + `chat_sessions.group_id` + `EnsureGroupMemberSession` + **新能力 `CapMCPTools`** + **`RunRequest.MCPServers`/`pkg/claudecode --mcp-config`** + **`SendRequest` 透传 `MCPServers`/`SystemPromptSuffix`（领域无关）** + **gateway 注册 `/mcp/group`**。`<mention>` 解析**保留但仅供前端高亮 chip + 点击跳转**，不参与路由。

**Tech Stack:** Go 1.26 / cago / gorm + gormigrate（SQLite，原生 SQL DDL）/ go.uber.org/mock（mockgen）/ goconvey；MCP over HTTP（复用 `internal/pkg/httpgateway`）；React 19 + TS + Vite + Tailwind v4 + shadcn `@/components/ui/*`（无 `Tabs`，复用 `chat-tabs/`）+ react-i18next + zustand + Vitest；Wails v2 IPC（`frontend/wailsjs` 生成绑定 + `wailsruntime.EventsEmit`）。

**Spec:** `docs/superpowers/specs/2026-06-03-group-chat-orchestration-design.md`（2026-06-03 已升级为 tool-send + MCP + 能力门控）

**执行顺序：A（chat_svc seam）→ A-MCP（能力 + MCP 注入管线）→ B（group 数据层）→ C（group_svc 编排）→ D（Wails 绑定 + 事件）→ E（前端）。** A/A-MCP 是地基，必须先做；E 依赖 D 跑过 `make generate`。

**全局命令备忘：**
- 聚焦后端单测：`go test -race -run TestName ./internal/...`
- 后端全量（排除 frontend）：`make test-backend`
- 生成 mock：`make mock`（= `go generate ./...`）
- 刷新 Wails 绑定：`make generate`
- 前端单测：`cd frontend && pnpm test -- path/to/file.test.tsx`
- lint：`make lint`
- **提交规约：gitmoji；只提交本特性 producer + 测试，禁 drive-by。**

---

## Phase A — `chat_*` seam（地基：group_id 列 / EnsureGroupMemberSession / ObserveTurn）

### Task A1: `chat_sessions` 增 `group_id` 列 + 实体字段 + 迁移

**Files:**
- Modify: `internal/model/entity/chat_entity/session.go`（`Session` 结构 + `ProjectID` 之后加 `GroupID`）
- Create: `migrations/202606030001_chat_session_group_id.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾 append）
- Test: `migrations/202606030001_chat_session_group_id_test.go`

- [ ] **Step 1: 写迁移测试（先红）**

`migrations/202606030001_chat_session_group_id_test.go`（迁移自身可起真库，属白名单例外）：

```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/gorm"
)

func TestMigration202606030001GroupID(t *testing.T) {
	Convey("给定全量迁移跑过, chat_sessions 应含 group_id 列且默认 0", t, func() {
		gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		So(err, ShouldBeNil)
		m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
		So(m.Migrate(), ShouldBeNil)

		So(gdb.Migrator().HasColumn("chat_sessions", "group_id"), ShouldBeTrue)

		// 既有列默认值 0：插一行不指定 group_id
		So(gdb.Exec(`INSERT INTO chat_sessions (agent_id, status) VALUES (1, 1)`).Error, ShouldBeNil)
		var gid int64
		So(gdb.Raw(`SELECT group_id FROM chat_sessions LIMIT 1`).Scan(&gid).Error, ShouldBeNil)
		So(gid, ShouldEqual, 0)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestMigration202606030001GroupID ./migrations/`
Expected: FAIL —— 编译错误（`migration202606030001` 未定义）或 `HasColumn` 返回 false。

- [ ] **Step 3: 写迁移**

`migrations/202606030001_chat_session_group_id.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606030001 给 chat_sessions 增 group_id 列(群聊成员 backing session 归属),
// 并加覆盖默认列表过滤维度的索引。group_id=0 表示普通单 agent 会话。
func migration202606030001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606030001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN group_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chat_sessions_agent_group_status
ON chat_sessions(agent_id, group_id, status, last_message_at)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_chat_sessions_agent_group_status`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN group_id`).Error
		},
	}
}
```

- [ ] **Step 4: append 到 `migrationList()`**

`migrations/migrations.go`，在 `migration202605220010()` 行后追加（**已对照真实 `migrations.go` 核对：当前最后一条是 `202605220010`，无 issues 迁移**）：

```go
		migration202605220010(), // projects.sort_order
		migration202606030001(), // chat_sessions.group_id + index
	}
}
```

- [ ] **Step 5: 实体加字段**

`internal/model/entity/chat_entity/session.go`，在 `ProjectID` 字段之后插入：

```go
	GroupID int64 `gorm:"column:group_id;type:bigint;not null;default:0"`
```

- [ ] **Step 6: 跑测试看通过**

Run: `go test -race -run TestMigration202606030001GroupID ./migrations/`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add internal/model/entity/chat_entity/session.go migrations/202606030001_chat_session_group_id.go migrations/202606030001_chat_session_group_id_test.go migrations/migrations.go
git commit -m "✨ group: chat_sessions 增 group_id 列 + 索引(群成员 backing session 归属)"
```

---

### Task A2: 默认会话列表查询全部加 `group_id = 0` 过滤

**Files:**
- Modify: `internal/repository/chat_repo/session.go`（9 个查询 + 1 个 scope helper）
- Test: `internal/repository/chat_repo/session_test.go`（sqlmock；若已存在则追加用例）

> 漏一个 list 查询，群成员的 backing session 就会泄进普通单 agent 会话列表。必须覆盖全部 9 个：`ListByAgent` / `ListByAgentPaged` / `ListIDsByAgents` / `ListAttentionByAgent` / `ListByProject`（列表）+ `CountByAgent` / `CountByAgents` / `CountRunningByAgents` / `CountActiveByProject`（计数）。`Find`（单条）**不加**。

- [ ] **Step 1: 写 sqlmock 回归测试（先红）**

在 `internal/repository/chat_repo/session_test.go` 追加（沿用文件已有的 `testutils.Database(t)` + sqlmock 风格；若文件不存在，参照同目录其它 `*_test.go` 建立同样的 setup）：

```go
func TestSessionRepo_ListByAgent_FiltersGroupSessions(t *testing.T) {
	Convey("ListByAgent 的 SQL 必须带 group_id = 0", t, func() {
		ctx, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT \* FROM .chat_sessions.*group_id. = .*`).
			WithArgs(int64(1), consts.ACTIVE, int64(0), sqlmock.AnyArg()).
			WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))

		_, err := chat_repo.Session().ListByAgent(ctx, 1, 20)
		So(err, ShouldBeNil)
		So(mock.ExpectationsWereMet(), ShouldBeNil)
	})
}
```

> 注：精确的 `ExpectQuery` 正则/参数顺序需对照 gorm 生成的 SQL 微调；目标是断言 `group_id` 进了 WHERE。对 5 个 list + 4 个 count 各补一条同型用例（计数用 `ExpectQuery` 匹配 `SELECT count`）。

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestSessionRepo_ListByAgent_FiltersGroupSessions ./internal/repository/chat_repo/`
Expected: FAIL —— 当前 SQL 不含 `group_id`，参数不匹配。

- [ ] **Step 3: 加 scope helper + 改 9 个查询**

`internal/repository/chat_repo/session.go` 顶部（包内）加：

```go
// defaultSessionScope 限定为「普通单 agent 会话」(排除群聊成员 backing session)。
// 所有默认会话列表/计数查询统一挂这个 scope, 避免逐个手写 group_id = 0 漏一个。
func defaultSessionScope(db *gorm.DB) *gorm.DB {
	return db.Where("group_id = ?", 0)
}
```

然后给 9 个查询各加 `.Scopes(defaultSessionScope)`。逐个改：

```go
// ListByAgent (line ~78)
db.Ctx(ctx).Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
	Scopes(defaultSessionScope).
	Order("last_message_at DESC, id DESC").Limit(limit)

// ListByAgentPaged (line ~91)
db.Ctx(ctx).Where("agent_id = ? AND status = ?", agentID, consts.ACTIVE).
	Scopes(defaultSessionScope).
	Order("last_message_at DESC, id DESC").Offset(offset).Limit(limit)

// ListIDsByAgents (line ~110)
db.Ctx(ctx).Table("chat_sessions").Select("agent_id, id").
	Where("agent_id IN ? AND status = ?", agentIDs, consts.ACTIVE).
	Scopes(defaultSessionScope).
	Order("agent_id ASC, last_message_at DESC, id DESC")

// ListAttentionByAgent (line ~130)
db.Ctx(ctx).Where("agent_id = ? AND status = ? AND agent_status IN ?", agentID, consts.ACTIVE, []string{"running", "waiting", "error"}).
	Scopes(defaultSessionScope).
	Order("last_message_at DESC, id DESC").Limit(limit)

// ListByProject (line ~209)
db.Ctx(ctx).Where("project_id = ? AND status = ?", projectID, consts.ACTIVE).
	Scopes(defaultSessionScope).
	Order("last_message_at DESC, id DESC")

// CountByAgent / CountByAgents / CountRunningByAgents / CountActiveByProject
// 各自在其 .Where(...) 之后挂 .Scopes(defaultSessionScope)
```

> `defaultSessionScope` 用 `.Scopes(...)`（gorm 标准 scope）而非裸 `.Where`，保证与已有链式 `.Where` 复合无误。`Table("chat_sessions")` 的 `ListIDsByAgents` 同样能用 Scopes。

- [ ] **Step 4: 跑全部 chat_repo 测试看通过**

Run: `go test -race ./internal/repository/chat_repo/`
Expected: PASS（含新加的 9 条过滤断言 + 原有用例）

- [ ] **Step 5: 提交**

```bash
git add internal/repository/chat_repo/session.go internal/repository/chat_repo/session_test.go
git commit -m "✨ group: 默认会话列表/计数查询加 group_id=0 过滤(隐藏群成员 backing session)"
```

---

### Task A3: `chat_svc.EnsureGroupMemberSession`

**Files:**
- Modify: `internal/service/chat_svc/chat.go`（接口 + 实现）
- Modify: `internal/service/chat_svc/types.go`（如需 req/resp，可直接用裸参数，不必加 struct）
- Test: `internal/service/chat_svc/chat_ensure_group_member_session_test.go`

> 创建/返回带 `group_id` 的 backing session，幂等：同 (groupID, agentID) 已有则返回既有 id。`group_svc` recruit 时调用，随后正常 `Send(sessionID=...)`。

- [ ] **Step 1: 在 `ChatSvc` 接口加方法声明**

`chat.go` 的 `type ChatSvc interface { ... }` 内追加：

```go
	// EnsureGroupMemberSession 创建/返回某 agent 在指定群的 backing session(带 group_id)。
	// 幂等: 同 (groupID, agentID) 的 active session 已存在则复用。
	EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error)
```

- [ ] **Step 2: 写 service 单测（先红）**

`chat_ensure_group_member_session_test.go`（service 单测不接真库——但这里要查/建 session，走 repo。沿用 chat_svc 既有 service 测试的 sqlmock setup 风格；查 `chat_svc` 目录里现有的 `*_test.go` 看它们如何起 `testutils.Database(t)`）：

```go
func TestEnsureGroupMemberSession_CreatesWithGroupID(t *testing.T) {
	Convey("给定群内该 agent 无 session, 应新建一个带 group_id 的 session", t, func() {
		ctx, mock := testutils.Database(t)
		// 查既有(group_id, agent_id) → 空
		mock.ExpectQuery(`SELECT \* FROM .chat_sessions.*group_id.*agent_id.*`).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		// 插入新 session
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO .chat_sessions.`).WillReturnResult(sqlmock.NewResult(7, 1))
		mock.ExpectCommit()

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3 /*agent*/, 0 /*project*/, 5 /*group*/)
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 7)
		So(mock.ExpectationsWereMet(), ShouldBeNil)
	})
}
```

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run TestEnsureGroupMemberSession_CreatesWithGroupID ./internal/service/chat_svc/`
Expected: FAIL —— 方法未实现。

- [ ] **Step 4: 加 repo 查询 + 实现方法**

先在 `internal/repository/chat_repo/session.go` 的 `SessionRepo` 接口加一个按群+agent 查的方法并实现：

```go
// FindByGroupAndAgent 查某 agent 在某群的 active backing session, 无则返回 nil。
FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*chat_entity.Session, error)
```

```go
func (r *sessionRepo) FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*chat_entity.Session, error) {
	out := &chat_entity.Session{}
	err := db.Ctx(ctx).Where("group_id = ? AND agent_id = ? AND status = ?", groupID, agentID, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}
```

再在 `chat.go` 实现 svc 方法：

```go
func (s *chatSvc) EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error) {
	if agentID <= 0 || groupID <= 0 {
		return 0, i18n.NewError(ctx, code.InvalidParameter)
	}
	existing, err := chat_repo.Session().FindByGroupAndAgent(ctx, groupID, agentID)
	if err != nil {
		return 0, err
	}
	if existing != nil {
		return existing.ID, nil
	}
	sess := &chat_entity.Session{
		AgentID:     agentID,
		ProjectID:   projectID,
		GroupID:     groupID,
		AgentStatus: "idle",
		Status:      consts.ACTIVE,
	}
	if err := chat_repo.Session().Create(ctx, sess); err != nil {
		logger.Ctx(ctx).Error("chat_svc.EnsureGroupMemberSession: create failed", zap.Int64("agentId", agentID), zap.Int64("groupId", groupID), zap.Error(err))
		return 0, i18n.NewError(ctx, code.OperationFailed)
	}
	return sess.ID, nil
}
```

> 同时给 `mockgen` 的 `SessionRepo` mock 加 `FindByGroupAndAgent`（`make mock` 自动生成）。

- [ ] **Step 5: 跑测试 + mock 生成**

Run: `make mock && go test -race -run TestEnsureGroupMemberSession ./internal/service/chat_svc/`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/service/chat_svc/chat.go internal/repository/chat_repo/session.go internal/repository/chat_repo/mock_chat_repo/ internal/service/chat_svc/chat_ensure_group_member_session_test.go
git commit -m "✨ group: chat_svc.EnsureGroupMemberSession + repo FindByGroupAndAgent"
```

---

### Task A4: `chat_svc.ObserveTurn` 服务端 turn 完成观察口

**Files:**
- Create: `internal/service/chat_svc/observe.go`（`TurnResult` + 订阅注册表 + publish）
- Modify: `internal/service/chat_svc/chat.go`（接口加 `ObserveTurn`；finalize 与 `failTurn` 各 publish 一次）
- Test: `internal/service/chat_svc/observe_test.go`

> **职责（tool-send 架构）：`ObserveTurn` 只管 turn 生命周期**（释放成员调度槽 + 判断 quiesce），**不**承载消息内容 —— 成员发言来自 turn 进行中的 `group_send` MCP tool 调用（Phase A-MCP / C5），不从最终文本解析。故 `TurnResult` 不含 `Text`。
> **不变量：订阅了就一定收到恰好一条终态。** 正常 finalize publish 一次；`failTurn` 的 5 个早退路径各 publish 一次（且早退后立即 return，不会再走 finalize）。abort 走 finalize 的 `aborted=true` 分支，不经 failTurn。

- [ ] **Step 1: 写 `observe.go`（注册表 + 类型，先建骨架以便测试编译）**

```go
package chat_svc

import "sync"

// TurnResult 一个 turn 的终态(服务端观察口产出, 不经 Wails)。
// tool-send 架构下只承载生命周期信号, 不含消息文本(发言走 group_send MCP tool)。
type TurnResult struct {
	SessionID          int64
	AssistantMessageID int64
	Aborted            bool
	Err                error
}

// turnObservers: sessionID -> set of buffered channels。
// 订阅必须发生在 turn 起点之前(group_svc 先 ObserveTurn 再 Send), 否则快 turn 的回执会丢。
func (s *chatSvc) ensureObservers() *sync.Map {
	if s.turnObservers == nil {
		s.turnObservers = &sync.Map{}
	}
	return s.turnObservers
}

// ObserveTurn 订阅指定 session 下一次 turn 的终态。返回只读 channel + 取消函数。
// channel 带缓冲(1), publish 非阻塞; 调用方收到一条后应 cancel()。
func (s *chatSvc) ObserveTurn(sessionID int64) (<-chan TurnResult, func()) {
	ch := make(chan TurnResult, 1)
	obs := s.ensureObservers()
	raw, _ := obs.LoadOrStore(sessionID, &sync.Map{})
	set := raw.(*sync.Map)
	set.Store(ch, struct{}{})
	cancel := func() {
		set.Delete(ch)
	}
	return ch, cancel
}

// publishTurnResult 向某 session 的所有订阅者非阻塞推送一条终态。
func (s *chatSvc) publishTurnResult(sessionID int64, r TurnResult) {
	if s.turnObservers == nil {
		return
	}
	raw, ok := s.turnObservers.Load(sessionID)
	if !ok {
		return
	}
	set := raw.(*sync.Map)
	set.Range(func(k, _ any) bool {
		ch := k.(chan TurnResult)
		select {
		case ch <- r:
		default: // 缓冲满(订阅方没收) → 丢弃, 不阻塞 turn
		}
		return true
	})
}
```

在 `chatSvc` struct 定义里加字段 `turnObservers *sync.Map`（与 `aborted *sync.Map` 等并列），并在 `NewChat` 里 `s.turnObservers = &sync.Map{}`。

- [ ] **Step 2: 接口加方法 + 写测试（先红）**

`chat.go` 接口加：

```go
	// ObserveTurn 订阅指定 session 下一次 turn 完成(服务端, 不经 Wails)。
	ObserveTurn(sessionID int64) (<-chan TurnResult, func())
```

`observe_test.go`：

```go
package chat_svc_test

import (
	"errors"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/service/chat_svc"
)

func TestObserveTurn_ReceivesTerminalOnce(t *testing.T) {
	Convey("订阅后, publish 的终态应被收到恰好一条", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{}).(interface {
			ObserveTurn(int64) (<-chan chat_svc.TurnResult, func())
			PublishTurnResultForTest(int64, chat_svc.TurnResult)
		})
		ch, cancel := svc.ObserveTurn(42)
		defer cancel()
		svc.PublishTurnResultForTest(42, chat_svc.TurnResult{SessionID: 42, Aborted: true})

		select {
		case r := <-ch:
			So(r.SessionID, ShouldEqual, 42)
			So(r.Aborted, ShouldBeTrue)
		case <-time.After(time.Second):
			t.Fatal("未收到 TurnResult")
		}
	})

	Convey("无订阅者时 publish 不 panic", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{}).(interface {
			PublishTurnResultForTest(int64, chat_svc.TurnResult)
		})
		So(func() { svc.PublishTurnResultForTest(99, chat_svc.TurnResult{Err: errors.New("x")}) }, ShouldNotPanic)
	})
}
```

加一个 test-only 导出（`observe.go` 内，紧跟 publish）：

```go
// PublishTurnResultForTest 仅供单测直接驱动 publish。
func (s *chatSvc) PublishTurnResultForTest(sessionID int64, r TurnResult) { s.publishTurnResult(sessionID, r) }
```

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run TestObserveTurn_ReceivesTerminalOnce ./internal/service/chat_svc/`
Expected: FAIL（首跑应在补齐 struct 字段/方法前编译失败；补齐后才该转测逻辑）。

- [ ] **Step 4: 在 finalize 与 failTurn 接 publish**

`chat.go` finalize 区（`acc.Finalize()` 落 blocks、定 agentStatus、emit StreamDone/Error/Aborted 之后，即 ~line 2580 `StreamClosed` emit 之后）追加：

```go
	s.publishTurnResult(sess.ID, TurnResult{
		SessionID:          sess.ID,
		AssistantMessageID: assistantMsg.ID,
		Aborted:            aborted,
		Err:                stopErr,
	})
```

`failTurn` 方法体末尾（最后一个 `StreamClosed` emit 之后）追加：

```go
	s.publishTurnResult(sess.ID, TurnResult{
		SessionID:          sess.ID,
		AssistantMessageID: msg.ID,
		Err:                err,
	})
```

> 不取 `textOfMessage` —— 生命周期信号不需要文本（发言走 `group_send` tool）。

- [ ] **Step 5: 写「失败 turn 也回灌恰好一条」回归测试**

`observe_test.go` 追加一条：构造一个会走 `failTurn` 的 `Send`（例如 `AgentID` 解析不到 runner），订阅后断言收到一条 `Err != nil` 的 `TurnResult`。若 `Send` 的失败路径在纯 mock 下难以稳定构造，**退而求其次**直接单元测 `failTurn` 会 publish：

```go
func TestFailTurn_PublishesErrTerminal(t *testing.T) {
	Convey("failTurn 必须向 observer 回灌一条 Err 终态", t, func() {
		ctx, mock := testutils.Database(t)
		mock.ExpectExec(`UPDATE .chat_messages.`).WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectExec(`UPDATE .chat_sessions.`).WillReturnResult(sqlmock.NewResult(0, 1))

		s := chat_svc.NewChatForTest(chat_svc.NoopEmitter{}) // 见下: 暴露 failTurn 的 test helper
		ch, cancel := s.ObserveTurn(8)
		defer cancel()
		s.FailTurnForTest(ctx, 8 /*sessID*/, 3 /*msgID*/, "stream:8:3", errors.New("boom"))

		select {
		case r := <-ch:
			So(r.Err, ShouldNotBeNil)
			So(r.SessionID, ShouldEqual, 8)
		case <-time.After(time.Second):
			t.Fatal("failTurn 未 publish")
		}
	})
}
```

并在 `observe.go` 加薄 test helper（接受裸 id，内部组装最小 `Session`/`Message` 调 `failTurn`）：

```go
// FailTurnForTest 仅供单测验证 failTurn 的 publish 不变量。
func (s *chatSvc) FailTurnForTest(ctx context.Context, sessID, msgID int64, stream string, err error) {
	s.failTurn(ctx, &chat_entity.Session{ID: sessID}, &chat_entity.Message{ID: msgID}, stream, err)
}
func NewChatForTest(e Emitter) *chatSvc { return NewChat(e).(*chatSvc) }
```

- [ ] **Step 6: 跑测试看通过**

Run: `go test -race -run 'TestObserveTurn|TestFailTurn' ./internal/service/chat_svc/`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/service/chat_svc/observe.go internal/service/chat_svc/chat.go internal/service/chat_svc/observe_test.go
git commit -m "✨ group: chat_svc.ObserveTurn 服务端 turn 完成观察口(finalize+failTurn 各回灌一次终态)"
```

---

## Phase A-MCP — 能力门控 + MCP 注入管线（地基，B 之前）

> 给 claudecode runtime 加 `CapMCPTools` 能力 + 让它能带 `group_send` MCP tool 启动。**不 backend-switch**：经 `RunRequest.MCPServers` + `SendRequest` 透传，chat_svc 领域无关。MCP **server handler** 在 Phase C（需 `IngestAgentMessage`）。

### Task AM1: 新能力 `CapMCPTools` + claudecode 声明 + matrix 测试

**Files:**
- Modify: `internal/pkg/agentruntime/capability/capability.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime.go`（`Capabilities()`，`runtime.go:69`）
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go`（`TestXxxCapabilities` matrix）

> agent-backend.md §0.5 要求 cap 三处同步：capability.go 常量 / runtime.go Capabilities() / runtime_test.go 矩阵断言。

- [ ] **Step 1: 加常量**

`capability.go` const 块末尾加：

```go
	CapMCPTools Capability = "mcp_tools" // 该 runtime 接受 RunRequest.MCPServers, 可带注入的 MCP tool 启动(群聊是首个消费者)
```

- [ ] **Step 2: 改 matrix 测试（先红）**

`runtime_test.go` 的 claudecode 能力断言里加一行 `So(caps.Has(capability.CapMCPTools), ShouldBeTrue)`；codex/builtin/piagent 的对应测试加 `ShouldBeFalse`。

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run TestCapabilities ./internal/pkg/agentruntime/...`
Expected: FAIL（claudecode 尚未声明）。

- [ ] **Step 4: claudecode 声明能力**

`runtime.go:69` `Capabilities()` 返回的 `Set` map 加 `capability.CapMCPTools: true`。其它 runtime 不动（默认 false）。

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race ./internal/pkg/agentruntime/...`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/pkg/agentruntime/capability/capability.go internal/pkg/agentruntime/runtimes/claudecode/runtime.go internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go
git commit -m "✨ group: 新能力 CapMCPTools(claudecode 声明)——群聊入群门控"
```

---

### Task AM2: `RunRequest.MCPServers` + `pkg/claudecode --mcp-config` + 映射

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go`（`MCPServerSpec` 类型 + `RunRequest.MCPServers`）
- Modify: `pkg/claudecode/args.go`（`flagMcpConfig` + `spec.mcpConfig`）
- Modify: `pkg/claudecode/options.go`（`WithMcpConfig`）
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/session.go`（`ccBuildClientOpts:177` 映射）
- Test: `pkg/claudecode/args_test.go` + `internal/pkg/agentruntime/runtimes/claudecode/session_test.go`

- [ ] **Step 1: 加共享类型 + RunRequest 字段**

`runner.go`（RunRequest 上方）：

```go
// MCPServerSpec 一个注入给 runtime 的 MCP tool server(http transport)。
type MCPServerSpec struct {
	Name    string            // server 名; claude tool 暴露为 mcp__<Name>__<tool>
	URL     string            // http MCP endpoint(如 http://127.0.0.1:<port>/mcp/group/)
	Headers map[string]string // 鉴权/scope header(如 {"Authorization":"Bearer <token>"})
}
```

`RunRequest` 加字段（与 `Compact`/`ForkAnchor` 并列）：

```go
	// MCPServers 非空 = 给本轮 CLI 注入额外 MCP tool server。仅声明 CapMCPTools
	// 的 runtime(claudecode)消费; 其它 runtime 忽略。群聊经此注入 group_send tool。
	MCPServers []MCPServerSpec
```

- [ ] **Step 2: 写 pkg/claudecode args 测试（先红）**

`args_test.go` 加：当 `spec.mcpConfig != ""` 时 `buildArgs` 含 `--mcp-config <json>`。

```go
func TestBuildArgs_McpConfig(t *testing.T) {
	args := buildArgs(runSpec{mcpConfig: `{"mcpServers":{}}`})
	So(containsPair(args, "--mcp-config", `{"mcpServers":{}}`), ShouldBeTrue)
}
```

- [ ] **Step 3: 跑看失败 → 实现 flag**

`args.go`：const 区加 `flagMcpConfig flag = "--mcp-config"`；`runSpec` 加 `mcpConfig string`；`buildArgs` 在 `--settings` 附近加：

```go
	if spec.mcpConfig != "" {
		args = append(args, string(flagMcpConfig), spec.mcpConfig)
	}
```

`options.go` 加：

```go
// WithMcpConfig 下发 --mcp-config <json-or-file>。claude CLI 原生兼容 JSON 串或文件路径。
func WithMcpConfig(value string) Option { return func(c *Client) { c.mcpConfig = value } }
```

（`Client` 加 `mcpConfig string` 字段，并在构造 runSpec 处透传 —— 仿现有 `settings` 字段的链路。）

Run: `go test -race -run TestBuildArgs_McpConfig ./pkg/claudecode/` → PASS

- [ ] **Step 4: 写 ccBuildClientOpts 映射测试（先红）**

`session_test.go` 加：`RunRequest.MCPServers` 非空 → opts 含 `WithMcpConfig`（断言生成的 mcp-config JSON 含 server name + url + header）+ `allowedTools` 含 `mcp__group__group_send`。仿现有 `ccBuildClientOpts` 测试不 spawn 真子进程。

- [ ] **Step 5: 实现映射**

`session.go` `ccBuildClientOpts`：当 `spec.Req.MCPServers` 非空时，序列化成 claude mcp-config JSON 并 `WithMcpConfig`，同时把每个 server 的工具加进 `allowedTools`：

```go
	if len(spec.Req.MCPServers) > 0 {
		cfg, allow := buildMcpConfigJSON(spec.Req.MCPServers) // {"mcpServers":{"group":{"type":"http","url":..,"headers":..}}}, ["mcp__group__group_send"]
		opts = append(opts, claudecode.WithMcpConfig(cfg))
		opts = append(opts, claudecode.WithAllowedTools(allow...)) // 若 WithAllowedTools 不存在则一并加(args 已支持 --allowedTools)
	}
```

> `buildMcpConfigJSON` 是本包私有小函数：对每个 `MCPServerSpec` 产 `{"type":"http","url":URL,"headers":Headers}`，工具名约定 `mcp__<Name>__group_send`（MVP 只有 group_send 一个；如需通用可后续扩展为按 server 声明的工具列表）。`WithAllowedTools` 若 options.go 没有就照 `WithSettings` 模式补一个（`args.go` 的 `--allowedTools` 已存在）。

- [ ] **Step 6: 跑测试看通过 + 提交**

Run: `go test -race ./pkg/claudecode/ ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: PASS

```bash
git add internal/pkg/agentruntime/runner.go pkg/claudecode/args.go pkg/claudecode/options.go pkg/claudecode/args_test.go internal/pkg/agentruntime/runtimes/claudecode/session.go internal/pkg/agentruntime/runtimes/claudecode/session_test.go
git commit -m "✨ group: RunRequest.MCPServers + pkg/claudecode --mcp-config + 映射(注入 group_send tool)"
```

---

### Task AM3: `chat_svc.SendRequest` 透传 `MCPServers` / `SystemPromptSuffix`（领域无关）

**Files:**
- Modify: `internal/service/chat_svc/types.go`（`SendRequest` 加两字段）
- Modify: `internal/service/chat_svc/chat.go`（`runTurn` 拼 SystemPrompt + 透传 MCPServers）
- Test: `internal/service/chat_svc/send_passthrough_test.go`

> **chat_svc 不 branch on group**：只是把两个通用可选字段从 `SendRequest` 透传到 `RunRequest`。group_svc 填它们，单聊留空。

- [ ] **Step 1: SendRequest 加字段**

`types.go` `SendRequest` 末尾加：

```go
	// MCPServers 透传到 RunRequest.MCPServers(注入额外 MCP tool server)。群聊用; 单聊空。
	MCPServers []agentruntime.MCPServerSpec `json:"-"`
	// SystemPromptSuffix 追加到 RunRequest.SystemPrompt 之后(群上下文/角色/roster)。群聊用; 单聊空。
	SystemPromptSuffix string `json:"-"`
```

（`json:"-"` —— 这两个字段是服务端内部 plumbing，不暴露给 Wails 前端。）

- [ ] **Step 2: 写测试（先红）**

断言：`Send` 带 `MCPServers`/`SystemPromptSuffix` 时，传给 runtime 的 `RunRequest.SystemPrompt` = `join(agent.GetPrompt()) + suffix` 且 `RunRequest.MCPServers` 透传。用注入的 fake runtime 捕获 RunRequest（仿现有 chat_svc runTurn 测试如何拿到 RunRequest；若无现成 seam，最小验证 `ccBuildClientOpts` 上游的拼接逻辑函数）。

- [ ] **Step 3: 跑看失败 → 实现透传**

`chat.go` 组装 RunRequest 处（`chat.go:2237` 附近）：

```go
	req := agentruntime.RunRequest{
		// ... 现有字段 ...
		SystemPrompt: strings.Join(a.GetPrompt(), "\n") + sendReq.SystemPromptSuffix,
		MCPServers:   sendReq.MCPServers,
	}
```

（把 `SendRequest` 的这两个字段沿 `send → startTurn → runTurn` 线程透传到 RunRequest 组装点；与现有 `req.Text`/`req.Images` 的透传同路径。）

- [ ] **Step 4: 跑测试 + 提交**

Run: `go test -race ./internal/service/chat_svc/`
Expected: PASS（含单聊回归：两字段空时行为不变）

```bash
git add internal/service/chat_svc/types.go internal/service/chat_svc/chat.go internal/service/chat_svc/send_passthrough_test.go
git commit -m "✨ group: SendRequest 透传 MCPServers/SystemPromptSuffix 到 RunRequest(领域无关 plumbing)"
```

---

## Phase B — `group_*` 数据层（entity / migration / repo）

### Task B1: `group_entity` 充血实体

**Files:**
- Create: `internal/model/entity/group_entity/group.go`
- Create: `internal/model/entity/group_entity/member.go`
- Create: `internal/model/entity/group_entity/message.go`
- Test: `internal/model/entity/group_entity/group_test.go`

- [ ] **Step 1: 写实体测试（先红）**

`group_test.go`：

```go
package group_entity_test

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/group_entity"
)

func TestGroupCanAdvance(t *testing.T) {
	Convey("CanAdvance 仅在 run_status 允许推进时为真(无轮数上限)", t, func() {
		So((&group_entity.Group{RunStatus: group_entity.RunIdle}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunRunning}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunWaitingUser}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunPaused}).CanAdvance(), ShouldBeFalse)
		So((&group_entity.Group{RunStatus: group_entity.RunError}).CanAdvance(), ShouldBeFalse)
	})
}

func TestGroupMessageRecipientsRoundTrip(t *testing.T) {
	Convey("SetRecipients/Recipients 应 json round-trip", t, func() {
		m := &group_entity.GroupMessage{}
		m.SetRecipients([]int64{3, 7, 9})
		So(m.Recipients(), ShouldResemble, []int64{3, 7, 9})
	})
	Convey("空收件人返回空切片而非 nil-panic", t, func() {
		So((&group_entity.GroupMessage{}).Recipients(), ShouldBeEmpty)
	})
}

func TestGroupMemberIsCoordinator(t *testing.T) {
	Convey("IsCoordinator 看 role", t, func() {
		So((&group_entity.GroupMember{Role: group_entity.RoleCoordinator}).IsCoordinator(), ShouldBeTrue)
		So((&group_entity.GroupMember{Role: group_entity.RoleMember}).IsCoordinator(), ShouldBeFalse)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race ./internal/model/entity/group_entity/`
Expected: FAIL —— 包/类型不存在。

- [ ] **Step 3: 写三个实体文件**

`group.go`：

```go
// Package group_entity 维护群聊编排的充血实体(Group / GroupMember / GroupMessage)。
package group_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

const (
	RunIdle        = "idle"
	RunRunning     = "running"
	RunPaused      = "paused"
	RunWaitingUser = "waiting_user"
	RunError       = "error"
)

// Group 一个群聊房间。
type Group struct {
	ID                 int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Title              string `gorm:"column:title;type:text;not null;default:''"`
	CoordinatorAgentID int64  `gorm:"column:coordinator_agent_id;type:bigint;not null;default:0"`
	DepartmentID       int64  `gorm:"column:department_id;type:bigint;not null;default:0"`
	ProjectID          int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	RunStatus          string `gorm:"column:run_status;type:text;not null;default:'idle'"`
	RoundCount         int    `gorm:"column:round_count;type:int;not null;default:0"`
	Status             int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime         int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime         int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Group) TableName() string { return "groups" }

func (g *Group) IsActive() bool { return g != nil && g.Status == consts.ACTIVE }

// CanAdvance 是否允许调度推进(无轮数上限, 仅看 run_status)。paused/error 不推进。
func (g *Group) CanAdvance() bool {
	if g == nil {
		return false
	}
	switch g.RunStatus {
	case RunIdle, RunRunning, RunWaitingUser:
		return true
	default:
		return false
	}
}

// Check 校验必填。
func (g *Group) Check(ctx context.Context) error {
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	if strings.TrimSpace(g.Title) == "" {
		return i18n.NewError(ctx, code.GroupTitleRequired)
	}
	if g.CoordinatorAgentID <= 0 {
		return i18n.NewError(ctx, code.GroupCoordinatorRequired)
	}
	return nil
}
```

`member.go`：

```go
package group_entity

const (
	RoleCoordinator = "coordinator"
	RoleMember      = "member"

	MemberActive = "active"
	MemberLeft   = "left"
)

// GroupMember 群内成员(稳定身份, 绑定一条 backing chat_session)。
type GroupMember struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID          int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	AgentID          int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	BackingSessionID int64  `gorm:"column:backing_session_id;type:bigint;not null;default:0"`
	Role             string `gorm:"column:role;type:text;not null;default:'member'"`
	Status           string `gorm:"column:status;type:text;not null;default:'active'"`
	JoinedAt         int64  `gorm:"column:joined_at;type:bigint;not null;default:0"`
}

func (*GroupMember) TableName() string { return "group_members" }

func (m *GroupMember) IsCoordinator() bool { return m != nil && m.Role == RoleCoordinator }
func (m *GroupMember) IsActive() bool      { return m != nil && m.Status == MemberActive }
```

`message.go`：

```go
package group_entity

import "encoding/json"

const (
	SenderKindUser   = "user"
	SenderKindAgent  = "agent"
	SenderKindSystem = "system" // 系统行(X 加入 / 工具审批冒泡)
)

// GroupMessage 群内一条消息(始终存原文)。
type GroupMessage struct {
	ID                 int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID            int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	Seq                int    `gorm:"column:seq;type:int;not null;default:0"`
	SenderKind         string `gorm:"column:sender_kind;type:text;not null;default:'agent'"`
	SenderMemberID     int64  `gorm:"column:sender_member_id;type:bigint;not null;default:0"`
	RecipientMemberIDs string `gorm:"column:recipient_member_ids;type:text;not null;default:'[]'"`
	ToUser             bool   `gorm:"column:to_user;type:boolean;not null;default:false"`
	Content            string `gorm:"column:content;type:text;not null;default:''"`
	SourceMessageID    int64  `gorm:"column:source_message_id;type:bigint;not null;default:0"`
	Createtime         int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
}

func (*GroupMessage) TableName() string { return "group_messages" }

// Recipients 反序列化收件成员 id 列表(空/坏数据返回空切片)。
func (m *GroupMessage) Recipients() []int64 {
	if m == nil || strings.TrimSpace(m.RecipientMemberIDs) == "" {
		return []int64{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(m.RecipientMemberIDs), &ids); err != nil {
		return []int64{}
	}
	return ids
}

// SetRecipients 序列化收件成员 id 列表。
func (m *GroupMessage) SetRecipients(ids []int64) {
	if ids == nil {
		ids = []int64{}
	}
	b, _ := json.Marshal(ids)
	m.RecipientMemberIDs = string(b)
}
```

> `message.go` 用到 `strings`，补 import：`import ("encoding/json"; "strings")`。

- [ ] **Step 4: 跑测试看通过**（先跳过 code.* 未定义——Task C7 才加错误码；为不阻塞，本 Task 可临时在 `group_test.go` 只测不依赖 `Check` 的方法，`Check` 的测试挪到 C7 之后。或先把 C7 的错误码常量提前加进来。**推荐：现在就先做 Task C7 的 code 常量**，避免 `group.go` 引用未定义符号。）

为保持线性可编译，调整：**先执行 Task C7（加 19000 段错误码常量），再回到本步**。然后：

Run: `go test -race ./internal/model/entity/group_entity/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/model/entity/group_entity/
git commit -m "✨ group: group_entity 充血实体(Group/GroupMember/GroupMessage)"
```

---

### Task B2: 三张群表迁移

**Files:**
- Create: `migrations/202606030002_group.go`
- Modify: `migrations/migrations.go`
- Test: `migrations/202606030002_group_test.go`

- [ ] **Step 1: 写迁移测试（先红）**

```go
func TestMigration202606030002Group(t *testing.T) {
	Convey("迁移后 groups/group_members/group_messages 三表存在", t, func() {
		gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		So(err, ShouldBeNil)
		m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
		So(m.Migrate(), ShouldBeNil)
		So(gdb.Migrator().HasTable("groups"), ShouldBeTrue)
		So(gdb.Migrator().HasTable("group_members"), ShouldBeTrue)
		So(gdb.Migrator().HasTable("group_messages"), ShouldBeTrue)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestMigration202606030002Group ./migrations/`
Expected: FAIL（`migration202606030002` 未定义）。

- [ ] **Step 3: 写迁移**

`migrations/202606030002_group.go`：

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606030002 建群聊三表: groups / group_members / group_messages。
func migration202606030002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606030002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL DEFAULT '',
	coordinator_agent_id INTEGER NOT NULL DEFAULT 0,
	department_id INTEGER NOT NULL DEFAULT 0,
	project_id INTEGER NOT NULL DEFAULT 0,
	run_status TEXT NOT NULL DEFAULT 'idle',
	round_count INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_groups_status ON groups(status, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_members (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	agent_id INTEGER NOT NULL DEFAULT 0,
	backing_session_id INTEGER NOT NULL DEFAULT 0,
	role TEXT NOT NULL DEFAULT 'member',
	status TEXT NOT NULL DEFAULT 'active',
	joined_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id, status)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_group_members_group_agent ON group_members(group_id, agent_id)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	seq INTEGER NOT NULL DEFAULT 0,
	sender_kind TEXT NOT NULL DEFAULT 'agent',
	sender_member_id INTEGER NOT NULL DEFAULT 0,
	recipient_member_ids TEXT NOT NULL DEFAULT '[]',
	to_user INTEGER NOT NULL DEFAULT 0,
	content TEXT NOT NULL DEFAULT '',
	source_message_id INTEGER NOT NULL DEFAULT 0,
	createtime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_messages_group_seq ON group_messages(group_id, seq)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS group_messages`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS group_members`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS groups`).Error
		},
	}
}
```

- [ ] **Step 4: append 到 `migrationList()`**

```go
		migration202606030001(), // chat_sessions.group_id + index
		migration202606030002(), // groups + group_members + group_messages
	}
}
```

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestMigration202606030002Group ./migrations/`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add migrations/202606030002_group.go migrations/202606030002_group_test.go migrations/migrations.go
git commit -m "✨ group: groups/group_members/group_messages 三表迁移"
```

---

### Task B3: `group_repo`（接口 + 实现 + mock）

**Files:**
- Create: `internal/repository/group_repo/group.go`（`GroupRepo` + `GroupMemberRepo` + `GroupMessageRepo` 三接口 + 实现 + `//go:generate`）
- Create (生成): `internal/repository/group_repo/mock_group_repo/mock_group.go`
- Test: `internal/repository/group_repo/group_test.go`

- [ ] **Step 1: 写 sqlmock 仓储测试（先红）**

`group_test.go`（沿用仓库现有 sqlmock 风格 + `testutils.Database(t)`，参考 `internal/repository/chat_repo/*_test.go`）：

```go
func TestGroupRepo_CreateAndList(t *testing.T) {
	Convey("Create 写入 + List 只返回 active", t, func() {
		ctx, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec(`INSERT INTO .groups.`).WillReturnResult(sqlmock.NewResult(1, 1))
		mock.ExpectCommit()
		So(group_repo.Group().Create(ctx, &group_entity.Group{Title: "队", CoordinatorAgentID: 1, Status: consts.ACTIVE}), ShouldBeNil)

		mock.ExpectQuery(`SELECT \* FROM .groups.*status. = .*`).
			WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow(1, "队"))
		rows, err := group_repo.Group().List(ctx)
		So(err, ShouldBeNil)
		So(len(rows), ShouldEqual, 1)
		So(mock.ExpectationsWereMet(), ShouldBeNil)
	})
}

func TestGroupMemberRepo_ListByGroup(t *testing.T) {
	Convey("ListByGroup 只返回 active 成员", t, func() {
		ctx, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT \* FROM .group_members.*group_id. = .*`).
			WithArgs(int64(5), group_entity.MemberActive).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id"}).AddRow(1, 3))
		rows, err := group_repo.Member().ListByGroup(ctx, 5)
		So(err, ShouldBeNil)
		So(len(rows), ShouldEqual, 1)
	})
}

func TestGroupMessageRepo_NextSeqAndCreate(t *testing.T) {
	Convey("NextSeq 取 max(seq)+1", t, func() {
		ctx, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT .*max.*seq.* FROM .group_messages.`).
			WithArgs(int64(5)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(7))
		n, err := group_repo.Message().NextSeq(ctx, 5)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 8)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race ./internal/repository/group_repo/`
Expected: FAIL —— 包不存在。

- [ ] **Step 3: 写 `group.go`（三接口 + 实现）**

```go
// Package group_repo 提供群聊 Group / Member / Message 的持久化访问。
package group_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/group_entity"
)

//go:generate mockgen -source group.go -destination mock_group_repo/mock_group.go

// GroupRepo 群房间仓储。
type GroupRepo interface {
	Create(ctx context.Context, g *group_entity.Group) error
	Update(ctx context.Context, g *group_entity.Group) error
	Find(ctx context.Context, id int64) (*group_entity.Group, error)
	List(ctx context.Context) ([]*group_entity.Group, error)
}

// GroupMemberRepo 群成员仓储。
type GroupMemberRepo interface {
	Create(ctx context.Context, m *group_entity.GroupMember) error
	Update(ctx context.Context, m *group_entity.GroupMember) error
	Find(ctx context.Context, id int64) (*group_entity.GroupMember, error)
	FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error)
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMember, error)
}

// GroupMessageRepo 群消息仓储。
type GroupMessageRepo interface {
	Create(ctx context.Context, m *group_entity.GroupMessage) error
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMessage, error)
	NextSeq(ctx context.Context, groupID int64) (int, error)
}

var (
	defaultGroup   GroupRepo
	defaultMember  GroupMemberRepo
	defaultMessage GroupMessageRepo
)

func Group() GroupRepo                       { return defaultGroup }
func Member() GroupMemberRepo                { return defaultMember }
func Message() GroupMessageRepo              { return defaultMessage }
func RegisterGroup(impl GroupRepo)           { defaultGroup = impl }
func RegisterMember(impl GroupMemberRepo)    { defaultMember = impl }
func RegisterMessage(impl GroupMessageRepo)  { defaultMessage = impl }
func NewGroup() GroupRepo                    { return &groupRepo{} }
func NewMember() GroupMemberRepo             { return &memberRepo{} }
func NewMessage() GroupMessageRepo           { return &messageRepo{} }

type groupRepo struct{}
type memberRepo struct{}
type messageRepo struct{}

func (r *groupRepo) Create(ctx context.Context, g *group_entity.Group) error {
	now := time.Now().UnixMilli()
	if g.Createtime == 0 {
		g.Createtime = now
	}
	g.Updatetime = now
	return db.Ctx(ctx).Create(g).Error
}

func (r *groupRepo) Update(ctx context.Context, g *group_entity.Group) error {
	g.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Model(&group_entity.Group{}).
		Where("id = ? AND status = ?", g.ID, consts.ACTIVE).
		Updates(map[string]any{
			"title":      g.Title,
			"run_status": g.RunStatus,
			"round_count": g.RoundCount,
			"status":     g.Status,
			"updatetime": g.Updatetime,
		}).Error
}

func (r *groupRepo) Find(ctx context.Context, id int64) (*group_entity.Group, error) {
	out := &group_entity.Group{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *groupRepo) List(ctx context.Context) ([]*group_entity.Group, error) {
	var rows []*group_entity.Group
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("updatetime DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *memberRepo) Create(ctx context.Context, m *group_entity.GroupMember) error {
	if m.JoinedAt == 0 {
		m.JoinedAt = time.Now().UnixMilli()
	}
	return db.Ctx(ctx).Create(m).Error
}

func (r *memberRepo) Update(ctx context.Context, m *group_entity.GroupMember) error {
	return db.Ctx(ctx).Model(&group_entity.GroupMember{}).
		Where("id = ?", m.ID).
		Updates(map[string]any{
			"backing_session_id": m.BackingSessionID,
			"role":               m.Role,
			"status":             m.Status,
		}).Error
}

func (r *memberRepo) Find(ctx context.Context, id int64) (*group_entity.GroupMember, error) {
	out := &group_entity.GroupMember{}
	err := db.Ctx(ctx).Where("id = ?", id).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *memberRepo) FindByGroupAndAgent(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error) {
	out := &group_entity.GroupMember{}
	err := db.Ctx(ctx).Where("group_id = ? AND agent_id = ?", groupID, agentID).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *memberRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMember, error) {
	var rows []*group_entity.GroupMember
	err := db.Ctx(ctx).Where("group_id = ? AND status = ?", groupID, group_entity.MemberActive).
		Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *messageRepo) Create(ctx context.Context, m *group_entity.GroupMessage) error {
	if m.Createtime == 0 {
		m.Createtime = time.Now().UnixMilli()
	}
	return db.Ctx(ctx).Create(m).Error
}

func (r *messageRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupMessage, error) {
	var rows []*group_entity.GroupMessage
	err := db.Ctx(ctx).Where("group_id = ?", groupID).Order("seq ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (r *messageRepo) NextSeq(ctx context.Context, groupID int64) (int, error) {
	var maxSeq int
	err := db.Ctx(ctx).Model(&group_entity.GroupMessage{}).
		Where("group_id = ?", groupID).
		Select("COALESCE(MAX(seq), 0)").Scan(&maxSeq).Error
	if err != nil {
		return 0, err
	}
	return maxSeq + 1, nil
}
```

- [ ] **Step 4: 生成 mock + bootstrap 注册**

Run: `make mock`（生成 `mock_group_repo/mock_group.go`）

在 `internal/bootstrap/cago.go`（约 line 88–100，已有一串 `chat_repo.RegisterSession(...)` / `project_repo.RegisterProject(...)` 等真实注册——已核对）末尾并列追加：

```go
group_repo.RegisterGroup(group_repo.NewGroup())
group_repo.RegisterMember(group_repo.NewMember())
group_repo.RegisterMessage(group_repo.NewMessage())
```

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race ./internal/repository/group_repo/`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/repository/group_repo/ internal/bootstrap/cago.go
git commit -m "✨ group: group_repo(Group/Member/Message 三仓储 + mock + 注册)"
```

---

## Phase C — `group_svc` 编排引擎

### Task C7: 错误码 19000 段（提前做，B1/C 都依赖）

**Files:**
- Modify: `internal/pkg/code/code.go`（新增 Group 19000~19999 段）
- Modify: `internal/pkg/code/zh_cn.go`
- Modify: `internal/pkg/code/en.go`

> **已对照真实 `code.go` 核对**：17000=Chat、18000=Project、18100=ProjectLocation、20300+=Server；**无 19000 段** → 19000 空闲，分给 group。（注：本仓库当前**没有** issue 错误码段。）

- [ ] **Step 1: code.go 加段**

在 `code.go` 末尾（Project Location 18100 段之后）追加新段：

```go
// Group 群聊编排 19000~19999
const (
	GroupNotFound            = iota + 19000 // 群不存在
	GroupTitleRequired                      // 群名不能为空
	GroupCoordinatorRequired                // 协调者不能为空
	GroupMemberNotFound                     // 群成员不存在
	GroupMemberExists                       // 该 agent 已在群中
	GroupMemberLimit                        // 群成员数已达上限
	GroupNotRecruitable                     // 该 agent 不在可招募名单
	GroupBackendUnsupported                 // 该 agent 的后端不支持群聊(缺 CapMCPTools)
)
```

- [ ] **Step 2: zh_cn.go / en.go 补文案**

`zh_cn.go`：

```go
// Group
GroupNotFound:            "群不存在",
GroupTitleRequired:       "群名不能为空",
GroupCoordinatorRequired: "协调者不能为空",
GroupMemberNotFound:      "群成员不存在",
GroupMemberExists:        "该 agent 已在群中",
GroupMemberLimit:         "群成员数已达上限",
GroupNotRecruitable:      "该 agent 不在可招募名单",
GroupBackendUnsupported:  "该 agent 的后端不支持群聊",
```

`en.go`：

```go
// Group
GroupNotFound:            "Group not found",
GroupTitleRequired:       "Group title is required",
GroupCoordinatorRequired: "Coordinator is required",
GroupMemberNotFound:      "Group member not found",
GroupMemberExists:        "Agent is already in the group",
GroupMemberLimit:         "Group member limit reached",
GroupNotRecruitable:      "Agent is not recruitable",
GroupBackendUnsupported:  "Agent backend does not support group chat",
```

- [ ] **Step 3: 编译确认**

Run: `go build ./internal/pkg/code/`
Expected: 通过。

- [ ] **Step 4: 提交**

```bash
git add internal/pkg/code/
git commit -m "✨ group: 错误码 19000 段 + zh/en 文案"
```

> 完成后回到 **Task B1 Step 4**（实体 `Check` 现在可编译）。

---

### Task C1: 窄 `chatGateway` 接口 + adapter + mock

**Files:**
- Create: `internal/service/group_svc/gateway.go`（窄接口 + 真实 adapter + `//go:generate`）
- Create (生成): `internal/service/group_svc/mock_group_svc/mock_gateway.go`

> ISP：group_svc 只声明它用到的 4 个 chat_svc 方法，不依赖 24 方法的胖 `ChatSvc`。adapter 委托给 `chat_svc.Chat()`。

- [ ] **Step 1: 写 gateway.go**

```go
package group_svc

import (
	"context"

	"agentre/internal/service/chat_svc"
)

//go:generate mockgen -source gateway.go -destination mock_group_svc/mock_gateway.go

// chatGateway 是 group_svc 对 chat_svc 的窄依赖(只用这 4 个 seam)。
type chatGateway interface {
	EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
}

// chatSvcGateway 委托给 chat_svc 默认单例。
type chatSvcGateway struct{}

func (chatSvcGateway) EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error) {
	return chat_svc.Chat().EnsureGroupMemberSession(ctx, agentID, projectID, groupID)
}
func (chatSvcGateway) Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
	return chat_svc.Chat().Send(ctx, req)
}
func (chatSvcGateway) ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func()) {
	return chat_svc.Chat().ObserveTurn(sessionID)
}
func (chatSvcGateway) Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error) {
	return chat_svc.Chat().Stop(ctx, req)
}
```

- [ ] **Step 2: 生成 mock**

Run: `make mock`
Expected: 生成 `internal/service/group_svc/mock_group_svc/mock_gateway.go`（package `mock_group_svc`，type `MockchatGateway`）。

> 注：mockgen 对小写(未导出)接口仍能生成 mock；目标包 `mock_group_svc` 与被测包 `group_svc` 不同，mock 需能被 `group_svc_test`（外部测试包）引用。**若 mockgen 因接口未导出而生成到非法引用**，将接口名改为导出 `ChatGateway`（仍只在包内用），mock 即可跨包引用。本计划后续按 `ChatGateway`（导出）书写，规避该坑：

将 `gateway.go` 的 `chatGateway` 全部改为导出名 `ChatGateway`（接口 + adapter 的方法接收者保持 `chatSvcGateway`）。重跑 `make mock`。

- [ ] **Step 3: 提交**

```bash
git add internal/service/group_svc/gateway.go internal/service/group_svc/mock_group_svc/
git commit -m "✨ group: group_svc→chat_svc 窄网关接口 ChatGateway + adapter + mock"
```

---

### Task C2:（设计说明）mention 解析一分为二 —— 无独立后端任务

> tool-send 架构下**不存在后端文本路由解析**（原 `ParseMentions` 纯函数已废弃）。mention 现在两条线：
> - **路由（结构化）**：成员经 `group_send(mentions []string)`、用户经 UI 解析出的 `recipientMemberIds` —— **名字→member id 的解析是 C5 `resolveMentionNames`**（在 ingest.go），不是独立的文本正则解析。
> - **展示（标记化）**：群消息正文里的 `@名字`/`<mention>名字</mention>` 渲染成高亮可点 chip + 点击跳转 —— **是前端 util，见 Task E5**。
>
> 故本 Task **无后端代码**，仅作为占位说明，避免读者去找一个被删掉的 `ParseMentions`。直接进 C3。

---

### Task C3: `group_svc` 骨架 + CRUD（Create/Load/AddMember/RemoveMember）

**Files:**
- Create: `internal/service/group_svc/group.go`（接口 + 单例 + impl struct + CRUD）
- Create: `internal/service/group_svc/types.go`（req/resp DTO）
- Test: `internal/service/group_svc/group_test.go`

- [ ] **Step 1: 写 types.go**

```go
package group_svc

import (
	"agentre/internal/model/entity/group_entity"
)

type CreateGroupRequest struct {
	Title              string
	CoordinatorAgentID int64
	DepartmentID       int64
	ProjectID          int64
}

type GroupDetail struct {
	Group    *group_entity.Group
	Members  []*group_entity.GroupMember
	Messages []*group_entity.GroupMessage
}

type SendGroupMessageRequest struct {
	GroupID           int64
	Text              string
	RecipientMemberIDs []int64 // 可选: 显式收件人(优先于解析)
	ToUser            bool
}

const maxMembers = 8
```

- [ ] **Step 2: 写 CRUD 测试（先红）**

```go
func TestGroupSvc_CreateGroup_AddsCoordinatorMember(t *testing.T) {
	Convey("建群应建协调者成员 + backing session", t, func() {
		ctx, _ := testutils.Database(t) // 仅给 ctx; repo 全 mock 时可不用真库
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(1), int64(0), int64(5)).Return(int64(11), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleCoordinator)
				So(m.BackingSessionID, ShouldEqual, 11)
				return nil
			})

		svc := group_svc.NewForTest(gw)
		detail, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", CoordinatorAgentID: 1})
		So(err, ShouldBeNil)
		So(detail.Group.ID, ShouldEqual, 5)
	})
}
```

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run TestGroupSvc_CreateGroup ./internal/service/group_svc/`
Expected: FAIL —— `group_svc.NewForTest` / `CreateGroup` 未定义。

- [ ] **Step 4: 写 group.go 骨架 + CRUD**

```go
// Package group_svc 提供群聊编排应用服务(架在 chat_svc 之上)。
package group_svc

import (
	"context"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/group_repo"
)

// Emitter 群事件出口(由 app 层注入 → wailsruntime.EventsEmit)。
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}
type EmitterFunc func(ctx context.Context, name string, payload any)
func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}
type NoopEmitter struct{}
func (NoopEmitter) Emit(context.Context, string, any) {}

// GroupSvc 群聊编排服务。
type GroupSvc interface {
	ListGroups(ctx context.Context) ([]*group_entity.Group, error)
	CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error)
	LoadGroup(ctx context.Context, id int64) (*GroupDetail, error)
	SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error
	// IngestAgentMessage 是 group_send MCP tool 的服务端入口(MCP handler 调, 非 Wails)。见 Task C5。
	IngestAgentMessage(ctx context.Context, memberID int64, body string, mentions []string) error
	AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error)
	RemoveGroupMember(ctx context.Context, memberID int64) error
	StopGroup(ctx context.Context, id int64) error
	PauseGroup(ctx context.Context, id int64) error
	ResumeGroup(ctx context.Context, id int64) error
	RenameGroup(ctx context.Context, id int64, title string) error
	ArchiveGroup(ctx context.Context, id int64) error
}

type groupSvc struct {
	gw         ChatGateway
	emitter    Emitter
	now        func() int64
	mu         sync.Mutex                // 保护 schedulers
	schedulers map[int64]*scheduler      // groupID -> 运行态(Task C5)
}

var defaultGroup GroupSvc = newGroupSvc(chatSvcGateway{}, NoopEmitter{})

func Default() GroupSvc          { return defaultGroup }
func SetDefault(s GroupSvc)      { defaultGroup = s }
func SetEmitter(e Emitter) {
	if g, ok := defaultGroup.(*groupSvc); ok && e != nil {
		g.emitter = e
	}
}

func newGroupSvc(gw ChatGateway, e Emitter) *groupSvc {
	return &groupSvc{
		gw:         gw,
		emitter:    e,
		now:        func() int64 { return time.Now().UnixMilli() },
		schedulers: map[int64]*scheduler{},
	}
}

// NewForTest 注入 mock 网关构造服务(单测用)。
func NewForTest(gw ChatGateway) GroupSvc { return newGroupSvc(gw, NoopEmitter{}) }

func (s *groupSvc) ListGroups(ctx context.Context) ([]*group_entity.Group, error) {
	return group_repo.Group().List(ctx)
}

func (s *groupSvc) CreateGroup(ctx context.Context, req *CreateGroupRequest) (*GroupDetail, error) {
	g := &group_entity.Group{
		Title:              req.Title,
		CoordinatorAgentID: req.CoordinatorAgentID,
		DepartmentID:       req.DepartmentID,
		ProjectID:          req.ProjectID,
		RunStatus:          group_entity.RunIdle,
		Status:             consts.ACTIVE,
	}
	if err := g.Check(ctx); err != nil {
		return nil, err
	}
	if err := group_repo.Group().Create(ctx, g); err != nil {
		return nil, err
	}
	// 协调者成员 + backing session
	if _, err := s.ensureMember(ctx, g, req.CoordinatorAgentID, group_entity.RoleCoordinator); err != nil {
		return nil, err
	}
	return s.LoadGroup(ctx, g.ID)
}

// ensureMember 幂等地把 agent 加入群(建 member + backing session)。
func (s *groupSvc) ensureMember(ctx context.Context, g *group_entity.Group, agentID int64, role string) (*group_entity.GroupMember, error) {
	existing, err := group_repo.Member().FindByGroupAndAgent(ctx, g.ID, agentID)
	if err != nil {
		return nil, err
	}
	if existing != nil && existing.IsActive() {
		return existing, nil
	}
	sessID, err := s.gw.EnsureGroupMemberSession(ctx, agentID, g.ProjectID, g.ID)
	if err != nil {
		return nil, err
	}
	m := &group_entity.GroupMember{
		GroupID:          g.ID,
		AgentID:          agentID,
		BackingSessionID: sessID,
		Role:             role,
		Status:           group_entity.MemberActive,
		JoinedAt:         s.now(),
	}
	if err := group_repo.Member().Create(ctx, m); err != nil {
		return nil, err
	}
	return m, nil
}

func (s *groupSvc) LoadGroup(ctx context.Context, id int64) (*GroupDetail, error) {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	msgs, err := group_repo.Message().ListByGroup(ctx, id)
	if err != nil {
		return nil, err
	}
	return &GroupDetail{Group: g, Members: members, Messages: msgs}, nil
}

func (s *groupSvc) AddGroupMember(ctx context.Context, groupID, agentID int64) (*group_entity.GroupMember, error) {
	g, err := group_repo.Group().Find(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	if len(members) >= maxMembers {
		return nil, i18n.NewError(ctx, code.GroupMemberLimit)
	}
	if !s.backendSupportsGroup(ctx, agentID) { // CapMCPTools 门控(helper 在 Task C5 实现)
		return nil, i18n.NewError(ctx, code.GroupBackendUnsupported)
	}
	return s.ensureMember(ctx, g, agentID, group_entity.RoleMember)
}

// 注: CreateGroup 的协调者、maybeRecruit 的被招募者同样要过 backendSupportsGroup ——
// 协调者在 CreateGroup 里校验(不支持则建群失败), recruit 已在 C5 maybeRecruit 内校验。

func (s *groupSvc) RemoveGroupMember(ctx context.Context, memberID int64) error {
	m, err := group_repo.Member().Find(ctx, memberID)
	if err != nil {
		return err
	}
	if m == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	m.Status = group_entity.MemberLeft
	return group_repo.Member().Update(ctx, m)
}

// RenameGroup/ArchiveGroup 见 Task C6; Stop/Pause/Resume/SendGroupMessage 见 C4/C5/C6。
```

> 给 group_repo 的 `GroupMemberRepo` 补 `FindByGroupAndAgent`（已在 B3 接口含）。`scheduler` 类型在 C5 定义——本 Task 先让 struct 字段存在但不实现调度，CRUD 测试不触发调度。为可编译，C5 之前先在 group.go 加一个最小占位：`type scheduler struct{}`，C5 再替换为完整定义。

- [ ] **Step 5: 跑测试看通过**

Run: `make mock && go test -race -run TestGroupSvc_CreateGroup ./internal/service/group_svc/`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/service/group_svc/group.go internal/service/group_svc/types.go internal/service/group_svc/group_test.go
git commit -m "✨ group: group_svc 骨架 + CRUD(建群/加载/加成员/移除)"
```

---

### Task C4: `SendGroupMessage` —— 结构化收件人 + 入队 + 落库

**Files:**
- Modify: `internal/service/group_svc/group.go`（`SendGroupMessage` + `resolveRecipientsFromRequest` + `persistMessage`）
- Test: `internal/service/group_svc/send_test.go`

> **用户侧收件人永远是结构化的**（前端把 composer 里的 `@名字` 解析成 `recipientMemberIds` 传后端）—— 后端**不**做文本 mention 解析。

> 本 Task 只做「post 一条消息：解析收件人 → 落 group_message → 把 agent 收件人入队」，**不**触发实际 turn（调度在 C5）。让 `kick` 暂为 no-op，便于隔离测试。

- [ ] **Step 1: 写测试（先红）**

```go
func TestSendGroupMessage_ResolvesMentionsAndPersists(t *testing.T) {
	Convey("用户发消息(结构化收件人=后端 member2) → 落库 + 收件人=后端", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t); defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunIdle, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 1, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
			{ID: 2, AgentID: 2, Status: group_entity.MemberActive},
		}, nil).AnyTimes()
		// 名字解析依赖 agent 名 → 用一个可注入的 nameResolver(见实现说明)
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindUser)
				So(m.Recipients(), ShouldResemble, []int64{2}) // 后端=member 2
				return nil
			})

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端"})
		// 前端已把 composer 里的 @后端 解析成结构化收件人 [2]
		err := svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "麻烦后端看下", RecipientMemberIDs: []int64{2}})
		So(err, ShouldBeNil)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestSendGroupMessage_ResolvesMentionsAndPersists ./internal/service/group_svc/`
Expected: FAIL。

- [ ] **Step 3: 实现 name 解析注入 + SendGroupMessage**

成员显示名来自 Agent 实体。为保持 service 单测可纯 mock（不连 agent_repo 真库），用一个可注入的 `nameResolver func(agentID int64) string`，默认实现走 `agent_svc`/`agent_repo` 取名：

在 `groupSvc` 加字段 `names func(ctx context.Context, agentID int64) string`，默认：

```go
func defaultNameResolver(ctx context.Context, agentID int64) string {
	a, err := agent_repo.Agent().Find(ctx, agentID) // 按实际 agent_repo accessor 调整
	if err != nil || a == nil {
		return ""
	}
	return a.Name
}
```

`newGroupSvc` 里 `names: defaultNameResolver`；加测试构造器：

```go
func NewForTestWithNames(gw ChatGateway, names map[int64]string) GroupSvc {
	s := newGroupSvc(gw, NoopEmitter{})
	s.names = func(_ context.Context, id int64) string { return names[id] }
	return s
}
```

实现 `SendGroupMessage` + `resolveRecipients` + `persistMessage`：

```go
func (s *groupSvc) SendGroupMessage(ctx context.Context, req *SendGroupMessageRequest) error {
	g, err := group_repo.Group().Find(ctx, req.GroupID)
	if err != nil {
		return err
	}
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return err
	}
	recipientIDs, toUser := s.resolveRecipientsFromRequest(req)
	if len(recipientIDs) == 0 && !toUser { // 用户没选收件人 → 默认投协调者(spec §17)
		for _, m := range members {
			if m.IsCoordinator() {
				recipientIDs = []int64{m.ID}
				break
			}
		}
	}
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindUser, 0, req.Text, recipientIDs, toUser, 0); err != nil {
		return err
	}
	// 用户发言重置 round_count(仅 UI 计数)
	g.RoundCount = 0
	_ = group_repo.Group().Update(ctx, g)
	// 把 agent 收件人入队 + 踢调度器(C5)。签名: enqueueDeliveries(groupID, recipientIDs, content, fromName)
	s.enqueueDeliveries(g.ID, recipientIDs, req.Text, "你"/*用户抬头*/)
	s.kick(ctx, g.ID)
	return nil
}

// resolveRecipientsFromRequest 用户消息的收件人 —— 前端已解析成结构化 recipientMemberIDs/toUser。
// 后端不做文本 mention 解析(agent 侧的名字→id 解析在 C5 resolveMentionNames)。
func (s *groupSvc) resolveRecipientsFromRequest(req *SendGroupMessageRequest) ([]int64, bool) {
	return req.RecipientMemberIDs, req.ToUser
}

func (s *groupSvc) persistMessage(ctx context.Context, g *group_entity.Group, kind string, senderMemberID int64, content string, recipients []int64, toUser bool, sourceMsgID int64) (*group_entity.GroupMessage, error) {
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
		Createtime:      s.now(),
	}
	m.SetRecipients(recipients)
	if err := group_repo.Message().Create(ctx, m); err != nil {
		return nil, err
	}
	s.emitter.Emit(ctx, groupEventName(g.ID), map[string]any{"kind": "message", "message": m})
	return m, nil
}

func groupEventName(groupID int64) string { return "group:event:" + strconv.FormatInt(groupID, 10) }
```

为本 Task 让 `enqueueDeliveries` / `kick` 暂为占位（C5 实现真逻辑）：

```go
func (s *groupSvc) enqueueDeliveries(groupID int64, recipientIDs []int64, content, fromName string) {}
func (s *groupSvc) kick(ctx context.Context, groupID int64)                                          {}
```

- [ ] **Step 4: 跑测试看通过**

Run: `go test -race -run TestSendGroupMessage_ResolvesMentionsAndPersists ./internal/service/group_svc/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/group_svc/group.go internal/service/group_svc/send_test.go
git commit -m "✨ group: SendGroupMessage 解析收件人 + 落 group_message + 重置轮数"
```

---

### Task C5: 并发 fan-out 调度器 + tool 路由入口（核心）

**Files:**
- Create: `internal/service/group_svc/scheduler.go`（`scheduler` 状态 + `kick` + `launchDelivery` + `handleTurnResult` 生命周期）
- Create: `internal/service/group_svc/ingest.go`（`IngestAgentMessage` 路由 + `resolveMentionNames` + `applyFallback` + `maybeRecruit`）
- Modify: `internal/service/group_svc/group.go`（移除 C4 的占位 `enqueueDeliveries`/`kick`/`type scheduler struct{}`；`GroupSvc` 接口加 `IngestAgentMessage`）
- Test: `internal/service/group_svc/scheduler_test.go`

> **tool-send 架构**：每群一个 `scheduler`，持 `pending map[memberID][]delivery`（每成员 FIFO）+ `inflight map[memberID]bool`。`kick` 对「有 pending 且 not inflight」的成员各起一个 turn（**跨成员并发，同成员串行**，无并发 cap）。
> - **路由 = `IngestAgentMessage(memberID, body, mentions[])`**（MCP handler 在成员 turn 进行中调，可多次）：解析 mentions 名字 → 落库 → 入队 → kick。**eager**：tool 一调用就路由，不等 turn 结束。
> - **`handleTurnResult` 只管生命周期**：turn 结束 → 释放 inflight 槽 + kick；队列空且无 inflight → `waiting_user`。**不解析文本、不落消息**（消息来自 tool 调用）。

- [ ] **Step 1: 写 fan-out + tool 路由测试（先红）**

```go
func TestScheduler_FanOutThenToolRoute(t *testing.T) {
	Convey("用户发给[后端,前端] → 两 turn 并发发起; 后端 turn 内调 group_send @前端 → 前端被投递", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t); defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
			{ID: 2, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
			{ID: 3, AgentID: 3, BackingSessionID: 13, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(members[1], nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		ch12 := make(chan chat_svc.TurnResult, 1)
		ch13 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch12), func() {}).AnyTimes()
		gw.EXPECT().ObserveTurn(int64(13)).Return((<-chan chat_svc.TurnResult)(ch13), func() {}).AnyTimes()
		sent := make(chan int64, 8)
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				// 群投递必须带 MCP server + 群 system prompt suffix(注入 group_send tool + 上下文)
				So(len(req.MCPServers), ShouldBeGreaterThan, 0)
				So(req.SystemPromptSuffix, ShouldNotBeBlank)
				sent <- req.SessionID
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端", 3: "前端"})
		// 用户发给 [后端=member2, 前端=member3]（前端 UI 已解析成结构化收件人）
		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "开工", RecipientMemberIDs: []int64{2, 3}}), ShouldBeNil)

		got := map[int64]bool{}
		for i := 0; i < 2; i++ {
			select {
			case sid := <-sent:
				got[sid] = true
			case <-time.After(2 * time.Second):
				t.Fatal("fan-out 投递不足")
			}
		}
		So(got[12] && got[13], ShouldBeTrue)

		// 先让前端(13) turn 结束释放槽, 再模拟后端调 group_send @前端 → 前端被二次投递
		ch13 <- chat_svc.TurnResult{SessionID: 13}
		time.Sleep(50 * time.Millisecond) // 等 handleTurnResult 释放 13 的 inflight
		So(svc.IngestAgentMessage(ctx, 2 /*后端 member*/, "做好了", []string{"前端"}), ShouldBeNil)
		select {
		case sid := <-sent:
			So(sid, ShouldEqual, 13)
		case <-time.After(2 * time.Second):
			t.Fatal("tool 路由二次投递未发生")
		}
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestScheduler_FanOutThenToolRoute ./internal/service/group_svc/`
Expected: FAIL —— `IngestAgentMessage` / 真实调度未实现。

- [ ] **Step 3: 写 scheduler.go（调度 + 生命周期）**

先删 group.go 里 C4 的占位 `type scheduler struct{}`、`enqueueDeliveries`、`kick`。`scheduler` / `newScheduler` / `schedulerFor` / `enqueueDeliveries` / `markDone` / `transitionRunStatus` 同前一版（保持不变，从略——见本仓库历史版本或下方完整块）。**变化在 `launchDelivery`（带 MCP + prompt）与 `handleTurnResult`（生命周期 only）**：

```go
// launchDelivery: 订阅 ObserveTurn → Send(带 MCP tool + 群 system prompt) → 后台等 turn 结束(仅生命周期)。
func (s *groupSvc) launchDelivery(g *group_entity.Group, members []*group_entity.GroupMember, d delivery, m *group_entity.GroupMember) {
	ch, cancel := s.gw.ObserveTurn(m.BackingSessionID)
	bg := context.Background()
	req := &chat_svc.SendRequest{
		SessionID:          m.BackingSessionID,
		AgentID:            m.AgentID,
		Text:               "(来自 " + d.fromName + ")\n" + d.content,
		MCPServers:         s.buildGroupMCP(g, m),               // 注入 group_send tool(带 per-member token)
		SystemPromptSuffix: s.buildGroupSystemPrompt(g, members, m), // 角色 + roster + tool 用法 + worktree 引导
	}
	if _, err := s.gw.Send(bg, req); err != nil {
		cancel()
		logger.Ctx(bg).Warn("group_svc.launchDelivery: send failed", zap.Int64("memberId", m.ID), zap.Error(err))
		s.markDone(m.GroupID, m.ID)
		s.kick(bg, m.GroupID)
		return
	}
	gogo.Go(func() error {
		defer cancel()
		res := <-ch
		s.handleTurnResult(context.Background(), m.GroupID, m, res)
		return nil
	}, gogo.WithIgnorePanic())
}

// handleTurnResult: 仅生命周期 —— 释放 inflight 槽 + kick。消息路由在 IngestAgentMessage(tool)。
func (s *groupSvc) handleTurnResult(ctx context.Context, groupID int64, m *group_entity.GroupMember, res chat_svc.TurnResult) {
	s.markDone(groupID, m.ID)
	if res.Err != nil {
		logger.Ctx(ctx).Warn("group_svc.handleTurnResult: member turn error", zap.Int64("memberId", m.ID), zap.Error(res.Err))
	}
	s.kick(ctx, groupID) // 填新槽; 全空则转 waiting_user(quiesce)
}
```

> `kick` 把 `g, members` 传给 `launchDelivery`（签名相应调整：`launchDelivery(g, members, d, m)`；`kick` 里已有 `g` 和 `members`）。
> `buildGroupMCP(g, m)` / `buildGroupSystemPrompt(g, members, m)` 在 ingest.go / 一个 `prompt.go` 实现（见 Step 4 + Task C8）。

- [ ] **Step 4: 写 ingest.go（tool 路由入口 + 名称解析 + 兜底 + 招募）**

```go
// IngestAgentMessage 是 group_send MCP tool 的服务端入口(MCP handler 调用)。
// memberID = 发送成员; body = 正文; mentions = 收件成员显示名(+ "用户")。
// 并发: MCP handler goroutine 调用, 可能与调度/同 turn 多次 group_send 并发 →
// 必须 per-group 串行化「解析→分配 seq→落库→入队」, 否则 NextSeq 重号 / round_count 丢更新。
func (s *groupSvc) IngestAgentMessage(ctx context.Context, memberID int64, body string, mentions []string) error {
	sender, err := group_repo.Member().Find(ctx, memberID)
	if err != nil || sender == nil {
		return i18n.NewError(ctx, code.GroupMemberNotFound)
	}
	// per-group 串行化「解析→分配 seq→落库→入队」(防 NextSeq 重号 / round_count 丢更新)。
	mu := s.ingestMu(sender.GroupID) // s.ingestMu: 返回该 group 的 *sync.Mutex(sync.Map 懒建)
	mu.Lock()
	defer mu.Unlock()

	g, err := group_repo.Group().Find(ctx, sender.GroupID)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	members, err := group_repo.Member().ListByGroup(ctx, g.ID)
	if err != nil {
		return err
	}
	recipientIDs, toUser := s.resolveMentionNames(ctx, g, members, sender, mentions)
	recipientIDs, toUser = s.applyFallback(g, sender, recipientIDs, toUser)

	g.RoundCount++
	_ = group_repo.Group().Update(ctx, g)
	if _, err := s.persistMessage(ctx, g, group_entity.SenderKindAgent, sender.ID, body, recipientIDs, toUser, 0); err != nil {
		logger.Ctx(ctx).Warn("group_svc.IngestAgentMessage: persist failed", zap.Error(err))
	}
	s.enqueueDeliveries(g.ID, recipientIDs, body, s.names(ctx, sender.AgentID))
	s.kick(ctx, g.ID)
	return nil
}

// resolveMentionNames 把成员显示名解析成 member id(+ 是否 @用户)。
// 解析不到的名字: sender 是协调者 → 尝试招募(maybeRecruit); 否则 flag 忽略。
func (s *groupSvc) resolveMentionNames(ctx context.Context, g *group_entity.Group, members []*group_entity.GroupMember, sender *group_entity.GroupMember, names []string) ([]int64, bool) {
	byName := map[string]int64{}
	for _, m := range members {
		if n := s.names(ctx, m.AgentID); n != "" {
			byName[n] = m.ID
		}
	}
	toUser := false
	ids := []int64{}
	for _, name := range names {
		switch {
		case name == "用户" || name == "你":
			toUser = true
		case byName[name] != 0 && byName[name] != sender.ID: // 剔除自我 mention(防自循环)
			ids = append(ids, byName[name])
		case byName[name] == sender.ID:
			// 自己 mention 自己 → 忽略
		case sender.IsCoordinator():
			if rid := s.maybeRecruit(ctx, g, name); rid > 0 {
				ids = append(ids, rid)
			} else {
				logger.Ctx(ctx).Info("group_svc.resolveMentionNames: unresolved/unrecruitable", zap.String("name", name), zap.Int64("groupId", g.ID))
			}
		default:
			logger.Ctx(ctx).Info("group_svc.resolveMentionNames: non-coordinator unresolved mention", zap.String("name", name))
		}
	}
	return ids, toUser
}

// applyFallback: 无任何 agent 收件人也不 @用户 → 回上一个发送者; 仍没有 → 回用户(quiesce)。
func (s *groupSvc) applyFallback(g *group_entity.Group, sender *group_entity.GroupMember, ids []int64, toUser bool) ([]int64, bool) {
	if len(ids) > 0 || toUser {
		return ids, toUser
	}
	if prev := s.lastSenderMemberID(g.ID, sender.ID); prev > 0 {
		return []int64{prev}, false
	}
	return ids, true
}

// maybeRecruit: 协调者 mention 了部门名单内、未进群、且支持 CapMCPTools 的 agent → 招募。
// 返回新成员 member id(0 = 没招到)。落一条 sender_kind=system 的"X 加入"消息。
func (s *groupSvc) maybeRecruit(ctx context.Context, g *group_entity.Group, name string) int64 {
	agentID := s.recruitableAgentByName(ctx, g, name) // 查部门名单内同名 agent; 0=不在名单
	if agentID == 0 {
		return 0
	}
	if !s.backendSupportsGroup(ctx, agentID) { // CapMCPTools 门控
		logger.Ctx(ctx).Info("group_svc.maybeRecruit: backend lacks CapMCPTools", zap.Int64("agentId", agentID))
		return 0
	}
	m, err := s.ensureMember(ctx, g, agentID, group_entity.RoleMember)
	if err != nil || m == nil {
		return 0
	}
	_, _ = s.persistMessage(ctx, g, group_entity.SenderKindSystem, 0, name+" 加入了群聊", nil, false, 0)
	return m.ID
}
```

> 实现说明（写代码时补全的小工具）：
> - `lastSenderMemberID(groupID, excludeMemberID)`：读最近一条非自己的 group_message 的 `sender_member_id`（`group_repo.Message().ListByGroup` 反向找）。
> - `recruitableAgentByName(ctx, g, name)`：查 `g.DepartmentID` 下名为 `name` 的 agent（走 `agent_repo` accessor）；0=不在可招募名单。
> - `backendSupportsGroup(ctx, agentID)`：解析该 agent 的 backend，查 `Capabilities().Has(capability.CapMCPTools)`（复用 chat_svc/agentruntime 的能力查询，注入接口避免反依赖）。同一函数供 `AddGroupMember`/`ensureMember` 门控复用（Task C3）。
> - `buildGroupMCP(g, m)` / `buildGroupSystemPrompt(g, members, m)`：见 Task C8（MCP token 签发）与本任务的 prompt 拼装；`buildGroupSystemPrompt` 产出角色 + 当前 roster 名字 + "用 group_send tool 发言, mentions 填名字, @用户=回人类" + worktree 引导。
> - `ingestMu(groupID) *sync.Mutex`：`sync.Map` 懒建的 per-group 锁（`groupSvc` 加字段 `ingestLocks *sync.Map`）。串行化 IngestAgentMessage 的 seq/round_count 临界区。
> - **token 生命周期**（spec §17）：`maybeRecruit`/`launchDelivery` 签发的 MCP token 用加密随机；成员离群（`RemoveGroupMember`）/ 群 `StopGroup`/`ArchiveGroup` 时吊销该 group/member 的 token（`s.mcp.RevokeGroup(groupID)` / `RevokeMember(memberID)`）。
> - **用户消息无收件人兜底**（spec §17）：`SendGroupMessage`（C4）里若 `recipientIDs` 空且 `!toUser` → 默认投给**协调者**（取 `members` 中 `IsCoordinator()` 的 member id）；与 agent 侧"回上一个发送者"区分。

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestScheduler_FanOutThenToolRoute ./internal/service/group_svc/`
Expected: PASS（含 race 检测——`scheduler.mu` 保护并发访问）。

- [ ] **Step 6: 补 recruit / quiesce / 生命周期测试**

`scheduler_test.go` 追加：
- **招募**：协调者 `IngestAgentMessage` 的 mentions 含名单内未进群且支持 CapMCPTools 的 agent → 断言 `ensureMember`(EnsureGroupMemberSession) 被调 + 系统消息落库 + 新成员被投递；不支持 CapMCPTools → 不招募 + flag。
- **quiesce**：成员 turn 结束（`ch <- TurnResult{}`）且无 pending → 断言 `run_status` 转 `waiting_user`（监听 emitter）。
- **生命周期**：turn `Err != nil` → 释放槽 + 不落消息 + 继续 kick。

- [ ] **Step 7: 跑全部 group_svc 测试 + 提交**

Run: `go test -race ./internal/service/group_svc/`
Expected: PASS

```bash
git add internal/service/group_svc/scheduler.go internal/service/group_svc/ingest.go internal/service/group_svc/group.go internal/service/group_svc/scheduler_test.go
git commit -m "✨ group: 并发 fan-out 调度器 + group_send tool 路由入口 IngestAgentMessage(生命周期/路由分离)"
```

---

### Task C6: Stop / Pause / Resume / Rename / Archive / MarkRead

**Files:**
- Modify: `internal/service/group_svc/group.go`
- Modify: `internal/service/group_svc/scheduler.go`（`stopAll`：清队列 + 对每个 inflight backing session 调 `gw.Stop`）
- Test: `internal/service/group_svc/control_test.go`

- [ ] **Step 1: 写测试（先红）**

```go
func TestStopGroup_AbortsInflightAndClearsQueue(t *testing.T) {
	Convey("StopGroup 应对每个在跑成员调 chat_svc.Stop + 清队列 + run_status=idle", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t); defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 2, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw).(group_svc.GroupSvc)
		// 手动置一个 inflight: 通过暴露的 test helper
		group_svc.MarkInflightForTest(svc, 5, 2, 12)
		gw.EXPECT().Stop(gomock.Any(), &chat_svc.StopRequest{SessionID: 12}).Return(&chat_svc.StopResponse{Stopped: true}, nil)

		So(svc.StopGroup(ctx, 5), ShouldBeNil)
		So(g.RunStatus, ShouldEqual, group_entity.RunIdle)
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestStopGroup_AbortsInflightAndClearsQueue ./internal/service/group_svc/`
Expected: FAIL。

- [ ] **Step 3: 实现控制方法**

scheduler.go 加：

```go
// inflightSessions 返回当前在跑成员的 backing session id(需 member 反查)。
func (s *groupSvc) stopAll(ctx context.Context, groupID int64) {
	sc := s.schedulerFor(groupID)
	sc.mu.Lock()
	inflight := make([]int64, 0, len(sc.inflight))
	for mid := range sc.inflight {
		inflight = append(inflight, mid)
	}
	sc.pending = map[int64][]delivery{}
	sc.inflight = map[int64]bool{}
	sc.mu.Unlock()

	members, _ := group_repo.Member().ListByGroup(ctx, groupID)
	sessByMember := map[int64]int64{}
	for _, m := range members {
		sessByMember[m.ID] = m.BackingSessionID
	}
	for _, mid := range inflight {
		if sid := sessByMember[mid]; sid > 0 {
			if _, err := s.gw.Stop(ctx, &chat_svc.StopRequest{SessionID: sid}); err != nil {
				logger.Ctx(ctx).Warn("group_svc.stopAll: stop failed", zap.Int64("sessionId", sid), zap.Error(err))
			}
		}
	}
}

// MarkInflightForTest 单测用。
func MarkInflightForTest(svc GroupSvc, groupID, memberID, _ int64) {
	if g, ok := svc.(*groupSvc); ok {
		sc := g.schedulerFor(groupID)
		sc.mu.Lock()
		sc.inflight[memberID] = true
		sc.mu.Unlock()
	}
}
```

group.go 加：

```go
func (s *groupSvc) StopGroup(ctx context.Context, id int64) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	s.stopAll(ctx, id)
	g.RunStatus = group_entity.RunIdle
	if err := group_repo.Group().Update(ctx, g); err != nil {
		return err
	}
	s.emitter.Emit(ctx, groupEventName(id), map[string]any{"kind": "run_status", "runStatus": group_entity.RunIdle})
	return nil
}

func (s *groupSvc) PauseGroup(ctx context.Context, id int64) error {
	return s.setRunStatus(ctx, id, group_entity.RunPaused) // 停止填新槽位; 在跑 turn 自然跑完
}

func (s *groupSvc) ResumeGroup(ctx context.Context, id int64) error {
	if err := s.setRunStatus(ctx, id, group_entity.RunRunning); err != nil {
		return err
	}
	s.kick(ctx, id) // 恢复后立刻填槽
	return nil
}

func (s *groupSvc) setRunStatus(ctx context.Context, id int64, status string) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	g.RunStatus = status
	if err := group_repo.Group().Update(ctx, g); err != nil {
		return err
	}
	s.emitter.Emit(ctx, groupEventName(id), map[string]any{"kind": "run_status", "runStatus": status})
	return nil
}

func (s *groupSvc) RenameGroup(ctx context.Context, id int64, title string) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	if strings.TrimSpace(title) == "" {
		return i18n.NewError(ctx, code.GroupTitleRequired)
	}
	g.Title = title
	return group_repo.Group().Update(ctx, g)
}

func (s *groupSvc) ArchiveGroup(ctx context.Context, id int64) error {
	g, err := group_repo.Group().Find(ctx, id)
	if err != nil || g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	s.stopAll(ctx, id)
	g.Status = consts.DELETE
	return group_repo.Group().Update(ctx, g)
}
```

> `strconv` / `strings` import 按需补。`PauseGroup` 用 `RunPaused`，`CanAdvance()` 已让 paused 不推进，所以 `kick` 在 pause 期间自然不填槽。

- [ ] **Step 4: 跑测试看通过**

Run: `go test -race ./internal/service/group_svc/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/service/group_svc/group.go internal/service/group_svc/scheduler.go internal/service/group_svc/control_test.go
git commit -m "✨ group: Stop/Pause/Resume/Rename/Archive 控制流"
```

---

### Task C8: `group_send` MCP server handler + token + 系统提示/MCP 拼装

**Files:**
- Create: `internal/service/group_svc/mcp.go`（MCP `http.Handler` + per-member token + `MintToken` + `buildGroupMCP` + `buildGroupSystemPrompt` + 网关 base URL）
- Modify: `internal/service/group_svc/group.go`（`groupSvc` 加字段 `mcp *groupMCP` / `gatewayBaseURL string`，`newGroupSvc` 初始化）
- Test: `internal/service/group_svc/mcp_test.go`

> 复用现有 `httpgateway.Gateway.RegisterMCP("/mcp/group/", handler)`（`gateway.go:276` 已支持，**gateway 代码不改**）。handler 是 group_svc 自己的，bootstrap 注册（Task D2）。
> MCP over HTTP 是 JSON-RPC（claude-code `--mcp-config` 的 `type:"http"` transport）：需处理 `initialize` / `notifications/initialized` / `tools/list` / `tools/call`。JSON-RPC 帧用 `internal/pkg/jsonrpc`（已存在）。
>
> **✅ 已 spike 验证（claude-code 2.1.161，2026-06-03）**——实测握手序列与要求：
> 1. `POST /mcp/ initialize`（client 报 `protocolVersion:"2025-11-25"` + clientInfo）→ 回 `application/json`（**纯 JSON 即可，无需 SSE**），result 里 **echo client 的 protocolVersion** + `serverInfo` + `capabilities.tools`。
> 2. `POST notifications/initialized` → **回 202**（无 body）。
> 3. `POST tools/list` → 回 tools。
> 4. `POST tools/call` → 跑 → 回 `result.content`。
> 5. `GET /mcp/`（client 开 SSE server→client 流）→ **回 405 即可，claude 容忍**（我们不需要服务端推送；想干净可回 200 空 SSE）。
> 6. **自定义 header 透传确认**：`Authorization: Bearer <token>`（spike 用 `X-Spike-Auth` 验证，到达时 header 名小写）→ **per-member token 身份方案成立**。
> 7. claude 在后续请求带 `mcp-session-id`（= 我们 initialize 时设的值）+ `mcp-protocol-version`；**我们用 token 做身份，session-id 可忽略**（容忍其存在即可）。
> 8. `--strict-mcp-config` 可隔离只用我们的 server；工具名 = `mcp__group__group_send`（server 名 group）。

- [ ] **Step 1: 写 handler 测试（先红）**

```go
func TestGroupMCP_ToolCallRoutesToIngest(t *testing.T) {
	Convey("合法 token 的 group_send tools/call → 调 IngestAgentMessage(memberID, body, mentions)", t, func() {
		var gotMember int64
		var gotBody string
		var gotMentions []string
		h := group_svc.NewGroupMCPForTest(func(_ context.Context, memberID int64, body string, mentions []string) error {
			gotMember, gotBody, gotMentions = memberID, body, mentions
			return nil
		})
		token := h.MintToken(5 /*group*/, 2 /*member*/)

		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{"body":"做好了","mentions":["前端"]}}}`
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		So(w.Code, ShouldEqual, 200)
		So(gotMember, ShouldEqual, 2)
		So(gotBody, ShouldEqual, "做好了")
		So(gotMentions, ShouldResemble, []string{"前端"})
	})

	Convey("无/坏 token → 拒绝, 不调 ingest", t, func() {
		called := false
		h := group_svc.NewGroupMCPForTest(func(context.Context, int64, string, []string) error { called = true; return nil })
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{}}}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		So(called, ShouldBeFalse)
		So(w.Code, ShouldNotEqual, 200) // 401/403
	})

	Convey("tools/list 暴露 group_send schema", t, func() {
		h := group_svc.NewGroupMCPForTest(nil)
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		So(w.Body.String(), ShouldContainSubstring, "group_send")
	})
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestGroupMCP ./internal/service/group_svc/`
Expected: FAIL —— handler 未实现。

- [ ] **Step 3: 写 mcp.go**

```go
package group_svc

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

type memberRef struct{ groupID, memberID int64 }

// groupMCP 是 group_send tool 的 MCP-over-HTTP server(挂在 gateway /mcp/group/)。
type groupMCP struct {
	mu     sync.Mutex
	tokens map[string]memberRef // token -> (group, member)
	ingest func(ctx context.Context, memberID int64, body string, mentions []string) error
	newTok func() string // token 生成器(测试可注入定值)
}

func newGroupMCP(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return &groupMCP{tokens: map[string]memberRef{}, ingest: ingest, newTok: randToken}
}

// MintToken 为某成员会话签一个绑定 (group, member) 的 token(投递时塞进 mcp-config header)。
func (h *groupMCP) MintToken(groupID, memberID int64) string {
	tok := h.newTok()
	h.mu.Lock()
	h.tokens[tok] = memberRef{groupID, memberID}
	h.mu.Unlock()
	return tok
}

func (h *groupMCP) lookup(tok string) (memberRef, bool) {
	h.mu.Lock(); defer h.mu.Unlock()
	r, ok := h.tokens[tok]
	return r, ok
}

// ServeHTTP 极简 MCP JSON-RPC: initialize / notifications/initialized / tools/list / tools/call。
func (h *groupMCP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet { // claude 开 server→client SSE 流; 我们不推送 → 405(已验证 claude 容忍)
		w.WriteHeader(http.StatusMethodNotAllowed); return
	}
	var rpc struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			ProtocolVersion string `json:"protocolVersion"`
			Name            string `json:"name"`
			Arguments       struct {
				Body     string   `json:"body"`
				Mentions []string `json:"mentions"`
			} `json:"arguments"`
		} `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
		writeRPCError(w, nil, -32700, "parse error"); return
	}
	switch rpc.Method {
	case "initialize":
		pv := rpc.Params.ProtocolVersion // echo client 版本(claude 报 "2025-11-25")
		if pv == "" {
			pv = "2025-06-18"
		}
		writeRPCResult(w, rpc.ID, map[string]any{
			"protocolVersion": pv,
			"serverInfo":      map[string]any{"name": "agentre-group", "version": "1"},
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
		})
	case "notifications/initialized":
		w.WriteHeader(http.StatusAccepted)
	case "tools/list":
		writeRPCResult(w, rpc.ID, map[string]any{"tools": []any{groupSendToolSchema()}})
	case "tools/call":
		ref, ok := h.lookup(bearer(r))
		if !ok {
			http.Error(w, "unauthorized", http.StatusUnauthorized); return
		}
		if rpc.Params.Name != "group_send" {
			writeRPCError(w, rpc.ID, -32601, "unknown tool"); return
		}
		if err := h.ingest(r.Context(), ref.memberID, rpc.Params.Arguments.Body, rpc.Params.Arguments.Mentions); err != nil {
			writeRPCError(w, rpc.ID, -32000, err.Error()); return
		}
		writeRPCResult(w, rpc.ID, map[string]any{"content": []any{map[string]any{"type": "text", "text": "sent"}}})
	default:
		writeRPCError(w, rpc.ID, -32601, "method not found")
	}
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func groupSendToolSchema() map[string]any {
	return map[string]any{
		"name":        "group_send",
		"description": "向群聊发送一条消息。mentions 填收件成员的显示名(@用户 = 回复人类)。一个回合可多次调用。",
		"inputSchema": map[string]any{
			"type":     "object",
			"required": []string{"body"},
			"properties": map[string]any{
				"body":     map[string]any{"type": "string", "description": "消息正文"},
				"mentions": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "收件成员显示名"},
			},
		},
	}
}

// writeRPCResult / writeRPCError / randToken: 用 internal/pkg/jsonrpc 或本地小工具实现(略)。
```

- [ ] **Step 4: 接 group_svc + 拼 MCP/prompt**

`group.go`：`groupSvc` 加 `mcp *groupMCP` + `gatewayBaseURL string`；`newGroupSvc` 里 `mcp: newGroupMCP(nil)`（ingest 在装配时回填 `s.IngestAgentMessage`，避免构造期循环）。加导出：

```go
// MCPHandler 供 bootstrap 注册到 gateway /mcp/group/。
func (s *groupSvc) MCPHandler() http.Handler { return s.mcp }
// SetGatewayBaseURL bootstrap 注入本机 gateway base(如 http://127.0.0.1:<port>)。
func (s *groupSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// NewGroupMCPForTest 仅测试用。
func NewGroupMCPForTest(ingest func(context.Context, int64, string, []string) error) *groupMCP {
	return newGroupMCP(ingest)
}
```

`buildGroupMCP` / `buildGroupSystemPrompt`（C5 launchDelivery 调）：

```go
func (s *groupSvc) buildGroupMCP(g *group_entity.Group, m *group_entity.GroupMember) []agentruntime.MCPServerSpec {
	tok := s.mcp.MintToken(g.ID, m.ID)
	return []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     s.gatewayBaseURL + "/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer " + tok},
	}}
}

func (s *groupSvc) buildGroupSystemPrompt(g *group_entity.Group, members []*group_entity.GroupMember, me *group_entity.GroupMember) string {
	var b strings.Builder
	role := "成员"
	if me.IsCoordinator() { role = "协调者(部门 leader)" }
	fmt.Fprintf(&b, "\n\n## 群聊「%s」\n你是本群的%s。", g.Title, role)
	b.WriteString("\n当前成员：")
	for _, m := range members {
		fmt.Fprintf(&b, "\n- %s（%s）", s.names(context.Background(), m.AgentID), m.Role)
	}
	b.WriteString("\n\n你只会收到 @ 到你的消息。要发言请调用 `group_send` 工具：body=正文，mentions=收件成员显示名数组（@用户 = 回复人类）。一个回合可多次调用、可分别对不同人发不同内容。**不调用 group_send 的内容不会进群**。")
	if me.IsCoordinator() {
		b.WriteString("\n作为协调者，mentions 里写一个本部门、尚未进群的同事名字即可把 ta 拉进群。")
	}
	b.WriteString("\n若你要修改文件且可能与他人并发，请先 `git worktree add` 在自己的工作树里作业。")
	return b.String()
}
```

- [ ] **Step 5: 跑测试看通过 + 提交**

Run: `go test -race ./internal/service/group_svc/`
Expected: PASS

```bash
git add internal/service/group_svc/mcp.go internal/service/group_svc/group.go internal/service/group_svc/mcp_test.go
git commit -m "✨ group: group_send MCP server handler + per-member token + 群 system prompt/MCP 拼装"
```

> bootstrap 装配（Task D2 一并做）：`gateway.RegisterMCP("/mcp/group/", group_svc.Default().MCPHandler())` + `group_svc.Default().SetGatewayBaseURL(gw.BaseURL())` + 回填 ingest（`group_svc` 内 `s.mcp.ingest = s.IngestAgentMessage`，在 `newGroupSvc` 末尾或一个 `wireMCP()` 里）。

---

## Phase D — Wails 绑定 + 事件

### Task D1: `internal/app/group.go` 绑定 + DTO

**Files:**
- Create: `internal/app/group.go`
- Test: `internal/app/group_test.go`（thin 绑定测试，mock svc）

- [ ] **Step 1: 写绑定 + DTO + 转换器**

```go
package app

import (
	"agentre/internal/model/entity/group_entity"
	"agentre/internal/service/group_svc"
)

type GroupItem struct {
	ID         int64  `json:"id"`
	Title      string `json:"title"`
	RunStatus  string `json:"runStatus"`
	RoundCount int    `json:"roundCount"`
	Createtime int64  `json:"createtime"`
	Updatetime int64  `json:"updatetime"`
}

type GroupMemberItem struct {
	ID               int64  `json:"id"`
	AgentID          int64  `json:"agentID"`
	BackingSessionID int64  `json:"backingSessionID"`
	Role             string `json:"role"`
	Status           string `json:"status"`
}

type GroupMessageItem struct {
	ID                 int64   `json:"id"`
	Seq                int     `json:"seq"`
	SenderKind         string  `json:"senderKind"`
	SenderMemberID     int64   `json:"senderMemberID"`
	RecipientMemberIDs []int64 `json:"recipientMemberIDs"`
	ToUser             bool    `json:"toUser"`
	Content            string  `json:"content"`
	Createtime         int64   `json:"createtime"`
}

type GroupDetailResponse struct {
	Group    *GroupItem          `json:"group"`
	Members  []*GroupMemberItem  `json:"members"`
	Messages []*GroupMessageItem `json:"messages"`
}

type GroupCreateRequest struct {
	Title              string `json:"title"`
	CoordinatorAgentID int64  `json:"coordinatorAgentID"`
	DepartmentID       int64  `json:"departmentID"`
	ProjectID          int64  `json:"projectID"`
}

type GroupSendRequest struct {
	GroupID            int64   `json:"groupID"`
	Text               string  `json:"text"`
	RecipientMemberIDs []int64 `json:"recipientMemberIDs"`
	ToUser             bool    `json:"toUser"`
}

func toGroupItem(g *group_entity.Group) *GroupItem {
	return &GroupItem{ID: g.ID, Title: g.Title, RunStatus: g.RunStatus, RoundCount: g.RoundCount, Createtime: g.Createtime, Updatetime: g.Updatetime}
}
func toGroupDetail(d *group_svc.GroupDetail) *GroupDetailResponse {
	members := make([]*GroupMemberItem, 0, len(d.Members))
	for _, m := range d.Members {
		members = append(members, &GroupMemberItem{ID: m.ID, AgentID: m.AgentID, BackingSessionID: m.BackingSessionID, Role: m.Role, Status: m.Status})
	}
	msgs := make([]*GroupMessageItem, 0, len(d.Messages))
	for _, m := range d.Messages {
		msgs = append(msgs, &GroupMessageItem{ID: m.ID, Seq: m.Seq, SenderKind: m.SenderKind, SenderMemberID: m.SenderMemberID, RecipientMemberIDs: m.Recipients(), ToUser: m.ToUser, Content: m.Content, Createtime: m.Createtime})
	}
	return &GroupDetailResponse{Group: toGroupItem(d.Group), Members: members, Messages: msgs}
}

func (a *App) GroupList() ([]*GroupItem, error) {
	gs, err := group_svc.Default().ListGroups(a.ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*GroupItem, 0, len(gs))
	for _, g := range gs {
		items = append(items, toGroupItem(g))
	}
	return items, nil
}

func (a *App) GroupCreate(req *GroupCreateRequest) (*GroupDetailResponse, error) {
	d, err := group_svc.Default().CreateGroup(a.ctx, &group_svc.CreateGroupRequest{Title: req.Title, CoordinatorAgentID: req.CoordinatorAgentID, DepartmentID: req.DepartmentID, ProjectID: req.ProjectID})
	if err != nil {
		return nil, err
	}
	return toGroupDetail(d), nil
}

func (a *App) GroupLoad(id int64) (*GroupDetailResponse, error) {
	d, err := group_svc.Default().LoadGroup(a.ctx, id)
	if err != nil {
		return nil, err
	}
	return toGroupDetail(d), nil
}

func (a *App) GroupSend(req *GroupSendRequest) error {
	return group_svc.Default().SendGroupMessage(a.ctx, &group_svc.SendGroupMessageRequest{GroupID: req.GroupID, Text: req.Text, RecipientMemberIDs: req.RecipientMemberIDs, ToUser: req.ToUser})
}

func (a *App) GroupAddMember(groupID, agentID int64) (*GroupMemberItem, error) {
	m, err := group_svc.Default().AddGroupMember(a.ctx, groupID, agentID)
	if err != nil {
		return nil, err
	}
	return &GroupMemberItem{ID: m.ID, AgentID: m.AgentID, BackingSessionID: m.BackingSessionID, Role: m.Role, Status: m.Status}, nil
}

func (a *App) GroupRemoveMember(memberID int64) error { return group_svc.Default().RemoveGroupMember(a.ctx, memberID) }
func (a *App) GroupStop(id int64) error               { return group_svc.Default().StopGroup(a.ctx, id) }
func (a *App) GroupPause(id int64) error              { return group_svc.Default().PauseGroup(a.ctx, id) }
func (a *App) GroupResume(id int64) error             { return group_svc.Default().ResumeGroup(a.ctx, id) }
func (a *App) GroupRename(id int64, title string) error { return group_svc.Default().RenameGroup(a.ctx, id, title) }
func (a *App) GroupArchive(id int64) error            { return group_svc.Default().ArchiveGroup(a.ctx, id) }
```

- [ ] **Step 2: 写 thin 绑定测试 + 跑（先红再绿）**

`group_test.go`：mock `group_svc`（用 `group_svc.SetDefault` 注入一个返回固定数据的 fake），断言 `GroupLoad` 转换正确。

```go
func TestApp_GroupLoad_MapsDetail(t *testing.T) {
	Convey("GroupLoad 应把 svc detail 映射为 DTO", t, func() {
		group_svc.SetDefault(fakeGroupSvc{detail: &group_svc.GroupDetail{
			Group:   &group_entity.Group{ID: 5, Title: "队", RunStatus: "running"},
			Members: []*group_entity.GroupMember{{ID: 1, AgentID: 2, Role: "coordinator"}},
		}})
		a := &App{ctx: context.Background()}
		resp, err := a.GroupLoad(5)
		So(err, ShouldBeNil)
		So(resp.Group.Title, ShouldEqual, "队")
		So(resp.Members[0].Role, ShouldEqual, "coordinator")
	})
}
```

Run: `go test -race -run TestApp_GroupLoad_MapsDetail ./internal/app/`
Expected: 先 FAIL（fake/绑定缺失）→ 补 `fakeGroupSvc` 实现 `GroupSvc` 接口 → PASS。

- [ ] **Step 3: 提交**

```bash
git add internal/app/group.go internal/app/group_test.go
git commit -m "✨ group: internal/app/group.go Wails 绑定 + DTO(thin)"
```

---

### Task D2: 注册 svc + 注入 emitter + 挂 MCP handler + 刷新绑定

**Files:**
- Modify: `internal/app/app.go`（仿 `chat_svc` emitter 注入：`app.go:136` 处）
- Modify: `internal/bootstrap/cago.go`（gateway 已构造处：`RegisterMCP` + `SetGatewayBaseURL`；B3 已注册 group repo）
- 生成: `frontend/wailsjs/**`（`make generate`）

- [ ] **Step 1: 注入 group 事件 emitter**

`internal/app/app.go`，在 chat emitter 注入（`app.go:136` `chat_svc.RegisterChat(chat_svc.NewChat(emitter))` 附近）追加：

```go
	group_svc.SetEmitter(group_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		wailsruntime.EventsEmit(a.ctx, name, payload)
	}))
```

- [ ] **Step 1b: 把 group_send MCP handler 挂到 gateway**

`internal/bootstrap/cago.go`，在 gateway 构造/启动处（与 `chat_svc.RegisterGateway(gw)` 等并列，gw 即 `httpgateway.Gateway`）追加：

```go
	gw.RegisterMCP("/mcp/group/", group_svc.Default().MCPHandler())
	group_svc.Default().SetGatewayBaseURL(gw.BaseURL()) // 如 http://127.0.0.1:<port>
```

> `gw.BaseURL()` 若不存在则用 `fmt.Sprintf("http://%s:%d", host, port)`（gateway 已持有 host/port，可加一个 `BaseURL()` 访问器——属本特性 in-scope 小改）。group_svc 内 `s.mcp.ingest` 在 `newGroupSvc`/`wireMCP` 回填 `s.IngestAgentMessage`（Task C8）。

- [ ] **Step 2: 刷新 Wails 绑定**

Run: `make generate`
Expected: `frontend/wailsjs/go/app/App.d.ts` 出现 `GroupList/GroupCreate/GroupLoad/GroupSend/...`；`models.ts` 出现 `app.GroupItem` 等。

- [ ] **Step 3: 后端全量回归**

Run: `make test-backend`
Expected: PASS（含 migrations / chat_repo / chat_svc / group_* 全绿）。

- [ ] **Step 4: 提交**

```bash
git add internal/app/app.go internal/bootstrap/cago.go frontend/wailsjs/
git commit -m "✨ group: 注入群事件 emitter + 挂 group_send MCP handler + 刷新 Wails 绑定"
```

> 注：`frontend/wailsjs/` 按仓库约定可能 gitignore（见 architecture.md）。若被忽略则不提交，仅本地生成供前端开发；CI 由 `make generate` 重生成。

---

## Phase E — 前端群聊面板

> 复用 §10 四区结构。本阶段每个 Task 都先写/补 i18n key（否则 `i18next/no-literal-string` + `i18n.test.ts` 会红），再写组件/hook，再 Vitest。所有静态文案走 `t(...)`，表单控件用 `@/components/ui/*`（仓库 shadcn **无 `tabs.tsx`** ——已核对；视图 tab 直接复用现有 `chat-tabs/`（`tab-strip.tsx`/`tab.tsx`/`use-tabs-view.ts`），右栏 成员/设置 小 tab 用 Button+`useState`）。

### Task E1: i18n key（group.*）

**Files:**
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`
- Modify: `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/__tests__/i18n.test.ts`（已存在，跑它校验覆盖）

- [ ] **Step 1: 加 `group` 段（两个 locale 同步）**

`en/common.json` 加：

```json
"group": {
  "title": "Groups",
  "section": "Group Chats",
  "tabs": { "members": "Members", "settings": "Settings" },
  "roster": { "coordinator": "Coordinator", "members": "Members", "invite": "Invite member", "backToGroup": "← Back to group" },
  "runStatus": { "running": "Running", "waitingUser": "Waiting for you", "paused": "Paused", "idle": "Idle", "error": "Error" },
  "rounds": "{{count}} rounds",
  "controls": { "pause": "Pause", "stop": "Stop", "resume": "Resume" },
  "composer": { "placeholder": "Message the group… use @ to mention", "send": "Send" },
  "settings": { "workdir": "Working directory", "archive": "Archive group" },
  "onlyXReceived": "Only {{name}} received this"
}
```

`zh-CN/common.json` 同结构中文：群聊 / 群聊 / 成员·设置 / 协调者·成员·邀请成员·← 返回群聊 / 运行中·等待你·已暂停·空闲·出错 / 已 {{count}} 轮 / 暂停·停止·继续 / 「给群里发消息… 用 @ 提及成员」·发送 / 工作目录·归档群 / 仅 {{name}} 收到。

- [ ] **Step 2: 跑 i18n 测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts`
Expected: PASS（en/zh key 集合一致）。

- [ ] **Step 3: 提交**

```bash
git add frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit -m "✨ group(fe): 群聊面板 i18n key(zh/en)"
```

---

### Task E2: `use-group` hook + live store

**Files:**
- Create: `frontend/src/stores/group-store.ts`（zustand：群详情 + live 消息追加）
- Create: `frontend/src/hooks/use-group.ts`
- Test: `frontend/src/hooks/use-group.test.ts`

- [ ] **Step 1: 写 hook 测试（先红）**

仿真实的 `use-chat-stream.test.ts` / `use-chat-session.test.ts`（均 mock `../../wailsjs/go/app/App` + `../../wailsjs/runtime/runtime`）：

```typescript
vi.mock("../../wailsjs/go/app/App", () => ({
  GroupLoad: vi.fn(),
  GroupSend: vi.fn(),
}));
vi.mock("../../wailsjs/runtime/runtime", () => ({ EventsOn: vi.fn(() => () => {}), EventsOff: vi.fn() }));

import { GroupLoad } from "../../wailsjs/go/app/App";
import { useGroup } from "./use-group";

describe("useGroup", () => {
  beforeEach(() => {
    (GroupLoad as ReturnType<typeof vi.fn>).mockResolvedValue({
      group: { id: 5, title: "队", runStatus: "running", roundCount: 3 },
      members: [{ id: 1, agentID: 2, role: "coordinator", status: "active" }],
      messages: [{ id: 1, seq: 1, senderKind: "user", content: "hi", recipientMemberIDs: [1], toUser: false }],
    });
  });
  it("loads group detail on mount and subscribes to events", async () => {
    const { result } = renderHook(() => useGroup(5));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.detail?.group.title).toBe("队");
    expect(result.current.detail?.members).toHaveLength(1);
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/hooks/use-group.test.ts`
Expected: FAIL。

- [ ] **Step 3: 写 store + hook**

`group-store.ts`（zustand，仿 `chat-streams-store.ts`）：持 `Map<groupId, GroupDetail>`，actions：`setDetail`、`appendMessage`、`patchRunStatus`。

`use-group.ts`：

```typescript
import { useCallback, useEffect } from "react";
import { GroupLoad } from "../../wailsjs/go/app/App";
import { EventsOff, EventsOn } from "../../wailsjs/runtime/runtime";
import { useGroupStore } from "../stores/group-store";

export function useGroup(groupId: number) {
  const detail = useGroupStore((s) => s.details.get(groupId));
  const setDetail = useGroupStore((s) => s.setDetail);
  const appendMessage = useGroupStore((s) => s.appendMessage);
  const patchRunStatus = useGroupStore((s) => s.patchRunStatus);
  // loading/error 用本地 state
  const reload = useCallback(async () => {
    const d = await GroupLoad(groupId);
    setDetail(groupId, d);
  }, [groupId, setDetail]);

  useEffect(() => {
    void reload();
    const evt = `group:event:${groupId}`;
    EventsOn(evt, (payload: { kind: string; message?: unknown; runStatus?: string }) => {
      if (payload.kind === "message" && payload.message) appendMessage(groupId, payload.message);
      if (payload.kind === "run_status" && payload.runStatus) patchRunStatus(groupId, payload.runStatus);
    });
    return () => EventsOff(evt);
  }, [groupId, reload, appendMessage, patchRunStatus]);

  return { detail, reload };
}
```

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/hooks/use-group.test.ts`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/src/stores/group-store.ts frontend/src/hooks/use-group.ts frontend/src/hooks/use-group.test.ts
git commit -m "✨ group(fe): use-group hook + group-store(live 事件)"
```

---

### Task E3: 群聊面板组件（四区 + 双 tab）

**Files:**
- Create: `frontend/src/components/agentre/group-chat/index.tsx`（面板入口）
- Create: `frontend/src/components/agentre/group-chat/group-transcript.tsx`
- Create: `frontend/src/components/agentre/group-chat/group-roster.tsx`（成员/设置 tab）
- Create: `frontend/src/components/agentre/group-chat/group-composer.tsx`（@ 自动补全）
- Test: `frontend/src/components/agentre/group-chat/group-chat.test.tsx`

> 右栏 成员/设置 tab 用 `useState<"members"|"settings">` + Button（**仓库无 shadcn `Tabs`**）。对话区顶部的视图 tab 栏（群聊 + 跳进的成员会话）**复用现有 `chat-tabs/`**（`tab-strip.tsx`/`tab.tsx`/`use-tabs-view.ts`）。消息着色 / `→ @收件人` chips / "仅 X 收到" 灰字按 §10。

- [ ] **Step 1: 写组件测试（先红）**

```typescript
import { render, screen } from "@testing-library/react";
// mock use-group 返回固定 detail; mock i18n t 透传 key
describe("GroupChat", () => {
  it("renders room title, run status pill and member roster", async () => {
    render(<GroupChat groupId={5} />);
    expect(await screen.findByText("队")).toBeInTheDocument();
    expect(screen.getByText(/group.runStatus.running|运行中|Running/)).toBeInTheDocument();
    // 成员 tab 默认激活, 显示协调者
    expect(screen.getByText(/group.roster.coordinator|协调者|Coordinator/)).toBeInTheDocument();
  });
  it("switches right panel to settings tab", async () => {
    render(<GroupChat groupId={5} />);
    fireEvent.click(screen.getByText(/group.tabs.settings|设置|Settings/));
    expect(screen.getByText(/group.settings.workdir|工作目录|Working directory/)).toBeInTheDocument();
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-chat.test.tsx`
Expected: FAIL。

- [ ] **Step 3: 写组件（四区中的「中 + 右」；左对话列表混排放 Task E4）**

`index.tsx` 顶层：`<main>` 内含 ChatArea（视图 tab 栏 + 房间头 + transcript + composer）+ Roster（右，成员/设置 tab）。所有文案 `t("group.*")`，控件 `@/components/ui/*`（Button/Badge/Textarea）。run_status pill 映射 `t("group.runStatus."+detail.group.runStatus)`，回合数 `t("group.rounds", { count })`。

> 完整组件代码较长；按真实的 `chat-panel.tsx` / `chat-page.tsx` 的子组件拆分风格逐个写。关键点：
> - 视图 tab 栏：`views: [{id:'group', label:title}, ...memberSessionViews]`，`useState(activeViewId)`，点群聊 tab 渲染群 transcript，点成员会话 tab 渲染复用的单聊视图（嵌 `<ChatView sessionId={member.backingSessionID}/>`）。
> - 成员行尾 `›` → push 一个成员会话 view tab。
> - 右 Roster：`useState<"members"|"settings">`；members 渲染协调者+成员（status dot）+邀请；settings 渲染 工作目录/归档群。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/group-chat.test.tsx`
Expected: PASS

- [ ] **Step 5: lint(i18n 守卫) + 提交**

Run: `cd frontend && pnpm lint`（确认无 `i18next/no-literal-string` 报错）
```bash
git add frontend/src/components/agentre/group-chat/
git commit -m "✨ group(fe): 群聊面板(视图 tab 栏 + 房间头 + transcript + composer + 成员/设置 roster)"
```

---

### Task E4: 左侧对话列表混排 + 接入导航

**Files:**
- Modify: 左侧会话列表（混入「群聊」分区——真实文件 `session-group.tsx` + `chat-panel.tsx` + `chat-sidebar-store.ts`，群条目渲染在单聊会话分组之上，带 run_status 点）
- Modify: 路由/导航（把 group-chat 面板挂到 rail/主区——参考 `chat-page.tsx` 的接入点）
- Test: 对应组件 test 补「群聊分区渲染」

- [ ] **Step 1: 写测试（先红）**：会话列表顶部渲染「群聊」分区 + 群条目带 run_status 点；点击群条目切换到 GroupChat 面板。

- [ ] **Step 2: 跑看失败** → **Step 3: 实现**（`GroupList()` 拉群，渲染在 AGENTS 单聊会话之上；选中态路由到 `<GroupChat groupId>`）→ **Step 4: 跑看通过**。

Run: `cd frontend && pnpm test -- <touched-test-files>`

- [ ] **Step 5: 全量前端 + lint**

Run: `cd frontend && pnpm test && pnpm lint`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add frontend/src/...
git commit -m "✨ group(fe): 左侧对话列表混排群聊 + 导航接入"
```

---

### Task E5: mention chip 渲染 + 点击跳转（展示层）

**Files:**
- Create: `frontend/src/components/agentre/group-chat/mention-text.tsx`（把正文里的 `@名字`/`<mention>名字</mention>` 渲染成高亮可点 chip）
- Test: `frontend/src/components/agentre/group-chat/mention-text.test.tsx`

> 这是用户明确要保留的「`<mention>` 解析」职责 —— **仅用于展示**：高亮 + 点击跳转到该成员会话。路由不经过它（路由是结构化的，见 C4/C5）。

- [ ] **Step 1: 写测试（先红）**

```tsx
import { render, screen, fireEvent } from "@testing-library/react";
import { MentionText } from "./mention-text";

describe("MentionText", () => {
  const roster = [{ memberId: 2, name: "后端" }, { memberId: 3, name: "前端" }];
  it("renders matched @name as a clickable chip, plain text otherwise", () => {
    const onJump = vi.fn();
    render(<MentionText text="麻烦 @后端 看下 @陌生人" roster={roster} onJump={onJump} />);
    const chip = screen.getByText("@后端");
    fireEvent.click(chip);
    expect(onJump).toHaveBeenCalledWith(2); // 跳到 member 2 的会话
    // 不在 roster 的 @陌生人 不应是 chip(无 onJump 绑定) —— 退化为普通文本
    expect(screen.getByText(/陌生人/)).toBeInTheDocument();
  });
  it("also recognizes <mention>name</mention> markup", () => {
    const onJump = vi.fn();
    render(<MentionText text="好的 <mention>前端</mention>" roster={roster} onJump={onJump} />);
    fireEvent.click(screen.getByText("@前端"));
    expect(onJump).toHaveBeenCalledWith(3);
  });
});
```

- [ ] **Step 2: 跑看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/mention-text.test.tsx`
Expected: FAIL。

- [ ] **Step 3: 实现 `mention-text.tsx`**

tokenize 正文：扫描 `<mention>([^<]+)</mention>` 与 `@(\S+)`，匹配 `roster` 的 name → 渲染 `<button>` chip（着色，`onClick={() => onJump(memberId)}`）；未匹配 → 原样文本。`onJump(memberId)` 由父级（transcript）接到「打开该成员会话视图 tab」（复用 E3 的视图 tab 逻辑）。无 i18n 文案（渲染的是动态消息内容，不进 `t()`）。

```tsx
type RosterEntry = { memberId: number; name: string };
export function MentionText({ text, roster, onJump }: { text: string; roster: RosterEntry[]; onJump: (memberId: number) => void }) {
  const byName = new Map(roster.map((r) => [r.name, r.memberId]));
  // 先把 <mention>X</mention> 归一成 @X, 再按 @token 切分; 匹配 roster 的渲染 chip。
  // (实现：正则 split + map，chip 用 <button> + 着色 class；非匹配 token 原样输出)
  // ...
}
```

- [ ] **Step 4: 跑看通过 + 在 transcript 接入**

E3 的 transcript 正文用 `<MentionText text={msg.content} roster={roster} onJump={openMemberView} />` 替换纯文本渲染（`openMemberView` = E3 已有的「成员会话视图 tab」打开函数）。

Run: `cd frontend && pnpm test -- src/components/agentre/group-chat/ && pnpm lint`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add frontend/src/components/agentre/group-chat/mention-text.tsx frontend/src/components/agentre/group-chat/mention-text.test.tsx
git commit -m "✨ group(fe): 消息正文 mention 高亮 chip + 点击跳转成员会话"
```

---

## 收尾校验

- [ ] **后端全量：** `make test-backend` → 全绿
- [ ] **前端全量：** `cd frontend && pnpm test` → 全绿
- [ ] **lint：** `make lint` → 全绿（含 `i18next/no-literal-string`）
- [ ] **mock 一致：** `make mock` 后 `git status` 无未提交 mock 漂移
- [ ] **手动冒烟（`make dev`，协调者/成员均用 claudecode 后端的 agent）：** 建群（协调者自动进群）→ 用户 @ 协调者 → 协调者 turn 内调 `group_send` @两个成员 → 两成员并发跑 → 各自 `group_send` 路由 → 只 @用户时 quiesce → 停止中止全部 → 点成员 `›`/消息里 mention chip 跳转 backing 会话 → 普通单聊列表**不含**群成员 session → 邀请列表只列 claudecode（CapMCPTools）agent，加 codex agent 报 `GroupBackendUnsupported`。

---

## 自检：spec 覆盖对照

| spec 章节 | 覆盖 Task |
| --- | --- |
| §3.1 ObserveTurn（起点订阅 + 覆盖 failTurn 早退 + 恰好一条终态 + **生命周期 only**） | A4 |
| §3.2 group_id 列 + **9** 个 list/count 查询过滤 + 索引 + EnsureGroupMemberSession | A1 / A2 / A3 |
| §3.3 能力门控 `CapMCPTools` + `RunRequest.MCPServers` + `--mcp-config` + `SendRequest` 透传 | **AM1 / AM2 / AM3** |
| §3.3 MCP `group_send` server handler + per-member token + 群 system prompt 拼装 | **C8** |
| §4 三表数据模型 + 充血方法 | B1 / B2 / B3 |
| §5 并发 fan-out（跨成员并发/同成员串行/eager/无 cap）+ **tool 路由 `IngestAgentMessage`** | C5 |
| §6 寻址（**结构化路由** `mentions[]`/`recipientMemberIds` + `(来自 X)` 抬头 + 兜底 + quiesce） | C4（用户侧）/ C5（agent 侧 `resolveMentionNames`） |
| §6 寻址（**展示** `<mention>`/`@` 高亮 chip + 点击跳转） | **E5** |
| §7 招募（协调者 `group_send` mention 名单内 + CapMCPTools 门控）/终止（无 max_rounds）/插话/暂停 | C5（maybeRecruit）/ C6 |
| §8 工具权限透传（`group_send` 自动放行；其它 tool 系统行冒泡 + 复用现有 handler） | AM2（allowedTools）/ E3（transcript 渲染审批卡）；后端事件经 backing session stream 既有路径，无新增 |
| §9 Wails 绑定（结构化 recipientMemberIds）+ `group:event:<id>` 事件流 | D1 / D2 |
| §10 四区 UI + 视图 tab 栏 + 成员/设置 tab + 成员/mention 跳转 + 邀请按 CapMCPTools 过滤 | E3 / E4 / E5 |
| §11 测试策略（capability matrix / MCP handler / sqlmock / mockgen / goconvey / Vitest） | 各 Task 的 Step 1-2 |
| §12 错误码 19000（含 `GroupBackendUnsupported`）+ i18n + 日志 | C7 + 各 svc 方法 `logger.Ctx` |
| §13 MVP IN/OUT | 全量（OUT 项不做：codex/builtin 群成员 / 强制 worktree / DAG / 跨群 / 环检测 / remote 专测） |

> **§8 工具权限**：`group_send` 经 `--allowedTools` 自动放行（AM2）；其它 tool 的审批仍复用现有 backing session 的 `ToolPermissionRequest`/`AnswerToolPermission`，群 transcript 当系统行展示（E3 渲染层），无独立后端 Task。
> **能力门控**：MVP 仅 claudecode 声明 `CapMCPTools`；codex/builtin/piagent 入群被 `GroupBackendUnsupported` 拒绝，前端邀请列表按 cap 过滤（E3/E4）。
