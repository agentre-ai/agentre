# Orchestration Design-Fidelity Rebuild Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the already-built orchestration UI (S3–S7) faithfully match the Pencil design frames — 3-pane Run shell, 结构图/活动流 toggle, top-down structure graph with connectors, restyled task board / activity feed / drill-in conversation, and the banner states — in both Light and Dark.

**Architecture:** Pure frontend restyle + light restructure. The behavioral layer (stores, data derivation, events, RunSpeak/pause/resume/stop, selection contract) built and reviewed in S3–S7 is CORRECT and must be preserved — every existing `data-testid` and i18n key keeps working; tests stay green. This is a visual/structural pass: restructure `index.tsx` (OrchestrationRun) into the design's 3-pane Main+right model, add a `ToggleBar` and a center speak-to-Leader `Footer`, and restyle each pane to the matching frame using the design tokens (which ALL already exist as Tailwind classes — no token work). Drill-in (TaskBoard ⇄ ConvPanel on the right) already matches the design and is kept.

**Tech Stack:** React 19 + TypeScript, Tailwind CSS v4 (design tokens already defined), Vitest + Testing Library, react-i18next (dual locale zh-CN/en), lucide icons, shadcn `@/components/ui/*`.

## Authoritative references (read before any task)

- **Design file (Pencil):** the active editor `/Users/codfrm/Code/agentre/agentre.pen`. Inspect frames with the `pencil` MCP tools: `get_screenshot({filePath, nodeId})` for visuals, `batch_get({filePath, nodeIds, readDepth})` for exact structure/values. Do NOT edit the .pen.
- **Current-code audit (testids, i18n keys, token inventory, DESIGN.md summary, app shell):** `/Users/codfrm/Code/agentre/agentre/.superpowers/sdd/design-audit-current-code.md`. This is the source of truth for what currently exists and which `data-testid`/i18n keys MUST be preserved.
- **Design frame IDs (all 1440×900; node IDs are stable within a frame):**
  | Frame | Light | Dark |
  |---|---|---|
  | Run · 结构图 (structure graph) | `iBqBl` | `r0NpeW` |
  | Run · 活动流 (activity feed) | `Z2P0Vn` | `zJoz9` |
  | Run · 进入会话(钻入) (drill-in conv) | `x4ZEP` | `RUK6J` |
  | Run · 暂停·错误 (paused/error) | `kx9LR` | `rtrNS` |
  | Run · ask·死锁 (ask/deadlock) | `RLgFO` | `XoZpZ` |
  - OUT of scope (do NOT build): 总览/overview (`K0q5q`/`Zk6xc`), 流程编辑器 (`yl83F`/`kRxup`), command palette in the top bar (new feature). The 新建Run/流程库 dialogs (`Y8hJ5`/`DEpdt`) are existing components — leave them unless a task says otherwise.

## Design token map (ALL already exist — use these Tailwind classes, never hardcode hex)

Backgrounds: `bg-rail bg-sidebar bg-sidebar-active-bg bg-card bg-background bg-muted bg-secondary bg-accent bg-popover bg-input-bg bg-primary bg-primary-soft bg-destructive bg-destructive-soft bg-status-running bg-status-running-bg bg-status-waiting bg-status-waiting-bg`.
Text: `text-foreground text-muted-foreground text-subtle-foreground text-sidebar-icon text-primary text-primary-text text-primary-foreground text-destructive text-status-running text-status-waiting text-status-error text-status-idle`.
Borders: `border-border border-border-strong border-primary border-destructive border-status-waiting`.
Radius: `rounded-sm`(4) `rounded-md`(6) `rounded-lg`(8) `rounded-xl`(12); pills use `rounded-full`(999).
Fonts: `font-sans` (Geist, default body), `font-mono` (JetBrains Mono — used for ids/counters like `#T3`, `5/12`). Agent colors: the 16-color `agent-*` palette via the existing `AgentAvatar`/`AgentColor` primitives (do not reinvent).
Icons: lucide (the design names map 1:1 to lucide: `waypoints target pause square ellipsis git-fork list list-checks users git-merge message-square send-horizontal check loader circle corner-down-right chevron-right triangle-alert`).

## Global Constraints

- **Preserve all behavior + every existing `data-testid` and i18n key from S3–S7.** This is a restyle, not a rewrite of logic. If a redesign moves an element, the testid moves with it. Net-new visible copy must be added to BOTH `frontend/src/i18n/locales/zh-CN/common.json` and `.../en/common.json` under `orchestration.*`; no hardcoded Chinese in JSX (ESLint `i18next/no-literal-string` enforces this). Dynamic data (agent names, messages, task titles, goals) is NOT translated.
- **Use design tokens only** (the classes above). Never hardcode hex colors; this is what makes Dark mode "just work". Any hardcoded color found in a touched file is a bug to fix in that task.
- **Shadcn `@/components/ui/*` for form controls** (no native `<select>`/`<input>` where a ui primitive exists). Reuse existing primitives: `AgentAvatar`, `StatusDot` (`../primitives`), `ChatTranscript`, `ResizableSidebar` (used by chat/projects — prefer it for the run sidebar + right panel so widths default to the design's 320 but stay resizable per app convention).
- **Strict TDD for any logic change** (view-toggle state, speak-to-leader wiring, selection): write/extend the behavior test first (RED), then implement (GREEN). Pure visual class changes are verified against the Pencil frame via `get_screenshot`, not via snapshot tests — do not add brittle class-name assertions.
- **Shared branch `develop/wyz`: commit with pathspec** (`git commit <files> -m ...`), never `-A`/bare.
- **No drive-by changes** outside each task's files. The app shell (AppTopBar/AppRail/AppStatusBar in `App.tsx`) already exists globally — do NOT rebuild it; orchestration renders into the content area only.
- **Authoritative gates** (vitest/esbuild do NOT typecheck): a task is done only when its covering tests pass, `npx tsc --noEmit` is clean, `npx eslint <touched files>` exits 0, and the result visually matches the frame in `get_screenshot`. LSP "unused/cannot-find" diagnostics mid-edit are usually stale — trust tsc/eslint exit codes.

## File Structure

- `frontend/src/components/agentre/orchestration/orchestration-page.tsx` — standalone page; provides RunSidebar + main area. (Task 1)
- `frontend/src/components/agentre/orchestration/index.tsx` — `OrchestrationRun`: the Main column (RunHeader + ToggleBar + Content + Footer) + right panel (TaskBoard ⇄ ConvPanel). Owns view-toggle + selectedSessionId state. (Task 1)
- `frontend/src/components/agentre/orchestration/run-header.tsx` — NEW (extracted): RunHeader. (Task 3)
- `frontend/src/components/agentre/orchestration/toggle-bar.tsx` — NEW: seg control + stat chips. (Task 4)
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — top-down graph + connectors. (Task 5)
- `frontend/src/components/agentre/orchestration/task-board.tsx` — restyle. (Task 6)
- `frontend/src/components/agentre/orchestration/activity-feed.tsx` — restyle. (Task 7)
- `frontend/src/components/agentre/orchestration/conversation-panel.tsx` — restyle + center Footer speak-to-Leader lives in index.tsx. (Task 8)
- `frontend/src/components/agentre/orchestration/run-list.tsx` — RunSidebar restyle. (Task 2)
- i18n: `frontend/src/i18n/locales/{zh-CN,en}/common.json`. Tests: each component's existing `__tests__/*.test.tsx`.

> Each task ends with: covering tests green, tsc clean, eslint 0, and a `get_screenshot` visual match against the named Light frame. Dark is verified centrally in Task 10 (tokens make it automatic; Task 10 catches stragglers).

---

### Task 1: Run shell — 3-pane layout + view-toggle/selection state

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/index.tsx` (OrchestrationRun)
- Modify: `frontend/src/components/agentre/orchestration/orchestration-page.tsx`
- Test: `frontend/src/components/agentre/orchestration/__tests__/` (the existing index/orchestration-page tests)

**Design refs:** `iBqBl` (graph view) and `x4ZEP` (drill-in). Body = RunSidebar(320) | Main(fill) | right(320). Main (id `lQqWS`/`aC6Ee`) = RunHeader + ToggleBar + Banner(conditional) + Content(graph|feed) + Footer. Right panel = TaskBoard (`oXNr6`, default) OR ConvPanel (`LXxwa`, when a session is selected).

**Interfaces:**
- Consumes: `OrchestrationRun({ runId, title })`; `detail` from `useOrchRunStore`/`RunLoad`; existing `RunSpeak`, pause/resume/stop, `useChatAgents`.
- Produces: a `view: "graph" | "feed"` state (default `"graph"`) passed to ToggleBar (Task 4) + Content; `selectedSessionId: number | null` (S6 contract, unchanged) that swaps right panel TaskBoard⇄ConversationPanel; a `leaderSessionId` derived from `detail` (the leader agent's session) for the center Footer speak-to-Leader.

**Steps:**
- [ ] **Step 1 (RED): extend the OrchestrationRun test** to assert the new shell: a `data-testid="orch-main"` column containing RunHeader + a `data-testid="orch-toggle"` slot + a `data-testid="orch-content"` + a `data-testid="orch-footer"` speak-to-leader input; and the right panel still shows TaskBoard by default, ConversationPanel when `selectedSessionId` set (preserve the S6 two-state test). Assert `view` defaults to `"graph"` and toggling to `"feed"` swaps Content (use a stubbed ToggleBar that calls `onChange`). Run it; watch it fail.
- [ ] **Step 2 (GREEN): restructure `index.tsx`** to the 3-pane model:
  - Main column: `<div data-testid="orch-main" class="flex min-h-0 min-w-0 flex-1 flex-col bg-background">` → `<RunHeader …/>` (Task 3 placeholder: keep current header for now, restyled in Task 3) + `<ToggleBar view={view} onView={setView} stats={…}/>` (Task 4 placeholder: a minimal seg toggle that flips `view`) + conditional `<Banner/>` (keep current banner logic) + `<div data-testid="orch-content" class="flex min-h-0 flex-1 flex-col bg-background">{view==="graph" ? <StructureGraph/> : <ActivityFeed/>}</div>` + `<Footer/>` speak-to-Leader.
  - Footer (speak to Leader): `bg-card border-t border-border px-5 py-3 flex items-center gap-2.5`; input row `bg-input-bg rounded-lg border border-border px-3 py-2.5` (message-square icon + i18n placeholder `orchestration.run.speakLeaderPlaceholder` = zh "对 Leader 说:调整优先级 / 追加约束 / 批准任务…" / en equivalent) + send button `bg-primary rounded-lg px-4 py-2.5 text-primary-foreground` (send-horizontal + `orchestration.run.send`). On send call `RunSpeak(leaderSessionId, text)` then clear; `data-testid="orch-footer"`, send `data-testid="orch-speak-leader-send"`. `leaderSessionId` = session of `detail.run.leaderAgentId` (find in `detail.tasks`); if none, disable the footer.
  - Right panel: keep S6 logic — `selectedSessionId ? <ConversationPanel/> : <TaskBoard/>` in a `w-80 shrink-0 border-l border-border bg-sidebar` column (or `ResizableSidebar` defaulting 320, side right).
  - `orchestration-page.tsx`: left RunSidebar stays (RunList), but widen to design 320 (`w-80`, `bg-sidebar`, `border-r`). Onboarding state unchanged.
- [ ] **Step 3:** remove the old `RunFlowBlueprint` strip from the Main column (the design replaces it with ToggleBar). If `RunFlowBlueprint` is now unused, leave the component file (out of scope to delete); just stop rendering it here. Confirm with tsc/eslint no-unused.
- [ ] **Step 4:** run the test (`cd frontend && pnpm test -- src/components/agentre/orchestration`) → PASS; `npx tsc --noEmit` 0; `npx eslint` touched files 0.
- [ ] **Step 5:** `get_screenshot({filePath, nodeId:"iBqBl"})` and eyeball the rendered shell matches the pane structure (sidebar | header/toggle/content/footer | task board). Commit: `git commit <index.tsx orchestration-page.tsx test> -m "♻️ orch 设计:Run 外壳 3 栏化(Main 头/Toggle/内容/Leader 发言 + 右栏任务板⇄会话)"`.

---

### Task 2: RunSidebar (run-list.tsx) restyle

**Files:** Modify `frontend/src/components/agentre/orchestration/run-list.tsx`; Test: its existing `__tests__/run-list.test.tsx`.

**Design ref:** `x4ZEP` → `RunSidebar` `t5Jkpr` (w320, `bg-sidebar`, border-r). Children: `listHead` (`L23yk3`, padding `[14,14,8,14]`): "编排 Runs" (13/600, text-foreground) + spacer + `newBtn` (`bg-primary rounded-md px-2 py-1`, plus icon + label, text-primary-foreground). `filters` (`dbkUX`, padding `[0,14,10,14]`, gap6): filter chips `rounded-full px-2 py-0.75` — active `bg-secondary border border-border`, inactive transparent. `runs` (`soq05`, padding `[0,8]`, gap2): run row `rounded-md px-2.5 py-2` — active `bg-secondary`, inactive transparent/hover; shows run name + status meta.

**Interfaces:** preserve `RunList({ activeRunId?, onSelect })`, `data-testid="run-onboarding-cta"` (empty state), `onSelect(id)`, the active-run highlight from `activeRunId`. Keep existing i18n keys; add only if the design shows new copy (e.g. filter chip labels — if added, both locales).

**Steps:**
- [ ] **Step 1 (RED):** extend test to assert listHead label + new-run button testid, filter chips, and a run row with active highlight; keep the onboarding-cta + onSelect tests.
- [ ] **Step 2 (GREEN):** restyle to the frame (tokens above). New-run button keeps existing new-run-dialog trigger behavior. Filter chips: if the design's chips imply a status filter not yet wired, render them as static visual chips with the default selected — do NOT invent new filtering logic (YAGNI); note it for a future slice.
- [ ] **Step 3:** tests green, tsc 0, eslint 0.
- [ ] **Step 4:** `get_screenshot` compare to the sidebar in `x4ZEP`/`iBqBl`. Commit: `git commit <run-list.tsx test> -m "🎨 orch 设计:Run 侧栏对齐(编排 Runs 头/新建按钮/筛选 chip/Run 行)"`.

---

### Task 3: RunHeader (extract + restyle)

**Files:** Create `frontend/src/components/agentre/orchestration/run-header.tsx`; Modify `index.tsx` (use it); Test: `__tests__/run-header.test.tsx` (new) or extend index test.

**Design ref:** `iBqBl` → `RunHeader` `omQyq` (`bg-card`, padding `[14,20]`, border-b, gap8, vertical). `topline` (gap10, items-center): icon badge `dW7F3` (`bg-primary-soft rounded-lg w-7.5 h-7.5`, waypoints icon `text-primary-text`) + title `JzdjM` (16/700, text-foreground; content = run goal/title) + status pill `sCRQG` (`bg-status-running-bg rounded-full px-2 py-0.75`, dot + label `text-status-running` 11/600; label = phase: 运行中/已暂停/已完成/已停止/出错 with matching status token) + spacer + `ctrls` (gap8): 暂停 btn (`bg-card rounded-md border border-border px-2.5 py-1.5`, pause icon + label 12) , 停止 btn (square icon + label, `text-destructive`), more btn (`w-7.5 h-7`, ellipsis). `subline` (gap6): target icon (12, muted) + goal/leader text (12, muted, fill-width) `orchestration.run.goalLine` pattern (goal + "· Leader:" + leader name — compose with dynamic values NOT translated).

**Interfaces:** `RunHeader({ detail, phase, onPause, onResume, onStop })`. Pause button toggles pause/resume by phase (preserve existing pause/resume/stop wiring + their testids — find current ones in the audit file). Status pill label/color derived from `phase`.

**Steps:**
- [ ] **Step 1 (RED):** test asserts title from `detail.run.goal`, the status pill reflects phase (running→运行中), pause/stop controls call the right handlers (preserve existing handler testids).
- [ ] **Step 2 (GREEN):** build/restyle. Map phase→{label i18n key, status token}. Reuse existing pause/resume/stop logic from current header.
- [ ] **Step 3:** tests green, tsc 0, eslint 0; i18n keys both locales.
- [ ] **Step 4:** `get_screenshot` compare `omQyq`. Commit: `git commit <run-header.tsx index.tsx test i18n> -m "🎨 orch 设计:RunHeader 对齐(图标徽标/标题/状态胶囊/暂停停止/目标行)"`.

---

### Task 4: ToggleBar (new component)

**Files:** Create `frontend/src/components/agentre/orchestration/toggle-bar.tsx`; Modify `index.tsx` (wire `view`/`onView` + stats); Test: `__tests__/toggle-bar.test.tsx`.

**Design ref:** `iBqBl` → `ToggleBar` `D1f3rS` (`bg-card`, padding `[10,20]`, border-b, gap12, items-center). `seg` `LFd7g` (`bg-secondary rounded-lg p-0.5 gap-0.5`): `segGraph`(结构图, active = `bg-card rounded-md border border-border px-3 py-1.25`, git-fork icon, 12/600) | `segFeed`(活动流, list icon, 12, muted, transparent when inactive). spacer. stat chips (`bg-secondary rounded-md px-2 py-0.75 gap-1.25`, mono 11, text-muted-foreground): `5/12 任务`(list-checks) · `深度 N`(git-fork) · `N agent · N sub`(users) · `N 子代理`(git-merge).

**Interfaces:** `ToggleBar({ view, onView, stats })` where `stats = { done, total, depth, agentCount, subCount }` computed in `index.tsx` from `detail`/graph/subagents (reuse S4 `buildGraph` depth + S5 subagent counts; done/total = S5 board-progress numbers). `data-testid`: `orch-toggle`, `toggle-graph`, `toggle-feed`, and `toggle-stat-tasks` etc. New i18n: `orchestration.toggle.graph`/`feed`, `orchestration.toggle.tasks`/`depth`/`agents`/`subagents` (both locales).

**Steps:**
- [ ] **Step 1 (RED):** test — renders two segments, active reflects `view`, clicking 活动流 calls `onView("feed")`; renders the 4 stat chips with the passed numbers.
- [ ] **Step 2 (GREEN):** build it; wire into `index.tsx` replacing the Task-1 placeholder toggle. Compute `stats` from existing derivations (do not refetch).
- [ ] **Step 3:** tests green, tsc 0, eslint 0; i18n both locales.
- [ ] **Step 4:** `get_screenshot` compare `D1f3rS`. Commit: `git commit <toggle-bar.tsx index.tsx test i18n> -m "✨ orch 设计:ToggleBar(结构图/活动流 分段 + 任务/深度/agent/子代理 统计 chip)"`.

---

### Task 5: Structure graph — top-down layout + connectors

**Files:** Modify `frontend/src/components/agentre/orchestration/structure-graph.tsx`; Test: `__tests__/structure-graph.test.tsx`.

**Design ref:** `iBqBl` → `graphView` `o4PcsP` (`bg-background`, padding `[30,20]`, vertical, items-center) → `agentsArea` `mQ01J`:
- **Leader node** `XXb3R` at TOP center: `w-53 bg-card rounded-xl border-[1.5px] border-primary p-3 gap-2`. `hd`: AgentAvatar(26, primary, round) + name block (name + role) + status dot(7). `mr`: a `bg-secondary rounded-md px-2 py-0.5` chip + spacer + a `bg-status-running-bg rounded-full px-2 py-0.5` status chip.
- **vl** `J6XMk`: vertical connector `bg-border-strong w-0.5 h-5.5` (rectangle).
- **bus** `s1juF`: horizontal connector `bg-border-strong h-0.5` spanning the children row (rectangle, width = children span).
- **cols** `k8aaF` (horizontal, gap20, justify-center): each column is a vertical stack of that branch's agent node cards. Agent node card = same shape as leader but `border-2` colored by status (`nodeBorderClass`, keep current logic) / deadlock ring; avatar uses the agent color; keep the merged `×N` and top-level grouped per-call sub-rows behavior from S4.

**Behavioral contracts to PRESERVE (from audit file — do not drop):** testids `node-{agentId}`, `node-{agentId}-multi`, `node-{agentId}-call-{taskId}`, `node-{agentId}-subagents`, `node-{agentId}-asking`; root `div role="button"` + keyboard (Enter/Space) + nested per-call `<button>` (S6 a11y); click → `onSelectSession`; the deadlock/completed/paused/stopped banners; `graph-empty`. Keep `buildGraph`/`computeDepths`/`lifecycle`. The change is the VISUAL arrangement: render the leader at top, a connector bus, and children in columns below — instead of left-to-right depth columns.

**Steps:**
- [ ] **Step 1 (RED):** extend test to assert top-down structure: leader node renders before/above the children container; a connector element (`data-testid="graph-bus"`) exists when there are children; existing node testids + click→onSelectSession still pass. (Assert structure/testids, not pixel classes.)
- [ ] **Step 2 (GREEN):** rebuild the layout: leader node centered on top → `vl` → `bus` → `cols` (children grouped by their parent edge / BFS depth ≥1 into columns). For depth >2, nest connectors recursively or render successive bus rows (keep it simple: leader → row of depth-1 nodes is the primary case; deeper levels stack with their own mini-connectors). Restyle node cards to the frame. Keep merged/grouped/sub-row logic.
- [ ] **Step 3:** tests green (`pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`), tsc 0, eslint 0.
- [ ] **Step 4:** `get_screenshot` compare `iBqBl` graphView. Commit: `git commit <structure-graph.tsx test> -m "🎨 orch 设计:结构图 top-down(Leader 顶置 + 连接母线 + 子节点分列)对齐设计稿"`.

---

### Task 6: Task board restyle

**Files:** Modify `frontend/src/components/agentre/orchestration/task-board.tsx`; Test: `__tests__/task-board.test.tsx`.

**Design ref:** `iBqBl` → `TaskBoard` `oXNr6` (w320, `bg-sidebar`, border-l). `tbHead` `LW0NC` (`bg-card`, padding `[12,14]`, border-b, gap10, vertical): `hr` ("任务板" 13/600 + spacer + "5 / 12 完成" mono 11 muted) + `tabs` `B7Ooxg` (`bg-secondary rounded-lg p-0.5`): `tabTask`(任务, active `bg-card rounded-md border px-3 py-1`) | `tabOut`(产出). `taskList` `jCAym` (`bg-sidebar p-2 gap-0.5`, vertical, clip): agent group header `grp` (color dot 6 + agent name 11/600 muted, padding `[8,8,4,8]`); task row `task-{id}` (status icon 14 [check=status-running / loader=status-running / circle=status-idle] + `#id` mono 10 subtle + title 12.5; `rounded-md` padding `[7,8,7,14]`; active = `bg-secondary`); subagent group `grp` indented (padding-left 20: corner-down-right + color dot + name 10.5/600 + `subagent` tag `bg-secondary rounded-full` mono 9 + optional `×N 次调用` `bg-primary-soft text-primary-text`); subagent task row indented (padding-left 30); collapsed `subagents-collapsed-{id}` (padding-left 38: chevron-right + git-merge + `+N 子代理` mono 11 + `auto-merge` tag).

**Behavioral contracts to PRESERVE:** testids `board-progress`, `board-agent-{id}`, `board-task-{id}`, `board-subagents-{taskId}`, `board-subagent-{taskId}-{i}`; agent grouping order; click → `onSelectTask`/session; subagent collapse toggle; existing i18n keys `orchestration.board.*`, `orchestration.subagent.*`. The 任务/产出 tabs: 任务 active by default; 产出 tab keeps the current outputs behavior (do not expand the stub ref viewer in this task — restyle only; leave the existing outputs content, note the structured-ref-viewer as future work).

**Steps:**
- [ ] **Step 1 (RED):** extend test for tbHead (任务板 + count + tabs) and that task rows show status icon + #id + title; preserve board-progress/grouping/subagent-row tests.
- [ ] **Step 2 (GREEN):** restyle to frame; map task status → icon (done→check, running→loader, idle→circle) using the run-status tokens.
- [ ] **Step 3:** tests green, tsc 0, eslint 0; any new copy (产出 tab label) both locales.
- [ ] **Step 4:** `get_screenshot` compare `oXNr6`. Commit: `git commit <task-board.tsx test i18n> -m "🎨 orch 设计:任务板对齐(任务/产出 tab + agent 分组行 + 状态图标 + 子代理缩进)"`.

---

### Task 7: Activity feed restyle

**Files:** Modify `frontend/src/components/agentre/orchestration/activity-feed.tsx`; Test: `__tests__/activity-feed.test.tsx`.

**Design ref:** `iBqBl` → `feedView` `FCZGQ` (`bg-background`, padding `[16,20]`, gap14, vertical, clip). Each `ev` row (gap10): AgentAvatar(24, agent color, round, initial) + `bd` (vertical, gap4): `hl` header row (agent name + meta/timestamp) + message text (12.5, lineHeight 1.4, text-foreground). A trailing `typing` row: avatar + "{agent} 正在执行 #Tn · …" (12, muted) + 3 animated dots (`bg-subtle-foreground`, 4px) — use `motion-safe:animate-pulse` (DESIGN.md §8 reduced-motion rule).

**Behavioral contracts to PRESERVE:** the S3 feed item kinds (event/ask/reply), their testids and i18n keys (`orchestration.feed.ask`/`reply`), the merge+sort from `feed-data.ts buildFeed(detail, askLog)`. Dynamic text (messages) not translated. ask/reply rows keep their distinct styling (amber for ask-waiting per the ask·死锁 frame `RLgFO`).

**Steps:**
- [ ] **Step 1 (RED):** extend test — an event row renders avatar + header + message; ask/reply rows still render with their testids; ordering preserved.
- [ ] **Step 2 (GREEN):** restyle rows to frame; add the typing/active row only if `detail` exposes a running agent (reuse existing running detection; if none available, skip the typing row — do not fabricate state).
- [ ] **Step 3:** tests green, tsc 0, eslint 0.
- [ ] **Step 4:** `get_screenshot` compare `FCZGQ` (and `RLgFO` for ask styling). Commit: `git commit <activity-feed.tsx test> -m "🎨 orch 设计:活动流对齐(头像+头部+正文 行 + 执行中 typing)"`.

---

### Task 8: Conversation drill-in panel restyle

**Files:** Modify `frontend/src/components/agentre/orchestration/conversation-panel.tsx`; Test: `__tests__/conversation-panel.test.tsx`.

**Design ref:** `x4ZEP` → `ConvPanel` `LXxwa` (w320, `bg-sidebar`, border-l). `cvHead` `LjaQV` (`bg-card`, padding `[12,14]`, border-b, vertical gap8): `back` (chevron-left + `orchestration.conversation.backToBoard`) + `who` (AgentAvatar + agent name + status). `cvBody` `ZRY6r` (`bg-sidebar`, padding14, gap12, vertical, clip): the read-only `ChatTranscript` (S6 — keep read-only, no Edit/Regenerate) — match spacing; a waiting/ask callout uses `bg-status-waiting-bg rounded-lg border-l-[3px] border-status-waiting p-2.5` when that agent is awaiting. `cvInput` `I4fHS` (`bg-card`, padding `[10,12]`, border-t): input (`bg-input-bg rounded-lg border px-2.5 py-2`) + send button (`bg-primary rounded-lg w-7.5 h-7.5`).

**Behavioral contracts to PRESERVE (S6):** read-only `ChatTranscript` (no onRerun/onEdit), `RunSpeak(sessionId, text)` + clear + reload-after-speak + in-flight dedup, `onBack`, testids (`orchestration.conversation.*` keys + back/speak/send testids from the audit file). agentName/color cast pattern unchanged.

**Steps:**
- [ ] **Step 1 (RED):** keep S6 tests; add assertion that the panel renders cvHead (back + who) + cvInput; read-only transcript still hides Edit/Regenerate.
- [ ] **Step 2 (GREEN):** restyle to frame; the speak input here is per-agent (distinct from the Main center speak-to-Leader footer from Task 1).
- [ ] **Step 3:** tests green, tsc 0, eslint 0.
- [ ] **Step 4:** `get_screenshot` compare `LXxwa` in `x4ZEP`. Commit: `git commit <conversation-panel.tsx test> -m "🎨 orch 设计:钻入会话面板对齐(返回/who 头 + 只读转录 + 等待高亮 + 发言输入)"`.

---

### Task 9: Banner states (paused/error/deadlock/ask) restyle

**Files:** Modify `frontend/src/components/agentre/orchestration/structure-graph.tsx` (banners) and wherever the Main `Banner` renders (`index.tsx`); Test: the structure-graph banner tests.

**Design ref:** `x4ZEP`/`kx9LR`/`RLgFO` → `Banner` `bwzi3`/`m4Lrk`: `bg-destructive-soft`, `border-l-[3px] border-destructive`, padding `[12,20]`, gap10: triangle-alert icon (`text-destructive`) + text block (title + detail) + action button (`bg-destructive rounded-md px-3 py-1.5` for error; for paused use `border-status-waiting`+`text-status-waiting`+`bg-status-waiting-bg`, for completed use status-running). The banner sits between ToggleBar and Content (Main column), NOT inside the graph scroll area — move it up to `index.tsx` Main if currently nested in the graph.

**Behavioral contracts to PRESERVE:** banner testids `graph-deadlock-banner`/`graph-completed-banner`/`graph-paused-banner`/`graph-stopped-banner` and their i18n keys; the deadlock amber badges (`node-{id}-asking`) from S3; phase-driven conditional rendering.

**Steps:**
- [ ] **Step 1 (RED):** test — each phase renders its banner with the matching status token classes location (assert testid presence + that it sits in the Main column, not the scroll body); ask·死锁 keeps the amber node badge.
- [ ] **Step 2 (GREEN):** restyle banners + relocate to Main; map phase→{token set, icon, action}. Reuse existing phase logic.
- [ ] **Step 3:** tests green, tsc 0, eslint 0; i18n both locales.
- [ ] **Step 4:** `get_screenshot` compare `kx9LR` + `RLgFO`. Commit: `git commit <structure-graph.tsx index.tsx test i18n> -m "🎨 orch 设计:状态横幅对齐(暂停/错误/死锁/完成 左条 + 操作 + ask 琥珀徽标)"`.

---

### Task 10: Dark theme + full verification

**Files:** any orchestration component file that still hardcodes a color (grep-driven); i18n files (coverage); no new logic.

**Steps:**
- [ ] **Step 1:** `grep -rnE '#[0-9a-fA-F]{3,8}|rgb\(' frontend/src/components/agentre/orchestration` → every hex/rgb is a bug (tokens are themed). Replace each with the matching token class. (The S3–S7 status/agent helpers already use tokens; this catches stragglers introduced during restyle.)
- [ ] **Step 2:** for each in-scope Dark frame (`r0NpeW zJoz9 RUK6J rtrNS XoZpZ`), `get_screenshot` and compare to the rendered app in dark mode; fix any token misuse. Because all components use themed tokens, dark should be automatic — this step is the safety net.
- [ ] **Step 3:** i18n — run `pnpm test -- src/__tests__/i18n.test.ts`; confirm all new keys (`orchestration.run.*`, `orchestration.toggle.*`, any new board/sidebar copy) exist in BOTH locales with no orphans.
- [ ] **Step 4 (full gates):** `cd frontend && pnpm test` (full vitest, all green), `npx tsc --noEmit` (0), `make lint` from repo root (the orchestration files must be eslint+prettier clean; pre-existing unrelated debt in other files is flagged-not-fixed per the no-drive-by rule). Record the final counts.
- [ ] **Step 5:** Commit any fixes: `git commit <files> -m "🎨 orch 设计:暗色核对 + 硬编码色清理 + i18n 覆盖 + 全量 gate"`.

---

## Self-Review (checklist run by the plan author)

- **Spec coverage:** shell/RunSidebar (T1/T2), RunHeader (T3), ToggleBar (T4), structure graph top-down+connectors (T5), task board (T6), activity feed (T7), drill-in conv (T8), banner states (T9), dark+verify (T10). Overview/flow-editor/command-palette explicitly out of scope per the user. ✓
- **Behavior preserved:** every task names the S3–S7 testids/i18n/behavior it must keep; no logic rewrite. The single net-new behaviors (view toggle, speak-to-Leader footer) are TDD'd. ✓
- **Type consistency:** `view: "graph"|"feed"`, `selectedSessionId: number|null`, `stats` shape defined in T1/T4 and consumed consistently. RunHeader/ToggleBar/ConvPanel prop names fixed at definition. ✓
- **Token discipline:** all tokens already exist (audit-confirmed); Task 10 grep enforces no hardcoded colors → Dark is automatic. ✓
