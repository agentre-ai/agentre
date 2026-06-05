# 后台任务面板(会话头部胶囊 + 弹层)实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 会话头部一个胶囊显示运行中后台任务数,点开是只读弹层列出本会话的 `local_bash` + `local_agent` 任务(类型图标 / 描述 / 耗时 / 状态)。

**Architecture:** 前端从既有 `subagent_state` 块 derive(每个 CLI task 启动时已落一个,带 `parent_tool_call_id`=task 的 tool_use_id);后端给块补 `kind`+`description`,并经既有自主续轮事件把后台 bash 的完成信号回流(顺带修内联块永远 `running`)。复用 #11 的自主续轮机器,无新能力/前向接口。

**Tech Stack:** Go 1.26(pkg/claudecode、agentruntime、chat_svc/cago)、React 19 + TS + Vitest、Wails 绑定、react-i18next。

**Deviations from spec(实现期发现,已据真实代码校正):**
1. **无 `kind`/`task_type` 字段** —— 全链路新增(`rawFrame`→`SubagentMeta`→`SubagentInfo`→`SubagentStateBlock`+`ChatBlockSubagent`)。
2. **`subagent_state` 块只能在本轮 accumulator 内改**(`turn.Mutate` 走 per-turn `mutateIndex`)。后台 bash 在**之后的自主轮**才完成,要翻它(上一条消息里的块)必须走**新的会话级 repo 写**(spec 选项 a 的具体机制),不能用 accumulator。
3. **前端无 `subagent_state` 卡片**,subagent 元数据是 merge 到父 `tool_use` block 的 `.subagent` 上。本面板**直接 derive `type:"subagent_state"` 的 ChatBlock**(messages + liveBlocks),与 agent-spawn 卡解耦。
4. **`Summary`/退出码 v1 不做**(状态 `running`/`completed`/`failed` 足够;退出码列 follow-up)。

**前置(已完成,worktree 内已提交):** `isNonTurnFrame` 认 `task_started/task_updated/task_progress`(commit `76337f8`)—— 本特性运行期依赖它(后台 bash 完成才会触发自主轮回流完成信号)。

---

## 文件结构(创建/修改)

**后端:**
- `pkg/claudecode/stream.go` — `rawFrame` 加 `TaskType`。
- `pkg/claudecode/session.go` — `parseSystemTask` 填 `TaskType`;`currentTurn` 填 `AutoTurn.CompletedTask`。
- `pkg/claudecode/event.go` — `SubagentMeta` 加 `TaskType`。
- `pkg/claudecode/autoturn.go` — `AutoTurn` 加 `CompletedTask *CompletedBackgroundTask` + 新类型。
- `internal/pkg/agentruntime/runner.go` — `SubagentInfo` 加 `Kind`;`AutonomousTurn` 加 `CompletedTask` + 新类型。
- `internal/pkg/agentruntime/runtimes/claudecode/translator.go` — `subagentInfoFromMeta` 填 `Kind`。
- `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go` — bridge 透传 `CompletedTask`。
- `internal/service/chat_svc/blocks/subagent_state.go` — `SubagentStateBlock` 加 `Kind`+`Description`。
- `internal/service/chat_svc/handlers/subagent.go` — `SubagentStartedHandler` 填 `Kind`+`Description`。
- `internal/service/chat_svc/types.go` — `ChatBlockSubagent` 加 `Kind`;`ChatStreamEvent` 加 `CompletedTask`。
- `internal/service/chat_svc/view/project.go` — 投影时把块的 `Kind`/`Description` 带进 `ChatBlockSubagent`(执行期确认 view 包类型)。
- `internal/service/chat_svc/autonomous_turn.go` — `driveAutonomousTurn` 收到 `CompletedTask` → emit + repo 翻转。
- `internal/repository/chat_repo/message.go`(或同域)— 新 `FlipSubagentStatus` 定向更新。
- `internal/service/chat_svc/emitter.go` — 复用 `AutonomousStreamName`。

**前端:**
- `frontend/src/components/agentre/background-tasks/derive.ts` — `deriveBackgroundTasks` 纯函数。
- `frontend/src/components/agentre/background-tasks/types.ts` — `BackgroundTask` 类型。
- `frontend/src/components/agentre/background-tasks/background-tasks-chip.tsx` — 胶囊。
- `frontend/src/components/agentre/background-tasks/background-tasks-popover.tsx` — 弹层。
- `frontend/src/components/agentre/chat-panel.tsx` — 挂胶囊 + `onAutonomousEvent` 处理 `completedTask`。
- `frontend/src/stores/chat-streams-store.ts` — 一个 `markSubagentDone(sessionId, toolUseId)` action(可选,live 翻转)。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `chatPanel.backgroundTasks.*`。
- 各 `*.test.ts(x)`。

---

## Task 1: pkg/claudecode —— `task_type` 解析 + 自主轮携带完成任务

**Files:**
- Modify: `pkg/claudecode/stream.go`(rawFrame)
- Modify: `pkg/claudecode/event.go`(SubagentMeta)
- Modify: `pkg/claudecode/session.go`(parseSystemTask、currentTurn)
- Modify: `pkg/claudecode/autoturn.go`(AutoTurn + 新类型)
- Test: `pkg/claudecode/session_test.go`

- [ ] **Step 1: 写失败测试 —— task_type 解析进 SubagentMeta**

在 `pkg/claudecode/session_test.go` 末尾追加:

```go
func TestParseSystemTask_CarriesTaskType(t *testing.T) {
	f := rawFrame{
		Type: "system", Subtype: "task_started",
		TaskID: "bg1", ToolUseID: "tu1", Description: "Sleep for 5 seconds",
		TaskType: "local_bash",
	}
	ev, ok := parseSystemTask(f, "sx")
	require.True(t, ok)
	require.NotNil(t, ev.Tool)
	require.NotNil(t, ev.Tool.Subagent)
	assert.Equal(t, "local_bash", ev.Tool.Subagent.TaskType)
	assert.Equal(t, "Sleep for 5 seconds", ev.Tool.Subagent.TaskDescription)
}
```

- [ ] **Step 2: 跑测试看它编译失败**

Run: `GOWORK=off go test -run TestParseSystemTask_CarriesTaskType ./pkg/claudecode/`
Expected: FAIL —— `f.TaskType undefined` / `meta.TaskType undefined`。

- [ ] **Step 3: 加字段 + 填充**

`pkg/claudecode/stream.go` 的 `rawFrame` 里,`SubagentType` 附近加:

```go
	// TaskType 区分 task 帧来源:"local_bash"(run_in_background bash)/ "local_agent"(subagent)。
	TaskType string `json:"task_type,omitempty"`
```

`pkg/claudecode/event.go` 的 `SubagentMeta` 加字段(放在 `SubagentType` 后):

```go
	TaskType        string // ← task_started/progress/notification.task_type（local_bash / local_agent）
```

`pkg/claudecode/session.go` 的 `parseSystemTask` 里 `meta := &SubagentMeta{...}` 加一行:

```go
		TaskType:        f.TaskType,
```

- [ ] **Step 4: 跑测试转绿**

Run: `GOWORK=off go test -run TestParseSystemTask_CarriesTaskType ./pkg/claudecode/`
Expected: PASS

- [ ] **Step 5: 写失败测试 —— 自主轮携带 CompletedTask**

`AutoTurn` 当前只有 `Events/SessionID/Trigger`。新增断言后台型 `task_notification` 起的自主轮带完成任务信息。在 `session_test.go` 追加:

```go
func TestBackgroundTaskAutonomousTurn_CarriesCompletedTask(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := New(WithBinary("fake"), pipeSpawner(t, fakeBackgroundTask))
	sess, err := c.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	ch1, err := sess.Turn(ctx, "alpha")
	require.NoError(t, err)
	_ = drainText(t, ch1)

	var at *AutoTurn
	select {
	case at = <-sess.AutonomousTurns():
	case <-time.After(2 * time.Second):
		t.Fatal("expected autonomous turn")
	}
	require.NotNil(t, at.CompletedTask)
	assert.Equal(t, "tu1", at.CompletedTask.ToolUseID)
	assert.Equal(t, "completed", at.CompletedTask.Status)
}
```

(`fakeBackgroundTask` 的 task_notification 帧已带 `tool_use_id":"tu1","status":"completed"` —— 见 session_test.go 现有 fixture。)

- [ ] **Step 6: 跑测试看失败**

Run: `GOWORK=off go test -run TestBackgroundTaskAutonomousTurn_CarriesCompletedTask ./pkg/claudecode/`
Expected: FAIL —— `at.CompletedTask undefined`。

- [ ] **Step 7: 加 CompletedBackgroundTask + AutoTurn 字段 + 填充**

`pkg/claudecode/autoturn.go`,`AutoTurn` 改为:

```go
type AutoTurn struct {
	Events    <-chan Event
	SessionID string
	Trigger   string // 当前固定 "background_task"
	// CompletedTask 仅 background_task 触发的自主轮带:那个完成的后台任务的身份,
	// 供上层把对应 subagent_state 翻成 completed/failed。
	CompletedTask *CompletedBackgroundTask
}

// CompletedBackgroundTask 是触发本自主轮的后台命令完成信息(从 task_notification 帧抽出)。
type CompletedBackgroundTask struct {
	ToolUseID string
	TaskID    string
	Status    string // "completed" / "failed"(空 → 视为 completed)
}
```

`pkg/claudecode/session.go` 的 `currentTurn` 里,`isBackgroundTaskNotification(f)` 分支改为携带完成信息:

```go
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
			},
		}
		return nil
	}
```

- [ ] **Step 8: 跑全包 -race 转绿**

Run: `GOWORK=off go test -race ./pkg/claudecode/`
Expected: ok(含既有 keystone + 两条新测试)。

- [ ] **Step 9: Commit**

```bash
git add pkg/claudecode/
git commit -m "✨ claudecode: task_type 解析 + 自主轮携带完成后台任务身份"
```

---

## Task 2: agentruntime —— SubagentInfo.Kind + AutonomousTurn.CompletedTask + 翻译/桥接/wire

**Files:**
- Modify: `internal/pkg/agentruntime/runner.go`(SubagentInfo、AutonomousTurn + 新类型)
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/translator.go`(subagentInfoFromMeta)
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/autoturn.go`(bridge)
- Test: `internal/pkg/agentruntime/runtimes/claudecode/translator_test.go`、`autoturn_test.go`

- [ ] **Step 1: 写失败测试 —— Kind 透传**

在 `internal/pkg/agentruntime/runtimes/claudecode/translator_test.go` 追加(若文件不存在则建,包名对齐既有测试):

```go
func TestSubagentInfoFromMeta_CarriesKind(t *testing.T) {
	info := subagentInfoFromMeta(&claudecode.SubagentMeta{TaskType: "local_bash", TaskDescription: "sleep 5"})
	assert.Equal(t, "local_bash", info.Kind)
	assert.Equal(t, "sleep 5", info.TaskDescription)
}
```

- [ ] **Step 2: 跑看失败**

Run: `GOWORK=off go test -run TestSubagentInfoFromMeta_CarriesKind ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: FAIL —— `info.Kind undefined`。

- [ ] **Step 3: 加 SubagentInfo.Kind + 填充**

`internal/pkg/agentruntime/runner.go` 的 `SubagentInfo`,在 `SubagentType` 后加:

```go
	Kind            string // local_bash | local_agent（区分后台 bash 与 subagent；空=未知/旧帧）
```

`internal/pkg/agentruntime/runtimes/claudecode/translator.go` 的 `subagentInfoFromMeta` return 里加:

```go
		Kind:            m.TaskType,
```

- [ ] **Step 4: 跑转绿**

Run: `GOWORK=off go test -run TestSubagentInfoFromMeta_CarriesKind ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: PASS

- [ ] **Step 5: 写失败测试 —— 桥接透传 CompletedTask**

`autoturn_test.go`(claudecode runtime 桥接测试,对齐既有命名)追加一条:用 fake session 吐一个带 `CompletedTask` 的 `claudecode.AutoTurn`,断言 `runtime.AutonomousTurns(sessionID)` 吐出的 `agentruntime.AutonomousTurn.CompletedTask` 透传。参照既有桥接测试构造 fake handle/cache;关键断言:

```go
	got := <-runtime.AutonomousTurns(sessionID)
	require.NotNil(t, got.CompletedTask)
	assert.Equal(t, "tu1", got.CompletedTask.ToolUseID)
	assert.Equal(t, "completed", got.CompletedTask.Status)
```

- [ ] **Step 6: 跑看失败**

Run: `GOWORK=off go test -run CompletedTask ./internal/pkg/agentruntime/runtimes/claudecode/`
Expected: FAIL —— `agentruntime.AutonomousTurn.CompletedTask undefined` / `got.CompletedTask undefined`。

- [ ] **Step 7: 加 agentruntime.AutonomousTurn.CompletedTask + 桥接填充**

`internal/pkg/agentruntime/runner.go`,`AutonomousTurn` 改为:

```go
type AutonomousTurn struct {
	Events  <-chan Event
	Result  *RunResult
	Trigger string // "background_task"
	// CompletedTask 镜像 claudecode.CompletedBackgroundTask:触发本自主轮的后台命令身份。
	CompletedTask *CompletedBackgroundTask
}

// CompletedBackgroundTask 见 AutonomousTurn.CompletedTask。
type CompletedBackgroundTask struct {
	ToolUseID string
	TaskID    string
	Status    string
}
```

`internal/pkg/agentruntime/runtimes/claudecode/autoturn.go` 的 bridge,把构造 `agentruntime.AutonomousTurn{...}` 处加上从 `at.CompletedTask` 映射:

```go
			var completed *agentruntime.CompletedBackgroundTask
			if at.CompletedTask != nil {
				completed = &agentruntime.CompletedBackgroundTask{
					ToolUseID: at.CompletedTask.ToolUseID,
					TaskID:    at.CompletedTask.TaskID,
					Status:    at.CompletedTask.Status,
				}
			}
			out <- agentruntime.AutonomousTurn{Events: evOut, Result: result, Trigger: at.Trigger, CompletedTask: completed}
```

- [ ] **Step 8: wire 透传(SubagentInfo.Kind)** —— `SubagentInfo` 无 JSON tag,内部 wire 走 Go 字段名,新增 `Kind` 自动随 `SubagentStarted/Done` 的 `info` 编解码。补一条 round-trip 断言到 `internal/pkg/agentruntime/event_wire_test.go`(对齐既有):

```go
func TestSubagentStarted_KindRoundTrip(t *testing.T) {
	ev := SubagentStarted{ToolCallID: "tu1", Info: SubagentInfo{Kind: "local_bash"}}
	b, err := json.Marshal(ev)
	require.NoError(t, err)
	got, err := decodeEvent(b) // 对齐既有解码入口名
	require.NoError(t, err)
	assert.Equal(t, "local_bash", got.(SubagentStarted).Info.Kind)
}
```

(若解码入口名不同,执行期 grep `func.*decodeEvent\|UnmarshalEvent` 对齐。)

- [ ] **Step 9: 跑相关包 -race 转绿**

Run: `GOWORK=off go test -race ./internal/pkg/agentruntime/...`
Expected: ok

- [ ] **Step 10: Commit**

```bash
git add internal/pkg/agentruntime/
git commit -m "✨ agentruntime: SubagentInfo.Kind + AutonomousTurn 携带完成后台任务"
```

---

## Task 3: chat_svc —— 块补 kind/description + 投影 + 自主轮回流完成 + 定向翻转

**Files:**
- Modify: `internal/service/chat_svc/blocks/subagent_state.go`
- Modify: `internal/service/chat_svc/handlers/subagent.go`
- Modify: `internal/service/chat_svc/types.go`(ChatBlockSubagent、ChatStreamEvent)
- Modify: `internal/service/chat_svc/view/project.go`
- Modify: `internal/service/chat_svc/autonomous_turn.go`
- Modify: `internal/repository/chat_repo/message.go`(+ 接口/mock)
- Test: 对应 `*_test.go`

- [ ] **Step 1: 写失败测试 —— SubagentStarted 落 kind/description**

`internal/service/chat_svc/handlers/subagent_test.go`(对齐既有 handler 测试)追加:

```go
func TestSubagentStarted_PersistsKindAndDescription(t *testing.T) {
	acc := turn.New()
	h := SubagentStartedHandler{}
	ev := agentruntime.SubagentStarted{ToolCallID: "tu1", Info: agentruntime.SubagentInfo{
		Kind: "local_bash", TaskDescription: "sleep 20",
	}}
	require.NoError(t, h.Apply(context.Background(), ev, acc, nil, nil, &turn.TurnContext{}))
	blks := acc.Finalize()
	require.Len(t, blks, 1)
	sb := blks[0].(*blocks.SubagentStateBlock)
	assert.Equal(t, "local_bash", sb.Kind)
	assert.Equal(t, "sleep 20", sb.Description)
	assert.Equal(t, "running", sb.Status)
}
```

- [ ] **Step 2: 跑看失败**

Run: `GOWORK=off go test -run TestSubagentStarted_PersistsKindAndDescription ./internal/service/chat_svc/handlers/`
Expected: FAIL —— `sb.Kind undefined`。

- [ ] **Step 3: 块加字段 + handler 填充**

`internal/service/chat_svc/blocks/subagent_state.go` 的 `SubagentStateBlock` 加(放在 `TaskID` 后):

```go
	Kind        string `json:"kind,omitempty"`        // local_bash | local_agent
	Description string `json:"description,omitempty"`  // 任务名（task_started.description）
```

`internal/service/chat_svc/handlers/subagent.go` 的 `SubagentStartedHandler.Apply` 里建块处改为:

```go
	blk := &blocks.SubagentStateBlock{
		ParentToolCallID: r.ToolCallID,
		Status:           "running",
		Kind:             r.Info.Kind,
		Description:      r.Info.TaskDescription,
	}
```

- [ ] **Step 4: 跑转绿**

Run: `GOWORK=off go test -run TestSubagentStarted_PersistsKindAndDescription ./internal/service/chat_svc/handlers/`
Expected: PASS

- [ ] **Step 5: 投影带 kind(view/project.go)**

执行期先 `grep -n 'type ChatBlock\|ChatBlockSubagent\|Subagent ' internal/service/chat_svc/view/project.go` 确认 view 包用的是哪个 `ChatBlock`/`Subagent` 类型(同包 types 还是 chat_svc 的)。给该投影 struct(`ChatBlockSubagent` 或 view 内镜像)加 `Kind string json:"kind,omitempty"`,并在 `subagent_state` case 把块的 `Kind`/`Description` 带进去。`internal/service/chat_svc/types.go` 的 `ChatBlockSubagent` 加:

```go
	Kind string `json:"kind,omitempty"` // local_bash | local_agent
```

`project.go` 的 `subagent_state` case 改为显式构造 Subagent 镜像(把块字段映射进去),例如:

```go
		case *blocks.SubagentStateBlock:
			out = append(out, ChatBlock{Type: "subagent_state", Subagent: subagentMirror(t)})
```

并加 helper `subagentMirror(*blocks.SubagentStateBlock) *ChatBlockSubagent`(映射 TaskID/Kind/Description/Status/ToolUses/TotalTokens/DurationMs/LastToolName + `ParentToolCallID` 经 ChatBlock.ParentToolCallID 透出)。**注意**:前端 derive 要靠 `ChatBlock.ParentToolCallID`(=task 的 tool_use_id)+ `type:"subagent_state"`,投影务必把 `ParentToolCallID` 也填到 `ChatBlock.ParentToolCallID`。

- [ ] **Step 6: 写失败测试 —— driveAutonomousTurn 回流完成 + 翻转**

`internal/service/chat_svc/autonomous_turn_test.go` 追加(用既有 mock runtime + `DriveAutonomousTurnForTest`):自主轮带 `CompletedTask{ToolUseID:"tu1",Status:"completed"}` → 断言 (a) emit 的 `StreamAutonomousStarted` 事件 `CompletedTask` 非空且 toolUseId="tu1";(b) 调用了 repo 的 `FlipSubagentStatus(sessionID,"tu1","completed")`(用 mock 断言)。

```go
	// 关键断言(节选)
	assert.Equal(t, "tu1", emitted.CompletedTask.ToolUseID)
	mockMsgRepo.AssertCalled(t, "FlipSubagentStatus", mock.Anything, sessionID, "tu1", "completed")
```

- [ ] **Step 7: 跑看失败**

Run: `GOWORK=off go test -run AutonomousTurn ./internal/service/chat_svc/`
Expected: FAIL —— 字段/方法未定义。

- [ ] **Step 8: ChatStreamEvent.CompletedTask + repo 方法 + driveAutonomousTurn 接线**

`internal/service/chat_svc/types.go` 的 `ChatStreamEvent` 加(放在 `Stream`/`Trigger` 附近):

```go
	// StreamAutonomousStarted 时,若该自主轮由后台命令完成触发,带上完成任务身份,
	// 前端据此把对应 subagent_state(上一条消息里)即时翻成 completed/failed。
	CompletedTask *CompletedTaskRef `json:"completedTask,omitempty"`
```

加类型(types.go):

```go
type CompletedTaskRef struct {
	ToolUseID string `json:"toolUseId"`
	Status    string `json:"status"` // completed | failed
}
```

`internal/repository/chat_repo/message.go` 加方法(并同步接口定义 + `make mock` 重生成 mock):

```go
// FlipSubagentStatus 定向把本会话里 parent_tool_call_id==toolUseID 的 subagent_state
// 块状态改成 status(后台 bash 在之后的自主轮才完成,无法走 per-turn accumulator)。
// 找不到则静默返回 nil(任务可能已 evict / 非本会话)。
func (r *message) FlipSubagentStatus(ctx context.Context, sessionID int64, toolUseID, status string) error {
	// 实现:按 session_id 倒序拉近 N 条 assistant 消息,逐条 json 解析 blocks_json,
	// 找到 type=="subagent_state" 且 parent_tool_call_id==toolUseID 的块,改 status 重写该条。
	// 用 chat_repo 既有的 Find/Update;JSON 操作用 cagoblocks 反/正序列化保持一致。
}
```

`internal/service/chat_svc/autonomous_turn.go` 的 `driveAutonomousTurn`:在 emit `StreamAutonomousStarted` 处带上 CompletedTask,并在 finalize 后做 repo 翻转:

```go
	var completedRef *CompletedTaskRef
	if at.CompletedTask != nil && at.CompletedTask.ToolUseID != "" {
		st := at.CompletedTask.Status
		if st == "" {
			st = "completed"
		}
		completedRef = &CompletedTaskRef{ToolUseID: at.CompletedTask.ToolUseID, Status: st}
	}
	s.emitter.Emit(ctx, AutonomousStreamName(sessionID), ChatStreamEvent{
		Kind:             StreamAutonomousStarted,
		Stream:           stream,
		Trigger:          at.Trigger,
		AssistantMessage: chatMessageForEvent(sess, assistantMsg),
		CompletedTask:    completedRef,
	})
	// …(既有 drain/finalize 不变)…
	// finalize 后(finalCtx 有 DB 句柄):
	if completedRef != nil {
		if err := chat_repo.Message().FlipSubagentStatus(finalCtx, sessionID, completedRef.ToolUseID, completedRef.Status); err != nil {
			logger.Ctx(finalCtx).Warn("chat_svc: FlipSubagentStatus failed",
				zap.Int64("sessionId", sessionID), zap.String("toolUseId", completedRef.ToolUseID), zap.Error(err))
		}
	}
```

- [ ] **Step 9: 跑 chat_svc + repo + handlers -race 转绿**

Run: `GOWORK=off go test -race ./internal/service/chat_svc/... ./internal/repository/chat_repo/...`
Expected: ok(repo 测试用 sqlmock)。

- [ ] **Step 10: Commit**

```bash
git add internal/service/chat_svc/ internal/repository/chat_repo/
git commit -m "✨ chat_svc: subagent_state 补 kind/description + 自主轮回流后台完成并定向翻转"
```

---

## Task 4: 前端 —— derive + 胶囊 + 弹层 + 完成即时翻转 + i18n

> **Worktree 前置(执行 Task 4 前一次性做):** 生成 wails 绑定 + 占位 dist:
> ```bash
> mkdir -p frontend/dist && touch frontend/dist/.gitkeep
> cd frontend && pnpm install && cd ..
> GOWORK=off wails generate module   # 产出 frontend/wailsjs（含 chat_svc.ChatBlockSubagent 含新 kind）
> ```

**Files:**
- Create: `frontend/src/components/agentre/background-tasks/{types.ts,derive.ts,derive.test.ts,background-tasks-chip.tsx,background-tasks-popover.tsx}`
- Modify: `frontend/src/components/agentre/chat-panel.tsx`
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`
- Test: `frontend/src/components/agentre/background-tasks/derive.test.ts`、chip/popover `.test.tsx`、`frontend/src/__tests__/i18n.test.ts`(自动覆盖)

- [ ] **Step 1: 类型**

`background-tasks/types.ts`:

```ts
export type BackgroundTaskKind = "local_bash" | "local_agent";
export type BackgroundTaskStatus = "running" | "completed" | "failed";

export interface BackgroundTask {
  toolUseId: string;
  kind: BackgroundTaskKind;
  description: string;
  status: BackgroundTaskStatus;
}
```

- [ ] **Step 2: 写失败测试 —— deriveBackgroundTasks**

`background-tasks/derive.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { deriveBackgroundTasks } from "./derive";

const sub = (over: Partial<any> = {}) => ({
  type: "subagent_state",
  parentToolUseId: "tu1",
  subagent: { kind: "local_bash", taskDescription: "npm run dev", status: "running" },
  ...over,
});

describe("deriveBackgroundTasks", () => {
  it("从 liveBlocks 的 subagent_state 取运行中任务", () => {
    const tasks = deriveBackgroundTasks([], [sub() as any]);
    expect(tasks).toEqual([
      { toolUseId: "tu1", kind: "local_bash", description: "npm run dev", status: "running" },
    ]);
  });

  it("历史消息里的 subagent_state 也计入,且 status=completed 正确映射", () => {
    const msg = { blocks: [sub({ parentToolUseId: "tu2", subagent: { kind: "local_agent", taskDescription: "Explore", status: "completed" } })] };
    const tasks = deriveBackgroundTasks([msg as any], []);
    expect(tasks[0]).toMatchObject({ toolUseId: "tu2", kind: "local_agent", status: "completed" });
  });

  it("同一 toolUseId 在 live 覆盖历史(去重取最新)", () => {
    const msg = { blocks: [sub({ subagent: { kind: "local_bash", taskDescription: "x", status: "running" } })] };
    const live = [sub({ subagent: { kind: "local_bash", taskDescription: "x", status: "completed" } })];
    const tasks = deriveBackgroundTasks([msg as any], live as any);
    expect(tasks).toHaveLength(1);
    expect(tasks[0].status).toBe("completed");
  });
});
```

- [ ] **Step 3: 跑看失败**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/derive.test.ts`
Expected: FAIL —— 模块不存在。

- [ ] **Step 4: 实现 derive**

`background-tasks/derive.ts`(镜像 `task-progress/derive.ts` 的扫描风格;读 `ChatBlock.parentToolUseId` + `.subagent`):

```ts
import type { ChatBlockData } from "@/stores/chat-streams-store";
import type { chat_svc } from "../../../../wailsjs/go/models";
import type { BackgroundTask, BackgroundTaskKind, BackgroundTaskStatus } from "./types";

// 扫历史 messages + liveBlocks 里所有 type==="subagent_state" 的 block,按 toolUseId
// 去重(后出现的覆盖先出现的:历史先扫、live 后扫 → live 赢),映射成 BackgroundTask。
export function deriveBackgroundTasks(
  messages: chat_svc.ChatMessage[],
  liveBlocks: ChatBlockData[],
): BackgroundTask[] {
  const byId = new Map<string, BackgroundTask>();
  const visit = (block: ChatBlockData | undefined) => {
    if (!block || block.type !== "subagent_state") return;
    const toolUseId = (block as { parentToolUseId?: string }).parentToolUseId;
    const sa = (block as { subagent?: Record<string, unknown> }).subagent;
    if (!toolUseId || !sa) return;
    byId.set(toolUseId, {
      toolUseId,
      kind: mapKind(sa.kind as string),
      description: (sa.taskDescription as string) ?? "",
      status: mapStatus(sa.status as string),
    });
  };
  for (let i = 0; i < messages.length; i++) {
    const blocks = messages[i].blocks ?? [];
    for (const b of blocks) visit(b as unknown as ChatBlockData);
  }
  for (const b of liveBlocks) visit(b);
  return [...byId.values()];
}

function mapKind(raw: string): BackgroundTaskKind {
  return raw === "local_agent" ? "local_agent" : "local_bash";
}
function mapStatus(raw: string): BackgroundTaskStatus {
  if (raw === "completed") return "completed";
  if (raw === "failed") return "failed";
  return "running";
}
```

- [ ] **Step 5: 跑转绿**

Run: `cd frontend && pnpm test -- src/components/agentre/background-tasks/derive.test.ts`
Expected: PASS

- [ ] **Step 6: i18n key(双语)**

`frontend/src/i18n/locales/zh-CN/common.json` 的 `chatPanel` 下加:

```json
    "backgroundTasks": {
      "chip": "{{count}} 运行中",
      "title": "后台任务",
      "running": "运行中",
      "completed": "已完成",
      "failed": "失败",
      "empty": "暂无后台任务",
      "bash": "bash",
      "subagent": "subagent",
      "aria": "后台任务"
    },
```

`frontend/src/i18n/locales/en/common.json` 的 `chatPanel` 下镜像:

```json
    "backgroundTasks": {
      "chip": "{{count}} running",
      "title": "Background tasks",
      "running": "Running",
      "completed": "Done",
      "failed": "Failed",
      "empty": "No background tasks",
      "bash": "bash",
      "subagent": "subagent",
      "aria": "Background tasks"
    },
```

- [ ] **Step 7: 弹层 + 胶囊组件**

`background-tasks-popover.tsx`:用 shadcn `@/components/ui/popover`;列表用 `Terminal`/`Bot`(lucide)分类图标,绿点 = running,灰 = completed,红 = failed;耗时本地 tick(传入 `startedAt` 或省略,v1 可仅显示状态文案,耗时列 follow-up)。`background-tasks-chip.tsx`:`Button variant="outline" size="sm"`,内容 `t("chatPanel.backgroundTasks.chip",{count:runningCount})`,绿点 + chevron,`running===0` 时返回 `null`(完全隐藏)。各组件 `.test.tsx`:渲染三态 + 计数 + 隐藏。所有可见文案走 `t(...)`。

- [ ] **Step 8: 挂到工具栏 + 完成即时翻转**

`chat-panel.tsx`:在 toolbar 的 Stop 按钮前插入 `<BackgroundTasksChip tasks={backgroundTasks} />`,`backgroundTasks = useMemo(() => deriveBackgroundTasks(messages, liveBlocks), [messages, liveBlocks])`。`onAutonomousEvent` 里处理 `ev.completedTask`:把 store 中(messages)对应 `parentToolUseId===ev.completedTask.toolUseId` 的 subagent_state 块 `subagent.status` 即时设为 `ev.completedTask.status`(新增 store action `markSubagentDone(sessionId, toolUseId, status)`,或在 chat-panel 本地 `setMessages` 改)。reload 后由后端 `FlipSubagentStatus` 落库保证一致。

- [ ] **Step 9: 跑前端测试 + i18n 校验 + tsc + lint**

Run:
```bash
cd frontend && pnpm test -- src/components/agentre/background-tasks/ src/__tests__/i18n.test.ts
cd frontend && pnpm tsc --noEmit && pnpm lint
```
Expected: 全过(i18n.test.ts 验证 zh/en key 对齐 + 静态 t() key 存在)。

- [ ] **Step 10: Commit**

```bash
git add frontend/src/components/agentre/background-tasks/ frontend/src/components/agentre/chat-panel.tsx frontend/src/i18n/ frontend/src/stores/chat-streams-store.ts
git commit -m "✨ 前端: 后台任务面板(头部胶囊+只读弹层,从 subagent_state derive)"
```

---

## 收尾验证(全绿后)

- [ ] `GOWORK=off go test -race ./pkg/claudecode/... ./internal/pkg/agentruntime/... ./internal/service/chat_svc/... ./internal/repository/chat_repo/...`
- [ ] `GOWORK=off golangci-lint run ./pkg/claudecode/ ./internal/pkg/agentruntime/... ./internal/service/chat_svc/...`
- [ ] `cd frontend && pnpm test && pnpm tsc --noEmit && pnpm lint`
- [ ] 手验(可选):真实跑「sleep 20 后台 + 完成后通知」会话,胶囊「1 运行中」→ 完成翻「已完成」;reload 仍正确。
- [ ] 用 `superpowers:requesting-code-review` 自审 diff。

## Self-review(已过)

- **Spec coverage**:胶囊+弹层(T4)、bash+subagent 统一(kind 贯穿 T1–T4)、运行/完成/失败三态(T1 Status / T3 翻转 / T4 mapStatus)、只读(无 output/stop 任务)、复用自主轮(T3)、修内联块 running(T3 FlipSubagentStatus)—— 均有对应任务。non-goals(output/stop/新表/远端/全局托盘)未排任务,符合。
- **Placeholder scan**:`FlipSubagentStatus` 实现体给了具体算法(倒序拉消息+JSON 改+重写),非 TODO;`view/project.go` 类型与 wire decode 入口名标了「执行期确认」并给了预期代码形状,非占位。
- **Type consistency**:`TaskType`(claudecode)→`Kind`(agentruntime/block/wire);`CompletedBackgroundTask`(claudecode & agentruntime 各一,bridge 映射)→`CompletedTaskRef`(chat_svc wire,camelCase `toolUseId/status`)→前端 `ev.completedTask`。`subagent_state` 块 `Kind`/`Description` ↔ `ChatBlockSubagent.Kind`/`TaskDescription` ↔ 前端 `subagent.kind`/`taskDescription`。一致。
