# 删除群聊（能力并入编排）实现计划（plan-2）

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 干净砍除群聊功能（编排是其超集，概念近 1:1 映射，spec §11）：删 `group_entity`/`group_repo`/`group_svc`/`app/group.go`/全部 group-chat 前端/`group_create` 等 MCP 工具/group 表/group e2e；清理 `chat_svc` 群接缝；把流程库（`workflow`）从「群计数」改「Run 计数」。**前置：plan-1（编排后端）+ plan-1b（编排前端）已合并**——群的能力已由编排承接，方可安全删除。

**Architecture:** 群逻辑高度模块化、与普通单聊不纠缠（`chat_svc` 经函数指针注册 group provider，**不反向 import** group 包），故多为文件级删除；唯一需小心的是 `chat_svc`/`chat_repo` 的 ~300 行群接缝（`ensureGroupMemberSession`、`Session.GroupID`、`defaultSessionScope` 的 `group_id` 维度）。app 未发布、可 hard delete、无兼容层（[[project_release_status]]）。删除顺序：**先断消费者（前端 → bootstrap/app 绑定 → chat 接缝 → 工具/流程），再删领域包，最后加 drop 迁移**，保证每个 task 结束都能编译 + 测试绿。

**Tech Stack:** Go 1.26、cago、gorm/gormigrate、React/Vitest、Playwright、golangci-lint v2。

## Global Constraints

- **删除即重构,但只在本任务范围内**；不夹带无关改动。每个 task 结束 **`make test-backend` + 前端 vitest 必须绿**（删除不得留下编译错误/悬空引用）。
- **迁移追加,禁改既有**：群表由**新迁移 DROP**（202606030001/202606160001 两个建群迁移**保持不动**）；DDL 原生 SQL。
- **概念映射已定**（spec §11）：Group→`OrchestrationRun`、HostAgentID→`LeaderAgentID`、GroupTask→`Task`、group_send/@→dispatch/ask/send、WorkflowID 绑定→编排流程、scheduler→已 lift 进编排 runtime（plan-1）。
- **会一并失去（已与用户确认）**：群聊室式单一共享对话流、群昵称、固定成员名单 → 改为任务树 + 逐会话介入。
- **会话隔离交接**：plan-1 已让 `defaultSessionScope = group_id=0 AND run_id=0`；本计划删 group 维度后变 `run_id=0`（编排独占子会话隐藏）。
- **i18n**：删 `group.*` key 时两语言文件同步删；`i18n.test` 仍须绿。
- **删除的安全网 = 回归测试**：删一处先确认相关测试仍绿；并在合适处加「该能力已不存在」的断言（如 agenttool registry 不再含 `group_create`）。

---

## File Structure（本计划删除/修改）

**删除（前端）：** `frontend/src/components/agentre/group-chat/`（约 22 文件含 `__tests__`）、`frontend/src/stores/group-list-store.ts`、`frontend/src/stores/group-store.ts`、其 `__tests__`、`e2e/tests/group-chat*.spec.ts`。
**删除（后端）：** `internal/app/group.go`、`internal/service/group_svc/`（含 `scheduler.go`/`mcp.go`/`create.go` 等）、`internal/repository/group_repo/`、`internal/model/entity/group_entity/`。
**修改（前端）：** `frontend/src/stores/chat-tabs-store.ts`（删 `group`/`groupSession` TabKind）、`chat-tabs/chat-panel-host.tsx`（删 group 路由）、`App.tsx`/`AppRail`（删群区段 + 群事件订阅）、`i18n/locales/{zh-CN,en}/common.json`（删 `group.*`）。
**修改（后端）：** `internal/bootstrap/cago.go`（删群 provider 注册 + `/mcp/group/` 挂载）、`internal/app/app.go`（删 `group_svc.SetEmitter`/deps 接线）、`internal/service/chat_svc/*` + `internal/repository/chat_repo/session.go`（删群接缝）、`internal/pkg/agenttool/agenttool.go`（删 `KeyGroupCreate`）、`internal/service/workflow_svc/*`（群计数→Run 计数）。
**新增（后端）：** `migrations/202606240002_drop_group.go`（DROP 群表 + `chat_sessions.group_id` + 重置 DEFAULT `tools_json`）。

---

## Task 1: 删群聊前端 + App 外壳引用 + i18n + group e2e

**Files:**
- Delete: `frontend/src/components/agentre/group-chat/`（整目录）、`frontend/src/stores/group-list-store.ts`、`frontend/src/stores/group-store.ts`、相关 `__tests__`、`e2e/tests/group-chat*.spec.ts`。
- Modify: `frontend/src/stores/chat-tabs-store.ts`、`frontend/src/components/agentre/chat-tabs/chat-panel-host.tsx`、`frontend/src/App.tsx`/`AppRail`、`i18n/locales/{zh-CN,en}/common.json`。

> 前端是叶子，先删它不破坏 Go build。删后前端 vitest 必须绿（连同删掉群组件测试）。

- [ ] **Step 1: 删目录 + store + e2e**

```bash
cd /Users/codfrm/Code/agentre/agentre/frontend
rm -rf src/components/agentre/group-chat
rm -f src/stores/group-list-store.ts src/stores/group-store.ts
rm -f src/stores/__tests__/group-list-store.test.ts src/stores/__tests__/group-store.test.ts 2>/dev/null || true
cd .. && rm -f e2e/tests/group-chat*.spec.ts
```

- [ ] **Step 2: 摘 App 外壳引用** — `chat-tabs-store.ts` 的 `TabKind` 删 `group`/`groupSession` 两变体；`chat-panel-host.tsx` 删 `case "group"`/`"groupSession"` 分支及 `import { GroupChat }`；`App.tsx`/`AppRail` 删「群聊」区段项与群事件订阅（`group:*`）；`i18n/locales/{zh-CN,en}/common.json` 删整个 `group` 命名空间。

- [ ] **Step 3: 跑全量 vitest 看红 → 清残引用 → 绿**

Run: `cd frontend && pnpm test`
Expected: 先因悬空 import / 缺失 `group.*` key 失败 → 按报错删干净 → 全绿（`i18n.test`/`App.test`/`foundation.test` 通过）。

> 判断哪些测试受影响要跑**全量**（非 focused），见 [[reference_frontend_wails_runtime_test_mock]]。

- [ ] **Step 4: lint + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
git add -A frontend/ e2e/
git commit -m "🔥 删群聊前端(group-chat 组件/store/e2e)+ App 外壳引用 + group i18n"
```

---

## Task 2: 断 bootstrap/app 群绑定 + 删 `app/group.go` + 重生成绑定

**Files:**
- Delete: `internal/app/group.go`
- Modify: `internal/bootstrap/cago.go`（删群 provider 注册 + `gw.RegisterMCP("/mcp/group/", …)`）、`internal/app/app.go`（删 `group_svc.SetEmitter` + group deps 接线）
- Generate: `frontend/wailsjs/`（`make generate` → `GroupXxx` 绑定消失）

> `chat_svc` 不 import group（DIP，经函数指针注册），故断开 bootstrap 注册 + 删 app 绑定即摘掉消费者。`group_svc` 本身此步还在（下个 task 才删）。

- [ ] **Step 1: 删 `internal/app/group.go`** 与 bootstrap/app 中的群接线：

```bash
rm internal/app/group.go
```

`internal/bootstrap/cago.go`：删
```go
chat_svc.RegisterTurnMCPProvider(group_svc.Default().BuildCreateTurnMCP)   // ~line 175
chat_svc.RegisterTurnExtrasProvider(group_svc.Default().BuildSendTurnExtras) // ~line 178
gw.RegisterMCP("/mcp/group/", group_svc.Default().MCPHandler())             // 群 MCP 挂载
```
及对应 `group_svc` import（若仅此处用）。`internal/app/app.go` 的 `registerChatService()`：删 `group_svc.SetEmitter(...)` 与 `group_svc.Default().RegisterDeps(...)`。

- [ ] **Step 2: 编译看红 → 清残引用**

Run: `cd /Users/codfrm/Code/agentre/agentre && go build ./... 2>&1 | head`
Expected: 报 `app/group.go` 已删后的悬空引用（如 `toGroupItem`、`GroupItem`）/ bootstrap 未用 import → 逐一清。

- [ ] **Step 3: 重生成绑定 + 后端测试 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
make generate           # GroupList/GroupCreate/... 从 App.d.ts 消失
make test-backend
git add -A internal/app/ internal/bootstrap/ frontend/wailsjs/
git commit -m "🔥 删 app/group.go + 断 bootstrap/app 群绑定 + 重生成 wails 绑定"
```

---

## Task 3: 清 `chat_svc`/`chat_repo` 群接缝（~300 行，谨慎）

**Files:**
- Modify: `internal/repository/chat_repo/session.go`（`defaultSessionScope` 去 `group_id` 维度）
- Modify: `internal/service/chat_svc/*`（删 `ensureGroupMemberSession`、`Session.GroupID` 分支、群 turn 接缝、`EnsureSessionRequest` 的 group 字段）
- Modify: `internal/model/entity/chat_entity/session.go`（删 `GroupID` 字段）
- Test: `internal/service/chat_svc/*_test.go`、`internal/repository/chat_repo/session_test.go`（删群用例、保单聊/subagent/orch 用例）

> **这是 spec §11 点名"要小心"的接缝。** 守纪律：先跑既有 chat 测试确认绿基线 → 删群相关代码 + 群测试 → 重跑确认单聊 / subagent / 编排会话路径**仍绿**（这些是真正要保住的）。

- [ ] **Step 1: 基线** — `go test ./internal/service/chat_svc/... ./internal/repository/chat_repo/...` 记录当前绿。

- [ ] **Step 2: `defaultSessionScope` 去群维度** — `chat_repo/session.go`：

```go
func defaultSessionScope(db *gorm.DB) *gorm.DB {
	return db.Where("run_id = ?", 0) // 编排子会话隐藏(group 维度已随群删除移除)
}
```
更新 `session_test.go`：删断言 `group_id` 的用例，保留/新增断言生成 SQL 含 `run_id`。

- [ ] **Step 3: 删 chat_svc 群分支** — 删 `ensureGroupMemberSession`（`chat.go` ~3612-3663）及 `EnsureSession` 里对它的分派；删 `EnsureSessionRequest` 的 group 入参；删 turn 组装里 `sess.GroupID` 相关分支（`appendTurnMCP`/`fillGroupTurnExtras` 调用处的 `groupID` 实参改传 0 或删形参——**注意 `TurnMCPProvider`/`TurnExtrasProvider` 签名含 `groupID int64`**：编排不需要它，最简是保留签名传 `0`，避免动 orch 的 provider；或统一去掉该形参并改 orch provider。**推荐保留签名传 0**，最小触达）。删 `chat_entity.Session.GroupID` 字段及所有读写点。

- [ ] **Step 4: 删群测试 + 重跑保关键路径绿**

```bash
cd /Users/codfrm/Code/agentre/agentre
# 删 chat_svc 下专测群成员会话的 *_test.go(如 chat_ensure_group_member_session_test.go)
rm -f internal/service/chat_svc/chat_ensure_group_member_session_test.go
go test ./internal/service/chat_svc/... ./internal/repository/chat_repo/...
```
Expected: 绿（单聊 send/edit/regenerate、subagent 会话、编排会话、`defaultSessionScope` 用例全过）。

- [ ] **Step 5: Commit**

```bash
git add -A internal/service/chat_svc/ internal/repository/chat_repo/ internal/model/entity/chat_entity/
git commit -m "🔥 清 chat_svc/chat_repo 群接缝(ensureGroupMemberSession/Session.GroupID/scope)"
```

---

## Task 4: 删 `group_create` 工具 + agenttool 注册项

**Files:**
- Modify: `internal/pkg/agenttool/agenttool.go`（删 `KeyGroupCreate` 常量 + registry 项）
- Test: `internal/pkg/agenttool/agenttool_test.go`（加断言 registry 不再含 `group_create`）

> 群 MCP 挂载已在 Task 2 摘除；此步删工具元数据。`group_svc/mcp.go`（含 group_create 等 handler）随 Task 6 删包一并消失。

- [ ] **Step 1: 写失败测试**（registry 不应再有 group_create）

```go
func TestRegistry_NoGroupCreate(t *testing.T) {
	_, ok := agenttool.Lookup("group_create")
	assert.False(t, ok)
	assert.NotContains(t, agenttool.Keys(), "group_create")
}
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（仍存在）。

- [ ] **Step 3: 删** `agenttool.go` 的 `KeyGroupCreate` 常量与 `{Key: KeyGroupCreate, MCPPath: "/mcp/group/", …}` registry 项。

- [ ] **Step 4: 跑测试通过 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/pkg/agenttool/...
git add -A internal/pkg/agenttool/ && git commit -m "🔥 删 agenttool group_create 注册项"
```

---

## Task 5: 流程库从「群计数」改「Run 计数」

**Files:**
- Modify: `internal/service/workflow_svc/workflow.go`（`groupCounts` → `runCounts`，依赖 `orch_repo` 而非 group）
- Test: `internal/service/workflow_svc/*_test.go`

> spec §11：`workflow`/流程库 → 重定位为「编排流程库」，`workflow_svc` 保留，**去掉 group 计数、改 Run 计数**（每个流程被多少个 Run 使用）。

- [ ] **Step 1: 写失败测试**（List 返回的每个 workflow 带 `runCount`，由 `orch_repo` 统计引用该 flowId 的 Run 数）

```go
// 注入 orch run repo mock: 返回 flowId=1 被 2 个 Run 使用 → workflow#1.runCount==2
// 断言 resp.Workflows[0].RunCount == 2, 且不再调用任何 group 仓储。
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（仍是 group 计数 / `RunCount` 字段不存在）。

- [ ] **Step 3: 实现** — `workflow_svc` 删 `groupCounts()`，加 `runCounts()`：经 `orch_repo.Run().List()` 统计各 `FlowID` 出现次数；`ListWorkflowsResponse` 的 item 字段 `GroupCount`→`RunCount`。前端流程库页（若有引用 groupCount）同步改名（plan-1b 的创建弹窗读 `WorkflowList`，用 runCount 展示"被 N 个 Run 使用"）。

- [ ] **Step 4: 跑测试通过 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./internal/service/workflow_svc/...
git add -A internal/service/workflow_svc/ && git commit -m "♻️ workflow_svc: 流程库计数 group→Run(编排流程库)"
```

---

## Task 6: 删 `group_svc` / `group_repo` / `group_entity` 包

**Files:**
- Delete: `internal/service/group_svc/`、`internal/repository/group_repo/`、`internal/model/entity/group_entity/`（含各自 `mock_*` 与 `*_test.go`）

> 此时三包已无外部消费者（前端/app/bootstrap/chat/workflow 均已断）。删后全量编译验证。

- [ ] **Step 1: 删包**

```bash
cd /Users/codfrm/Code/agentre/agentre
rm -rf internal/service/group_svc internal/repository/group_repo internal/model/entity/group_entity
```

- [ ] **Step 2: 编译 + 全量后端测试看残引用 → 清干净**

Run: `go build ./... && make test-backend`
Expected: 若仍有 import `group_svc`/`group_repo`/`group_entity` 的残点 → 报错定位 → 删除。直到全绿。

- [ ] **Step 3: Commit**

```bash
git add -A && git commit -m "🔥 删 group_svc/group_repo/group_entity 三包(能力已并入编排)"
```

---

## Task 7: 迁移 — DROP 群表 + `chat_sessions.group_id` + 重置工具种子

**Files:**
- Create: `migrations/202606240002_drop_group.go`
- Modify: `migrations/migrations.go`（`migrationList()` 末尾追加）
- Test: `migrations/202606240002_drop_group_test.go`

> 迁移追加、禁改既有；app 未发布 + hard delete，无需保数据。`workflows` 表**保留**（编排流程库复用）。

- [ ] **Step 1: 写失败测试**（跑全量迁移后群表不存在、`chat_sessions.group_id` 不存在、`workflows` 仍在、DEFAULT `tools_json` 不含 group_create）

```go
func TestMigration202606240002_DropGroup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))
	for _, tb := range []string{"groups", "group_members", "group_messages", "group_tasks"} {
		require.False(t, tableExists(t, db, tb), tb+" 应已删")
	}
	require.False(t, columnExists(t, db, "chat_sessions", "group_id"))
	require.True(t, tableExists(t, db, "workflows"), "流程库表保留")
	var tools string
	require.NoError(t, db.Raw(`SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1`).Scan(&tools).Error)
	require.NotContains(t, tools, "group_create")
	require.Contains(t, tools, "orchestrate")
}
```

- [ ] **Step 2: 跑测试看它失败** → FAIL（群表仍在 / `group_id` 仍在）。

- [ ] **Step 3: 写迁移 `202606240002_drop_group.go`**

```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240002 删群聊(能力并入编排):DROP 群 4 表 + chat_sessions.group_id + 重置 DEFAULT tools_json。
func migration202606240002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240002",
		Migrate: func(tx *gorm.DB) error {
			for _, sql := range []string{
				`DROP TABLE IF EXISTS group_tasks`,
				`DROP TABLE IF EXISTS group_messages`,
				`DROP TABLE IF EXISTS group_members`,
				`DROP TABLE IF EXISTS groups`,
				`ALTER TABLE chat_sessions DROP COLUMN group_id`, // SQLite ≥3.35
				// 群删除后重置 DEFAULT 工具集为 org + orchestrate(去 group_create)。
				`UPDATE agents SET tools_json='[{"key":"org","enabled":true},{"key":"orchestrate","enabled":true}]' WHERE system_badge='DEFAULT'`,
			} {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
			}
			return nil
		},
		// 不写 Rollback:群已硬删、无回滚需求(app 未发布)。
	}
}
```

并在 `migrations.go` 的 `migrationList()` 末尾追加 `migration202606240002(), // 删群聊(能力并入编排)`。

> 若 `ALTER TABLE chat_sessions DROP COLUMN group_id` 在目标 SQLite 版本不支持，退化为「建新表无 group_id → 拷数据 → 换名」的标准 SQLite 重建表法（参考仓库既有重建迁移）；并在测试里据实选路。

- [ ] **Step 4: 跑测试通过 + 全量迁移回归 + Commit**

```bash
cd /Users/codfrm/Code/agentre/agentre
go test ./migrations/...
git add -A migrations/ && git commit -m "🔥 migration 202606240002: DROP 群表 + chat_sessions.group_id + 重置工具种子"
```

---

## Task 8: 终扫 — 残留 `group` 引用清零 + 全绿

**Files:** 全仓库扫描（不改文件除非清残留）。

- [ ] **Step 1: 扫残留**

```bash
cd /Users/codfrm/Code/agentre/agentre
grep -rn --include=*.go -iE "group_svc|group_repo|group_entity|GroupID|group_create|ensureGroupMemberSession" internal/ cmd/ migrations/ | grep -v "_test.go" | grep -viE "subgroup|grouping"
grep -rn "group-chat\|group-store\|groupSession\|\"group\"" frontend/src | grep -v node_modules
```
Expected: 仅剩注释/无关词（`grouping` 之类）；真实残留 → 清。

> 注意 spec §11：按 `group` 子串粗筛后端 `.go` 命中可达 ~114（含注释/无关词），需逐一甄别——只删真正的群功能引用，别误伤 `grouping`/`subgroup` 等。

- [ ] **Step 2: 全绿校验**

```bash
make test-backend          # Go 全绿
cd frontend && pnpm test   # 前端全绿
cd .. && make lint         # golangci v2 + ESLint 干净
make e2e                   # 编排 e2e 绿(无 group spec 残留)
```

- [ ] **Step 3: 文档同步**（in-scope：删功能的文档引用）— 按 [[doc-maintenance]] 更新 `AGENTS.md`/`docs/*`/`CLAUDE.md` 中关于群聊的描述（删 stale 事实,不留 deprecation 注释）。单独 commit。

```bash
git add -A && git commit -m "📝 删群聊后文档同步(移除群聊 stale 描述)"
```

- [ ] **Step 4: 真机手验（不阻塞）** — 起一个真编排 Run，确认单聊/编排/会话列表正常，群入口与残留 UI 彻底消失。

## 交付边界

- **含**：群聊全栈删除（前端 + 后端三包 + app 绑定 + chat 接缝 + group_create 工具 + group 表）、流程库改 Run 计数、drop 迁移、文档同步。
- **不含**：编排自身功能（plan-1/plan-1b 已交付）；`workflows` 表/流程库本体（保留复用）。
- **取舍（已确认）**：失去群聊室式共享对话流、群昵称、固定成员名单——由任务树 + 逐会话介入取代。
