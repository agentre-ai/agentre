# 会话级「后台运行中」指示 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让侧栏 / tab / 命令面板能看出「该会话有后台 subagent 在跑」，即使会话已读、`agent_status=idle`，而不改动 `running` 门控语义。

**Architecture:** chat_svc 用内存 `sync.Map`（sessionID → 运行中后台 subagent 的 tool_use_id 集合）作真源；集合非空 = `bgRunning`。该 bit 随既有 `session_status` 事件 + `ListChatAgents`/`LoadSession` DTO 传到前端 `session-status-store`；`attention-store` 据此产出新 reason `bg_running`，复用 running 显示色 + 「后台」pill 文案。后台 subagent 易失，重启后 map 空=0 天然正确，无需迁移。

**Tech Stack:** Go 1.26 / cago / goconvey+sqlmock+mockgen；React 19 / TS / Vitest / zustand / react-i18next。

## Global Constraints

- **TDD 铁律**：先写失败测试并**跑一次看它按预期失败**，再写实现。不得先实现后补测试。
- **不改 `running` 语义**：`CountActive` / `CountActiveByProject` / `CountRunningByAgents`（呼吸灯/退出确认/删项目门控）**不得**因后台 subagent 变化，本计划**不碰**这些函数。
- **不加数据库迁移**；不改后台任务芯片（`background-tasks/*`）既有行为。
- **i18n**：新可见 UI 文案必须走 `t(...)` 并同时更新 `frontend/src/i18n/locales/{zh-CN,en}/common.json`；`i18next/no-literal-string` 会拦硬编码中文。
- **共享分支提交纪律**：本分支 `develop/wyz` 有并发会话，**永远 `git commit <pathspec>` 带文件名**，不裸 `git commit`。
- 后端判定后台 subagent 的判据 = 与前端 `frontend/src/components/agentre/background-tasks/derive.ts` **同款**：`subagent_state` 块 `status=="running"` 且其父 `tool_use`（`ParentToolCallID`）入参 `run_in_background===true`。前台 subagent（无 `run_in_background`）主轮本就 running，不纳入。
- 每个后端 focused 测试用 `go test -race -run TestXxx ./internal/service/chat_svc/...`；前端 `cd frontend && pnpm test -- <path>`。

---

### Task 1: 后端 bg_running 状态模块（map + accessors + 纯判据 helper）

**Files:**
- Create: `internal/service/chat_svc/bg_running.go`
- Create: `internal/service/chat_svc/bg_running_test.go`
- Modify: `internal/service/chat_svc/chat.go`（chatSvc 结构体加字段，约 199 行 `subagentActivityWatchers` 附近）

**Interfaces:**
- Produces:
  - `func (s *chatSvc) addBgRunning(sessionID int64, ids ...string) bool`（有新增返 true）
  - `func (s *chatSvc) removeBgRunning(sessionID int64, id string) bool`（有移除返 true）
  - `func (s *chatSvc) clearBgRunning(sessionID int64) bool`（原本非空返 true）
  - `func (s *chatSvc) bgRunningActive(sessionID int64) bool`
  - `func runningBgSubagentIDs(blocks []cagoblocks.ContentBlock) []string`

- [ ] **Step 1: chatSvc 加字段**

在 `internal/service/chat_svc/chat.go` 的 chatSvc 结构体里（`subagentActivityWatchers sync.Map` 同组）新增：

```go
	// bgRunning: sessionID(int64) → *bgRunningSet。per-session「运行中后台 subagent 的
	// tool_use_id 集合」。集合非空 = 该会话有后台 subagent 在跑。后台 subagent 易失
	// (随 CLI 子进程/重启消失)，故不落库；重启后 map 空 = 0 天然正确。见 bg_running.go。
	bgRunning sync.Map
```

- [ ] **Step 2: 写失败测试 `bg_running_test.go`**

```go
package chat_svc

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

func TestBgRunningSet_AddRemoveClearActive(t *testing.T) {
	s := &chatSvc{}
	if s.bgRunningActive(7) {
		t.Fatal("empty session should be inactive")
	}
	if !s.addBgRunning(7, "tu-1", "tu-2") {
		t.Fatal("first add should report changed")
	}
	if !s.bgRunningActive(7) {
		t.Fatal("session should be active after add")
	}
	if s.addBgRunning(7, "tu-1") {
		t.Fatal("re-add existing id should be no-op (idempotent)")
	}
	if !s.removeBgRunning(7, "tu-1") {
		t.Fatal("remove existing should report changed")
	}
	if s.removeBgRunning(7, "tu-1") {
		t.Fatal("remove missing should be no-op")
	}
	if !s.bgRunningActive(7) {
		t.Fatal("still active: tu-2 remains")
	}
	if !s.clearBgRunning(7) {
		t.Fatal("clear non-empty should report changed")
	}
	if s.bgRunningActive(7) {
		t.Fatal("inactive after clear")
	}
	if s.clearBgRunning(7) {
		t.Fatal("clear empty should be no-op")
	}
}

func TestRunningBgSubagentIDs(t *testing.T) {
	blks := []cagoblocks.ContentBlock{
		// 后台 subagent: Agent tool_use run_in_background=true + running subagent_state
		&cagoblocks.ToolUseBlock{ID: "bg-1", Input: map[string]any{"run_in_background": true}},
		&blocks.SubagentStateBlock{ParentToolCallID: "bg-1", Kind: "local_agent", Status: "running"},
		// 前台 subagent: 无 run_in_background → 不纳入
		&cagoblocks.ToolUseBlock{ID: "fg-1", Input: map[string]any{}},
		&blocks.SubagentStateBlock{ParentToolCallID: "fg-1", Kind: "local_agent", Status: "running"},
		// 后台但已完成 → status 非 running → 不纳入
		&cagoblocks.ToolUseBlock{ID: "done-1", Input: map[string]any{"run_in_background": true}},
		&blocks.SubagentStateBlock{ParentToolCallID: "done-1", Kind: "local_agent", Status: "completed"},
	}
	got := runningBgSubagentIDs(blks)
	if len(got) != 1 || got[0] != "bg-1" {
		t.Fatalf("want [bg-1], got %v", got)
	}
}
```

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run 'TestBgRunningSet_AddRemoveClearActive|TestRunningBgSubagentIDs' ./internal/service/chat_svc/...`
Expected: 编译失败（`addBgRunning` / `runningBgSubagentIDs` undefined）。

- [ ] **Step 4: 实现 `bg_running.go`**

```go
package chat_svc

import (
	"sync"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// bgRunningSet 是单会话「运行中后台 subagent 的 tool_use_id 集合」。用集合而非计数器：
// add/remove 幂等，杜绝加减泄漏。
type bgRunningSet struct {
	mu  sync.Mutex
	ids map[string]struct{}
}

func (s *chatSvc) bgSet(sessionID int64) *bgRunningSet {
	v, _ := s.bgRunning.LoadOrStore(sessionID, &bgRunningSet{ids: map[string]struct{}{}})
	return v.(*bgRunningSet)
}

// addBgRunning 把 ids 加入会话集合，有真正新增时返 true。
func (s *chatSvc) addBgRunning(sessionID int64, ids ...string) bool {
	if sessionID <= 0 || len(ids) == 0 {
		return false
	}
	set := s.bgSet(sessionID)
	set.mu.Lock()
	defer set.mu.Unlock()
	changed := false
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := set.ids[id]; !ok {
			set.ids[id] = struct{}{}
			changed = true
		}
	}
	return changed
}

// removeBgRunning 移除一个 id，有真正移除时返 true。
func (s *chatSvc) removeBgRunning(sessionID int64, id string) bool {
	if sessionID <= 0 || id == "" {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	if _, ok := set.ids[id]; !ok {
		return false
	}
	delete(set.ids, id)
	return true
}

// clearBgRunning 清空会话集合，原本非空时返 true。
func (s *chatSvc) clearBgRunning(sessionID int64) bool {
	if sessionID <= 0 {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	if len(set.ids) == 0 {
		return false
	}
	set.ids = map[string]struct{}{}
	return true
}

// bgRunningActive 报告会话是否有后台 subagent 在跑（集合非空）。
func (s *chatSvc) bgRunningActive(sessionID int64) bool {
	if sessionID <= 0 {
		return false
	}
	v, ok := s.bgRunning.Load(sessionID)
	if !ok {
		return false
	}
	set := v.(*bgRunningSet)
	set.mu.Lock()
	defer set.mu.Unlock()
	return len(set.ids) > 0
}

// runningBgSubagentIDs 从一批已 finalize 的块里挑出「运行中后台 subagent」的父 tool_use_id。
// 判据与前端 background-tasks/derive.ts 同款：SubagentStateBlock.Status=="running" 且其父
// tool_use(ParentToolCallID)入参 run_in_background===true。前台 subagent(无该入参)不纳入。
func runningBgSubagentIDs(finalBlocks []cagoblocks.ContentBlock) []string {
	inputByToolUse := map[string]map[string]any{}
	for _, b := range finalBlocks {
		switch tu := b.(type) {
		case *cagoblocks.ToolUseBlock:
			inputByToolUse[tu.ID] = tu.Input
		case cagoblocks.ToolUseBlock:
			inputByToolUse[tu.ID] = tu.Input
		}
	}
	var out []string
	for _, b := range finalBlocks {
		sb, ok := b.(*blocks.SubagentStateBlock)
		if !ok || sb.Status != "running" || sb.ParentToolCallID == "" {
			continue
		}
		input := inputByToolUse[sb.ParentToolCallID]
		if bg, _ := input["run_in_background"].(bool); bg {
			out = append(out, sb.ParentToolCallID)
		}
	}
	return out
}
```

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run 'TestBgRunningSet_AddRemoveClearActive|TestRunningBgSubagentIDs' ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/bg_running.go internal/service/chat_svc/bg_running_test.go internal/service/chat_svc/chat.go
git commit internal/service/chat_svc/bg_running.go internal/service/chat_svc/bg_running_test.go internal/service/chat_svc/chat.go -m "✨ chat_svc: 后台 subagent 运行集合(bgRunning map + runningBgSubagentIDs 判据)"
```

---

### Task 2: 后端 session_status 携带 BgRunning + emit helper

**Files:**
- Modify: `internal/service/chat_svc/types.go:215-224`（`ChatSessionStatusPatch` 加字段）
- Modify: `internal/service/chat_svc/dispatcher_emitter.go`（`sessionStatusFromMap` 约 300-310 行，转发时带上 bgRunning）
- Create: emit helper 放 `internal/service/chat_svc/bg_running.go`（Task 1 文件末尾追加）
- Test: `internal/service/chat_svc/bg_running_test.go`

**Interfaces:**
- Consumes: `bgRunningActive`（Task 1）
- Produces: `ChatSessionStatusPatch.BgRunning bool`；`func (s *chatSvc) emitBgRunningStatus(ctx context.Context, sess *chat_entity.Session, stream string)`

- [ ] **Step 1: types.go 加字段**

在 `ChatSessionStatusPatch`（types.go:215）里，`NeedsAttention` 之后加：

```go
	// BgRunning 会话是否有后台 subagent 在跑(run_in_background)。总是带最新值；
	// 独立于 AgentStatus——后台 subagent 期间 AgentStatus 仍为 idle。见 bg_running.go。
	BgRunning bool `json:"bgRunning"`
```

- [ ] **Step 2: dispatcher_emitter 转发带 bgRunning**

在 `sessionStatusFromMap`（dispatcher_emitter.go 约 304 行）的 `out := &ChatSessionStatusPatch{...}` 里加一行：

```go
		BgRunning:      boolOf(m, "bgRunning"),
```

- [ ] **Step 3: 写失败测试**

在 `bg_running_test.go` 追加：

```go
func TestEmitBgRunningStatus_CarriesFlag(t *testing.T) {
	rec := &recordingEmitter{}
	s := &chatSvc{emitter: rec}
	s.addBgRunning(9, "tu-x")
	sess := &chat_entity.Session{Model: chat_entity.Session{}.Model}
	sess.ID = 9
	sess.AgentStatus = "idle"
	s.emitBgRunningStatus(context.Background(), sess, "stream-9")

	if len(rec.events) != 1 {
		t.Fatalf("want 1 event, got %d", len(rec.events))
	}
	ev := rec.events[0]
	if ev.Kind != StreamSessionStatus || ev.SessionStatus == nil {
		t.Fatalf("want session_status event, got %+v", ev)
	}
	if !ev.SessionStatus.BgRunning {
		t.Fatal("want BgRunning=true")
	}
	if ev.SessionStatus.AgentStatus != "idle" {
		t.Fatalf("want agentStatus idle, got %q", ev.SessionStatus.AgentStatus)
	}
}
```

> 注：`recordingEmitter` 若包内已有测试替身则复用；没有则在本测试文件加最小实现：
> ```go
> type recordingEmitter struct{ events []ChatStreamEvent }
> func (r *recordingEmitter) Emit(_ context.Context, _ string, ev ChatStreamEvent) { r.events = append(r.events, ev) }
> ```
> 先 `grep -rn "func.*Emit(ctx" internal/service/chat_svc/*_test.go` 找现成替身（`s.emitter` 的接口方法名以现有 `dispatcherEmitter`/`emitter` 字段类型为准；若签名不符按实际接口调整）。

- [ ] **Step 4: 跑测试看失败**

Run: `go test -race -run TestEmitBgRunningStatus_CarriesFlag ./internal/service/chat_svc/...`
Expected: 编译失败（`emitBgRunningStatus` undefined）。

- [ ] **Step 5: 实现 emit helper（追加到 bg_running.go）**

```go
// emitBgRunningStatus 推一帧 session_status，携带当前 agentStatus/needsAttention + 最新
// bgRunning。后台 subagent 起/完成时调用，让前端 store 即时刷新。stream 为空则不 emit。
func (s *chatSvc) emitBgRunningStatus(ctx context.Context, sess *chat_entity.Session, stream string) {
	if sess == nil || stream == "" {
		return
	}
	s.emitter.Emit(ctx, stream, ChatStreamEvent{
		Kind: StreamSessionStatus,
		SessionStatus: &ChatSessionStatusPatch{
			AgentStatus:    sess.AgentStatus,
			NeedsAttention: sess.NeedsAttention,
			BgRunning:      s.bgRunningActive(sess.ID),
		},
	})
}
```

> `bg_running.go` 顶部 import 需补 `"context"` 与 `"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"`。

- [ ] **Step 6: 跑测试看通过 + 全包编译**

Run: `go test -race -run TestEmitBgRunningStatus_CarriesFlag ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 7: Commit**

```bash
git add internal/service/chat_svc/types.go internal/service/chat_svc/dispatcher_emitter.go internal/service/chat_svc/bg_running.go internal/service/chat_svc/bg_running_test.go
git commit internal/service/chat_svc/types.go internal/service/chat_svc/dispatcher_emitter.go internal/service/chat_svc/bg_running.go internal/service/chat_svc/bg_running_test.go -m "✨ chat_svc: session_status 携带 bgRunning + emitBgRunningStatus"
```

---

### Task 3: 后端 DTO（ChatSessionLite / ChatSessionDetail）携带 BgRunning

**Files:**
- Modify: `internal/service/chat_svc/types.go:374-383`（`ChatSessionLite` 加字段）、`ChatSessionDetail`（约 401-410，`NeedsAttention` 附近加字段）
- Modify: `internal/service/chat_svc/chat.go:422-427`（`toChatSessionLite`）、`:464-471`（LoadSession 组 ChatSessionDetail 处）
- Test: `internal/service/chat_svc/chat_internal_test.go`（若已存在同类 DTO 测试）或新增

**Interfaces:**
- Consumes: `bgRunningActive`
- Produces: `ChatSessionLite.BgRunning bool` / `ChatSessionDetail.BgRunning bool`

- [ ] **Step 1: types.go 两个 DTO 加字段**

`ChatSessionLite`（types.go:378 `NeedsAttention` 后）：

```go
	BgRunning bool `json:"bgRunning"`
```

`ChatSessionDetail`（types.go:410 `NeedsAttention` 后）同样加：

```go
	BgRunning bool `json:"bgRunning"`
```

- [ ] **Step 2: 写失败测试**

```go
func TestToChatSessionLite_CarriesBgRunning(t *testing.T) {
	s := &chatSvc{}
	s.addBgRunning(42, "tu-bg")
	sess := &chat_entity.Session{}
	sess.ID = 42
	sess.AgentStatus = "idle"
	lite := s.toChatSessionLite(sess) // 若为包级函数则去掉 s. 前缀，按实际签名调整
	if !lite.BgRunning {
		t.Fatal("want ChatSessionLite.BgRunning=true")
	}
}
```

> 先 `grep -n "func.*toChatSessionLite" internal/service/chat_svc/chat.go` 确认它是方法还是包级函数（当前 chat.go:422 附近）。若为包级 `toChatSessionLite(sess)` 无法读 `s.bgRunningActive`，则改成方法 `func (s *chatSvc) toChatSessionLite(...)` 并更新调用点——这是本 task 允许的最小重构。

- [ ] **Step 3: 跑测试看失败**

Run: `go test -race -run TestToChatSessionLite_CarriesBgRunning ./internal/service/chat_svc/...`
Expected: FAIL（`BgRunning` 恒 false / 或签名不符编译失败）。

- [ ] **Step 4: 实现**

`toChatSessionLite`（chat.go:422 的 `ChatSessionLite{...}` literal）加：

```go
		BgRunning:      s.bgRunningActive(sess.ID),
```

LoadSession 组 `ChatSessionDetail`（chat.go:464-471 的 literal，`NeedsAttention` 后）加：

```go
			BgRunning:              s.bgRunningActive(sess.ID),
```

（若 `toChatSessionLite` 原为包级函数，改为 `chatSvc` 方法并同步其调用点。）

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestToChatSessionLite_CarriesBgRunning ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/types.go internal/service/chat_svc/chat.go internal/service/chat_svc/chat_internal_test.go
git commit internal/service/chat_svc/types.go internal/service/chat_svc/chat.go internal/service/chat_svc/chat_internal_test.go -m "✨ chat_svc: ListChatAgents/LoadSession DTO 携带 bgRunning"
```

---

### Task 4: 后端维护点 — 主轮 finalize 加入 + abort/error 清空

**Files:**
- Modify: `internal/service/chat_svc/chat.go`（finalize 段，`finalBlocks := acc.Finalize()` 约 2649；abort 分支约 2654-2657；收尾 emit 约 2801-2808 之后）
- Test: `internal/service/chat_svc/chat_internal_test.go`（用现有 Send-path 测试骨架）或 `subagent_activity_test.go` 同款风格

**Interfaces:**
- Consumes: `runningBgSubagentIDs`, `addBgRunning`, `clearBgRunning`, `emitBgRunningStatus`

- [ ] **Step 1: 写失败测试（单元覆盖判据接线）**

优先写一个**不依赖整条 Send 流程**的窄测试，直接验证「finalize 后集合被更新」的接线函数。为此把接线抽成可测方法：

```go
func TestReconcileBgRunningOnFinalize_AddsAndEmits(t *testing.T) {
	rec := &recordingEmitter{}
	s := &chatSvc{emitter: rec}
	sess := &chat_entity.Session{}
	sess.ID = 5
	sess.AgentStatus = "idle"
	final := []cagoblocks.ContentBlock{
		&cagoblocks.ToolUseBlock{ID: "bg-1", Input: map[string]any{"run_in_background": true}},
		&blocks.SubagentStateBlock{ParentToolCallID: "bg-1", Kind: "local_agent", Status: "running"},
	}
	s.reconcileBgRunningOnFinalize(context.Background(), sess, final, "stream-5")
	if !s.bgRunningActive(5) {
		t.Fatal("want active after finalize with running bg subagent")
	}
	if len(rec.events) != 1 || !rec.events[0].SessionStatus.BgRunning {
		t.Fatalf("want 1 session_status event with BgRunning=true, got %+v", rec.events)
	}
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestReconcileBgRunningOnFinalize_AddsAndEmits ./internal/service/chat_svc/...`
Expected: 编译失败（`reconcileBgRunningOnFinalize` undefined）。

- [ ] **Step 3: 实现接线方法（追加到 bg_running.go）**

```go
// reconcileBgRunningOnFinalize 在一轮 finalize 后，把该轮新起的后台 subagent 加入会话集合，
// 有变化则 emit session_status。主轮 / 自主轮 finalize 都调它。
func (s *chatSvc) reconcileBgRunningOnFinalize(ctx context.Context, sess *chat_entity.Session, finalBlocks []cagoblocks.ContentBlock, stream string) {
	if sess == nil {
		return
	}
	ids := runningBgSubagentIDs(finalBlocks)
	if s.addBgRunning(sess.ID, ids...) {
		s.emitBgRunningStatus(ctx, sess, stream)
	}
}
```

- [ ] **Step 4: 接进 chat.go Send-path**

在 chat.go 主轮收尾：`_ = assistantMsg.SetBlocks(finalBlocks)`（约 2668）**之后**、且在 `finalCtx` 就绪之后，接一行（用 `finalCtx`，因 abort 时 turnCtx 已 cancel）：

```go
	if aborted || stopErr != nil {
		if s.clearBgRunning(sess.ID) {
			s.emitBgRunningStatus(finalCtx, sess, stream)
		}
	} else {
		s.reconcileBgRunningOnFinalize(finalCtx, sess, finalBlocks, stream)
	}
```

> 放在 `finalCtx := context.WithoutCancel(ctx)` 定义之后（约 2728 行附近的 finalCtx）。`stream`/`sess`/`finalBlocks` 此处均在作用域内。

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestReconcileBgRunningOnFinalize_AddsAndEmits ./internal/service/chat_svc/...`
Expected: PASS。同时跑整包编译：`go test -race -run TestNothingXYZ ./internal/service/chat_svc/...`（确认 chat.go 改动编译通过）。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/bg_running.go internal/service/chat_svc/chat.go internal/service/chat_svc/chat_internal_test.go
git commit internal/service/chat_svc/bg_running.go internal/service/chat_svc/chat.go internal/service/chat_svc/chat_internal_test.go -m "✨ chat_svc: 主轮 finalize 登记后台 subagent, abort/error 清空"
```

---

### Task 5: 后端维护点 — 自主轮加入 + FlipSubagentStatus 移除

**Files:**
- Modify: `internal/service/chat_svc/autonomous_turn.go`（finalize 段约 139-168；FlipSubagentStatus 段约 177-184）
- Test: `internal/service/chat_svc/autonomous_turn_test.go`

**Interfaces:**
- Consumes: `reconcileBgRunningOnFinalize`, `removeBgRunning`, `emitBgRunningStatus`

- [ ] **Step 1: 写失败测试（FlipSubagentStatus 后移除）**

```go
func TestDriveAutonomousTurn_RemovesBgRunningOnComplete(t *testing.T) {
	rec := &recordingEmitter{}
	s := &chatSvc{emitter: rec}
	s.addBgRunning(11, "bg-done")
	if !s.bgRunningActive(11) {
		t.Fatal("precondition: active")
	}
	sess := &chat_entity.Session{}
	sess.ID = 11
	sess.AgentStatus = "idle"
	// 直接验证移除接线（抽成方法便于窄测）：
	s.reconcileBgRunningOnComplete(context.Background(), sess, "bg-done", "stream-11")
	if s.bgRunningActive(11) {
		t.Fatal("want inactive after complete removes bg-done")
	}
	if len(rec.events) != 1 || rec.events[0].SessionStatus.BgRunning {
		t.Fatalf("want 1 session_status event with BgRunning=false, got %+v", rec.events)
	}
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestDriveAutonomousTurn_RemovesBgRunningOnComplete ./internal/service/chat_svc/...`
Expected: 编译失败（`reconcileBgRunningOnComplete` undefined）。

- [ ] **Step 3: 实现接线方法（追加到 bg_running.go）**

```go
// reconcileBgRunningOnComplete 后台 subagent 完成时从集合移除，有变化则 emit。
func (s *chatSvc) reconcileBgRunningOnComplete(ctx context.Context, sess *chat_entity.Session, toolUseID, stream string) {
	if sess == nil {
		return
	}
	if s.removeBgRunning(sess.ID, toolUseID) {
		s.emitBgRunningStatus(ctx, sess, stream)
	}
}
```

- [ ] **Step 4: 接进 autonomous_turn.go**

(a) 自主轮 finalize：在 `_ = chat_repo.Message().Update(finalCtx, assistantMsg)`（约 163）与 `sess.AgentStatus = "idle"`（约 165）之后，加：

```go
	s.reconcileBgRunningOnFinalize(finalCtx, sess, finalBlocks, stream)
```

(b) FlipSubagentStatus 成功后（约 177-184 的 `if completedRef != nil { ... }` 块内，`FlipSubagentStatus` 调用**成功**分支后）加：

```go
		s.reconcileBgRunningOnComplete(finalCtx, sess, completedRef.ToolUseID, stream)
```

> 注意 completedRef.Status 可能是 canceled/failed；不论何种终态都应从集合移除（`reconcileBgRunningOnComplete` 无条件移除该 id）。

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestDriveAutonomousTurn_RemovesBgRunningOnComplete ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/bg_running.go internal/service/chat_svc/autonomous_turn.go internal/service/chat_svc/autonomous_turn_test.go
git commit internal/service/chat_svc/bg_running.go internal/service/chat_svc/autonomous_turn.go internal/service/chat_svc/autonomous_turn_test.go -m "✨ chat_svc: 自主轮登记 + 后台 subagent 完成移除 bgRunning"
```

---

### Task 6: 后端安全网 — 活动 watcher 退出时清空

**Files:**
- Modify: `internal/service/chat_svc/subagent_activity.go:36-42`（watcher goroutine 的 defer）
- Test: `internal/service/chat_svc/subagent_activity_test.go`

**Interfaces:**
- Consumes: `clearBgRunning`

- [ ] **Step 1: 写失败测试**

```go
func TestSubagentActivityWatcher_ClearsBgRunningOnExit(t *testing.T) {
	s := &chatSvc{}
	s.addBgRunning(3, "bg-x")
	// 直接验证安全网函数（抽成方法）：
	s.clearBgRunningOnSourceClosed(3)
	if s.bgRunningActive(3) {
		t.Fatal("want inactive after source closed")
	}
}
```

- [ ] **Step 2: 跑测试看失败**

Run: `go test -race -run TestSubagentActivityWatcher_ClearsBgRunningOnExit ./internal/service/chat_svc/...`
Expected: 编译失败。

- [ ] **Step 3: 实现（bg_running.go 追加）**

```go
// clearBgRunningOnSourceClosed 后台活动 channel 关闭(子进程 evict/CloseSession)时清空会话
// 集合——CLI 子进程死了它派的后台 subagent 也都死了，防止 bgRunning 永久泄漏。
func (s *chatSvc) clearBgRunningOnSourceClosed(sessionID int64) {
	s.clearBgRunning(sessionID)
}
```

- [ ] **Step 4: 接进 watcher defer**

`subagent_activity.go` 的 watcher goroutine（约 37）：

```go
	go func() {
		defer s.subagentActivityWatchers.Delete(sessionID)
		defer s.clearBgRunningOnSourceClosed(sessionID)
		for act := range src.SubagentActivity(sessionID) {
			s.driveSubagentActivity(context.Background(), sessionID, &beCopy, act)
		}
	}()
```

- [ ] **Step 5: 跑测试看通过**

Run: `go test -race -run TestSubagentActivityWatcher_ClearsBgRunningOnExit ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add internal/service/chat_svc/bg_running.go internal/service/chat_svc/subagent_activity.go internal/service/chat_svc/subagent_activity_test.go
git commit internal/service/chat_svc/bg_running.go internal/service/chat_svc/subagent_activity.go internal/service/chat_svc/subagent_activity_test.go -m "✨ chat_svc: 后台活动 watcher 退出清空 bgRunning(防泄漏安全网)"
```

---

### Task 7: 前端类型 — AttentionReason + bgRunning 字段

**Files:**
- Modify: `frontend/src/stores/types.ts`

**Interfaces:**
- Produces: `AttentionReason` 增 `"bg_running"`；`SessionStatusPatch` / `SessionView` / `AttentionInput` / `ChatSessionStatusEvent` 增 `bgRunning: boolean`

- [ ] **Step 1: 改类型（无独立测试，随消费方测试覆盖）**

`types.ts` 依次改：

`SessionStatusPatch`（约 34）加 `bgRunning: boolean;`
`SessionView`（约 46）加 `bgRunning: boolean;`
`AttentionInput`（约 58）加 `bgRunning: boolean;`
`AttentionReason`（约 62）改为：

```ts
export type AttentionReason =
  | "needs_attention"
  | "running"
  | "error"
  | "bg_running"
  | "unread";
```

`ChatSessionStatusEvent`（约 71）加 `bgRunning?: boolean;`（后端边界字段，可选防旧帧）。

- [ ] **Step 2: 类型编译自检**

Run: `cd frontend && pnpm exec tsc --noEmit 2>&1 | head -30`
Expected: 出现下游未适配处的报错（预期，后续 task 修）；本 task 只确保 types.ts 自身语法正确（无 types.ts 内报错）。

- [ ] **Step 3: Commit**

```bash
git add frontend/src/stores/types.ts
git commit frontend/src/stores/types.ts -m "✨ stores/types: AttentionReason 加 bg_running + bgRunning 字段"
```

---

### Task 8: 前端 session-status-store 携带 bgRunning

**Files:**
- Modify: `frontend/src/stores/session-status-store.ts`
- Test: `frontend/src/stores/__tests__/session-status-store.test.ts`（若无则新建）

**Interfaces:**
- Consumes: `SessionStatusPatch.bgRunning`
- Produces: `SessionStatusValue.bgRunning`

- [ ] **Step 1: 写失败测试**

```ts
import { describe, it, expect, beforeEach } from "vitest";
import { useSessionStatusStore } from "../session-status-store";

describe("session-status-store bgRunning", () => {
  beforeEach(() => useSessionStatusStore.getState().__reset());

  it("carries bgRunning through upsert", () => {
    useSessionStatusStore.getState().upsert(1, {
      agentStatus: "idle",
      needsAttention: false,
      bgRunning: true,
    });
    expect(useSessionStatusStore.getState().statuses.get(1)?.bgRunning).toBe(true);
  });

  it("isSamePatch short-circuits only when bgRunning also equal", () => {
    const s = useSessionStatusStore.getState();
    s.upsert(2, { agentStatus: "idle", needsAttention: false, bgRunning: false });
    const ref1 = s.statuses.get(2);
    s.upsert(2, { agentStatus: "idle", needsAttention: false, bgRunning: true });
    const ref2 = useSessionStatusStore.getState().statuses.get(2);
    expect(ref2).not.toBe(ref1);
    expect(ref2?.bgRunning).toBe(true);
  });
});
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/session-status-store.test.ts`
Expected: FAIL（`bgRunning` 为 undefined / 类型错误）。

- [ ] **Step 3: 实现**

`session-status-store.ts`：
- `SessionStatusValue`（约 45）加 `bgRunning: boolean;`
- `isSamePatch`（约 76）加比较项：`&& a.bgRunning === b.bgRunning`
- `upsert`（约 92）与 `bulkUpsert`（约 109）的 `statuses.set(...)` 对象加 `bgRunning: patch.bgRunning,`
- `bumpDone`（约 132）的 `next` 对象加 `bgRunning: prev?.bgRunning ?? false,`

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/session-status-store.test.ts`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/stores/session-status-store.ts frontend/src/stores/__tests__/session-status-store.test.ts
git commit frontend/src/stores/session-status-store.ts frontend/src/stores/__tests__/session-status-store.test.ts -m "✨ session-status-store: 携带 bgRunning 字段"
```

---

### Task 9: 前端 computeAttention 新优先级 + hooks 接线

**Files:**
- Modify: `frontend/src/stores/attention-store.ts`
- Test: `frontend/src/stores/__tests__/attention-store.test.ts`

**Interfaces:**
- Consumes: `AttentionInput.bgRunning`, `SessionStatusValue.bgRunning`, `SessionView.bgRunning`
- Produces: `computeAttention` 可返回 `"bg_running"`

- [ ] **Step 1: 写失败测试**

在 `attention-store.test.ts` 追加（沿用文件既有 helper 构造 input；bgRunning 缺省补 false 到既有用例）：

```ts
it("idle + 已读 + bgRunning → bg_running（独立于已读）", () => {
  expect(
    computeAttention({ agentStatus: "idle", needsAttention: false, lastMessageAt: 100, lastReadAt: 100, bgRunning: true }),
  ).toBe("bg_running");
});
it("idle + unread + bgRunning → bg_running（压过 unread）", () => {
  expect(
    computeAttention({ agentStatus: "idle", needsAttention: false, lastMessageAt: 200, lastReadAt: 100, bgRunning: true }),
  ).toBe("bg_running");
});
it("running + bgRunning → running（running 优先）", () => {
  expect(
    computeAttention({ agentStatus: "running", needsAttention: false, lastMessageAt: 100, lastReadAt: 100, bgRunning: true }),
  ).toBe("running");
});
it("error + unread + bgRunning → error（error 压过 bg_running）", () => {
  expect(
    computeAttention({ agentStatus: "error", needsAttention: false, lastMessageAt: 200, lastReadAt: 100, bgRunning: true }),
  ).toBe("error");
});
```

> 文件内既有 `computeAttention(...)` 调用未带 `bgRunning` 的用例需补 `bgRunning: false`，否则 TS 报缺字段。

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/attention-store.test.ts`
Expected: FAIL（bg_running 分支未实现 / 类型缺字段）。

- [ ] **Step 3: 实现**

`attention-store.ts` `computeAttention`（约 31）改为：

```ts
export function computeAttention(
  input: AttentionInput,
): AttentionReason | null {
  if (input.needsAttention) return "needs_attention";
  if (input.agentStatus === "running") return "running";
  const unread = input.lastMessageAt > input.lastReadAt;
  if (input.agentStatus === "error" && unread) return "error";
  // bg_running 独立于已读/未读：idle 会话只要有后台 subagent 在跑就冒。压过 unread。
  if (input.bgRunning) return "bg_running";
  if (input.agentStatus === "idle" && unread) return "unread";
  return null;
}
```

`useSessionAttention`（约 49 的 `computeAttention({...})`）加 `bgRunning: view.bgRunning,`
`useSessionAttentionList`（约 79 的 `computeAttention({...})`）加 `bgRunning: status?.bgRunning ?? false,`

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/attention-store.test.ts`
Expected: PASS。

- [ ] **Step 5: 补 useSessionWithOverlays 带 bgRunning**

`useSessionAttention` 读的是 `useSessionWithOverlays(sessionId)` 返回的 `SessionView`。改 `frontend/src/hooks/use-session-with-overlays.ts`：组 `SessionView` 时加 `bgRunning: status?.bgRunning ?? false,`（`status` 来自 session-status-store）。

Run: `cd frontend && pnpm exec tsc --noEmit 2>&1 | grep -i "session-with-overlays\|attention-store" | head`
Expected: 无报错。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/stores/attention-store.ts frontend/src/stores/__tests__/attention-store.test.ts frontend/src/hooks/use-session-with-overlays.ts
git commit frontend/src/stores/attention-store.ts frontend/src/stores/__tests__/attention-store.test.ts frontend/src/hooks/use-session-with-overlays.ts -m "✨ attention-store: bg_running reason(压过 unread, 让位 error/running)"
```

---

### Task 10: 前端 attention-display 映射 + i18n

**Files:**
- Modify: `frontend/src/lib/attention-display.ts`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json`、`frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/lib/__tests__/attention-display.test.ts`（若无则新建）

**Interfaces:**
- Consumes: `AttentionReason "bg_running"`
- Produces: `reasonToDisplayStatus("bg_running") === "running"`；`reasonToPillText("bg_running")` = i18n `attention.background`

- [ ] **Step 1: 加 i18n key**

`zh-CN/common.json` 的 `attention` 对象加：`"background": "后台"`
`en/common.json` 的 `attention` 对象加：`"background": "Background"`

> 先 `grep -n '"attention"' frontend/src/i18n/locales/zh-CN/common.json` 定位对象；`needsAttention`/`unread`/`error` 同级。

- [ ] **Step 2: 写失败测试**

```ts
import { describe, it, expect } from "vitest";
import { reasonToDisplayStatus, reasonToPillText } from "../attention-display";

describe("attention-display bg_running", () => {
  it("maps bg_running to running display color", () => {
    expect(reasonToDisplayStatus("bg_running", "idle")).toBe("running");
  });
  it("pill text for bg_running is the background label", () => {
    expect(reasonToPillText("bg_running")).toBeTruthy();
  });
});
```

- [ ] **Step 3: 跑测试看失败**

Run: `cd frontend && pnpm test -- src/lib/__tests__/attention-display.test.ts`
Expected: FAIL（bg_running 走 fallback 返回 "idle"；pill 返回 null）。

- [ ] **Step 4: 实现**

`attention-display.ts`：

```ts
export function reasonToDisplayStatus(
  reason: AttentionReason | null,
  fallback: AgentStatus,
): AgentStatus {
  if (reason === "needs_attention" || reason === "unread") return "waiting";
  if (reason === "running" || reason === "bg_running") return "running";
  if (reason === "error") return "error";
  return fallback;
}

export function reasonToPillText(
  reason: AttentionReason | null,
): string | null {
  if (reason === "needs_attention") return i18n.t("attention.needsAttention");
  if (reason === "error") return i18n.t("attention.error");
  if (reason === "unread") return i18n.t("attention.unread");
  if (reason === "bg_running") return i18n.t("attention.background");
  return null;
}
```

- [ ] **Step 5: 跑测试看通过 + i18n 覆盖测试**

Run: `cd frontend && pnpm test -- src/lib/__tests__/attention-display.test.ts src/__tests__/i18n.test.ts`
Expected: PASS（i18n.test.ts 确认 `attention.background` 双语齐全）。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/lib/attention-display.ts frontend/src/lib/__tests__/attention-display.test.ts frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json
git commit frontend/src/lib/attention-display.ts frontend/src/lib/__tests__/attention-display.test.ts frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json -m "✨ attention-display: bg_running→running 色 + 「后台」pill(i18n)"
```

---

### Task 11: 前端 label 特判 — bg_running 出「后台」pill

**Files:**
- Modify: `frontend/src/components/agentre/chat-page.tsx`（`agentSessionFromMeta` 约 60-71 的 trailingLabel）
- Modify: `frontend/src/components/agentre/project-page.tsx`（约 122-131 同款 trailingLabel）
- Modify: `frontend/src/components/agentre/command-palette/sources/chat-sessions-source.tsx`（约 143 `status=` 处，若也展示 pill 文案则同步）
- Test: 相应组件测试或 `chat-page` 既有测试

**Interfaces:**
- Consumes: `reasonToDisplayStatus`, `reasonToPillText`, `AttentionReason "bg_running"`

- [ ] **Step 1: 写失败测试（chat-page trailingLabel）**

先 `grep -rn "agentSessionFromMeta\|trailingLabel" frontend/src/components/agentre/__tests__ frontend/src/components/agentre/*.test.tsx` 找现有测试位置；若 `agentSessionFromMeta` 非导出，则对渲染出 sidebar 行的组件写断言：bg_running 会话行显示「后台」文案而非 "running"。最小断言（若函数可导出则直接单测）：

```tsx
// 断言：reason=bg_running 时 trailingLabel === reasonToPillText("bg_running")
// 若 agentSessionFromMeta 未导出，改为 export 供测试（本 task 允许的最小改动）。
```

- [ ] **Step 2: 跑测试看失败**

Run: `cd frontend && pnpm test -- <该测试文件>`
Expected: FAIL（当前 status==="running" 硬编码返回 "running"）。

- [ ] **Step 3: 实现 trailingLabel 特判**

`chat-page.tsx` `agentSessionFromMeta`（约 64）改：

```tsx
  const trailingLabel =
    reason === "bg_running"
      ? (reasonToPillText(reason) ?? "")
      : status === "running"
        ? "running"
        : status === "waiting"
          ? (reasonToPillText(reason) ?? "")
          : status === "error"
            ? "error"
            : relativeTime(lastMessageAt);
```

`project-page.tsx` 对应 trailingLabel 逻辑（约 122-131）做同款特判：`reason === "bg_running"` 优先 `reasonToPillText(reason)`。

- [ ] **Step 4: 跑测试看通过**

Run: `cd frontend && pnpm test -- <该测试文件>`
Expected: PASS。

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/agentre/chat-page.tsx frontend/src/components/agentre/project-page.tsx frontend/src/components/agentre/command-palette/sources/chat-sessions-source.tsx <测试文件>
git commit frontend/src/components/agentre/chat-page.tsx frontend/src/components/agentre/project-page.tsx frontend/src/components/agentre/command-palette/sources/chat-sessions-source.tsx <测试文件> -m "✨ sidebar/命令面板: bg_running 会话行显示「后台」pill"
```

---

### Task 12: 前端事件 / DTO 摄入 bgRunning

**Files:**
- Modify: `frontend/src/components/agentre/chat-streams-host.tsx:255-265`（session_status upsert）
- Modify: `frontend/src/stores/chat-agents-store.ts:135-141`（bulkUpsert entries）
- Modify: `frontend/src/hooks/use-chat-session.ts:88-93`（LoadSession upsert）
- Test: `frontend/src/components/agentre/__tests__/chat-streams-host.test.tsx`（若无则挑现有 host 测试）

**Interfaces:**
- Consumes: `ChatSessionStatusEvent.bgRunning`, `ChatSessionLite.bgRunning`(经 wails models), `ChatSessionDetail.bgRunning`

- [ ] **Step 1: 先 `make generate` 刷新 wails 绑定**

Run: `cd /Users/codfrm/Code/agentre/agentre && make generate 2>&1 | tail -5`
Expected: `frontend/wailsjs/go/models.ts` 里 `ChatSessionLite` / `ChatSessionDetail` / `ChatSessionStatusPatch` 出现 `bgRunning` 字段（后端 Task 2/3 已加 json tag）。

- [ ] **Step 2: 写失败测试**

对 chat-streams-host 的 session_status 分支断言：收到 `sessionStatus.bgRunning=true` 后 store 里该 sid 的 `bgRunning===true`。沿用该文件既有 host 测试骨架（`grep -rn "session_status" frontend/src/components/agentre/__tests__`）。

- [ ] **Step 3: 跑测试看失败**

Run: `cd frontend && pnpm test -- <host 测试文件>`
Expected: FAIL（upsert 未带 bgRunning）。

- [ ] **Step 4: 实现三处摄入**

(a) `chat-streams-host.tsx` upsert（约 255）加：

```tsx
            bgRunning: hasStatus
              ? (ev.sessionStatus.bgRunning ?? false)
              : (prev?.bgRunning ?? false),
```

(b) `chat-agents-store.ts` bulkUpsert entries（约 135-141）加：

```ts
              entries.push([
                s.id,
                {
                  agentStatus: snapshotStatus,
                  needsAttention: s.needsAttention ?? false,
                  bgRunning: s.bgRunning ?? false,
                },
              ]);
```

(c) `use-chat-session.ts` LoadSession upsert（约 89）加：

```ts
        bgRunning: resp.session.bgRunning ?? false,
```

- [ ] **Step 5: 跑测试看通过**

Run: `cd frontend && pnpm test -- <host 测试文件>`
Expected: PASS。

- [ ] **Step 6: Commit**

```bash
git add frontend/src/components/agentre/chat-streams-host.tsx frontend/src/stores/chat-agents-store.ts frontend/src/hooks/use-chat-session.ts frontend/wailsjs <host 测试文件>
git commit frontend/src/components/agentre/chat-streams-host.tsx frontend/src/stores/chat-agents-store.ts frontend/src/hooks/use-chat-session.ts <host 测试文件> -m "✨ 前端: session_status 事件 / ListChatAgents / LoadSession 摄入 bgRunning"
```

> wailsjs 是生成物（gitignore），不 add 也可；若仓库有跟踪则一并提交。

---

### Task 13: 全量门禁

**Files:** 无新增，跑收尾门禁。

- [ ] **Step 1: 后端全量**

Run: `cd /Users/codfrm/Code/agentre/agentre && make test-backend 2>&1 | tail -20`
Expected: 全绿（看真实 exit code，非 `| tail` 吞掉的）。用 `echo "EXIT=${PIPESTATUS[0]}"` 或不接管道确认。

- [ ] **Step 2: lint**

Run: `make lint 2>&1 | tail -30`
Expected: golangci + ESLint 全绿（含 i18next/no-literal-string）。

- [ ] **Step 3: 前端全量 vitest + tsc**

Run: `cd frontend && pnpm test 2>&1 | tail -20 && pnpm exec tsc --noEmit`
Expected: 全绿。特别确认 `i18n.test.ts` 与 attention/session-status 相关用例通过。

- [ ] **Step 4: 真实 CLI 手验（可选但建议）**

`make dev` 起应用，发消息让 agent 派后台 subagent（`Task 工具 run_in_background=true, sleep 15`）。观察：主轮结束后**侧栏该会话行显示绿点 + 「后台」pill**（不是"已读"）；~15s 后台完成后指示消失、变正常已读/未读。

- [ ] **Step 5: 无独立 commit（各 task 已提交）**

---

## Self-Review 记录

- **Spec 覆盖**：真源=内存 map(Task 1) / session_status 携带(Task 2) / DTO 携带(Task 3) / 4 维护点(Task 4 主轮+abort、Task 5 自主轮+完成、Task 6 evict 安全网) / 优先级(Task 9) / 视觉复用 running+「后台」pill(Task 10-11) / 摄入(Task 12) / 测试面(各 task + Task 13)。覆盖齐。
- **类型一致**：`bgRunningActive`/`addBgRunning`/`removeBgRunning`/`clearBgRunning`/`reconcileBgRunningOnFinalize`/`reconcileBgRunningOnComplete`/`clearBgRunningOnSourceClosed`/`runningBgSubagentIDs`/`emitBgRunningStatus` 全计划一致；前端 `bgRunning` 字段名贯穿 store/patch/view/event。
- **已知边界**：remote claudecode(agentred)不携带 CompletedTask / 无 SubagentActivitySource → 远端后台 subagent bgRunning 走不通，保持现状（spec 已记，本计划不扩 remote）。`toChatSessionLite` 若为包级函数需提升为方法（Task 3 已注明）。
