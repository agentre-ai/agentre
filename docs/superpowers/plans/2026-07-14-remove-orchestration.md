# Remove Orchestration Subsystem — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Delete the orchestration (编排) subsystem and the flow library (流程库) in full — backend engine, MCP tools, Wails bindings, DB tables, frontend pages/stores, and related docs.

**Architecture:** Removal in dependency order — frontend consumers first (so `pnpm test`/tsc stay green while the backend still exposes bindings), then backend packages + wiring + chat_svc integration, then an appended drop migration, then docs. Each task ends on a green gate and its own pathspec commit.

**Tech Stack:** Go 1.26 (cago, gormigrate, SQLite), Wails v2, React 19 / TypeScript / Vite, pnpm, Vitest.

## Global Constraints

- **Branch:** `develop/wyz`. Concurrent sessions share the index — **always** `git commit <pathspec>` with explicit files, never bare `git commit`. Never sweep another session's WIP into a commit.
- **Pending foreign changes at plan time:** `frontend/src/i18n/locales/{en,zh-CN}/common.json` and `frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts` have uncommitted edits from another session. Inspect (`git diff <file>`) before touching; if you cannot cleanly separate your removals from their WIP, STOP and flag the maintainer.
- **Migrations:** append the new drop migration to the **end** of `migrationList()` in `migrations/migrations.go`. Do **not** modify or delete any existing migration file. Prefer native SQL for DDL.
- **i18n parity:** every key removed from `zh-CN/common.json` must be removed from `en/common.json` and vice-versa; `frontend/src/__tests__/i18n.test.ts` must stay green.
- **Gitmoji** commit messages. golangci-lint **v2**.
- **Keep (out of scope):** `org` / `subagent` / `hook` MCP tools; the generic MCP reverse-tunnel + `turn_mcp` provider registry + `agentruntime` MCPServers plumbing; `reloadSidebarSources` / `stores/sidebar-reload.ts`; the turn event dispatcher (`chat_svc/dispatcher_*.go`, `turn/dispatcher.go`) and daemon RPC `Dispatch`.
- **Line numbers in this plan are from a snapshot — re-grep identifiers before editing; they may have shifted.**

---

### Task 1: Frontend — remove orchestration + flow-library UI

**Files:**
- Delete (whole): `frontend/src/components/agentre/orchestration/` (entire dir incl. `__tests__/`)
- Delete (whole): `frontend/src/components/agentre/workflows/` (entire dir)
- Delete: `frontend/src/components/agentre/orch-events-host.tsx`, `frontend/src/components/agentre/orch-notifier.tsx`, and `frontend/src/components/agentre/__tests__/orch-events-host.test.tsx`, `.../__tests__/orch-notifier.test.tsx`
- Delete: `frontend/src/stores/orch-run-store.ts`, `orch-run-list-store.ts`, `orch-subagents-store.ts`, `workflow-manager-store.ts` (+ their `__tests__/*.test.ts`)
- Delete: `frontend/src/hooks/use-workflows.ts` (+ test)
- Delete (after consumer re-verify): `frontend/src/hooks/use-live-conversation.ts` (+ test), `frontend/src/hooks/use-composer-send.ts` (+ test)
- Delete: `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx` (+ test)
- Modify: `frontend/src/App.tsx`, `frontend/src/components/agentre/index.ts`, `frontend/src/components/agentre/command-palette/command-palette.tsx`, `frontend/src/components/agentre/org/tool-catalog.ts`
- Modify (i18n): `frontend/src/i18n/locales/zh-CN/common.json`, `frontend/src/i18n/locales/en/common.json`
- Modify (tests/mocks): `frontend/src/__tests__/mocks/wailsApp.ts`, `frontend/src/__tests__/App.test.tsx`, `frontend/src/components/agentre/__tests__/chat-page.test.tsx`, `frontend/src/components/agentre/tool-approval/card.test.tsx`

**Interfaces:**
- Produces: nothing consumed by later tasks (frontend is a leaf here). After this task the frontend no longer references any orchestration/workflow Wails binding, so Task 2 can delete those `App` methods without breaking TS.

- [ ] **Step 1: Inspect the foreign pending changes**

Run: `git diff frontend/src/i18n/locales/en/common.json frontend/src/i18n/locales/zh-CN/common.json frontend/src/components/agentre/org/__tests__/tool-catalog.test.ts`
Confirm those hunks are unrelated to orchestration removal. If they overlap the `orchestration.*` / `workflows.*` sections you're about to delete, STOP and flag the maintainer. Otherwise proceed and, at commit time, commit only the specific files with pathspec.

- [ ] **Step 2: Re-verify the two shared hooks are orchestration-only before deleting**

Run:
```bash
cd frontend
rg -l "use-composer-send|useComposerSend" src | rg -v "orchestration/|use-composer-send"
rg -l "use-live-conversation|useLiveConversation" src | rg -v "orchestration/|use-live-conversation"
```
Expected: no output (only orchestration + the hook's own files match). If any non-orchestration consumer prints, do NOT delete that hook — flag it.

- [ ] **Step 3: Detach orchestration from the app shell (`App.tsx`)**

In `frontend/src/App.tsx` remove: the imports of `OrchEventsHost`, `OrchNotifier`, `OrchestrationPage`, `WorkflowManagerDialog` (and the now-unused `topologyStar3Icon` icon import); the `navItems` entry for `/orchestration` (`nav.orchestration`); the `pageBreadcrumbKeys["/orchestration"]` entry; the breadcrumb fallback branch `location.pathname.startsWith("/orchestration") ? ...`; the root-mounted `<OrchEventsHost />`, `<OrchNotifier />`, `<WorkflowManagerDialog />` (and their comment); and both `<Route path="/orchestration" ...>` / `<Route path="/orchestration/:runId" ...>` routes.

- [ ] **Step 4: Remove the barrel exports + palette source + tool-catalog entry**

- `frontend/src/components/agentre/index.ts`: remove the `OrchEventsHost`, `OrchNotifier`, `WorkflowManagerDialog`, `OrchestrationPage` exports.
- `frontend/src/components/agentre/command-palette/command-palette.tsx`: remove the `workflowActionsSource` import and its entry in the `SOURCES` array.
- `frontend/src/components/agentre/org/tool-catalog.ts`: remove `"workflow"` from `APPROVAL_TOOLS` (leave `"org"`, `"hook"`).

- [ ] **Step 5: Delete the whole-file orchestration/flow-library frontend code**

Run:
```bash
cd frontend
rm -rf src/components/agentre/orchestration
rm -rf src/components/agentre/workflows
rm -f src/components/agentre/orch-events-host.tsx src/components/agentre/orch-notifier.tsx
rm -f src/components/agentre/__tests__/orch-events-host.test.tsx src/components/agentre/__tests__/orch-notifier.test.tsx
rm -f src/stores/orch-run-store.ts src/stores/orch-run-list-store.ts src/stores/orch-subagents-store.ts src/stores/workflow-manager-store.ts
rm -f src/stores/__tests__/orch-run-store.test.ts src/stores/__tests__/orch-run-list-store.test.ts src/stores/__tests__/orch-subagents-store.test.ts src/stores/__tests__/workflow-manager-store.test.ts
rm -f src/hooks/use-workflows.ts src/hooks/__tests__/use-workflows.test.ts
rm -f src/hooks/use-live-conversation.ts src/hooks/__tests__/use-live-conversation.test.ts
rm -f src/hooks/use-composer-send.ts src/hooks/__tests__/use-composer-send.test.ts
rm -f src/components/agentre/command-palette/sources/workflow-actions-source.tsx src/components/agentre/command-palette/sources/workflow-actions-source.test.tsx
```
(Adjust exact test paths if `__tests__` layout differs — `rg --files | rg 'orch|workflow'` to confirm none are orphaned.)

- [ ] **Step 6: Update shared test mocks**

- `frontend/src/__tests__/mocks/wailsApp.ts`: remove the `WorkflowList/WorkflowCreate/WorkflowUpdate/WorkflowDelete` mock block and the `RunCreate/RunList/RunLoad/RunPause/RunResume/RunStop/RunSpeak` mock block.
- `frontend/src/__tests__/App.test.tsx`: remove assertions referencing the `/orchestration` route / `nav.orchestration` nav item.
- `frontend/src/components/agentre/__tests__/chat-page.test.tsx`: remove the `WorkflowList: vi.fn()` stub.
- `frontend/src/components/agentre/tool-approval/card.test.tsx`: remove the `describe("workflow", ...)` block that tests `workflow_create` approval routing.

- [ ] **Step 7: Remove i18n keys (both locales, keep parity)**

In each of `frontend/src/i18n/locales/zh-CN/common.json` and `en/common.json` remove: `nav.orchestration`; the top-level `"orchestration": { ... }` section; the top-level `"workflows": { ... }` section; `commandPalette.workflows`; and inside `org.*.tools`: `names.workflow`, `names.orchestrate`, `descriptions.workflow`, `descriptions.orchestrate`. Leave all other keys byte-for-byte unchanged. Verify the JSON still parses (`node -e "require('./src/i18n/locales/en/common.json')"` from `frontend/`).

- [ ] **Step 8: Run the frontend gates**

Run:
```bash
cd frontend
pnpm test 2>&1 | tail -30
pnpm exec tsc --noEmit 2>&1 | tail -20
pnpm exec eslint src 2>&1 | tail -20
```
Expected: Vitest all pass (no orphaned imports), tsc clean, eslint clean (no `no-literal-string`, no unused imports). Fix any dangling references the compiler surfaces.

- [ ] **Step 9: Commit (pathspec)**

Stage only the files this task touched, then commit with an explicit pathspec list. Example:
```bash
git add -A frontend/src/components/agentre/orchestration frontend/src/components/agentre/workflows   # captures deletions
git commit \
  frontend/src/App.tsx \
  frontend/src/components/agentre/index.ts \
  frontend/src/components/agentre/command-palette/command-palette.tsx \
  frontend/src/components/agentre/org/tool-catalog.ts \
  frontend/src/__tests__/mocks/wailsApp.ts \
  frontend/src/__tests__/App.test.tsx \
  frontend/src/components/agentre/__tests__/chat-page.test.tsx \
  frontend/src/components/agentre/tool-approval/card.test.tsx \
  frontend/src/i18n/locales/zh-CN/common.json \
  frontend/src/i18n/locales/en/common.json \
  <all deleted paths> \
  -m "🔥 chat(fe): 移除编排页面与流程库 UI"
```
For deletions, `git commit <deleted-path>` records the removal; list each deleted file (or use `git add -A <dir>` for whole dirs then include the dir/files in the pathspec). Verify `git show --stat HEAD` contains **only** orchestration/workflow files — no foreign `common.json` WIP beyond your key removals. If the other session's `common.json` edits got swept in, reset and redo the i18n removal as an isolated hunk.

---

### Task 2: Backend — remove orchestration + flow-library packages, tools, wiring

**Files:**
- Delete (whole dirs): `internal/service/orch_svc/` (incl. `mock_orch_svc/`), `internal/repository/orch_repo/` (incl. `mock_orch_repo/`), `internal/model/entity/orch_entity/`, `internal/service/workflow_svc/`, `internal/repository/workflow_repo/` (incl. mocks), `internal/model/entity/workflow_entity/`, `internal/service/workflowtool_svc/` (incl. mocks)
- Delete: `internal/app/orch.go`, `internal/app/orch_adapter.go`, `internal/app/workflow.go`
- Modify: `internal/pkg/agenttool/agenttool.go`, `internal/bootstrap/cago.go`, `internal/app/app.go`, `internal/pkg/code/code.go`, `internal/pkg/code/zh_cn.go`
- Modify (chat_svc): `internal/service/chat_svc/chat.go`, `types.go`, `turn_extras.go`, `turn_mcp.go` (only to drop orch/workflow registration, keep the registry)
- Modify (chat schema): `internal/model/entity/chat_entity/session.go`, `internal/repository/chat_repo/session.go` (+ the chat_repo `*_test.go` referencing `run_id` scoping)
- Modify (data export, if present): `internal/model/entity/doc.go`, `internal/service/data_svc/doc.go`

**Interfaces:**
- Consumes: Task 1 already removed all frontend references to `Run*` / `Workflow*` bindings.
- Produces: after `make generate`, the Wails bindings no longer expose `RunCreate/RunList/RunLoad/RunPause/RunResume/RunStop/RunSpeak/WorkflowList/WorkflowCreate/WorkflowUpdate/WorkflowDelete`.

- [ ] **Step 1: Remove the tool-set registry entries**

`internal/pkg/agenttool/agenttool.go`: remove the `KeyOrchestrate` and `KeyWorkflow` consts and their entries in the tool-set registry (the `/mcp/orchestrate/` and `/mcp/workflow/` definitions with their `ToolNames`). Leave `KeyOrg`, `KeySubagent`, `KeyHook` intact.

- [ ] **Step 2: Remove bootstrap wiring**

`internal/bootstrap/cago.go`: remove `gw.RegisterMCP("/mcp/orchestrate/", ...)`, `gw.RegisterMCP("/mcp/workflow/", ...)`, the two `SetGatewayBaseURL` calls for orch/workflow, `chat_svc.RegisterTurnMCPProvider(orch_svc.Default().BuildTurnMCP)`, `chat_svc.RegisterTurnMCPProvider(workflowtool_svc...BuildTurnMCP)`, and `chat_svc.RegisterTurnExtrasProvider(orch_svc.Default().BuildTurnExtras)`. Leave the org/subagent/hook `RegisterTurnMCPProvider` calls and `remote.RegisterMCPProxyDispatcher`.

- [ ] **Step 3: Remove App wiring + bindings**

- `internal/app/app.go`: remove `orch_svc.Default().RegisterDeps(...)`, `RegisterWorkflowReader(...)`, `RegisterTodoRepo(...)`, and `workflowtool_svc.RegisterDeps(...)` (the `RunCreateDeps` block and the workflowtool deps block).
- Delete `internal/app/orch.go`, `internal/app/orch_adapter.go`, `internal/app/workflow.go`.

- [ ] **Step 4: Remove chat_svc orchestration integration**

In `internal/service/chat_svc/`:
- `types.go`: remove `SessionPurposeOrchChild`, and the `RunID`, `MCPServers`, `SystemPromptSuffix`, `EmitTurnStartedBypass` fields.
- `chat.go`: remove `createOrchChildSession(...)` and its `SessionPurposeOrchChild` case; remove the `buildTurnExtras` / `fillGroupTurnExtras` call site and the `extras.systemPromptSuffix` append onto `RunRequest.SystemPrompt`; remove any `RunID`/`MCPServers`/`systemPromptSuffix` threading.
- `turn_extras.go`: delete the file (the `TurnExtrasProvider` type + `RegisterTurnExtrasProvider` + `fillGroupTurnExtras` — dead once orch is gone). Grep first: `rg "TurnExtrasProvider|RegisterTurnExtrasProvider|fillGroupTurnExtras|systemPromptSuffix|SystemPromptSuffix" internal` and confirm every hit is orch-related before removing.
- `turn_mcp.go`: KEEP the file and the `TurnMCPProvider` registry (org/subagent/hook use it); just confirm no orch/workflow registration remains here.

- [ ] **Step 5: Remove the run_id column usage from chat schema**

- `internal/model/entity/chat_entity/session.go`: remove `SessionPurposeOrchChild` and the `RunID` struct field/column tag.
- `internal/repository/chat_repo/session.go`: remove the `run_id = 0` filter in `defaultSessionScope` and the `run_id > 0` special-cases in the count/list queries.
- Update the chat_repo `*_test.go` (sqlmock) expectations that asserted the `run_id` predicate in the generated SQL.

- [ ] **Step 6: Remove the error code + data-export references**

- `internal/pkg/code/code.go` + `internal/pkg/code/zh_cn.go`: remove `WorkflowNotFound` (20800) and its message.
- Run `rg "orch_entity|workflow_entity|orchestration_runs|orch_dispatches|\bworkflows\b" internal/model/entity/doc.go internal/service/data_svc/doc.go` and remove any registry entries for the removed tables/entities.

- [ ] **Step 7: Delete the backend packages**

Run:
```bash
rm -rf internal/service/orch_svc internal/repository/orch_repo internal/model/entity/orch_entity
rm -rf internal/service/workflow_svc internal/repository/workflow_repo internal/model/entity/workflow_entity
rm -rf internal/service/workflowtool_svc
```

- [ ] **Step 8: Regenerate mocks/bindings and compile**

Run:
```bash
make mock 2>&1 | tail -20
make generate 2>&1 | tail -20
go build ./... 2>&1 | tail -40   # or: GOWORK inherited; must compile
```
Expected: builds clean. Fix every remaining reference the compiler reports (there should be none outside the files above; if an unexpected consumer appears, grep it and remove — do not add a shim).

- [ ] **Step 9: Run backend gates**

Run:
```bash
make test-backend 2>&1 | tail -30
gofmt -l internal | tail
make lint 2>&1 | tail -30
```
Expected: tests pass, `gofmt -l` prints nothing, lint clean.

- [ ] **Step 10: Commit (pathspec)**

Commit the modified files + deletions with an explicit pathspec (list each modified file and deleted dir/file), plus the regenerated `frontend/wailsjs/` bindings and any regenerated mocks:
```bash
git commit \
  internal/pkg/agenttool/agenttool.go internal/bootstrap/cago.go internal/app/app.go \
  internal/app/orch.go internal/app/orch_adapter.go internal/app/workflow.go \
  internal/service/chat_svc/chat.go internal/service/chat_svc/types.go internal/service/chat_svc/turn_extras.go \
  internal/model/entity/chat_entity/session.go internal/repository/chat_repo/session.go \
  internal/pkg/code/code.go internal/pkg/code/zh_cn.go \
  internal/service/orch_svc internal/repository/orch_repo internal/model/entity/orch_entity \
  internal/service/workflow_svc internal/repository/workflow_repo internal/model/entity/workflow_entity \
  internal/service/workflowtool_svc \
  frontend/wailsjs \
  <any modified chat_repo test + doc.go files> \
  -m "🔥 移除编排引擎+流程库(后端包/工具/绑定/chat_svc 集成)"
```
Verify `git show --stat HEAD` is orchestration-scoped only.

---

### Task 3: Backend — append the drop migration

**Files:**
- Create: `migrations/202607140001_drop_orchestration.go`
- Create: `migrations/202607140001_drop_orchestration_test.go`
- Modify: `migrations/migrations.go` (append one line to `migrationList()`)

**Interfaces:**
- Consumes: nothing from Task 2 code (migrations are self-contained native SQL).
- Produces: on `RunMigrations`, tables `orchestration_runs` / `orch_dispatches` / `orch_tasks` / `workflows` and column `chat_sessions.run_id` are gone; `orch_child` sessions + their messages are deleted; `orchestrate`/`workflow` tool seeds are stripped from `agents.tools_json`.

- [ ] **Step 1: Write the failing migration test**

Create `migrations/202607140001_drop_orchestration_test.go`:
```go
package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607140001_DropsOrchestration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	hasTable := func(table string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		return n > 0
	}
	hasCol := func(table, col string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col).Scan(&n)
		return n > 0
	}

	// Orchestration + flow-library tables are gone.
	assert.False(t, hasTable("orchestration_runs"))
	assert.False(t, hasTable("orch_dispatches"))
	assert.False(t, hasTable("orch_tasks"))
	assert.False(t, hasTable("workflows"))
	// chat_sessions.run_id column is gone.
	assert.False(t, hasCol("chat_sessions", "run_id"))

	// DEFAULT agent tools_json no longer contains orchestrate/workflow.
	var tools string
	db.Raw("SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1").Scan(&tools)
	assert.NotContains(t, tools, `"orchestrate"`)
	assert.NotContains(t, tools, `"workflow"`)
}
```

- [ ] **Step 2: Run the test, watch it fail for the right reason**

Run: `go test ./migrations/ -run TestMigration202607140001_DropsOrchestration -v 2>&1 | tail -20`
Expected: FAIL — either compile error (`migration202607140001` undefined) once the list line is added, or, before wiring, the tables/column still exist so the `assert.False` checks fail. Confirm the failure is "tables still present / column still present", not something unrelated.

- [ ] **Step 3: Write the migration**

Create `migrations/202607140001_drop_orchestration.go`:
```go
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607140001 删除编排子系统:DROP 编排/流程库 4 表 + chat_sessions.run_id 列,
// 清掉孤儿编排子会话(purpose='orch_child')及其消息,并从 agents.tools_json 去掉
// orchestrate/workflow 工具种子(保留 org/subagent/hook)。
// 编排能力被整体移除,应用未发布,硬删除,无 Rollback。
func migration202607140001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607140001",
		Migrate: func(tx *gorm.DB) error {
			for _, sql := range []string{
				// 先删孤儿编排子会话消息,再删会话(此时 run_id 列还在,可用于过滤)。
				`DELETE FROM chat_messages WHERE session_id IN (
					SELECT id FROM chat_sessions WHERE purpose='orch_child' OR run_id > 0)`,
				`DELETE FROM chat_sessions WHERE purpose='orch_child' OR run_id > 0`,
				// 去掉 chat_sessions.run_id 列(SQLite >= 3.35 原生支持 DROP COLUMN)。
				`ALTER TABLE chat_sessions DROP COLUMN run_id`,
				// DROP 编排/流程库表(各自索引随表一并删除)。
				`DROP TABLE IF EXISTS orch_dispatches`,
				`DROP TABLE IF EXISTS orch_tasks`,
				`DROP TABLE IF EXISTS orchestration_runs`,
				`DROP TABLE IF EXISTS workflows`,
				// 从 tools_json 里剔除 orchestrate/workflow,保留其余工具(org/subagent/hook)。
				`UPDATE agents SET tools_json = (
					SELECT COALESCE(json_group_array(json(value)), '[]')
					FROM json_each(CASE WHEN json_valid(tools_json) THEN tools_json ELSE '[]' END)
					WHERE json_extract(value, '$.key') NOT IN ('orchestrate','workflow'))
				WHERE json_valid(tools_json)
				  AND (instr(tools_json, '"orchestrate"') > 0 OR instr(tools_json, '"workflow"') > 0)`,
			} {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
			}
			return nil
		},
		// No Rollback: orchestration hard-deleted, app unreleased.
	}
}
```
Note on `DROP COLUMN`: `202606240001` added `chat_sessions.run_id` with no index, so the drop is direct. If a later migration added an index on `run_id`, `DROP INDEX IF EXISTS <name>` must precede the `ALTER`. Grep `rg "run_id" migrations/*.go` to confirm no index exists before finalizing.

- [ ] **Step 4: Register the migration**

In `migrations/migrations.go`, append to the end of the `migrationList()` slice (after `migration202607090003()`):
```go
		migration202607140001(), // 移除编排子系统:DROP 编排/流程库 4 表 + chat_sessions.run_id + 清工具种子
```

- [ ] **Step 5: Run the test, watch it pass**

Run: `go test ./migrations/ -run TestMigration202607140001_DropsOrchestration -v 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 6: Run the full migrations package (ensure no existing migration test broke)**

Run: `go test ./migrations/ 2>&1 | tail -20`
Expected: all pass. (The existing `202607090002` checklist test etc. still run the create-migrations before the new drop — the drop runs last, so those intermediate assertions are unaffected; the drop test asserts the final state.)

- [ ] **Step 7: Commit (pathspec)**

```bash
git commit \
  migrations/202607140001_drop_orchestration.go \
  migrations/202607140001_drop_orchestration_test.go \
  migrations/migrations.go \
  -m "🔥 迁移: 删除编排/流程库表 + chat_sessions.run_id + 工具种子"
```

---

### Task 4: Docs — delete orchestration specs/plans + surgical edits

**Files:**
- Delete: the orchestration/flow-library/workflow-relocation/group-task-orchestration/group-chat-orchestration spec files under `docs/superpowers/specs/` and plan files under `docs/superpowers/plans/` (list below)
- Modify: `docs/session-lifecycle.md`, `docs/agent-backend.md`, `docs/debugging.md`, `docs/e2e-harness-guide.md`, `docs/DESIGN.md`, `docs/doc-maintenance.md`, `AGENTS.md`

**Interfaces:** none (docs only).

- [ ] **Step 1: Delete the orchestration spec + plan files**

Run (verify each path exists first with `ls`; this today's design doc `2026-07-14-remove-orchestration-design.md` and this plan are NOT deleted):
```bash
cd docs/superpowers
rm -f specs/2026-06-03-group-chat-orchestration-design.md \
  specs/2026-06-11-group-task-orchestration-design.md \
  specs/2026-06-13-workflow-library-relocation-and-agent-tool-design.md \
  specs/2026-06-23-agent-orchestration-design.md \
  specs/2026-06-25-agent-orchestration-board-design.md \
  specs/2026-07-03-orchestration-harness-design.md \
  specs/2026-07-03-orchestration-project-selection-design.md \
  specs/2026-07-03-orchestration-run-new-team-department-picker-design.md \
  specs/2026-07-04-orch-conversation-streaming-design.md \
  specs/2026-07-04-orchestration-dag-designer-phase2-design.md \
  specs/2026-07-04-orchestration-dag-phase3-overlay-design.md \
  specs/2026-07-04-orchestration-flow-dag-designer-design.md \
  specs/2026-07-05-orchestration-flow-prompt-template-design.md \
  specs/2026-07-08-orchestration-default-flow-library-design.md \
  specs/2026-07-08-orchestration-remove-flow-dag-steps-design.md \
  specs/2026-07-09-orch-dispatch-rename-and-task-checklist-design.md
rm -f plans/2026-06-03-group-chat-orchestration.md \
  plans/2026-06-11-group-task-orchestration-pr1-backend.md \
  plans/2026-06-12-group-task-orchestration-pr2-frontend.md \
  plans/2026-06-12-group-task-orchestration-pr3-workflows.md \
  plans/2026-06-12-group-task-orchestration-pr4-e2e.md \
  plans/2026-06-12-group-task-orchestration-pr5-group-create.md \
  plans/2026-06-13-workflow-relocation-pr1-frontend.md \
  plans/2026-06-13-workflow-relocation-pr2-tool-approval.md \
  plans/2026-06-13-workflow-relocation-pr3-workflowtool.md \
  plans/2026-06-13-workflow-relocation-pr4-frontend-tool.md \
  plans/2026-06-13-workflow-relocation-pr5-group-create-workflow.md \
  plans/2026-06-24-agent-orchestration-backend.md \
  plans/2026-06-24-agent-orchestration-frontend.md \
  plans/2026-06-24-remove-group-chat.md \
  plans/2026-06-25-orchestration-board-roadmap.md \
  plans/2026-06-25-workflow-tags-outline.md \
  plans/2026-06-26-orchestration-design-fidelity.md \
  plans/2026-06-26-orchestration-s3-peer-ask-reply.md \
  plans/2026-06-26-orchestration-s4-structure-graph-model.md \
  plans/2026-06-26-orchestration-s5-task-board-and-subagents.md \
  plans/2026-06-26-orchestration-s6-drilldown-conversation.md \
  plans/2026-06-26-orchestration-s7-top-level-page.md \
  plans/2026-07-02-orchestration-overview-and-gaps.md \
  plans/2026-07-03-orchestration-harness-a-layered-reporting.md \
  plans/2026-07-03-orchestration-harness-e-participant-scoping.md \
  plans/2026-07-03-orchestration-project-selection.md \
  plans/2026-07-03-orchestration-team-department-picker.md \
  plans/2026-07-04-orch-conversation-streaming.md \
  plans/2026-07-04-orchestration-dag-designer-phase2.md \
  plans/2026-07-04-orchestration-dag-phase3-overlay.md \
  plans/2026-07-04-orchestration-flow-dag-designer-phase1.md \
  plans/2026-07-04-orchestration-harness-slice-b.md \
  plans/2026-07-05-orchestration-flow-prompt-template.md \
  plans/2026-07-08-orchestration-default-flow-library.md \
  plans/2026-07-08-orchestration-remove-flow-dag-steps.md \
  plans/2026-07-09-orch-dispatch-rename-and-task-checklist.md \
  plans/2026-06-11-agent-org-tool.md \
  plans/2026-06-15-subagent-call-tool.md
rm -f specs/2026-06-11-agent-org-tool-design.md \
  specs/2026-06-15-subagent-call-tool-design.md \
  specs/2026-06-15-group-create-tool-gating-design.md
```
Note: `org`/`subagent` tools are KEPT in code (Task 1/2), but their historical build **specs/plans** describe the orchestration lineage; per the maintainer's "remove all related docs" they are deleted here. If you'd rather keep the org/subagent build docs, drop the last two `rm` blocks — confirm with the reviewer.

- [ ] **Step 2: Surgical doc edits**

- `docs/session-lifecycle.md`: delete the `### Orchestration Child Sessions` subsection; remove `orch_svc` from the "must not call `chat_repo.Session().Create` directly" list; reword the intro line that lists "orchestration dispatch" and the checklist step-5 "orch child" example.
- `docs/agent-backend.md`: delete the "Orchestration passthrough (no per-backend work)" blockquote; edit the two `CapMCPTools` rows that cite orchestration tools; edit the Chinese note "编排子 agent 各自持有独立 session". Leave the remote-`dispatch` false positives.
- `docs/debugging.md`: delete the table rows `orchestration_runs`/`orch_tasks` → "Orchestration…" and `workflows` → "Reusable workflow (process) library".
- `docs/e2e-harness-guide.md`: edit the "Done for orchestration tools (workflow, org, subagent, dispatch, finish)" example and the "orchestration flows (the fake acts as an MCP client…)" line to drop the orchestrate/workflow references. Leave CI/harness false positives.
- `docs/DESIGN.md`: remove the `RunAvatar` primitives-table row (the component was deleted in Task 1).
- `docs/doc-maintenance.md`: update the `session-lifecycle.md` description that says "including orchestration child sessions…".
- `AGENTS.md` (repo root): update the doc-index entry (line ~69) that cites "orchestration child sessions, future issue/hook dispatch" — keep the issue/hook part, drop orchestration.

- [ ] **Step 3: Verify no dangling doc references**

Run: `rg -n "orch_svc|orchestration_runs|orch_dispatches|流程库|workflow_svc|orchestrate 工具|RunAvatar" docs AGENTS.md | rg -v "specs/2026-07-14-remove-orchestration|plans/2026-07-14-remove-orchestration"`
Expected: only intentional/false-positive hits remain (remote dispatch, CI workflows, generic "orchestrates external dependencies"). Clean up any real leftover.

- [ ] **Step 4: Commit (pathspec)**

```bash
git commit \
  docs/session-lifecycle.md docs/agent-backend.md docs/debugging.md \
  docs/e2e-harness-guide.md docs/DESIGN.md docs/doc-maintenance.md AGENTS.md \
  <all deleted docs/superpowers paths> \
  -m "🔥 docs: 移除编排/流程库文档与历史 specs/plans"
```

---

### Task 5: Final verification gate (no new code)

**Files:** none (verification only; if a gate surfaces a leftover, fix + fold into the relevant prior commit's follow-up).

- [ ] **Step 1: Regenerate + full backend gates**

Run:
```bash
make generate 2>&1 | tail -10
make test-backend 2>&1 | tail -30 ; echo "backend exit: ${PIPESTATUS[0]}"
gofmt -l internal migrations | tail
make lint 2>&1 | tail -30 ; echo "lint exit: ${PIPESTATUS[0]}"
```
Expected: real exit code 0, `gofmt -l` empty.

- [ ] **Step 2: Full frontend gates**

Run:
```bash
cd frontend
pnpm test 2>&1 | tail -20 ; echo "vitest exit: ${PIPESTATUS[0]}"
pnpm exec tsc --noEmit 2>&1 | tail -10
pnpm exec eslint src 2>&1 | tail -10
```
Expected: all clean, i18n parity test green.

- [ ] **Step 3: Whole-repo orphan sweep**

Run:
```bash
rg -n "orch_svc|orch_repo|orch_entity|workflow_svc|workflow_repo|workflowtool_svc|OrchestrationPage|WorkflowManagerDialog|KeyOrchestrate|KeyWorkflow|SessionPurposeOrchChild|RegisterTurnExtrasProvider" internal frontend/src cmd \
  | rg -v "_test\.go|node_modules"
```
Expected: no output. Any hit is a missed reference — remove it and re-run the affected task's gate before finishing.

- [ ] **Step 4: Confirm the branch state**

Run: `git log --oneline -6 && git status`
Expected: the 4 removal commits present, working tree clean (aside from any unrelated foreign WIP that was never yours to commit).

## Self-Review notes

- Every spec section maps to a task: backend packages/tools/wiring/chat_svc/code → Task 2; migration → Task 3; frontend dirs/stores/hooks/surgical/i18n/mocks → Task 1; docs delete + surgical → Task 4; verification strategy + gates → Task 5.
- No placeholders: the migration and its test are complete code; deletions list exact paths; surgical edits name exact identifiers.
- Ordering guarantees each commit compiles: FE first (backend bindings still present), then BE + `make generate`, then migration (self-contained), then docs.
- Type/name consistency: migration id `202607140001` and function `migration202607140001` match between the file, its test, and the `migrationList()` entry.
