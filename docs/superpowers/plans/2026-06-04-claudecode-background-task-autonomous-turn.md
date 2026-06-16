# Claude Code 后台任务「自主续轮」捕获 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 claude CLI「后台任务完成后自主跑的续轮」被 agentre 捕获成一条自动追加的 assistant 轮,消除帧错位(续不上对话)。

**Architecture:** `pkg/claudecode.Session` 改成单 `readLoop` demux reader,空闲时遇到「后台型 task_notification」开头的一轮就路由到新的 `AutonomousTurns()` channel;`agentruntime` 加 `AutonomousTurnSource` 前向接口 + `CapAutonomousTurn`;claudecode runtime 桥接;chat_svc 每会话起 watcher 落「纯 assistant 轮」;remote 加 wire 帧。

**Tech Stack:** Go 1.26、stream-json、cago、go.uber.org/mock、goconvey/testify、JSON-RPC-over-WebSocket(agentred)。

**worktree 注意:** 本分支在 `.claude/worktrees/claudecode-autonomous-turn`(off main)。所有 `go` 命令前缀 `GOWORK=off`;git 写操作需关 sandbox。

设计依据:`docs/superpowers/specs/2026-06-04-claudecode-background-task-autonomous-turn-design.md`。

---

## File Structure

| 文件 | 职责 | 动作 |
|---|---|---|
| `pkg/claudecode/session.go` | 常驻 demux reader + 活跃 sink 路由 + AutonomousTurns | Modify(核心重写) |
| `pkg/claudecode/autoturn.go` | `AutoTurn` 类型 + 后台型 task_notification predicate | Create |
| `pkg/claudecode/session_test.go` | 基石回归测试 + 既有多轮测试 | Modify |
| `pkg/claudecode/autoturn_test.go` | predicate 单测(后台型 vs subagent 型) | Create |
| `internal/pkg/agentruntime/runner.go` | `AutonomousTurnSource` 接口 + `AutonomousTurn` 结构 | Modify |
| `internal/pkg/agentruntime/capability/capability.go` | `CapAutonomousTurn` 常量 | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/runtime.go` | 实现 `AutonomousTurnSource` + 声明能力 | Modify |
| `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go` | Session.AutonomousTurns → agentruntime.Event 桥接 | Create |
| `internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go` | 矩阵 + 桥接测试 | Modify |
| `internal/service/chat_svc/autonomous_turn.go` | watcher + driveAutonomousTurn(纯 assistant 落库) | Create |
| `internal/service/chat_svc/autonomous_turn_test.go` | watcher 单测(mock runtime) | Create |
| `internal/service/chat_svc/chat.go` | watcher 启停挂载点 | Modify |
| `internal/pkg/agentruntime/runtimes/remote/wire/wire.go` | 新 wire 帧编解码 | Modify |
| `internal/pkg/agentruntime/runtimes/remote/wire/wire_test.go` | round-trip | Modify |
| `internal/pkg/agentruntime/runtimes/remote/runtime.go` | client 侧 `AutonomousTurnSource` | Modify |
| `internal/daemon/handlers/runtime.go` | daemon 转发 AutonomousTurns | Modify |

> 接口名锚点(后续 task 一律用这些,勿改名):`Session.AutonomousTurns() <-chan *AutoTurn`、`AutoTurn{Events <-chan Event; SessionID string; Trigger string}`、`isBackgroundTaskNotification(rawFrame) bool`、`agentruntime.AutonomousTurnSource.AutonomousTurns(sessionID int64) <-chan AutonomousTurn`、`agentruntime.AutonomousTurn{Events <-chan Event; Result *RunResult; Trigger string}`、`CapAutonomousTurn = "autonomous_turn"`、`chatSvc.driveAutonomousTurn(...)`、`chatSvc.startAutonomousWatcher(...)`。

---

## Phase 0 — 基石回归测试(Red)

### Task 0.1: 后台任务自主续轮的失败回归测试

**Files:**
- Modify: `pkg/claudecode/session_test.go`

- [ ] **Step 1: 写 fake CLI + 失败测试**

在 `session_test.go` 末尾追加。`fakeBackgroundTask` 模拟真实 CLI 抓到的帧序(见 spec「根因」):第一条 user frame → turn1(启动后台任务)+ result#1,**紧接着自主吐** task_notification(后台型)+ 续轮 + result#2,然后回到读 stdin 等 turn2。

```go
// fakeBackgroundTask 复刻真实 CLI 2.1.162 抓到的「后台任务 + 自主续轮」帧序。
// turn1:启动 run_in_background → result#1;随后不等 stdin 自主吐
// task_notification(后台型) + 续轮(init+text+result#2);turn2:正常回声。
func fakeBackgroundTask(stdin io.Reader, stdout io.Writer) {
	const sid = "sess-bgtask"
	sc := bufio.NewScanner(stdin)
	sc.Buffer(make([]byte, 0, 64<<10), maxFrameBytes)
	turn := 0
	for sc.Scan() {
		turn++
		reply := extractTextField(sc.Text())
		if turn == 1 {
			// turn1:启动后台任务,以 result#1 收尾(模型主动结束本轮)。
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"a1","content":[{"type":"tool_use","id":"tu1","name":"Bash","input":{"command":"sleep 1","run_in_background":true}}]}}`)
			writeFrame(stdout, `{"type":"user","message":{"content":[{"type":"tool_result","tool_use_id":"tu1","content":"Command running in background with ID: bg1"}]}}`)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"a2","content":[{"type":"text","text":"started:%s"}]}}`, reply)
			writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
			// —— 不等下一条 stdin,自主吐后台完成续轮 ——
			writeFrame(stdout, `{"type":"system","subtype":"task_notification","task_id":"bg1","tool_use_id":"tu1","status":"completed","output_file":"/tmp/tasks/bg1.output","summary":"Background command completed"}`)
			writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
			writeFrame(stdout, `{"type":"assistant","message":{"id":"a3","content":[{"type":"text","text":"autonomous:listing"}]}}`)
			writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":2,"output_tokens":2}}`, sid)
			continue
		}
		// turn2:普通回声。
		writeFrame(stdout, `{"type":"system","subtype":"init","session_id":%q,"cwd":"/tmp","model":"m","tools":[]}`, sid)
		writeFrame(stdout, `{"type":"assistant","message":{"id":"a4","content":[{"type":"text","text":"echo:%s"}]}}`, reply)
		writeFrame(stdout, `{"type":"result","subtype":"success","session_id":%q,"usage":{"input_tokens":1,"output_tokens":1}}`, sid)
	}
}

// TestSession_BackgroundTaskAutonomousTurn 是本案基石回归:
//   (a) Turn1 channel 只收到 turn1 文本("started:..."),在 result#1 后 close,
//       不串入自主续轮的 "autonomous:listing";
//   (b) Session.AutonomousTurns() 吐出自主续轮,其文本 = "autonomous:listing";
//   (c) Turn2 只收到 "echo:beta",无错位。
func TestSession_BackgroundTaskAutonomousTurn(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := New(WithBinary("fake"), pipeSpawner(t, fakeBackgroundTask))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	// (a) Turn1 干净收尾。
	ch1, err := sess.Turn(ctx, "alpha")
	require.NoError(t, err)
	got1 := drainText(t, ch1)
	assert.Equal(t, "started:alpha", got1)
	assert.NotContains(t, got1, "autonomous", "Turn1 不应吞掉自主续轮帧")

	// (b) 自主续轮经 AutonomousTurns 吐出。
	var at *AutoTurn
	select {
	case at = <-sess.AutonomousTurns():
	case <-time.After(2 * time.Second):
		t.Fatal("expected an autonomous turn within 2s")
	}
	require.NotNil(t, at)
	assert.Equal(t, "background_task", at.Trigger)
	assert.Equal(t, "autonomous:listing", drainText(t, at.Events))

	// (c) Turn2 无错位。
	ch2, err := sess.Turn(ctx, "beta")
	require.NoError(t, err)
	assert.Equal(t, "echo:beta", drainText(t, ch2))
}
```

- [ ] **Step 2: 跑测试看它 fail(编译失败即 Red)**

Run: `GOWORK=off go test ./pkg/claudecode/ -run TestSession_BackgroundTaskAutonomousTurn -v`
Expected: 编译失败 —— `sess.AutonomousTurns undefined` / `AutoTurn undefined`。这是预期的 Red。

- [ ] **Step 3: 暂不实现,进入 Phase 1。** 先不 commit(留着这条红测一路驱动 Phase 1)。

---

## Phase 1 — Session 常驻 demux reader(Green)

> 总目标:把「每 Turn 一个 `routeUntilResult` 占 scanner」改成「一个 `readLoop` goroutine 占 scanner 整个生命周期 + 单一活跃 sink 槽位」,新增 `AutonomousTurns()`。每步跑**全量** `pkg/claudecode` 保证既有并发测试(MultiTurn/Interrupt/SetPermissionMode/Retry/…)不回归。

### Task 1.1: AutoTurn 类型 + 后台型 task_notification predicate

**Files:**
- Create: `pkg/claudecode/autoturn.go`
- Create: `pkg/claudecode/autoturn_test.go`

- [ ] **Step 1: 写 predicate 测试**

```go
package claudecode

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsBackgroundTaskNotification(t *testing.T) {
	bg := `{"type":"system","subtype":"task_notification","task_id":"bg1","tool_use_id":"tu1","status":"completed","output_file":"/tmp/tasks/bg1.output","summary":"Background command completed"}`
	sub := `{"type":"system","subtype":"task_notification","task_id":"t9","subagent_type":"general","status":"completed","summary":"Subagent finished"}`
	notNotif := `{"type":"system","subtype":"init","session_id":"s"}`

	parse := func(s string) rawFrame {
		var f rawFrame
		_ = json.Unmarshal([]byte(s), &f)
		return f
	}
	assert.True(t, isBackgroundTaskNotification(parse(bg)), "后台型应为 true")
	assert.False(t, isBackgroundTaskNotification(parse(sub)), "subagent 型应为 false")
	assert.False(t, isBackgroundTaskNotification(parse(notNotif)), "非 notification 应为 false")
}
```

- [ ] **Step 2: 跑测试看它 fail**

Run: `GOWORK=off go test ./pkg/claudecode/ -run TestIsBackgroundTaskNotification -v`
Expected: 编译失败 —— `isBackgroundTaskNotification undefined` / `AutoTurn undefined` / `rawFrame.OutputFile undefined`。

- [ ] **Step 3: 实现 autoturn.go**

> 先确认 `rawFrame`(在 event.go)是否已有 `OutputFile` / `SubagentType` 字段;`SubagentType` 已有(parseSystemTask 用 `f.SubagentType`),`OutputFile` 需要在 event.go 的 `rawFrame` 加 `OutputFile string \`json:"output_file"\``。在 Step 3 一并加。

```go
package claudecode

// AutoTurn 是 CLI 在没有新 user 输入的情况下自主跑的一轮(后台任务完成续轮)。
// Events 与普通 Turn 同形:result 帧到达后 close。
type AutoTurn struct {
	Events    <-chan Event
	SessionID string
	Trigger   string // 当前固定 "background_task"
}

// isBackgroundTaskNotification 判定一帧是否为「后台命令完成」通知 —— 它是自主续轮
// 的起始标记,与 subagent(Task 工具)的 task_notification 区分:
//   - 后台型:有 output_file(落在 tasks/<id>.output),无 subagent_type。
//   - subagent 型:有 subagent_type / description,无 output_file。
func isBackgroundTaskNotification(f rawFrame) bool {
	return f.Type == "system" &&
		f.Subtype == "task_notification" &&
		f.OutputFile != "" &&
		f.SubagentType == ""
}
```

在 `event.go` 的 `rawFrame` 结构体补字段(若缺):

```go
OutputFile string `json:"output_file"`
```

- [ ] **Step 4: 跑测试看它 pass**

Run: `GOWORK=off go test ./pkg/claudecode/ -run TestIsBackgroundTaskNotification -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/claudecode/autoturn.go pkg/claudecode/autoturn_test.go pkg/claudecode/event.go
git commit -m "✨ claudecode: AutoTurn 类型 + 后台型 task_notification predicate"
```

### Task 1.2: 引入 readLoop + 活跃 sink 路由(保持既有行为)

**Files:**
- Modify: `pkg/claudecode/session.go`

目标:把 `Turn` 从「自己起 goroutine 跑 routeUntilResult 占 scanner」改成「向常驻 readLoop 注册一个 sink」。这一步**先不处理自主轮**(仍把 idle 时到达的帧丢弃或忽略),只保证既有测试全绿 —— 把风险拆小。

- [ ] **Step 1: 在 Session 结构加 reader 状态**

`Session` 结构体增加(替换现有「Turn 内 goroutine 直接读 scanner」模型):

```go
// readLoop 相关:单一活跃 sink 槽位 + 启动同步。
sinkMu     sync.Mutex
activeSink chan<- Event   // 当前消费帧的 channel;nil = 槽位空闲
turnDone   chan struct{}  // 当前 turn 的 sink 关闭信号(routeUntilResult 等价物)
autoCh     chan *AutoTurn // AutonomousTurns() 返回的 channel(buffered)
```

`OpenSession` 返回前启动 readLoop(替换原来「不读、等首个 Turn」的模型):

```go
sess := &Session{proc: p, scanner: sc, sessionID: spec.sessionID, autoCh: make(chan *AutoTurn, 8)}
go sess.readLoop()
return sess, nil
```

- [ ] **Step 2: 写 readLoop + 改 Turn**

`readLoop`(新):单 goroutine 读 scanner,把每帧解析 → 路由到 activeSink;`result` 关闭当前 sink、清空槽位。control_response 永远 dispatch 到 ctrlPending(不论 sink 状态)。

```go
// readLoop 占住 scanner 整个子进程生命周期。把帧 demux 到当前活跃 sink;
// result 帧关闭该 sink。control_response 任何时候都 dispatch 给 ctrlPending。
func (s *Session) readLoop() {
	for s.scanner.Scan() {
		line := s.scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		events, done, ctrl := s.classify(line) // classify = parseLine 重排:返回 (events, isResult, isControlResp)
		if ctrl {
			continue // 已在 classify 内 dispatchControlResponse
		}
		s.deliver(events, done)
	}
	// scanner EOF:子进程死亡。snapshot 真错,关闭活跃 sink + autoCh。
	if exitErr := s.proc.exitErrIfDone(); exitErr != nil {
		s.rememberExitErr(exitErr)
	}
	s.closeActiveSink()
	close(s.autoCh)
}
```

`deliver`:把 events 推给活跃 sink;若 `done`,关闭 sink、清空槽位。**本步**:idle 时(activeSink==nil)的非 control 帧暂时丢弃(下一步 Task 1.3 再接自主轮)。

```go
func (s *Session) deliver(events []Event, done bool) {
	s.sinkMu.Lock()
	sink := s.activeSink
	s.sinkMu.Unlock()
	if sink == nil {
		return // Task 1.3 会在这里识别自主轮;本步先丢弃
	}
	for _, ev := range events {
		sink <- ev
	}
	if done {
		s.closeActiveSink()
	}
}

func (s *Session) closeActiveSink() {
	s.sinkMu.Lock()
	defer s.sinkMu.Unlock()
	if s.activeSink != nil {
		close(s.activeSink)
		s.activeSink = nil
	}
}
```

`Turn` 改为:注册 sink(写 activeSink 槽位)→ 写 user frame → 返回 sink 的只读端。turnMu 仍串行化 user turn;sink 关闭(readLoop 在 result 时 close)即 turn 完成 → 释放 turnMu。需要一个等 sink 关闭再放 turnMu 的小 goroutine:

```go
func (s *Session) Turn(ctx context.Context, prompt string, images ...Image) (<-chan Event, error) {
	s.turnMu.Lock()
	s.lastAssistantUsage = nil
	s.stdinMu.Lock()
	if s.closed {
		s.stdinMu.Unlock(); s.turnMu.Unlock()
		return nil, errors.New("claudecode: session closed")
	}
	enc, err := buildUserFrame(prompt, images)
	if err != nil {
		s.stdinMu.Unlock(); s.turnMu.Unlock()
		return nil, err
	}
	ch := make(chan Event, 16)
	done := make(chan struct{})
	s.sinkMu.Lock()
	s.activeSink = ch
	s.turnDone = done
	s.sinkMu.Unlock()
	if _, err := fmt.Fprintf(s.proc.stdin, "%s\n", enc); err != nil {
		s.sinkMu.Lock(); s.activeSink = nil; s.sinkMu.Unlock()
		s.stdinMu.Unlock(); s.turnMu.Unlock()
		if exitErr := s.proc.exitErrIfDone(); exitErr != nil {
			s.rememberExitErr(exitErr); return nil, exitErr
		}
		return nil, err
	}
	s.stdinMu.Unlock()
	// sink 关闭后释放 turnMu。closeActiveSink 关闭 ch;这里靠 ctx / ch 关闭判定。
	go func() {
		defer s.turnMu.Unlock()
		// 等 ch 被 readLoop 关闭(result)或 ctx 取消。
		s.waitTurnEnd(ctx, ch)
	}()
	return ch, nil
}
```

> 注:`waitTurnEnd` 的实现 + ctx 取消时如何让 readLoop 停止往已弃用 sink 写(避免阻塞),是本步要打磨的并发细节 —— 参照既有 `routeUntilResult` 的 ctx 处理。用 `turnDone`/select 协调。**这些细节在实现时对照既有测试逐个调通**,不在计划里臆测行号。

`classify` = 把现有 `parseLine` 拆成「返回 events/isResult」+「control_response 已自行 dispatch 返回 ctrl=true」。复用 parseLine 主体,仅把 `case "control_response"` 标记 ctrl=true。

- [ ] **Step 3: 跑全量 pkg/claudecode 保证不回归**

Run: `GOWORK=off go test ./pkg/claudecode/ -race`
Expected: 既有测试全绿(MultiTurn / Interrupt / SetPermissionMode / Retry / 健康检查 / ExitErr …)。基石测试仍 fail(AutonomousTurns 还没接)。
若 Interrupt / SetPermissionMode 因 reader 模型改变而挂 → 在本步调通(control 路由现在统一走 readLoop;SetPermissionMode 的自drain 分支删除见 Task 1.4)。

- [ ] **Step 4: Commit**

```bash
git add pkg/claudecode/session.go
git commit -m "♻️ claudecode: Session 改单 readLoop + 活跃 sink 路由(行为不变)"
```

### Task 1.3: idle 帧识别为自主轮 → 路由 AutonomousTurns

**Files:**
- Modify: `pkg/claudecode/session.go`

- [ ] **Step 1: deliver 在 idle 时识别后台型 task_notification 起自主轮**

把 Task 1.2 里 `deliver` 的「sink==nil 丢弃」替换为:idle 时若首帧是后台型 task_notification,新建一个 autonomous sink,推上 autoCh,并把它装进 activeSink(后续帧路由给它直到 result)。需要在 classify 之前先 peek 原始帧判 predicate —— 调整 readLoop 让它把「原始 rawFrame + 解析后的 events」一起传给 deliver:

```go
func (s *Session) deliver(f rawFrame, events []Event, done bool) {
	s.sinkMu.Lock()
	if s.activeSink == nil {
		if isBackgroundTaskNotification(f) {
			// 起一个自主轮:后台型 task_notification 是起始标记,本身不下发为事件。
			ch := make(chan Event, 16)
			s.activeSink = ch
			s.sinkMu.Unlock()
			s.autoCh <- &AutoTurn{Events: ch, SessionID: s.sessionID, Trigger: "background_task"}
			return // 这一帧(标记)消费掉,不进 sink
		}
		s.sinkMu.Unlock()
		return // 其它 idle 帧(理论上不该有)丢弃
	}
	sink := s.activeSink
	s.sinkMu.Unlock()
	for _, ev := range events {
		sink <- ev
	}
	if done {
		s.closeActiveSink()
	}
}
```

> readLoop 改为 `s.deliver(f, events, done)`(把 rawFrame 透传)。

- [ ] **Step 2: 加 AutonomousTurns 访问器**

```go
// AutonomousTurns 返回 CLI 自主续轮的 channel。子进程退出时 close。
func (s *Session) AutonomousTurns() <-chan *AutoTurn { return s.autoCh }
```

- [ ] **Step 3: 跑基石测试 + 全量**

Run: `GOWORK=off go test ./pkg/claudecode/ -run TestSession_BackgroundTaskAutonomousTurn -race -v`
Expected: PASS(三段断言全过)。
Run: `GOWORK=off go test ./pkg/claudecode/ -race`
Expected: 全绿。

- [ ] **Step 4: Commit**

```bash
git add pkg/claudecode/session.go
git commit -m "✨ claudecode: 后台型 task_notification 起自主轮 → AutonomousTurns"
```

### Task 1.4: 清理 SetPermissionMode 自drain 分支

**Files:**
- Modify: `pkg/claudecode/session.go`

- [ ] **Step 1:** 删除 `SetPermissionMode` 里 `turnMu.TryLock` + 自己 `for s.scanner.Scan()` drain 的分支(session.go:511-547 一带)。现在 readLoop 一直在读,control_response 一定经 readLoop dispatch 到 ctrlPending,SetPermissionMode 只需在 ch 上等。

- [ ] **Step 2: 跑 SetPermissionMode 相关测试**

Run: `GOWORK=off go test ./pkg/claudecode/ -run 'TestSession_SetPermissionMode' -race -v`
Expected: PASS(含 MidTurn / InvalidMode / AfterClose)。

- [ ] **Step 3: 全量 + Commit**

```bash
GOWORK=off go test ./pkg/claudecode/ -race
git add pkg/claudecode/session.go
git commit -m "♻️ claudecode: 删 SetPermissionMode 自drain 分支(readLoop 统一收口)"
```

---

## Phase 2 — agentruntime 接口 + 能力 + claudecode 实现

### Task 2.1: AutonomousTurnSource 接口 + AutonomousTurn 结构

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go`

- [ ] **Step 1:** 在 runner.go 末尾(其它子接口附近)加:

```go
// AutonomousTurnSource 由「会自发产生 turn」的 runtime 实现 —— 当前仅 claudecode:
// CLI 在 run_in_background Bash 任务完成后,自主注入 <task-notification> 并跑完整
// 一轮。这是唯一的「前向」子接口(backend→host),区别于 Steerer/Aborter 等反向通道。
//
// chat_svc 每会话订阅一次。channel 在子进程退出 / 会话 evict 时 close。
type AutonomousTurnSource interface {
	AutonomousTurns(sessionID int64) <-chan AutonomousTurn
}

// AutonomousTurn 是一轮自发 turn:事件流 + 异步填充的 RunResult。
type AutonomousTurn struct {
	Events  <-chan Event // 与 Run 的事件流同形;result 后 close
	Result  *RunResult   // Events close 后才可读
	Trigger string       // "background_task"
}
```

- [ ] **Step 2: 重新生成 mock(runner.go 顶部有 go:generate mockgen)**

Run: `cd internal/pkg/agentruntime && GOWORK=off go generate ./...`(或 `make mock`)
Expected: `mock_agentruntime/mock_runner.go` 更新(若 mockgen 对子接口也生成)。编译通过。

- [ ] **Step 3: Commit**

```bash
git add internal/pkg/agentruntime/runner.go internal/pkg/agentruntime/mock_agentruntime/
git commit -m "✨ agentruntime: AutonomousTurnSource 前向接口 + AutonomousTurn"
```

### Task 2.2: CapAutonomousTurn 能力常量 + claudecode 声明 + 矩阵测试

**Files:**
- Modify: `internal/pkg/agentruntime/capability/capability.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime.go`

- [ ] **Step 1: 矩阵测试加断言(Red)**

在 `runtime_test.go` 的 `TestClaudecodeCapabilities`(或同名)里加:`caps.Has(capability.CapAutonomousTurn)` 为 true,且 `runtime` 可 type-assert 成 `agentruntime.AutonomousTurnSource`。

```go
assert.True(t, caps.Has(capability.CapAutonomousTurn))
_, ok := agentruntime.Runtime(defaultRuntime).(agentruntime.AutonomousTurnSource)
assert.True(t, ok, "claudecode 必须实现 AutonomousTurnSource")
```

- [ ] **Step 2: 跑看 fail**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run Capabilities -v`
Expected: FAIL —— 常量未定义 / 能力未声明 / 接口未实现。

- [ ] **Step 3: 加常量 + 声明能力**

`capability.go` 加 `CapAutonomousTurn Capability = "autonomous_turn"`,并(若有 AllCaps 列表)登记。
`runtime.go` 的 `Capabilities()` 在能力 set 里加 `CapAutonomousTurn`。

- [ ] **Step 4: 实现 stub AutonomousTurns(让矩阵测试编译过)** —— 真正逻辑在 Task 2.3。先放最小返回(返回一个永不产值、随 session close 关闭的 channel),保证 type-assert 过。

- [ ] **Step 5: 跑看 pass(矩阵) + Commit**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/claudecode/ -run Capabilities -v` → PASS

```bash
git add internal/pkg/agentruntime/capability/capability.go internal/pkg/agentruntime/runtimes/claudecode/runtime.go internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go
git commit -m "✨ agentruntime: CapAutonomousTurn + claudecode 声明能力"
```

### Task 2.3: claudecode 桥接 Session.AutonomousTurns → agentruntime.Event

**Files:**
- Create: `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime.go`
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/runtime_test.go`

- [ ] **Step 1: 桥接测试(Red)**

用 fake session(实现 `ccSessionHandle` 或在 cache 里放一个能吐 AutonomousTurns 的 fake)驱动:session 吐一个 AutoTurn(含 text 事件 + 隐含 result)→ runtime 的 `AutonomousTurns(sessionID)` 应吐出一个 `agentruntime.AutonomousTurn`,其 `Events` 翻译后含对应 `EventTextDelta`,`Result.ProviderSessionID` 填好。

> 注:当前 `ccSessionHandle` 接口(session.go)没暴露 AutonomousTurns。本步给它加方法 `AutonomousTurns() <-chan *claudecode.AutoTurn`,并在 `ccClientAdapter` 透传 `a.sess.AutonomousTurns()`。fake handle 也实现它。

- [ ] **Step 2: 实现桥接**

抽出 `Run` 里「把一个 claudecode event channel 经 translator 聚合成 agentruntime event channel + 填 RunResult」的逻辑为共享 helper(如 `func (a *active) pump(in <-chan claudecode.Event) (<-chan agentruntime.Event, *agentruntime.RunResult)`),`Run` 与自主轮都用它。`AutonomousTurns(sessionID)`:

```go
func (r *Runtime) AutonomousTurns(sessionID int64) <-chan agentruntime.AutonomousTurn {
	out := make(chan agentruntime.AutonomousTurn, 4)
	go func() {
		defer close(out)
		cur := r.cache.Get(sessionKey(sessionID))
		if cur == nil { return }
		for at := range cur.handle.AutonomousTurns() {
			events, result := translateAutoTurn(at) // 复用 pump/translator
			out <- agentruntime.AutonomousTurn{Events: events, Result: result, Trigger: at.Trigger}
		}
	}()
	return out
}
```

> cache / active 的精确字段名以 runtime.go 现状为准(本步对照实现)。

- [ ] **Step 3: 跑桥接测试 + 全量 runtime 包**

Run: `GOWORK=off go test ./internal/pkg/agentruntime/... -race`
Expected: PASS。

- [ ] **Step 4: Commit**

```bash
git add internal/pkg/agentruntime/runtimes/claudecode/
git commit -m "✨ agentruntime/claudecode: 桥接 Session.AutonomousTurns → agentruntime"
```

---

## Phase 3 — chat_svc 每会话 watcher

### Task 3.1: driveAutonomousTurn —— 纯 assistant 落库(单测先行)

**Files:**
- Create: `internal/service/chat_svc/autonomous_turn.go`
- Create: `internal/service/chat_svc/autonomous_turn_test.go`

- [ ] **Step 1: 写 watcher 单测(Red)**

mock runtime 实现 `AutonomousTurnSource`,吐一个含 text 事件的 AutonomousTurn。断言:
- 落了**一条 assistant chat_message**(role=assistant,**无新 user 消息**);
- emit 了 stream(StreamMessage/StreamDone)给该会话 stream;
- 会话状态最终 `idle`。

用既有 chat_svc 单测脚手架(sqlmock + emitter fake + mockgen runner)。参照 `persistAutoContinueTurn` 的既有测试风格。

- [ ] **Step 2: 跑看 fail**

Run: `GOWORK=off go test ./internal/service/chat_svc/ -run Autonomous -v`
Expected: FAIL(`driveAutonomousTurn` 未定义)。

- [ ] **Step 3: 实现 driveAutonomousTurn**

新建 assistant 消息(seq 续在末尾,无 user 行),drain `at.Events` 复用既有逐事件 handler / acc 累积 / 落 blocks / stream(抽取 runTurn 里 drain+persist 的可复用部分;若耦合过深,先复制最小子集并标注 TODO 收敛),最后从 `at.Result` 落 provider sid / usage,状态 `running→idle`。错误 / 落库失败 → 落 error 消息 + 回 idle(对照 chat.go:2581 失败路径)。

- [ ] **Step 4: 跑看 pass + Commit**

```bash
GOWORK=off go test ./internal/service/chat_svc/ -run Autonomous -race
git add internal/service/chat_svc/autonomous_turn.go internal/service/chat_svc/autonomous_turn_test.go
git commit -m "✨ chat_svc: driveAutonomousTurn 落纯 assistant 轮"
```

### Task 3.2: watcher 启停挂载

**Files:**
- Modify: `internal/service/chat_svc/chat.go`
- Modify: `internal/service/chat_svc/autonomous_turn.go`

- [ ] **Step 1:** 在 claudecode 会话首次 spawn 后(runTurn 首轮成功路径,或 selectRunner 返回 claudecode runtime 时)惰性启动 `startAutonomousWatcher(sess, be, ...)`:type-assert runner 为 `AutonomousTurnSource`,起 goroutine `for at := range src.AutonomousTurns(sess.ID) { s.driveAutonomousTurn(...) }`。用一个 `map[int64]struct{}` + mu 防重复启动;channel close 时清理。

- [ ] **Step 2:** watcher 与 `CloseSession`(chat.go:3072)/ cache evict 对齐:session 关闭时 AutonomousTurns channel 应 close → watcher 退出。加单测验证「channel close → watcher goroutine 退出」(用 fake source close channel,goleak 或计数验证)。

- [ ] **Step 3: 全量 chat_svc + Commit**

```bash
GOWORK=off go test ./internal/service/chat_svc/ -race
git add internal/service/chat_svc/
git commit -m "✨ chat_svc: 每会话自主轮 watcher 启停"
```

---

## Phase 4 — remote(agentred)wire 支持

### Task 4.1: wire 帧编解码 + round-trip

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/remote/wire/wire.go`
- Modify: `internal/pkg/agentruntime/runtimes/remote/wire/wire_test.go`

- [ ] **Step 1: round-trip 测试(Red)** —— 新增三类帧的对称编解码:`AutonomousTurnStarted{SessionID, Trigger}`、`AutonomousTurnEvent{TurnTag, Event}`(复用既有 Event 编解码,加一个 turn tag 区分归属普通 Run 还是某自主轮)、`AutonomousTurnDone{TurnTag, RunResult}`。断言 encode→decode 等值。

- [ ] **Step 2: 跑看 fail** → `GOWORK=off go test ./internal/pkg/agentruntime/runtimes/remote/wire/ -v`

- [ ] **Step 3: 实现编解码**(参照既有 `runtime.event` / RunResult 的 wire 形态,加新 method/notification tag)。

- [ ] **Step 4: pass + Commit**

```bash
GOWORK=off go test ./internal/pkg/agentruntime/runtimes/remote/wire/ -race
git add internal/pkg/agentruntime/runtimes/remote/wire/
git commit -m "✨ remote/wire: 自主轮帧编解码 + round-trip"
```

### Task 4.2: daemon 转发 AutonomousTurns

**Files:**
- Modify: `internal/daemon/handlers/runtime.go`

- [ ] **Step 1:** daemon 在为某 session 跑 Run 的同时(或首次 Run 时),若真实 runtime 实现 `AutonomousTurnSource`,起 goroutine 订阅 `AutonomousTurns(sessionID)`,把每一轮经 Task 4.1 的帧推到对应 WebSocket 连接。注意 `agent-backend.md` §2.5:daemon 不 bootstrap chat_repo,只透传。

- [ ] **Step 2:** in-memory client↔daemon 集成测试(参照既有 remote 测试):真实(fake)runtime 吐自主轮 → daemon 转发 → client 收到。

- [ ] **Step 3: Commit**

```bash
GOWORK=off go test ./internal/daemon/... -race
git add internal/daemon/handlers/runtime.go
git commit -m "✨ daemon: 转发 runtime 自主轮到 WebSocket"
```

### Task 4.3: remote.Runtime 实现 AutonomousTurnSource

**Files:**
- Modify: `internal/pkg/agentruntime/runtimes/remote/runtime.go`

- [ ] **Step 1:** `remote.Runtime` 实现 `AutonomousTurns(sessionID)`:把 daemon push 的 `AutonomousTurnStarted/Event/Done` 帧还原成 `agentruntime.AutonomousTurn` 值吐出。能力 Prefetch 已从 daemon 同步 `CapAutonomousTurn`(确认 Prefetch 透传新 cap;若靠 Capabilities set 自动同步则无需额外改)。

- [ ] **Step 2:** 集成测试:remote runtime 经 fake wire 收到自主轮帧 → `AutonomousTurns` 吐出等价值。

- [ ] **Step 3: Commit**

```bash
GOWORK=off go test ./internal/pkg/agentruntime/runtimes/remote/ -race
git add internal/pkg/agentruntime/runtimes/remote/runtime.go
git commit -m "✨ remote: Runtime 实现 AutonomousTurnSource"
```

---

## Phase 5 — 集成验证

### Task 5.1: 全量后端测试 + lint

- [ ] **Step 1:** `GOWORK=off make test-backend`(排除 frontend)→ 全绿。
- [ ] **Step 2:** `GOWORK=off make lint`(golangci-lint v2)→ 0 issue。
- [ ] **Step 3:** daemon registry / runtime_imports 矩阵测试仍绿:`GOWORK=off go test ./internal/daemon/ -run RuntimeImports -v`。

### Task 5.2: 真机手动验证(可选,verify skill)

- [ ] 启动 app,claudecode 会话发「后台跑 sleep 10 完成后看看目录」。
- [ ] 期望:先出「已在后台启动…」,~10s 后**自动追加**一条 assistant 列出目录;随后再发任意消息,回复**对应当前消息**(无错位)。

---

## Self-Review(已对照 spec)

- **Spec coverage:** ① readLoop/AutonomousTurns(Phase 1)② 接口+cap(Phase 2)③ watcher(Phase 3)④ remote wire(Phase 4)—— 四组件全覆盖;task_notification 辨析(Task 1.1)、错误处理(Task 3.1 Step3)、测试矩阵(各 Phase)均落到 task。
- **类型一致性:** `AutoTurn`/`AutonomousTurns()`(pkg)、`AutonomousTurn`/`AutonomousTurnSource`/`CapAutonomousTurn`(agentruntime)、`driveAutonomousTurn`/`startAutonomousWatcher`(chat_svc)全程同名。
- **已知务实留白(非 placeholder,是「对照现状实现」的诚实标注):** Session readLoop 的 ctx/turnDone 并发细节、runtime cache/active 字段名、chat_svc drain 复用边界、wire turn-tag 具体编码 —— 这些依赖既有代码现状,实现时对照调通,计划不臆造行号。
```
