# Background-Subagent Live Nesting (Phase 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Render a `run_in_background` subagent's internal activity (its tool calls / text) live-nested under the launch turn's AgentSpawnCard, exactly like a foreground subagent, while the parent session is idle.

**Architecture:** Phase 1 (committed `75e60b1`) stops the reader wedge by dropping the subagent's idle activity. Phase 2 instead *routes* it: the claudecode `Session` delivers the idle subagent sub-conversation as a new **"subagent activity turn"** keyed by the Agent `tool_use_id`, ended by the background-completion notification (which still triggers the existing autonomous-summary turn, unchanged). `chat_svc` consumes the activity turn, locates the **launch message** (the message holding that Agent `tool_use`), dispatches the events into a fresh accumulator seeded with the launch message's blocks (so the dispatcher manufactures `NestedToolUseBlock`/`NestedToolResultBlock`), streams them live on the **launch message's** per-turn stream, and persists the appended blocks. The frontend re-opens the launch message's stream so `buildRenderItems` groups the live nested blocks under the AgentSpawnCard (during idle the launch message is the latest message, so the session-keyed live store targets it cleanly).

**Tech Stack:** Go 1.26 (`pkg/claudecode`, `internal/pkg/agentruntime`, `internal/service/chat_svc`, `internal/repository/chat_repo`); React 19 / TS / Zustand (`frontend/src`); tests: goconvey + testify + sqlmock + mockgen (backend), Vitest (frontend).

## Global Constraints

- **Strict TDD: Red → Green → Refactor.** No implementation without a failing test first. (AGENTS.md)
- **Repository unit tests use `testutils.Database(t)` + sqlmock; service unit tests inject mockgen repo mocks, no DB.** (AGENTS.md)
- **Do not modify the bash autonomous-turn path** (`isBackgroundTaskNotification`, `AutonomousTurns`, `driveAutonomousTurn`) — Phase 2 is additive; the proven bash flow must stay byte-identical.
- **Frontend UI copy via i18n** (`react-i18next` + `frontend/src/i18n/locales/{zh-CN,en}/common.json`); no hardcoded Chinese in JSX. (AGENTS.md) — Phase 2 adds no visible copy, but the lint still runs.
- **Wails bindings only parse → svc.Method → return**; no business logic in `internal/app`. (AGENTS.md)
- Run backend tests with `GOWORK=off go test -race ./...` per package, or `make test-backend`. Frontend: `cd frontend && pnpm test -- <file>`.
- Ground-truth frame capture: `/tmp/bgsub_capture/capture.jsonl` (real CLI 2.1.185). The idle window after `result#1`: subagent internal `assistant`/`user` frames carry top-level `parent_tool_use_id` = the Agent `tool_use_id`; task frames (`task_progress`/`task_updated`/`task_notification`) carry `tool_use_id` = the same Agent id and `task_id` = subagent id; the completion notification has `output_file` set + no `subagent_type` (→ `isBackgroundTaskNotification` true).

---

## File Structure

| File | Responsibility | Action |
| --- | --- | --- |
| `pkg/claudecode/autoturn.go` | `SubagentActivity` value + doc | Modify |
| `pkg/claudecode/session.go` | `activeTurn.subagentToolUseID`, `subagentCh`, `SubagentActivity()` accessor, `currentTurn` routing | Modify |
| `pkg/claudecode/session_test.go` | session-layer routing tests | Modify |
| `internal/pkg/agentruntime/runner.go` | `SubagentActivitySource` interface + `SubagentActivity` struct | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go` | wrap `Session.SubagentActivity()` → translate events | Modify |
| `internal/repository/chat_repo/message.go` | `AppendSubagentChildren` cross-message append | Modify |
| `internal/repository/chat_repo/message_test.go` | repo append tests (sqlmock) | Modify |
| `internal/service/chat_svc/types.go` | `StreamSubagentActivityStarted` event kind + payload field | Modify |
| `internal/service/chat_svc/subagent_activity.go` | watcher + `driveSubagentActivity` | Create |
| `internal/service/chat_svc/subagent_activity_test.go` | service tests (repo mock) | Create |
| `internal/service/chat_svc/chat.go` | start the watcher when runtime implements `SubagentActivitySource` | Modify |
| `frontend/src/hooks/use-chat-stream.ts` | `subagent_activity_started` event field | Modify |
| `frontend/src/components/agentre/chat-panel.tsx` | handle `subagent_activity_started`: re-open launch-message stream (no new message row) | Modify |
| `frontend/src/components/agentre/chat-panel.test.tsx` | frontend handler test | Modify |

---

## Task 1: Session delivers a "subagent activity turn"

**Files:**
- Modify: `pkg/claudecode/autoturn.go`
- Modify: `pkg/claudecode/session.go`
- Test: `pkg/claudecode/session_test.go`

**Interfaces:**
- Produces: `func (s *Session) SubagentActivity() <-chan *SubagentActivity`; `type SubagentActivity struct { ToolUseID string; Events <-chan Event; SessionID string }`.
- Consumes: existing `activeTurn`, `newActiveTurn`, `isIdleBackgroundSubagentFrame`, `isBackgroundTaskNotification`, `finishActiveTurn`, `rawFrame{Type,Subtype,ParentToolUseID,ToolUseID}`.

**Behavior:** At idle, the first `isIdleBackgroundSubagentFrame(f)` starts a subagent-activity turn keyed by the frame's owning Agent `tool_use_id` (`ParentToolUseID` for assistant/user frames; `ToolUseID` for task frames), pushes a `*SubagentActivity` to `subagentCh`, sets it active, and **returns it** (the first frame's events are fed — unlike the AutoTurn trigger, which is dropped). While that activity turn is active, an `isBackgroundTaskNotification(f)` (the completion) **finishes** the activity turn and falls through to start the AutoTurn (existing path). All other frames feed the active activity turn.

- [ ] **Step 1: Write the failing test** — append to `session_test.go`. Reuse `fakeBackgroundSubagent` from Phase 1 (it already replays the real idle sub-conversation). Assert the activity turn surfaces with the nested events, then the autonomous summary still surfaces.

```go
// TestSession_BackgroundSubagentActivityTurn 锁定 Phase 2:后台 subagent 的空闲内部活动
// 经 SubagentActivity() 作为一轮独立事件流吐出(keyed by Agent tool_use_id),其后台完成
// 仍触发既有自主续轮(AutonomousTurns)。
func TestSession_BackgroundSubagentActivityTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := New(WithBinary("fake"), pipeSpawner(t, fakeBackgroundSubagent))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch1, err := sess.Turn(ctx, "alpha")
	require.NoError(t, err)
	assert.Equal(t, "started:alpha", drainText(t, ch1))

	// (a) subagent 活动轮:keyed by Agent tool_use_id,事件流含子 agent 内部文本/工具。
	var act *SubagentActivity
	select {
	case act = <-sess.SubagentActivity():
	case <-time.After(2 * time.Second):
		t.Fatal("expected a subagent activity turn within 2s")
	}
	require.NotNil(t, act)
	assert.Equal(t, fakeBgSubAgentTU, act.ToolUseID)
	var sawNestedText, sawNestedTool bool
	for ev := range act.Events {
		if ev.ParentToolUseID != fakeBgSubAgentTU {
			continue
		}
		if ev.Kind == EventTextDelta && ev.Text == "subagent thinking" {
			sawNestedText = true
		}
		if ev.Kind == EventPreToolUse {
			sawNestedTool = true
		}
	}
	assert.True(t, sawNestedText, "活动轮应含子 agent 内部文本帧")
	assert.True(t, sawNestedTool, "活动轮应含子 agent 内部工具调用帧")

	// (b) 后台完成仍触发既有自主续轮(总结)。
	var at *AutoTurn
	select {
	case at = <-sess.AutonomousTurns():
	case <-time.After(2 * time.Second):
		t.Fatal("expected autonomous summary turn within 2s")
	}
	require.NotNil(t, at)
	assert.Equal(t, "autonomous:subagent-summary", drainText(t, at.Events))
	require.NotNil(t, at.CompletedTask)
	assert.Equal(t, fakeBgSubAgentTU, at.CompletedTask.ToolUseID)

	// (c) turn2 无错位。
	ch2, err := sess.Turn(ctx, "beta")
	require.NoError(t, err)
	assert.Equal(t, "echo:beta", drainText(t, ch2))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `GOWORK=off go test -race -run TestSession_BackgroundSubagentActivityTurn ./pkg/claudecode/`
Expected: FAIL — `sess.SubagentActivity` undefined (compile error).

- [ ] **Step 3: Add the `SubagentActivity` value** — in `autoturn.go`, after the `CompletedBackgroundTask` block:

```go
// SubagentActivity 是一轮「后台 subagent 在空闲态产生的内部活动」的事件流。它由 readLoop
// 在空闲态遇到第一帧后台 subagent 内部活动时开出(keyed by 发起该 subagent 的 Agent 工具
// tool_use_id),以「后台型 task_notification(子 agent 完成)」收尾——该完成帧随即触发既有
// 自主续轮(AutonomousTurns)跑主 agent 总结。Events 与普通 Turn 同形:子 agent 内部 assistant/
// user 帧(ParentToolUseID==ToolUseID)、子 agent 的 task_progress/task_updated 等。
//
// 消费方(chat_svc)据 ToolUseID 定位发起 subagent 的那条「发起消息」,把事件嵌套渲染回那张
// AgentSpawnCard(见 docs/superpowers/plans/2026-06-23-bg-subagent-live-nesting.md)。
type SubagentActivity struct {
	ToolUseID string
	Events    <-chan Event
	SessionID string
}
```

- [ ] **Step 4: Add `subagentToolUseID` to `activeTurn` + `subagentCh` to `Session`** — in `session.go`:

In `type activeTurn struct { ... }` add field:

```go
	// subagentToolUseID 非空 = 本轮是「后台 subagent 活动轮」,值为发起该 subagent 的 Agent
	// 工具 tool_use_id。用于:readLoop 在收到后台完成 task_notification 时识别要收尾的是活动轮。
	subagentToolUseID string
```

In the `Session` struct (next to `autoCh`) add:

```go
	subagentCh chan *SubagentActivity // 后台 subagent 活动轮的出口(无消费方时缓冲兜底)
```

In the `Session` constructor (where `autoCh` is `make`d — find `autoCh: make(chan *AutoTurn, 8)` or equivalent in `OpenSession`/`newSession`) add alongside it:

```go
		subagentCh: make(chan *SubagentActivity, 8),
```

Add the accessor near `AutonomousTurns()`:

```go
// SubagentActivity 返回「后台 subagent 活动轮」的 channel。子进程退出时 close。消费方 range
// 它,每个 *SubagentActivity 是一轮独立事件流(见类型注释)。无消费方时缓冲(8)兜底。
func (s *Session) SubagentActivity() <-chan *SubagentActivity { return s.subagentCh }
```

In `shutdownReader()` (where `close(s.autoCh)` happens) add `close(s.subagentCh)`.

- [ ] **Step 5: Add the activity-turn helper + wire `currentTurn`** — in `session.go`.

Add a helper that extracts the owning Agent id from an idle subagent frame:

```go
// subagentOwnerID 取一帧空闲后台 subagent 活动帧所属的 Agent 工具 tool_use_id:assistant/user
// 帧用 parent_tool_use_id;子 agent 的 task 帧(task_progress/task_updated/非后台 task_notification)
// 用 tool_use_id(真 CLI 该字段即发起 Agent 的 tool_use_id)。取不到返回 ""。
func subagentOwnerID(f rawFrame) string {
	if (f.Type == "assistant" || f.Type == "user") && f.ParentToolUseID != "" {
		return f.ParentToolUseID
	}
	if f.Type == "system" && f.ToolUseID != "" {
		return f.ToolUseID
	}
	return ""
}
```

Replace the `currentTurn` body's active-turn short-circuit and the Phase-1 drop branch. The new `currentTurn`:

```go
func (s *Session) currentTurn(f rawFrame) *activeTurn {
	s.sinkMu.Lock()
	if s.active != nil {
		// 后台 subagent 活动轮收到「后台型完成通知」→ 收尾活动轮,落到下方起自主续轮。
		if s.active.subagentToolUseID != "" && isBackgroundTaskNotification(f) {
			done := s.active
			s.sinkMu.Unlock()
			s.finishActiveTurn(done) // 清 active 槽 + close 活动轮 ch/done
			s.sinkMu.Lock()
		} else {
			at := s.active
			s.sinkMu.Unlock()
			return at
		}
	}
	if isBackgroundTaskNotification(f) {
		at := newActiveTurn(true)
		s.active = at
		s.sinkMu.Unlock()
		s.autoCh <- &AutoTurn{
			Events:    at.ch,
			SessionID: s.sessionID,
			Trigger:   triggerBackgroundTask,
			CompletedTask: &CompletedBackgroundTask{
				ToolUseID: f.ToolUseID,
				TaskID:    f.TaskID,
				Status:    f.Status,
				Summary:   f.Summary,
			},
		}
		return nil
	}
	if isNonTurnFrame(f) {
		s.sinkMu.Unlock()
		return nil
	}
	if owner := subagentOwnerID(f); owner != "" && isIdleBackgroundSubagentFrame(f) {
		at := newActiveTurn(true)
		at.subagentToolUseID = owner
		s.active = at
		s.sinkMu.Unlock()
		s.subagentCh <- &SubagentActivity{ToolUseID: owner, Events: at.ch, SessionID: s.sessionID}
		return at // 与 AutoTurn 不同:首帧(子 agent 内部活动)要喂进活动轮
	}
	if isIdleBackgroundSubagentFrame(f) {
		s.sinkMu.Unlock()
		return nil // owner 取不到的兜底:仍按 Phase 1 丢弃,不卡读循环
	}
	s.sinkMu.Unlock()
	at := <-s.pendingTurns
	s.sinkMu.Lock()
	s.active = at
	s.sinkMu.Unlock()
	return at
}
```

Update the `currentTurn` doc comment to mention the activity-turn branch. Update `isIdleBackgroundSubagentFrame`'s trailing note from "Phase 1 仅止血(丢弃)" to point at this plan (the frames are now routed, not dropped).

- [ ] **Step 6: Run the test to verify it passes**

Run: `GOWORK=off go test -race -run TestSession_BackgroundSubagentActivityTurn ./pkg/claudecode/`
Expected: PASS.

- [ ] **Step 7: Run the full package to verify no regression (esp. Phase 1 + bash AutoTurn tests)**

Run: `GOWORK=off go test -race ./pkg/claudecode/`
Expected: ok. Confirm `TestSession_IdleBackgroundSubagentKeepsReaderAlive`, `TestSession_BackgroundTaskAutonomousTurn`, `TestSession_IdleSetPermissionModeKeepsReaderAlive` all still pass.

- [ ] **Step 8: Commit**

```bash
git add pkg/claudecode/autoturn.go pkg/claudecode/session.go pkg/claudecode/session_test.go
git commit -m "✨ claudecode: 后台 subagent 空闲内部活动作为独立活动轮吐出(SubagentActivity)"
```

---

## Task 2: Expose subagent activity through the runtime

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go`
- Test: `internal/pkg/agentruntime/runtimes/claudecode/autoturn_test.go` (or the file holding the AutonomousTurns wrapper test)

**Interfaces:**
- Produces: `type SubagentActivitySource interface { SubagentActivity(sessionID int64) <-chan SubagentActivity }`; `type SubagentActivity struct { ToolUseID string; Events <-chan Event }`.
- Consumes: `claudecode.Session.SubagentActivity()` (Task 1), the runtime's existing `drainStream` translator + active-session-handle cache used by `AutonomousTurns`.

- [ ] **Step 1: Write the failing test** — mirror the existing `AutonomousTurns` wrapper test. Open `runtimes/claudecode/autoturn_test.go`, find the test that drives `AutonomousTurns` through a fake session, and add a sibling that drives `SubagentActivity`, asserting the translated events carry `ParentToolCallID == "<agent tool id>"` and the channel yields one activity with the right `ToolUseID`. (Reuse the same fake-session harness already in that test file; copy its setup verbatim and swap `AutonomousTurns` → `SubagentActivity`.)

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test -race -run SubagentActivity ./internal/pkg/agentruntime/...`
Expected: FAIL — `SubagentActivity` undefined.

- [ ] **Step 3: Add the interface + struct** — in `runner.go`, after `AutonomousTurnSource`:

```go
// SubagentActivitySource 由能在轮间(空闲)产生「后台 subagent 内部活动流」的 runtime 实现
// —— 当前仅 claudecode。事件流与 Run 同形,ToolUseID 是发起该 subagent 的 Agent 工具 tool_use_id。
type SubagentActivitySource interface {
	SubagentActivity(sessionID int64) <-chan SubagentActivity
}

// SubagentActivity 镜像 claudecode.SubagentActivity:一轮后台 subagent 内部活动的事件流。
type SubagentActivity struct {
	ToolUseID string
	Events    <-chan Event
}
```

- [ ] **Step 4: Implement the wrapper** — in `runtimes/claudecode/autoturn.go`, mirror `AutonomousTurns` exactly. Find the existing `func (a *...) AutonomousTurns(sessionID int64) <-chan agentruntime.AutonomousTurn` and add the analogous method. It must: get the active session handle for `sessionID` from the same cache; read `handle.SubagentActivity()`; for each `*claudecode.SubagentActivity`, create an `evOut` channel, send `agentruntime.SubagentActivity{ToolUseID: a.ToolUseID, Events: evOut}` to the consumer, then `drainStream(a.Events, evOut)` (the same translator used for `AutonomousTurns`, which already maps `ParentToolUseID`→`ParentToolCallID`), closing `evOut` when done. Return an empty closed channel if no handle (mirror the `AutonomousTurns` nil-handle branch).

- [ ] **Step 5: Run to verify it passes**

Run: `GOWORK=off go test -race -run SubagentActivity ./internal/pkg/agentruntime/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/pkg/agentruntime/runner.go internal/pkg/agentruntime/runtimes/claudecode/autoturn.go internal/pkg/agentruntime/runtimes/claudecode/autoturn_test.go
git commit -m "✨ agentruntime: SubagentActivitySource 暴露后台 subagent 活动流(claudecode 实现)"
```

---

## Task 3: Cross-message append repo method

**Files:**
- Modify: `internal/repository/chat_repo/message.go`
- Test: `internal/repository/chat_repo/message_test.go`

**Interfaces:**
- Produces: `func (r *messageRepo) AppendSubagentChildren(ctx context.Context, sessionID int64, parentToolUseID string, childBlocksJSON string, childIDs []string) error` — finds the recent assistant message whose blocks contain a `subagent_state` with `parent_tool_call_id == parentToolUseID`, appends the StoredBlocks in `childBlocksJSON`, and appends `childIDs` to that block's `nested_tool_call_ids`. Add to the `MessageRepo` interface + regenerate the mock.
- Consumes: existing `FlipSubagentInBlocksJSON` scan pattern (`seq DESC`, `flipSubagentScanLimit`), `cagoblocks.StoredBlock`.

- [ ] **Step 1: Write the failing test** — in `message_test.go`, mirror `TestMessageRepo_FlipSubagentStatus_FlipsMatchingBlock` (sqlmock). Seed an assistant message whose `blocks_json` has a `subagent_state` block (`parent_tool_call_id:"toolu_agent"`, `nested_tool_call_ids:[]`) + the outer Agent `tool_use` block; expect `AppendSubagentChildren(ctx, sid, "toolu_agent", <two nested StoredBlocks JSON>, ["sub_bash"])` to issue an `UPDATE` whose `blocks_json` now contains the two nested blocks and `nested_tool_call_ids:["sub_bash"]`. Assert via the sqlmock `ExpectExec` arg matcher (substring checks on the JSON).

- [ ] **Step 2: Run to verify it fails**

Run: `GOWORK=off go test -race -run TestMessageRepo_AppendSubagentChildren ./internal/repository/chat_repo/`
Expected: FAIL — method undefined.

- [ ] **Step 3: Implement `AppendSubagentChildren`** — mirror `FlipSubagentStatus`'s scan; add a pure helper `AppendSubagentChildrenInBlocksJSON(blocksJSON, parentToolUseID, childBlocksJSON string, childIDs []string) (string, bool, error)` that unmarshals `[]StoredBlock`, finds the `subagent_state` with matching `parent_tool_call_id`, appends `childIDs` into its `nested_tool_call_ids` (dedup), unmarshals `childBlocksJSON` into `[]StoredBlock` and appends them to the slice, and re-marshals. The repo method scans `seq DESC` limited by `flipSubagentScanLimit`, calls the helper, and `Update`s the first matching message.

```go
func (r *messageRepo) AppendSubagentChildren(ctx context.Context, sessionID int64, parentToolUseID, childBlocksJSON string, childIDs []string) error {
	if parentToolUseID == "" || childBlocksJSON == "" {
		return nil
	}
	var rows []*chat_entity.Message
	if err := db.Ctx(ctx).
		Where("session_id = ? AND role = ?", sessionID, "assistant").
		Order("seq DESC").Limit(flipSubagentScanLimit).Find(&rows).Error; err != nil {
		return err
	}
	for _, msg := range rows {
		rewritten, ok, err := AppendSubagentChildrenInBlocksJSON(msg.BlocksJSON, parentToolUseID, childBlocksJSON, childIDs)
		if err != nil {
			logger.Ctx(ctx).Warn("chat_repo.AppendSubagentChildren: decode blocks failed; skipping",
				zap.Int64("messageId", msg.ID), zap.Error(err))
			continue
		}
		if !ok {
			continue
		}
		msg.BlocksJSON = rewritten
		return r.Update(ctx, msg)
	}
	return nil
}
```

Write `AppendSubagentChildrenInBlocksJSON` next to `FlipSubagentInBlocksJSON`, following the same `StoredBlock` envelope discipline (only touch the matched `subagent_state` block's `nested_tool_call_ids`; append the child StoredBlocks to the array tail).

- [ ] **Step 4: Add to interface + regen mock**

Add the method to the `MessageRepo` interface in `chat_repo`. Run: `cd /Users/codfrm/Code/agentre/agentre && make mock` (or `GOWORK=off go generate ./internal/repository/chat_repo/...`).

- [ ] **Step 5: Run to verify it passes**

Run: `GOWORK=off go test -race -run TestMessageRepo_AppendSubagentChildren ./internal/repository/chat_repo/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/repository/chat_repo/message.go internal/repository/chat_repo/message_test.go internal/repository/chat_repo/mock_chat_repo/
git commit -m "✨ chat_repo: AppendSubagentChildren 跨消息把后台 subagent 内部块并入发起卡"
```

---

## Task 4: chat_svc — subagent-activity event kind + payload

**Files:**
- Modify: `internal/service/chat_svc/types.go`
- Modify: `frontend/src/hooks/use-chat-stream.ts` (the TS mirror of `ChatStreamEvent`, regenerated or hand-kept — check how the existing `StreamAutonomousStarted`/`autonomous_started` mirror is maintained and follow it)

**Interfaces:**
- Produces: `StreamSubagentActivityStarted` stream kind (string `"subagent_activity_started"`); `ChatStreamEvent` gains no new struct, reuses `Stream` (the launch message's per-turn stream name) + a new `LaunchMessageID int64` field + `ToolUseID string`.

- [ ] **Step 1: Write the failing test** — add a small Go test asserting `StreamSubagentActivityStarted` constant equals `"subagent_activity_started"` and that `ChatStreamEvent` JSON-marshals `LaunchMessageID`/`ToolUseID`. (Mirror any existing constant test; if none, this is a trivial compile-guard test.)

- [ ] **Step 2: Run to verify it fails** — `GOWORK=off go test -run StreamSubagentActivityStarted ./internal/service/chat_svc/` → FAIL (undefined).

- [ ] **Step 3: Add the constant + fields** — in `types.go`, beside `StreamAutonomousStarted`:

```go
	// StreamSubagentActivityStarted:后台 subagent 在空闲态开始产出内部活动。前端据此对
	// 发起消息(LaunchMessageID)重开 per-turn 流(Stream),把活动块嵌套渲染回 AgentSpawnCard。
	// 与 StreamAutonomousStarted 不同:不插入新 assistant 行(发起消息已存在)。
	StreamSubagentActivityStarted ChatStreamKind = "subagent_activity_started"
```

Add to `ChatStreamEvent`:

```go
	LaunchMessageID int64  `json:"launchMessageId,omitempty"`
	ToolUseID       string `json:"toolUseId,omitempty"`
```

- [ ] **Step 4: Run to verify it passes** — `GOWORK=off go test -run StreamSubagentActivityStarted ./internal/service/chat_svc/` → PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/service/chat_svc/types.go
git commit -m "✨ chat_svc: StreamSubagentActivityStarted 事件类型(后台 subagent 活动重开发起流)"
```

---

## Task 5: chat_svc — watcher + driveSubagentActivity

**Files:**
- Create: `internal/service/chat_svc/subagent_activity.go`
- Create: `internal/service/chat_svc/subagent_activity_test.go`
- Modify: `internal/service/chat_svc/chat.go` (start the watcher where `startAutonomousWatcher` is started, gated on `SubagentActivitySource`)

**Interfaces:**
- Consumes: `agentruntime.SubagentActivitySource`, `chat_repo.Message().AppendSubagentChildren` (Task 3), `chat_repo.Message()` lookup (find launch message by tool_use_id — reuse the `seq DESC` scan, or add `FindAssistantByToolUseID`), the dispatcher (`s.dispatcher.Apply`), `dispatcherEmitter`, `turn.New()`, `s.newTurnContext`, `StreamName(sessionID, launchMessageID)`.
- Produces: `func (s *chatSvc) startSubagentActivityWatcher(sessionID int64, be *agent_backend_entity.AgentBackend, src agentruntime.SubagentActivitySource)`; `func (s *chatSvc) driveSubagentActivity(ctx, sessionID, be, act agentruntime.SubagentActivity)`.

**Behavior of `driveSubagentActivity`:**
1. Locate the launch message: the assistant message containing a `subagent_state` block with `parent_tool_call_id == act.ToolUseID`. If none → drain `act.Events` and return (don't wedge the session reader).
2. Build `stream := StreamName(sessionID, launchMsg.ID)`. Emit a session-level `StreamSubagentActivityStarted{Stream: stream, LaunchMessageID: launchMsg.ID, ToolUseID: act.ToolUseID}` on `AutonomousStreamName(sessionID)` (reuse the existing session-level bypass channel the frontend already subscribes to).
3. Seed an accumulator from the launch message's existing blocks (so nested-block IDs/dedup are consistent), dispatch `act.Events` through `s.dispatcher.Apply(ctx, ev, acc, dispEmit, nil, turnCtx)` with `turnCtx` bound to `launchMsg` + `stream` — the `ToolCallHandler` already routes `ParentToolCallID != ""` to `NestedToolUseBlock`; live chunks stream on `stream`.
4. On channel close: compute the **new** blocks added this activity (the nested blocks created), serialize them to StoredBlock JSON + collect their IDs, call `chat_repo.Message().AppendSubagentChildren(finalCtx, sessionID, act.ToolUseID, childJSON, childIDs)` to persist into the launch message. Emit `StreamDone` for `stream`.
5. Concurrency: like `startAutonomousWatcher`, the watcher goroutine must NOT hold the chat-session lock while draining `act.Events` (avoid wedging `Session.readLoop`).

- [ ] **Step 1: Write the failing service test** — `subagent_activity_test.go`, goconvey + mockgen repo mock, no DB. Construct a `chatSvc` with mock `MessageRepo`. Feed a fake `SubagentActivity` whose `Events` channel emits: a nested `ToolCall{ParentToolCallID:"toolu_agent", ID:"sub_bash", Name:"Bash"}` then a nested `ToolResult{ParentToolCallID:"toolu_agent", ToolCallID:"sub_bash", Content:"SUBAGENT_DONE"}` then close. Expect: (a) one `StreamSubagentActivityStarted` emitted with `ToolUseID:"toolu_agent"` and the launch message's id; (b) `AppendSubagentChildren(ctx, sid, "toolu_agent", <json containing nested_tool_use sub_bash>, ["sub_bash"])` called once; (c) `StreamDone` emitted on the launch stream. Stub the launch-message lookup to return a message with a `subagent_state{parent_tool_call_id:"toolu_agent"}` block.

(Write the full test body mirroring `autonomous_turn`'s service test if one exists; otherwise mirror the structure of an existing `chat_svc` goconvey service test that injects `dispatcherEmitter` and asserts emitted events via a capture emitter.)

- [ ] **Step 2: Run to verify it fails** — `GOWORK=off go test -race -run SubagentActivity ./internal/service/chat_svc/` → FAIL (undefined).

- [ ] **Step 3: Implement `subagent_activity.go`** — model `startSubagentActivityWatcher` on `startAutonomousWatcher` (lazy per-session goroutine over `src.SubagentActivity(sessionID)`), and `driveSubagentActivity` on `driveAutonomousTurn` minus the message-creation/seq/session-status transitions (it updates an EXISTING message, does not create one, and does not flip session status to running — the session is idle background work). Use a capturing accumulator diff to get the new nested blocks, or have `AppendSubagentChildren` receive `acc.Finalize()`'s nested blocks filtered to `ParentToolCallID == act.ToolUseID`.

- [ ] **Step 4: Wire the watcher** — in `chat.go`, where the runtime is checked for `AutonomousTurnSource` and `startAutonomousWatcher` is called, add a parallel type-assert for `agentruntime.SubagentActivitySource` and call `startSubagentActivityWatcher`.

- [ ] **Step 5: Run to verify it passes** — `GOWORK=off go test -race -run SubagentActivity ./internal/service/chat_svc/` → PASS.

- [ ] **Step 6: Run the chat_svc package** — `GOWORK=off go test -race ./internal/service/chat_svc/...` → ok.

- [ ] **Step 7: Commit**

```bash
git add internal/service/chat_svc/subagent_activity.go internal/service/chat_svc/subagent_activity_test.go internal/service/chat_svc/chat.go
git commit -m "✨ chat_svc: 后台 subagent 活动流嵌套渲染回发起卡并跨消息落库"
```

---

## Task 6: Frontend — re-open the launch message's stream

**Files:**
- Modify: `frontend/src/hooks/use-chat-stream.ts`
- Modify: `frontend/src/components/agentre/chat-panel.tsx`
- Test: `frontend/src/components/agentre/chat-panel.test.tsx`

**Interfaces:**
- Consumes: the session-level autonomous bypass subscription `chat:autonomous:<sessionId>` (already wired in `onAutonomousEvent`), `openStream`, `useChatStreamsStore`.
- Behavior: on `ev.kind === "subagent_activity_started"`, call `openStream({ name: ev.stream, sessionId, assistantMessageId: ev.launchMessageId, streamStartedAt: Date.now() })` WITHOUT `setMessages` (the launch message already exists). During idle the launch message is the latest, so `liveTargetId === launchMessageId` and `buildRenderItems` nests the live blocks under the card. Do NOT mark session running (background work; keep the idle pill). On `StreamDone` for that stream, the existing `reloadSession` path reloads the persisted nested blocks.

- [ ] **Step 1: Write the failing test** — in `chat-panel.test.tsx`, mirror the existing `onAutonomousEvent` test. Fire a `subagent_activity_started` event `{kind, stream:"chat:event:1:42", sessionId:1, launchMessageId:42, toolUseId:"toolu_agent"}` and assert `openStream` was called with `assistantMessageId: 42` and that `setMessages` was NOT called with a new row. (Use the same mocking approach the existing autonomous test uses; per memory `reference_frontend_wails_runtime_test_mock`, this file already mocks the wails runtime — follow its existing `vi.mock`.)

- [ ] **Step 2: Run to verify it fails** — `cd /Users/codfrm/Code/agentre/agentre/frontend && pnpm test -- src/components/agentre/chat-panel.test.tsx` → FAIL.

- [ ] **Step 3: Add the event field** — in `use-chat-stream.ts`, extend the `ChatStreamEvent` TS type with `launchMessageId?: number; toolUseId?: string;` and add `"subagent_activity_started"` to the `kind` union.

- [ ] **Step 4: Handle it in `onAutonomousEvent`** — extend the callback:

```ts
      if (ev.kind === "subagent_activity_started") {
        if (!ev.stream || !ev.launchMessageId) return;
        openStream({
          name: ev.stream,
          sessionId,
          assistantMessageId: ev.launchMessageId,
          streamStartedAt: Date.now(),
        });
        return;
      }
```

Place this branch before the existing `if (ev.kind !== "autonomous_started") return;` guard (or widen the guard to accept both kinds).

- [ ] **Step 5: Run to verify it passes** — `cd frontend && pnpm test -- src/components/agentre/chat-panel.test.tsx` → PASS.

- [ ] **Step 6: Run lint** — `cd /Users/codfrm/Code/agentre/agentre && make lint` (or `cd frontend && pnpm lint`). Expected: no new issues.

- [ ] **Step 7: Commit**

```bash
git add frontend/src/hooks/use-chat-stream.ts frontend/src/components/agentre/chat-panel.tsx frontend/src/components/agentre/chat-panel.test.tsx
git commit -m "✨ chat: 后台 subagent 活动重开发起消息流,内部步骤实时嵌套渲染"
```

---

## Task 7: End-to-end verification

**Files:** none (verification only).

- [ ] **Step 1: Backend full suite** — `cd /Users/codfrm/Code/agentre/agentre && make test-backend` → all green.
- [ ] **Step 2: Frontend suite** — `cd frontend && pnpm test` → all green.
- [ ] **Step 3: Lint** — `make lint` → clean.
- [ ] **Step 4: Manual real-CLI smoke (optional but recommended)** — launch a real background subagent via the running app (or re-capture as in Phase 0) and confirm the launch card fills in with the subagent's Bash step live, then flips to completed, and the summary lands as a new assistant message.
- [ ] **Step 5: Commit any doc/checklist updates** (none expected).

---

## Self-Review Notes

- **Spec coverage:** Task 1 (session activity turn) + Task 2 (runtime) + Task 3 (persist) + Task 5 (render/stream) + Task 6 (frontend live) together deliver "render nested live during idle"; the bash path is untouched (Global Constraints). Card flip to completed remains via the existing autonomous turn (`FlipSubagentStatus`), which now runs AFTER `AppendSubagentChildren` (Task 5 persists at activity-turn close, before the completion notification triggers the autonomous turn) — `FlipSubagentStatus` preserves sibling blocks, so the nested children survive the flip.
- **Known limitation (document, don't fix here):** if the user sends a new turn WHILE the background subagent is mid-activity, the new turn's frames could route into the active subagent-activity turn (session has one active slot). The real CLI's serialization of concurrent user input during background work is unverified; flag in the Task 1 commit body and revisit only if observed.
- **Interleaving of inner-bash task frames (L18/L19):** these have `tool_use_id` = the inner bash id (not the Agent id), so `subagentOwnerID` returns the inner bash id for them. They arrive while the activity turn is already active (`active != nil`), so they feed the active turn regardless — `subagentOwnerID` only matters for the FIRST frame that opens the turn (L15, an assistant frame with `parent_tool_use_id` = Agent id). Verify in Task 1 Step 7 that the activity turn opens on L15, not L18.
