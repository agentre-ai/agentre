# Remove the Orchestration Subsystem — Design

**Date:** 2026-07-14
**Branch:** `develop/wyz` (pathspec commits; concurrent sessions active)
**Status:** Approved (design), pending implementation plan

## Goal

Delete the orchestration (编排) subsystem and the flow library (流程库) in their
entirety — backend engine, MCP tools, Wails bindings, DB tables, frontend pages
and stores, and all related documentation. The maintainer has decided the
orchestration module was a mistake and wants it fully removed, not deprecated.

This is a **removal**, not a feature. There is no new user-facing behavior to
test-drive. Correctness is defined as: after deletion the build compiles and the
full test/lint gate suite passes green, with orchestration tests deleted and the
tests that referenced removed surfaces updated.

## Scoping decisions (locked)

1. **Tool boundary — remove orchestration surface only.** Remove the
   `orchestrate` MCP tool set (including the `task_list` / `task_add` /
   `task_update` todo-checklist tools, which live inside that set) and the
   `workflow` flow-library tool set. **Keep** the `org`, `subagent`, and `hook`
   tools — they exist independently of orchestration (usable in normal chat and
   the agent-management UI) and share only generic plumbing.
2. **Migrations — append one drop migration.** Per the repo's hard invariant
   ("append new migrations to the end, never modify existing migrations"), add a
   single new migration that drops the orchestration/flow-library tables and the
   `chat_sessions.run_id` column, deletes orphaned orchestration data, and clears
   the `orchestrate`/`workflow` tool seeds. The ~13 existing create-migrations
   stay untouched so existing dev/prod gormigrate ledgers replay cleanly.
3. **Branch — stay on `develop/wyz`.** Commit with pathspec (never bare
   `git commit`) because concurrent sessions share the index. Be especially
   careful with the two `common.json` locale files, which currently have
   uncommitted edits from another session.

## In scope — what gets removed

### Backend — whole packages deleted

- `internal/service/orch_svc/` (+ `mock_orch_svc/`) — the orchestration engine
  (create/dispatch/ask/send/finish/report/read/status/complete/control/query/
  todo/scheduler/deadlock/envelope/deps/mcp/turn).
- `internal/repository/orch_repo/` (+ `mock_orch_repo/`) — `RunRepo`
  (`orchestration_runs`), `DispatchRepo` (`orch_dispatches`), `TaskRepo`
  (`orch_tasks` checklist).
- `internal/model/entity/orch_entity/` — `OrchestrationRun`, `Dispatch`, `Task`.
- `internal/service/workflow_svc/` — flow-library CRUD service.
- `internal/repository/workflow_repo/` (+ mocks) — `Workflow` repo (`workflows`).
- `internal/model/entity/workflow_entity/` — `Workflow` entity.
- `internal/service/workflowtool_svc/` (+ mocks) — the `workflow` MCP tool surface
  (workflow_list/create/update/delete).
- `internal/app/orch.go`, `internal/app/orch_adapter.go`, `internal/app/workflow.go`
  — Wails bindings + adapters.

### Backend — surgical edits

- `internal/pkg/agenttool/agenttool.go` — remove `KeyOrchestrate` + `KeyWorkflow`
  consts and their tool-set registry entries.
- `internal/bootstrap/cago.go` — remove the `/mcp/orchestrate/` and `/mcp/workflow/`
  mounts, `SetGatewayBaseURL` calls for them, the orch/workflow
  `RegisterTurnMCPProvider` calls, and the sole `RegisterTurnExtrasProvider`
  (orch is the only extras provider).
- `internal/app/app.go` — remove `orch_svc.RegisterDeps` / `RegisterWorkflowReader`
  / `RegisterTodoRepo` wiring and the `workflowtool_svc.RegisterDeps` wiring.
- `internal/service/chat_svc/` — remove:
  - `SessionPurposeOrchChild` and `createOrchChildSession`.
  - The `RunID`, `MCPServers`, `SystemPromptSuffix`, `EmitTurnStartedBypass`
    turn/session fields and their threading (`types.go`, `chat.go`).
  - `fillGroupTurnExtras` / `buildTurnExtras` call sites and the
    `systemPromptSuffix` append onto `RunRequest.SystemPrompt`.
  - `turn_extras.go` — the `TurnExtrasProvider` type + registry (dead once orch is
    gone). **Keep** `turn_mcp.go` (org/subagent/hook still register there); just
    ensure no orch/workflow registration remains.
- `internal/model/entity/chat_entity/session.go` — remove
  `SessionPurposeOrchChild` and the `RunID` column field.
- `internal/repository/chat_repo/session.go` — remove the `run_id = 0`
  `defaultSessionScope` filter and the `run_id > 0` special-cases in
  count/list queries.
- `internal/pkg/code/code.go` + `zh_cn.go` — remove `WorkflowNotFound` (20800).
- `internal/model/entity/doc.go`, `internal/service/data_svc/doc.go` — remove any
  orch/workflow entity references (verify data-export scope entries).

### Backend — one new migration (appended)

`migrations/202607140001_drop_orchestration.go` (+ `_test.go`; adjust the
numeric prefix if a later timestamp already exists at commit time):

- `DROP TABLE` `orchestration_runs`, `orch_dispatches`, `orch_tasks`, `workflows`
  (native SQL; `IF EXISTS`).
- Drop `chat_sessions.run_id` column (SQLite ≥ 3.35 `DROP COLUMN`; native SQL).
- `DELETE` orphaned orchestration data: `chat_sessions` with
  `purpose = 'orch_child'` (and their `chat_messages`), so once the `run_id`
  filter is gone they don't leak into the sidebar.
- Remove the `orchestrate` and `workflow` agent-tool seeds (whatever seed rows
  reference those tool keys).
- Down migration: minimal / no-op recreate is acceptable (project is unreleased,
  hard delete is allowed).
- Register the new migration at the **end** of `migrationList()`; do not modify
  or delete the existing create-migrations (`202606160001`, `202606240001`,
  `202606240003`, `202606250002`, `202607030001/2`, `202607040001/2`,
  `202607050001`, `202607080001/2`, `202607090001/2/3`).

### Frontend — whole files/dirs deleted (~50 files)

- `frontend/src/components/agentre/orchestration/` (entire dir incl. `__tests__/`).
- `frontend/src/components/agentre/workflows/` (entire dir).
- `frontend/src/components/agentre/orch-events-host.tsx` +
  `orch-notifier.tsx` (+ their tests).
- `frontend/src/stores/orch-run-store.ts`, `orch-run-list-store.ts`,
  `orch-subagents-store.ts`, `workflow-manager-store.ts` (+ tests).
- `frontend/src/hooks/use-workflows.ts` (+ test).
- `frontend/src/hooks/use-live-conversation.ts` (+ test) — orch-only; re-verify
  no other consumer before deleting.
- `frontend/src/hooks/use-composer-send.ts` (+ test) — grep says orch-only but a
  comment claims broader reuse; re-verify no non-orch consumer before deleting.
- `frontend/src/components/agentre/command-palette/sources/workflow-actions-source.tsx`
  (+ test).

### Frontend — surgical edits

- `frontend/src/App.tsx` — remove imports (`OrchEventsHost`, `OrchNotifier`,
  `OrchestrationPage`, `WorkflowManagerDialog`, unused `topologyStar3Icon`), the
  `/orchestration` nav item, `pageBreadcrumbKeys` entry, breadcrumb fallback,
  root-mounted `<OrchEventsHost/>` `<OrchNotifier/>` `<WorkflowManagerDialog/>`,
  and the two `/orchestration` routes.
- `frontend/src/components/agentre/index.ts` — remove the four barrel exports.
- `frontend/src/components/agentre/command-palette/command-palette.tsx` — remove
  the `workflowActionsSource` import + `SOURCES` entry.
- `frontend/src/components/agentre/org/tool-catalog.ts` — remove `"workflow"` from
  `APPROVAL_TOOLS`. (This is the file with a concurrent test edit — see hazards.)
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — remove `nav.orchestration`,
  the top-level `orchestration.*` section, the top-level `workflows.*` section,
  `commandPalette.workflows`, and the `org.*.tools` `names.workflow` /
  `names.orchestrate` / `descriptions.workflow` / `descriptions.orchestrate`
  entries. Keep parity between both locales; `i18n.test.ts` must stay green.
- Test/mock shared files: `frontend/src/__tests__/mocks/wailsApp.ts` (remove
  Workflow + Orchestration run binding mocks), `App.test.tsx` (route/nav
  assertions), `chat-page.test.tsx` (WorkflowList stub),
  `tool-approval/card.test.tsx` (the `workflow` approval describe block).
- Optional comment-only cleanups (no functional change): `message-row.tsx`,
  `use-chat-session.ts`, `use-chat-stream.ts`, `chat-streams-store.ts`,
  `org/reporting.ts` — do only if trivially in-scope; otherwise leave.

### Docs

- Delete the ~75 orchestration/flow-library/workflow-relocation spec + plan files
  under `docs/superpowers/specs/` and `docs/superpowers/plans/` (the group-task-
  orchestration, workflow-relocation, agent-orchestration, orchestration-board,
  orchestration-harness, DAG-designer, flow-prompt-template, default-flow-library,
  orch-dispatch-rename, and group-chat-orchestration lineages). Keep incidental-
  mention docs that are primarily about other features (issue dispatch, hooks,
  terminal PTY, e2e harness/CI, markdown links).
- Surgical doc edits:
  - `docs/session-lifecycle.md` — remove the "Orchestration Child Sessions"
    subsection and the `orch_svc` / orch-child references.
  - `docs/agent-backend.md` — remove the orchestration-passthrough note and the
    orchestration references in the `CapMCPTools` rows.
  - `docs/debugging.md` — remove the `orchestration_runs`/`orch_tasks` and
    `workflows` rows from the table-to-feature map.
  - `docs/e2e-harness-guide.md` — remove the orchestration-tool fake-backend
    example lines.
  - `docs/DESIGN.md` — remove the `RunAvatar` primitives row iff the component is
    deleted.
  - `docs/doc-maintenance.md` — update the `session-lifecycle.md` description.
  - `AGENTS.md` (repo root) — update the doc-index entry that cites orchestration
    child sessions.
- `docs/superpowers/specs/` self-note: this design doc stays.

## Out of scope — explicitly kept

- `org`, `subagent`, `hook` MCP tool sets and their subsystems (agent
  organization/teams/departments UI, subagent-call, hooks).
- The generic MCP reverse-tunnel (`internal/daemon/handlers/mcpproxy.go`), the
  `turn_mcp` provider registry, `MCPServers` injection plumbing in
  `agentruntime`, and `remote.RegisterMCPProxyDispatcher` — orchestration merely
  rode on these; other tools still use them.
- `reloadSidebarSources` / `stores/sidebar-reload.ts` (shared by chat/project).
- The turn event dispatcher (`chat_svc/dispatcher_*.go`, `turn/dispatcher.go`) and
  daemon RPC `Dispatch` — unrelated to orchestration despite the name overlap.

## Verification strategy

Because this is a removal, the gate is a green build + green suites, not a new
red test:

- New migration `_test.go` proves the drop migration runs up cleanly (tables/
  column/data gone).
- Update tests that referenced removed surfaces: chat_repo `run_id` scoping tests,
  `App.test.tsx`, `wailsApp.ts` mock, `chat-page.test.tsx`, `card.test.tsx`.
- Run `make generate` so the Wails bindings regenerate and drop `Run*` /
  `Workflow*` from the TS surface.
- Full finish gates (per the repo's SDD discipline — check real exit codes, not
  `| tail`): `make test-backend`, `make lint`, `gofmt -l`, `cd frontend && pnpm test`
  (full, not focused — catches i18n parity + tsc + eslint), plus `tsc` and eslint.

## Hazards / risks

- **Concurrent `common.json` + `org/__tests__/tool-catalog.test.ts` edits.**
  Another session has uncommitted changes to these files. Inspect the diffs
  before editing; do i18n edits carefully; commit with pathspec and do **not**
  sweep the other session's WIP into these commits. If clean separation isn't
  possible, flag to the maintainer rather than guessing.
- **`use-composer-send` / `use-live-conversation` deletion.** Re-verify no
  non-orchestration consumer before deleting each (comments hint at broader reuse
  that grep does not confirm).
- **Real DB cleanup.** The maintainer's large prod DB may hold `orch_child`
  sessions / runs; the drop migration must delete them so they don't surface in
  the sidebar once the `run_id = 0` filter is removed.
- **`turn_extras.go` removal.** Confirm orch is genuinely the only
  `TurnExtrasProvider` and that `systemPromptSuffix` has no non-orch producer
  before deleting the registry.

## Commit plan

`develop/wyz`, gitmoji, pathspec commits, grouped so `git bisect` stays sane:

1. Frontend removal (dirs + stores + hooks + surgical `App.tsx` / index / palette
   / tool-catalog / i18n / test mocks).
2. Backend removal (packages + `agenttool` / `cago.go` / `app.go` / chat_svc /
   chat_entity / chat_repo / code) + the new drop migration.
3. Docs (delete spec/plan files + surgical doc edits + root `AGENTS.md`).

(Exact commit boundaries may adjust so each commit compiles/tests independently;
the implementation plan will finalize ordering.)
