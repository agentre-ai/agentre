# AskUserQuestion 失效终态 + 失败可诊断性 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 turn 结束时仍未回答的 AskUserQuestion 卡片进入持久化的 `expired` 终态(锁定 + 「已失效」展示),并给提交失败路径补结构化日志,根治 `sess-1174` 出现的死卡 +「提交失败,请稍后再试」无日志可查。

**Architecture:** 完全复刻仓库既有的 `expired` 先例(`handlers.MarkRunningSubagentsCancelled` 在 finalize 改 finalBlocks、`chatSvc.takeToolApprovals` 把 pending 审批标 expired、前端 `toolApproval.status.expired`)。新增第三个互斥终态 `Expired`,贯穿 持久化 block → canonical → wire DTO → 前端 DTO;在 turn finalize 时把未答 ask 块标 `expired`(落库 + emit live 锁定 patch);前端渲染失效态并在提交失败时本地锁卡。

**Tech Stack:** Go 1.26 / cago / goconvey + gomock + sqlmock;React 19 / TypeScript / Vitest / react-i18next / shadcn。

## Global Constraints

- 仅改 `agentre/` 仓库;**不要碰** 工作区已有的 `frontend/src/components/agentre/orchestration/graph-data.ts`(无关改动,不纳入任何提交)。
- 严格 TDD:每个任务先写失败测试、跑一次看它失败、再实现、再跑绿、再提交。
- gitmoji 提交;golangci-lint v2 必须过。
- 新增前端可见文案一律走 `react-i18next` 的 `t(...)`,同时改 `frontend/src/i18n/locales/zh-CN/common.json` 与 `frontend/src/i18n/locales/en/common.json`;禁止硬编码中文。
- Go 测试跑 `make test-backend`(排除 `/frontend/`);聚焦跑 `go test -race -run TestName ./internal/service/chat_svc/...`。
- 前端测试跑 `cd frontend && pnpm test -- <file>`。
- `Expired` 仅在 `!Answered && !Skipped` 时被置位(三终态互斥不变量)。
- 范围:**只做 AskUserQuestion**;ToolPermission CLI 审批卡同病但本次不做。

---

### Task 1: `Expired` 字段贯穿 Go 模型 + 两条投影路径

把 `Expired` 加到持久化 block / canonical / wire DTO,并让 **历史回放**(`askUserQuestionBlockToChatBlock`)与 **live emit**(`askUserQuestionFromMap` + `dispatcher_emitter` 的 canonical 构造)两条路径都透传它。

**Files:**
- Modify: `internal/service/chat_svc/blocks/user_ask.go`(`UserAskBlock` 加字段)
- Modify: `internal/pkg/agentruntime/canonical/user_ask.go`(`UserAsk` 加字段)
- Modify: `internal/service/chat_svc/types.go`(`ChatBlockAskUserQuestion` 加字段)
- Modify: `internal/service/chat_svc/ask_user_question.go`(`askUserQuestionBlockToChatBlock` 透传)
- Modify: `internal/service/chat_svc/dispatcher_emitter.go`(`askUserQuestionFromMap` + `StreamAskUserQuestion` case)
- Test: `internal/service/chat_svc/dispatcher_emitter_test.go`(内部包 `chat_svc`)

**Interfaces:**
- Produces: `blocks.UserAskBlock.Expired bool`;`canonical.UserAsk.Expired bool`;`ChatBlockAskUserQuestion.Expired bool`。后续任务依赖这三个字段名。

- [ ] **Step 1: 写失败测试** —— 追加到 `dispatcher_emitter_test.go`:

```go
func TestDispatcherEmitter_AskUserQuestion_CarriesExpired(t *testing.T) {
	Convey("kind=ask_user_question 带 expired 的 block → ev + canonical 都透传 expired", t, func() {
		de, em := newTestDispatcherEmitter()
		de.Emit(context.Background(), "s", map[string]any{
			"kind":      "ask_user_question",
			"requestId": "r-exp",
			"askUserQuestion": &blocks.UserAskBlock{
				RequestID: "r-exp",
				Questions: []blocks.AskQuestionDTO{{Question: "ok?"}},
				Expired:   true,
			},
		})
		So(em.events, ShouldHaveLength, 1)
		So(em.events[0].Kind, ShouldEqual, StreamAskUserQuestion)
		So(em.events[0].AskUserQuestion, ShouldNotBeNil)
		So(em.events[0].AskUserQuestion.Expired, ShouldBeTrue)
		So(em.events[0].Canonical, ShouldNotBeNil)
		So(em.events[0].Canonical.UserAsk, ShouldNotBeNil)
		So(em.events[0].Canonical.UserAsk.Expired, ShouldBeTrue)
	})
}
```

- [ ] **Step 2: 跑测试看它失败(编译失败)**

Run: `go test -race -run TestDispatcherEmitter_AskUserQuestion_CarriesExpired ./internal/service/chat_svc/...`
Expected: 编译失败 —— `blocks.UserAskBlock has no field Expired` / `ChatBlockAskUserQuestion has no field Expired` / `canonical.UserAsk has no field Expired`。

- [ ] **Step 3: 加字段 + 透传**

`internal/service/chat_svc/blocks/user_ask.go`(`UserAskBlock` 结构体,在 `Skipped` 后):

```go
	Skipped    bool             `json:"skipped,omitempty"`
	Expired    bool             `json:"expired,omitempty"`
```

`internal/pkg/agentruntime/canonical/user_ask.go`(`UserAsk` 结构体,在 `Skipped` 后):

```go
	Skipped   bool `json:"skipped,omitempty"`
	Expired   bool `json:"expired,omitempty"`
```

`internal/service/chat_svc/types.go`(`ChatBlockAskUserQuestion`,在 `Skipped` 后):

```go
	Skipped   bool                    `json:"skipped,omitempty"`
	Expired   bool                    `json:"expired,omitempty"`
```

`internal/service/chat_svc/ask_user_question.go`(`askUserQuestionBlockToChatBlock`):wire DTO 与 canonical 各加一行 `Expired: b.Expired,`:

```go
		AskUserQuestion: &ChatBlockAskUserQuestion{
			RequestID: b.RequestID,
			Questions: b.Questions,
			Answered:  b.Answered,
			Answers:   b.Answers,
			Skipped:   b.Skipped,
			Expired:   b.Expired,
		},
		Canonical: view.FromCanonical(canonical.UserAsk{
			RequestID: b.RequestID,
			Questions: b.Questions,
			Answers:   b.Answers,
			Answered:  b.Answered,
			Skipped:   b.Skipped,
			Expired:   b.Expired,
		}),
```

`internal/service/chat_svc/dispatcher_emitter.go` —— `askUserQuestionFromMap` 的 block 指针分支加 `Expired: blk.Expired,`:

```go
	if blk, ok := m["askUserQuestion"].(*blocks.UserAskBlock); ok && blk != nil {
		return &ChatBlockAskUserQuestion{
			RequestID: blk.RequestID,
			Questions: blk.Questions,
			Answered:  blk.Answered,
			Answers:   blk.Answers,
			Skipped:   blk.Skipped,
			Expired:   blk.Expired,
		}
	}
```

同文件 `StreamAskUserQuestion` case 的 `canonical.UserAsk{...}` 加 `Expired: ev.AskUserQuestion.Expired,`:

```go
		ev.Canonical = view.FromCanonical(canonical.UserAsk{
			RequestID: ev.AskUserQuestion.RequestID,
			Questions: ev.AskUserQuestion.Questions,
			Answers:   ev.AskUserQuestion.Answers,
			Answered:  ev.AskUserQuestion.Answered,
			Skipped:   ev.AskUserQuestion.Skipped,
			Expired:   ev.AskUserQuestion.Expired,
		})
```

- [ ] **Step 4: 跑测试看它绿**

Run: `go test -race -run TestDispatcherEmitter_AskUserQuestion_CarriesExpired ./internal/service/chat_svc/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/chat_svc/blocks/user_ask.go internal/pkg/agentruntime/canonical/user_ask.go internal/service/chat_svc/types.go internal/service/chat_svc/ask_user_question.go internal/service/chat_svc/dispatcher_emitter.go internal/service/chat_svc/dispatcher_emitter_test.go
git commit -m "✨ ask: UserAsk 模型贯穿 Expired 字段 + 两条投影透传"
```

---

### Task 2: `MarkUnansweredUserAsksExpired` 纯函数

与 `MarkRunningSubagentsCancelled` 同形:在 finalize 的 `finalBlocks` 上把未答/未跳过的 `UserAskBlock` 标 `Expired`,返回被标记的 block 指针(供调用方 emit live 锁定 patch)。

**Files:**
- Modify: `internal/service/chat_svc/handlers/user_ask.go`(新增自由函数)
- Test: `internal/service/chat_svc/handlers/user_ask_test.go`

**Interfaces:**
- Consumes: `blocks.UserAskBlock.Expired`(Task 1)。
- Produces: `func MarkUnansweredUserAsksExpired(finalBlocks []cagoblocks.ContentBlock) []*blocks.UserAskBlock`。Task 3 依赖此签名。

- [ ] **Step 1: 写失败测试** —— 追加到 `handlers/user_ask_test.go`(注意补 import `cagoblocks "github.com/cago-frame/agents/agent/blocks"`):

```go
func TestMarkUnansweredUserAsksExpired(t *testing.T) {
	Convey("仅未答/未跳过/未失效的 UserAskBlock 被标 expired 并返回", t, func() {
		pending := &blocks.UserAskBlock{RequestID: "r-pending"}
		answered := &blocks.UserAskBlock{RequestID: "r-answered", Answered: true}
		skipped := &blocks.UserAskBlock{RequestID: "r-skipped", Skipped: true}
		already := &blocks.UserAskBlock{RequestID: "r-expired", Expired: true}
		other := &blocks.TextBlock{Text: "hi"}
		final := []cagoblocks.ContentBlock{pending, answered, skipped, already, other}

		marked := MarkUnansweredUserAsksExpired(final)

		So(marked, ShouldHaveLength, 1)
		So(marked[0].RequestID, ShouldEqual, "r-pending")
		So(pending.Expired, ShouldBeTrue)
		So(answered.Expired, ShouldBeFalse)
		So(skipped.Expired, ShouldBeFalse)
		So(already.Expired, ShouldBeTrue) // 不重复返回
	})
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test -race -run TestMarkUnansweredUserAsksExpired ./internal/service/chat_svc/handlers/...`
Expected: 编译失败 —— `undefined: MarkUnansweredUserAsksExpired`。

- [ ] **Step 3: 实现** —— 在 `internal/service/chat_svc/handlers/user_ask.go` 末尾追加(`cagoblocks` 已在 `subagent.go` 同包导入,本文件需自行 import):

```go
// MarkUnansweredUserAsksExpired finalize 时把仍未答/未跳过的 AskUserQuestion block
// 标 expired —— 与 MarkRunningSubagentsCancelled / chatSvc.takeToolApprovals 同模式。
// turn 结束后该卡再提交必然失败(claudecode SubmitAnswer 走 ErrNoActiveTurn / 无 waiter),
// 标 expired 让前端锁卡并展示「已失效」,且落库后 reload 仍可见。
// 返回被本次标记的 block 指针,供调用方 emit live 锁定 patch。
func MarkUnansweredUserAsksExpired(finalBlocks []cagoblocks.ContentBlock) []*blocks.UserAskBlock {
	var marked []*blocks.UserAskBlock
	for _, b := range finalBlocks {
		ua, ok := b.(*blocks.UserAskBlock)
		if !ok || ua.Answered || ua.Skipped || ua.Expired {
			continue
		}
		ua.Expired = true
		marked = append(marked, ua)
	}
	return marked
}
```

文件顶部 import 块加 `cagoblocks "github.com/cago-frame/agents/agent/blocks"`。

- [ ] **Step 4: 跑测试看它绿**

Run: `go test -race -run TestMarkUnansweredUserAsksExpired ./internal/service/chat_svc/handlers/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/chat_svc/handlers/user_ask.go internal/service/chat_svc/handlers/user_ask_test.go
git commit -m "✨ ask: MarkUnansweredUserAsksExpired finalize 标记函数"
```

---

### Task 3: 接入 turn finalize —— 标记 + 持久化 + live emit

在 `chat.go` 的 finalize 路径,`acc.Finalize()` 之后、`SetBlocks` 之前调标记函数;待 `finalCtx` 就绪后对被标记的 block emit 一条 `ask_user_question`(expired)patch,让在屏活卡不用 reload 立即锁定。

**Files:**
- Modify: `internal/service/chat_svc/chat.go`(finalize 段,`MarkRunningSubagentsCancelled` 调用点附近 + `finalCtx` 之后)
- Test: `internal/service/chat_svc/chat_test.go`

**Interfaces:**
- Consumes: `handlers.MarkUnansweredUserAsksExpired`(Task 2);`dispEmit *dispatcherEmitter`、`stream string`、`finalCtx context.Context`(finalize 段已有局部变量)。

- [ ] **Step 1: 写失败测试** —— 追加到 `chat_test.go`(mirror `TestSend_CodexPlanUpdatedPersistsVisiblePlanBlock`;import `chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"`):

```go
func TestSend_UnansweredAskUserQuestionExpiresAtFinalize(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventAskUserQuestion, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "ask-1",
			Questions: []agentruntime.AskQuestion{{ID: "q1", Question: "ok?", Options: []agentruntime.AskOption{{Label: "Y"}}}},
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "user" {
				msg.ID = 1000
			} else {
				msg.ID = 1001
			}
			return nil
		}).Times(2)
	m.dbMock.ExpectCommit()

	var final *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			cp := *msg
			final = &cp
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.NotNil(t, final)
	bs, err := final.GetBlocks()
	require.NoError(t, err)
	var found bool
	for _, b := range bs {
		switch ua := b.(type) {
		case *chatblocks.UserAskBlock:
			if ua.RequestID == "ask-1" {
				found = true
				assert.True(t, ua.Expired, "未答 ask 应在 finalize 标 expired")
				assert.False(t, ua.Answered)
				assert.False(t, ua.Skipped)
			}
		case chatblocks.UserAskBlock:
			if ua.RequestID == "ask-1" {
				found = true
				assert.True(t, ua.Expired)
			}
		}
	}
	assert.True(t, found, "应持久化 ask-1 的 UserAskBlock")
}
```

- [ ] **Step 2: 跑测试看它失败**

Run: `go test -race -run TestSend_UnansweredAskUserQuestionExpiresAtFinalize ./internal/service/chat_svc/...`
Expected: FAIL —— `ua.Expired` 为 false(未答 ask 块未被标记)。

- [ ] **Step 3: 接入 finalize** —— `internal/service/chat_svc/chat.go`。

在 `MarkRunningSubagentsCancelled(finalBlocks)` 调用(`if aborted { ... }` 块)之后、`for _, b := range s.takeToolApprovals(...)` 之前,加:

```go
	// 未答的 AskUserQuestion 在 turn 结束后会变死卡(再提交走 ErrNoActiveTurn / 无 waiter
	// 必然失败)。与 MarkRunningSubagentsCancelled / takeToolApprovals 同模式标 expired:
	// 落库让 reload 可见,下方 finalCtx 就绪后 emit 锁定 patch 让在屏活卡立即锁。
	expiredAsks := handlers.MarkUnansweredUserAsksExpired(finalBlocks)
```

在 `finalCtx := context.WithoutCancel(ctx)` 之后(`assistantMsg` 已落 `SetBlocks`)加 live emit:

```go
	for _, blk := range expiredAsks {
		dispEmit.Emit(finalCtx, stream, map[string]any{
			"kind":            "ask_user_question",
			"requestId":       blk.RequestID,
			"askUserQuestion": blk,
		})
	}
```

(`handlers` 包在 `chat.go` 已 import;`dispEmit`、`stream`、`finalCtx` 均为 finalize 段已有局部变量。)

- [ ] **Step 4: 跑测试看它绿 + 全包回归**

Run: `go test -race -run TestSend_UnansweredAskUserQuestionExpiresAtFinalize ./internal/service/chat_svc/...`
Expected: PASS。
Run: `go test -race ./internal/service/chat_svc/...`
Expected: PASS(无回归)。

- [ ] **Step 5: 提交**

```bash
git add internal/service/chat_svc/chat.go internal/service/chat_svc/chat_test.go
git commit -m "✨ ask: turn finalize 标记未答 AskUserQuestion 失效 + emit 锁定 patch"
```

---

### Task 4: 失败路径补结构化日志(可诊断性)

`AnswerUserQuestion` 全程零日志是「查不到」的根因。在服务边界 `chat_svc.AnswerUserQuestion` 每个 error 分支补 `Warn`(覆盖所有后端,且携带 runtime 返回的具体错误串如 `ErrNoActiveTurn` / `no waiting AskUserQuestion`);claudecode `SubmitAnswer` 同补,便于贴近失败点定位。

**Files:**
- Modify: `internal/service/chat_svc/ask_user_question.go`(`AnswerUserQuestion` 各 error 分支)
- Modify: `internal/pkg/agentruntime/runtimes/claudecode/control.go`(`SubmitAnswer` 各 error 分支)
- Test: `internal/service/chat_svc/ask_user_question_test.go`(断言错误返回;日志副作用不强测,与仓库惯例一致)

**Interfaces:**
- Consumes: `logger.Ctx(ctx)` + `zap`(两个文件按各自包既有用法)。

- [ ] **Step 1: 写失败测试** —— 在 `ask_user_question_test.go` 查现有 `AnswerUserQuestion` 测试;若无「sink 返回 error 时 AnswerUserQuestion 透传该 error」用例则补一条(用既有 `fakeAskRunner.err` 字段):

```go
func TestAnswerUserQuestion_PropagatesSinkError(t *testing.T) {
	convey.Convey("sink.SubmitAnswer 报错时 AnswerUserQuestion 返回该 error", t, func() {
		// 复用本文件既有 setup(注册 fakeAskRunner + session/agent/backend repo mock)。
		// 设 fakeAskRunner.err = errors.New("no waiting AskUserQuestion for requestID r-x")
		// 调 AnswerUserQuestion({SessionID, RequestID:"r-x", Answers:[...]})
		// 断言返回 err != nil。
	})
}
```

> 注:按本文件既有 setup 把骨架补成可编译可运行的具体用例(repo mock、`fakeAskRunner` 注册方式照抄文件内现成 helper)。若已有等价用例,跳过本任务的测试改动,只做日志实现 + 跑现有测试看仍绿。

- [ ] **Step 2: 跑测试看它失败/现状**

Run: `go test -race -run TestAnswerUserQuestion ./internal/service/chat_svc/...`
Expected: 新用例 FAIL(若该错误路径此前未覆盖),或已有用例 PASS(则本任务仅加日志、保持绿)。

- [ ] **Step 3: 加日志** —— `internal/service/chat_svc/ask_user_question.go`,每个 `return nil, i18n.NewError(...)` / `return nil, err` 前补一行 `Warn`。示例(其余分支同理,reason 文案对应各分支):

```go
	runner, err := s.selectRunner(ctx, be, sess.ID)
	if err != nil {
		logger.Ctx(ctx).Warn("chat_svc.AnswerUserQuestion: selectRunner failed",
			zap.Int64("sessionId", req.SessionID), zap.String("requestId", req.RequestID), zap.Error(err))
		return nil, i18n.NewError(ctx, code.AgentBackendTypeUnsupported)
	}
	...
	if err := sink.SubmitAnswer(ctx, req.SessionID, req.RequestID, nil, rtAnswers, req.Skipped); err != nil {
		logger.Ctx(ctx).Warn("chat_svc.AnswerUserQuestion: SubmitAnswer failed",
			zap.Int64("sessionId", req.SessionID), zap.String("requestId", req.RequestID), zap.Error(err))
		return nil, err
	}
```

文件顶部补 import:`"github.com/cago-frame/cago/pkg/logger"` 与 `"go.uber.org/zap"`(若未导入)。

`internal/pkg/agentruntime/runtimes/claudecode/control.go` 的 `SubmitAnswer`,在 `ErrNoActiveTurn` 与 `no waiting AskUserQuestion` 两个失败分支补 `Warn`(对齐同包 `runtime.go` 的 `logger.Ctx(ctx)` + `zap` 用法):

```go
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		logger.Ctx(ctx).Warn("claudecode runtime: SubmitAnswer no active turn",
			zap.Int64("sessionID", sessionID), zap.String("requestID", requestID))
		return agentruntime.ErrNoActiveTurn
	}
	a := v.(*claudeActive)
	waiter := a.takeAskWaiter(requestID)
	if waiter == nil {
		logger.Ctx(ctx).Warn("claudecode runtime: SubmitAnswer no waiting AskUserQuestion",
			zap.Int64("sessionID", sessionID), zap.String("requestID", requestID))
		return fmt.Errorf("agentruntime/runtimes/claudecode: no waiting AskUserQuestion for requestID %s", requestID)
	}
```

`control.go` 顶部补 import:`"github.com/cago-frame/cago/pkg/logger"` 与 `"go.uber.org/zap"`。

- [ ] **Step 4: 跑测试看它绿 + 全包回归**

Run: `go test -race ./internal/service/chat_svc/... ./internal/pkg/agentruntime/runtimes/claudecode/...`
Expected: PASS。

- [ ] **Step 5: 提交**

```bash
git add internal/service/chat_svc/ask_user_question.go internal/service/chat_svc/ask_user_question_test.go internal/pkg/agentruntime/runtimes/claudecode/control.go
git commit -m "📝 ask: AnswerUserQuestion / SubmitAnswer 失败路径补结构化日志"
```

---

### Task 5: 前端 —— 失效态渲染 + 提交失败锁卡 + i18n

`UserAskDTO` 加 `expired`;`isExpired` 并入 `isLocked`(自动隐藏提交按钮);`StatusPill` 加灰色「已失效」分支;`handleSubmit` catch 改为明确文案并本地锁卡防止反复失败;补 i18n 两语种。

**Files:**
- Modify: `frontend/src/components/agentre/canonical-tool/types.ts`(`UserAskDTO.expired`)
- Modify: `frontend/src/components/agentre/canonical-tool/user-ask/card.tsx`
- Modify: `frontend/src/i18n/locales/zh-CN/common.json` + `frontend/src/i18n/locales/en/common.json`
- Test: `frontend/src/components/agentre/canonical-tool/user-ask/card.test.tsx`

**Interfaces:**
- Consumes: 后端 wire/canonical 的 `expired`(Task 1)经 `canonical.userAsk.expired` 到达前端。

- [ ] **Step 1: 写失败测试** —— 追加到 `card.test.tsx`:

```tsx
  it("renders EXPIRED state locked with no submit button", () => {
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-exp",
          questions: [{ question: "?", header: "h", options: [{ label: "A", description: "" }] }],
          expired: true,
        },
      },
    } as unknown as ChatBlockData;
    render(<UserAskCard toolBlock={block} sessionId={1} />);
    expect(screen.getByText(/已失效|EXPIRED/i)).toBeDefined();
    expect(screen.queryByText("提交回复")).toBeNull();
    expect(screen.queryByText("Submit reply")).toBeNull();
  });

  it("on submit failure shows expired message and locks the card", async () => {
    const user = userEvent.setup();
    (AnswerUserQuestion as unknown as ReturnType<typeof vi.fn>).mockRejectedValueOnce(
      "no waiting AskUserQuestion",
    );
    const block = {
      type: "tool_use",
      toolName: "AskUserQuestion",
      canonical: {
        kind: "user.ask",
        userAsk: {
          requestId: "req-1",
          questions: [{ question: "?", header: "h", options: [{ label: "A", description: "" }] }],
        },
      },
    } as unknown as ChatBlockData;
    render(<UserAskCard toolBlock={block} sessionId={1} />);
    await user.click(screen.getByText("A"));
    await user.click(screen.getByText("提交回复"));
    await waitFor(() => {
      expect(screen.getByText(/提问已失效|已结束|superseded/i)).toBeDefined();
    });
    // 锁卡:提交按钮消失
    expect(screen.queryByText("提交回复")).toBeNull();
  });
```

- [ ] **Step 2: 跑测试看它失败**

Run: `cd frontend && pnpm test -- src/components/agentre/canonical-tool/user-ask/card.test.tsx`
Expected: FAIL —— 找不到「已失效」/失败文案,或提交按钮仍在。

- [ ] **Step 3: 实现** —— `frontend/src/components/agentre/canonical-tool/types.ts`,`UserAskDTO` 加 `expired?: boolean;`:

```ts
export type UserAskDTO = {
  requestId: string;
  questions: AskQuestionDTO[];
  answers?: AskAnswerDTO[];
  answered?: boolean;
  skipped?: boolean;
  expired?: boolean;
};
```

`card.tsx`:加本地失败态 + 失效判定:

```tsx
  const [failed, setFailed] = React.useState(false);
  const isAnswered = !!payload?.answered;
  const isSkipped = !!payload?.skipped;
  const isExpired = !!payload?.expired || failed;
  const isLocked = isAnswered || isSkipped || isExpired || submitting;
```

`handleSubmit` 的 catch 分支改为明确文案 + 锁卡:

```tsx
      } catch {
        setFailed(true);
        setError(t("canonical.userAsk.errors.expired"));
      } finally {
```

`StatusPill` 增加 `expired` 入参与分支,并在调用处传入:

```tsx
        <StatusPill answered={isAnswered} skipped={isSkipped} expired={isExpired} />
```

```tsx
function StatusPill({
  answered,
  skipped,
  expired,
}: {
  answered: boolean;
  skipped: boolean;
  expired: boolean;
}) {
  const { t } = useTranslation();
  if (expired) {
    return (
      <span className="flex items-center gap-1.5 rounded-sm bg-muted px-1.5 py-0.5 text-2xs font-semibold tracking-wider text-muted-foreground">
        <span className="h-1.5 w-1.5 rounded-full bg-muted-foreground" />
        {t("canonical.userAsk.expired")}
      </span>
    );
  }
  if (skipped) {
    // ...既有 skipped 分支不变
```

(`isLocked` 已 gate `!isLocked &&` 的提交/跳过按钮区,失效态自动无按钮,无需另改。)

- [ ] **Step 4: 补 i18n** —— `frontend/src/i18n/locales/zh-CN/common.json` 的 `canonical.userAsk`:

```json
      "answered": "ANSWERED",
      "expired": "已失效",
      "errors": {
        "optionRequired": "请先选择一个选项再提交",
        "otherRequired": "已选「自定义答案」请填写内容",
        "submitFailed": "提交失败，请稍后再试",
        "expired": "提问已失效：会话已结束或已被新消息跳过"
      },
```

`frontend/src/i18n/locales/en/common.json` 对应 `canonical.userAsk`:

```json
      "answered": "ANSWERED",
      "expired": "EXPIRED",
      "errors": {
        "optionRequired": "Pick an option before submitting",
        "otherRequired": "You picked \"Custom answer\" — please fill it in",
        "submitFailed": "Submit failed. Try again later.",
        "expired": "This prompt has expired: the session ended or it was superseded by a newer message"
      },
```

(键名/缩进以两文件 `canonical.userAsk` 现有结构为准照抄追加;保留既有 `submitFailed` 不删。)

- [ ] **Step 5: 跑测试看它绿(含 i18n 覆盖校验)**

Run: `cd frontend && pnpm test -- src/components/agentre/canonical-tool/user-ask/card.test.tsx src/__tests__/i18n.test.ts`
Expected: PASS。

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/agentre/canonical-tool/types.ts frontend/src/components/agentre/canonical-tool/user-ask/card.tsx frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json frontend/src/components/agentre/canonical-tool/user-ask/card.test.tsx
git commit -m "✨ ask: 前端 AskUserQuestion 失效态渲染 + 提交失败锁卡 + i18n"
```

---

## 最终验证

- [ ] `make test-backend` 全绿。
- [ ] `cd frontend && pnpm test` 全绿。
- [ ] `make lint` 全绿(golangci-lint v2 + ESLint i18next/no-literal-string)。
- [ ] `git status --porcelain` 确认 `orchestration/graph-data.ts` 仍为未跟踪/未改动状态,未被卷入任何提交。

## Self-Review(对照 spec)

- 模型加 `Expired`(A 节)→ Task 1 ✓
- 历史回放 + live emit 两条投影透传(A 节)→ Task 1 ✓
- finalize 标记 + 持久化 + live 锁定(B 节)→ Task 2 + Task 3 ✓
- 前端失效渲染 + 提交失败锁卡 + 明确文案(C 节)→ Task 5 ✓
- 失败路径补日志(D 节)→ Task 4 ✓
- i18n 两语种(全局约束)→ Task 5 ✓
- 偏离 spec:spec 表列了「`UserAskResolved` 加 `Expired`」,但实现走 chat_svc finalize 直接 emit block,**不经 runtime 事件**,故省去该字段(更小 diff、不碰运行时事件协议);失效检测对所有后端在 finalize 统一生效。
- 无 TBD/占位;后续任务用到的 `Expired` 字段名、`MarkUnansweredUserAsksExpired` 签名与前序定义一致。
