# 编排 S3 — peer ask/reply(busy 目标 steer + XML 注入 + 前端渲染)Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让 agent↔agent 的 `ask`「对方处理中也能问」落地,并把 ask/reply 渲染到前端:① 后端 `Ask()` idle 走 `SendAndForget`、**busy 时走 `Enqueue`/steer 注入对方当前 turn**(复用既有 steer setter);② 注入格式从 `【收到提问 ask_id=…】` 纯文本改为 `<peer_ask ask_id="…" from="…">问题</peer_ask>` **XML**(闭合标签天然边界,steer 进对方 turn 不被其输出污染);③ 后端 emit `orch:run:ask`/`orch:run:reply` 事件;④ 前端 `orch-run-store` 累积 ask 日志/在飞 ask → `feed-data` 渲染 ask/reply + 结构图「提问·等待回复」徽标。

**Architecture:** 后端无新机制——复用 `chat_svc.Enqueue`(steer 进 running turn)。检测 busy 不靠状态查询(adapter 的 `AgentStatus` 只反映上一轮完成态,测不出"正在跑"),而是**先 `Send`、撞 `ChatSendInFlight` 再 `Enqueue`**:adapter 把 `chat_svc` 的 `ChatSendInFlight`(`*httputils.Error.Code`)映射成 `orch_svc.ErrSessionBusy` 哨兵,`Ask` 据此回退到 `Enqueue`。事件链路镜像现有 deadlock:`orch_svc.emit` → `OrchEventsHost` → `orch-run-store`(新增 `askLog`/`activeAsks`)→ `feed-data`/`structure-graph`。

**Tech Stack:** Go 1.26(orch_svc/chat_svc/app)+ goconvey + gomock;React 19 + zustand + Vitest。

## Global Constraints

- **严格 TDD:Red → Green → Refactor。** 每 Task 先写失败测试 → 跑看正确失败 → 最小实现 → 跑过 →(门控)提交。**先证 bug/缺口存在**:Task 1 的 Red 是「busy 时 Ask 直接 `ChatSendInFlight` 报错、不 steer」。
- **修根因不打补丁**:busy 投递走既有 steer(`chat_svc.Enqueue`),**不**在 `Ask` 里塞 retry/sleep 兜避锁;XML 注入**替换**旧纯文本前缀(不是两套并存)。
- **只动本切片**:后端 `orch_svc/{deps.go,ask.go,orch.go}` + `internal/app/orch_adapter.go` + 重生成 `mock_orch_svc`;前端 `orchestration/{events.ts,feed-data.ts,activity-feed.tsx,structure-graph.tsx}` + `stores/orch-run-store.ts` + i18n + 各测试。**禁止** drive-by 改 dispatch/complete/scheduler。
- **依赖 S4 已落地**:结构图 ask 徽标挂在 S4 后的 `NodeCard` 上。执行顺序在 S4 之后(roadmap 把 S3 排在 Phase 0 末)。
- **接受的权衡(spec §9 已拍板)**:busy 注入进对方**当前 turn**;若对方本 turn 内不调 `reply` → 提问方 4min 超时返错(走既有 `timeAfter(approvalTimeout)` 分支,不新增机制)。
- **emit 容错**:`s.emit` 可能为 nil(测试/headless),所有 emit 前 `if s.emit != nil`(对齐现有 deadlock emit)。
- **i18n**:新可见文案(feed ask/reply 文案、graph 徽标)走 `t(...)` 双语;动态内容(问题/答案正文)不翻译。
- **共享分支 develop/wyz**:提交带 pathspec;**Commit 门控**。Go 改动后跑 `make mock`(deps.go 改了接口)。
- **测试命令**:后端 `go test -race ./internal/service/orch_svc/... ./internal/app/...`;前端聚焦 `cd frontend && pnpm test -- <path>`;收尾 `make test-backend` + `make test-frontend` + `make lint`。

---

## File Structure

- `internal/service/orch_svc/deps.go` — `ChatGateway` 接口加 `Enqueue(ctx, sessionID, text) error`;声明 `ErrSessionBusy`(放 ask.go 或 orch.go,见 Task 1)。
- `internal/service/orch_svc/ask.go` — `Ask` busy 回退 `Enqueue` + `<peer_ask>` XML 格式 + emit ask/reply;`askEnvelope` 加 `runID`。
- `internal/service/orch_svc/orch.go` — `askEnvelope` struct 加 `runID int64`(若 struct 在 orch.go)。
- `internal/app/orch_adapter.go` — `SendAndForget` 映射 `ChatSendInFlight`→`orch_svc.ErrSessionBusy`;新增 `Enqueue` 方法 → `chat_svc.Chat().Enqueue`。
- `internal/service/orch_svc/mock_orch_svc/mock_deps.go` — `make mock` 重生成(加 Enqueue)。
- `frontend/src/components/agentre/orchestration/events.ts` — 加 `ask`/`reply` 事件名。
- `frontend/src/stores/orch-run-store.ts` — `askLog: Map<runId, AskLogItem[]>` + `activeAsks: Map<runId, ActiveAsk[]>` + `onRunEvent` 处理 ask/reply。
- `frontend/src/components/agentre/orchestration/feed-data.ts` — `buildFeed(detail, askLog?)` 合并 ask/reply。
- `frontend/src/components/agentre/orchestration/activity-feed.tsx` — 传 store 的 askLog 进 buildFeed;渲染 `ask`/`reply` kind。
- `frontend/src/components/agentre/orchestration/structure-graph.tsx` — asker 节点「提问·等待回复」徽标(读 activeAsks)。
- `frontend/src/i18n/locales/{zh-CN,en}/common.json` — `orchestration.feed.{ask,reply}` + `orchestration.graph.askWaiting`。

---

## Task 1: 后端 `Ask` busy 回退 steer + `<peer_ask>` XML 注入

**Files:**
- Modify: `internal/service/orch_svc/deps.go:28-34`(ChatGateway 加 Enqueue)
- Modify: `internal/service/orch_svc/ask.go`(ErrSessionBusy + 回退 + XML)
- Modify: `internal/service/orch_svc/orch.go`(askEnvelope 加 runID)
- Regenerate: `mock_orch_svc/mock_deps.go`(`make mock`)
- Test: `internal/service/orch_svc/ask_test.go`

**Interfaces:**
- Produces:
  ```go
  // ChatGateway 新增
  Enqueue(ctx context.Context, sessionID int64, text string) error
  // ask.go 新增哨兵
  var ErrSessionBusy = errors.New("orch: target session has an in-flight turn")
  // 注入格式
  // <peer_ask ask_id="<id>" from="<askerName>"><question></peer_ask>\n请调用 reply(ask_id="<id>", answer=...) 回复。
  ```

- [ ] **Step 1: 写失败测试 — busy 目标走 Enqueue 而非报错**

`ask_test.go` 追加(mock gateway:SendAndForget 返回 ErrSessionBusy → 断言 Enqueue 被调且消息是 `<peer_ask`):
```go
func TestAsk_BusyTargetSteersIntoCurrentTurn(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "前端"}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	// 目标 busy:SendAndForget 返 ErrSessionBusy → 必须回退 Enqueue
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).Return(orch_svc.ErrSessionBusy)
	injCh := make(chan string, 1)
	chat.EXPECT().Enqueue(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		injCh <- msg
		return nil
	})

	Convey("busy 目标:Ask 回退 Enqueue/steer, 注入 <peer_ask> XML", t, func() {
		done := make(chan string, 1)
		go func() { ans, _ := orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权?"); done <- ans }()
		msg := <-injCh
		So(msg, ShouldContainSubstring, "<peer_ask")
		So(msg, ShouldContainSubstring, `from="前端"`)
		askID := parseAskID(msg)
		So(askID, ShouldNotBeBlank)
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "session+cookie"), ShouldBeNil)
		select {
		case ans := <-done:
			So(ans, ShouldEqual, "session+cookie")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}
```
并把 `parseAskID` 升级成能解析 XML 属性:`ask_id="<id>"`(现有 `IndexAny(after, "】\" ")` 已能在遇到 `"` 处截断 → 对 `ask_id="x"` 返回空串前的内容?复核:`after`=`x" from=...`,`IndexAny` 命中第 0 位的 `x`?不,命中 `"`(在 `x` 之后)→ 返回 `x`。**实际**:`ask_id="x"` → Cut 后 `after`=`"x" ...`,首字符是 `"` → `IndexAny` 返回 0 → 返回空。**需修** `parseAskID`:Cut `ask_id="` 再到下一个 `"`:
```go
func parseAskID(msg string) string {
	_, after, found := strings.Cut(msg, `ask_id="`)
	if !found {
		// 兼容旧格式 ask_id=xxx
		_, after, found = strings.Cut(msg, "ask_id=")
		if !found {
			return ""
		}
		if j := strings.IndexAny(after, "】\" "); j >= 0 {
			return after[:j]
		}
		return after
	}
	if j := strings.IndexByte(after, '"'); j >= 0 {
		return after[:j]
	}
	return after
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test -race -run TestAsk_BusyTargetSteersIntoCurrentTurn ./internal/service/orch_svc/...`
Expected: FAIL — 编译失败(`ErrSessionBusy`/`Enqueue` 不存在)。先证缺口。

- [ ] **Step 3: 加 ChatGateway.Enqueue + 重生成 mock**

`deps.go` `ChatGateway` 接口加:
```go
	// Enqueue 把文本 steer 进该会话**正在进行**的 turn(对方 busy 时用,复用 chat_svc.Enqueue)。
	Enqueue(ctx context.Context, sessionID int64, text string) error
```
跑 `make mock` 重生成 `mock_orch_svc/mock_deps.go`。

- [ ] **Step 4: 实现 ErrSessionBusy + Ask 回退 + XML + askEnvelope.runID**

`ask.go` 顶部 var 块加:
```go
	ErrSessionBusy = errors.New("orch: target session has an in-flight turn")
```
`orch.go` 的 `askEnvelope` struct 加字段 `runID int64`;`ask.go` 构造 env 时 `runID: from.RunID`。
`Ask` 内取 asker 名(`from` 是发起方 task):
```go
	askerName := ""
	if a, _ := s.agents.Find(ctx, from.AgentID); a != nil {
		askerName = a.Name
	}
```
把注入段(`:54-58`)替换为:
```go
	// XML 注入:闭合标签是天然边界,busy steer 进对方当前 turn 时不被其输出污染。
	msg := fmt.Sprintf(
		`<peer_ask ask_id="%s" from="%s">%s</peer_ask>`+"\n"+`请调用 reply(ask_id="%s", answer=...) 回复。`,
		askID, askerName, question, askID,
	)
	if err := s.chat.SendAndForget(ctx, toSession, msg); err != nil {
		if errors.Is(err, ErrSessionBusy) {
			// 对方正在跑 turn → steer 进它当前 turn。
			if e2 := s.chat.Enqueue(ctx, toSession, msg); e2 != nil {
				return "", e2
			}
		} else {
			return "", err
		}
	}
```

- [ ] **Step 5: 跑测试通过(busy + 既有 idle 用例)**

Run: `go test -race -run 'TestAsk' ./internal/service/orch_svc/...`
Expected: PASS。既有 `TestAsk_InjectLiveSessionThenReplyResolves` 的注入断言若硬编码旧文案需同步(它用 `parseAskID` + `ShouldNotBeBlank`,XML 下仍通过;若某用例断言 `【收到提问`,改为断言 `<peer_ask`)。

- [ ] **Step 6:(门控)提交**

```bash
git commit internal/service/orch_svc/deps.go internal/service/orch_svc/ask.go \
  internal/service/orch_svc/orch.go internal/service/orch_svc/mock_orch_svc/mock_deps.go \
  internal/service/orch_svc/ask_test.go \
  -m "✨ orch ask:busy 目标回退 Enqueue/steer + <peer_ask> XML 注入"
```

---

## Task 2: adapter 映射 ChatSendInFlight→ErrSessionBusy + 实现 Enqueue

**Files:**
- Modify: `internal/app/orch_adapter.go`
- Test: `internal/app/orch_test.go`(若有 adapter 单测;否则靠 build + Task 1 的接口契约 + 收尾集成)

**Interfaces:**
- Produces:`orchChatAdapter.Enqueue(ctx, sessionID, text) error` → `chat_svc.Chat().Enqueue`;`SendAndForget` 把 `code.ChatSendInFlight` 映射成 `orch_svc.ErrSessionBusy`。

- [ ] **Step 1: 实现 adapter(无纯单测则先实现,Step 3 用 build+lint 兜底)**

`orch_adapter.go` import 加 `"errors"`、`"github.com/cago-frame/cago/pkg/utils/httputils"`、`"github.com/agentre-ai/agentre/internal/pkg/code"`。
`SendAndForget` 改为映射 busy:
```go
func (a *orchChatAdapter) SendAndForget(ctx context.Context, sessionID int64, text string) error {
	_, err := chat_svc.Chat().Send(ctx, &chat_svc.SendRequest{
		SessionID:             sessionID,
		Text:                  text,
		EmitTurnStartedBypass: true,
	})
	if err != nil {
		var herr *httputils.Error
		if errors.As(err, &herr) && herr.Code == code.ChatSendInFlight {
			return orch_svc.ErrSessionBusy // 让 orch.Ask 回退 steer
		}
	}
	return err
}
```
新增:
```go
// Enqueue 把文本 steer 进该会话正在进行的 turn(对方 busy 时用)。
func (a *orchChatAdapter) Enqueue(ctx context.Context, sessionID int64, text string) error {
	_, err := chat_svc.Chat().Enqueue(ctx, &chat_svc.EnqueueRequest{
		SessionID: sessionID,
		Text:      text,
	})
	return err
}
```

- [ ] **Step 2: 写测试(可行则做)— SendAndForget 映射 busy**

若 `orch_test.go` 已有用 `chat_svc` 单例的脚手架,加一例:让目标会话处于 in-flight,断言 `SendAndForget` 返 `orch_svc.ErrSessionBusy`。**若脚手架不便(chat_svc 单例难拉起)**:跳过纯单测,以 build + `make lint` + 收尾后端集成保证,并在提交信息注明「adapter 映射由接口契约 + 集成覆盖」。

- [ ] **Step 3: 编译 + 后端校验**

Run: `go build ./... && go test -race ./internal/app/... ./internal/service/orch_svc/...`
Expected: PASS / 编译通过。

- [ ] **Step 4:(门控)提交**

```bash
git commit internal/app/orch_adapter.go internal/app/orch_test.go \
  -m "✨ orch adapter:ChatSendInFlight→ErrSessionBusy + Enqueue→chat_svc.Enqueue"
```

---

## Task 3: 后端 emit `orch:run:ask` / `orch:run:reply` 事件

**Files:**
- Modify: `internal/service/orch_svc/ask.go`
- Test: `internal/service/orch_svc/ask_test.go`

**Interfaces:**
- Produces(payload,前端 Task 4 据此解析):
  ```
  orch:run:ask   { runId, askId, askerAgentId, askerSessionId, targetAgentId, targetSessionId, question }
  orch:run:reply { runId, askId, answer, timedOut }
  ```

- [ ] **Step 1: 写失败测试 — Ask emit ask, reply 后 emit reply**

`ask_test.go` 追加(mock Emitter):
```go
func TestAsk_EmitsAskAndReplyEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	agents.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{ID: 2, Name: "前端"}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error { injCh <- m; return nil })
	askEvt := make(chan struct{}, 1)
	emit.EXPECT().Emit(gomock.Any(), "orch:run:ask", gomock.Any()).Do(func(_ context.Context, _ string, _ any) { askEvt <- struct{}{} })
	emit.EXPECT().Emit(gomock.Any(), "orch:run:reply", gomock.Any())

	Convey("Ask emit ask 事件, reply 后 emit reply 事件", t, func() {
		go func() { _, _ = orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权?") }()
		<-askEvt
		askID := parseAskID(<-injCh)
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "ok"), ShouldBeNil)
		time.Sleep(50 * time.Millisecond) // 让 select 收到 reply 并 emit
	})
}
```
> 注:`detectAskCycle` 在无环时不 emit deadlock,故只 expect ask + reply 两个 Emit。若 mock 严格匹配次数有干扰,给 deadlock emit 加 `.AnyTimes()` 兜底:`emit.EXPECT().Emit(gomock.Any(), "orch:run:deadlock", gomock.Any()).AnyTimes()`。

- [ ] **Step 2: 跑确认失败**

Run: `go test -race -run TestAsk_EmitsAskAndReplyEvents ./internal/service/orch_svc/...`
Expected: FAIL — 未 emit ask/reply。

- [ ] **Step 3: 实现 emit**

`ask.go` 在注入成功后(`recordAskWait`/deadlock 检测之后、`select` 之前)加:
```go
	if s.emit != nil {
		s.emit.Emit(ctx, "orch:run:ask", map[string]any{
			"runId": from.RunID, "askId": askID,
			"askerAgentId": from.AgentID, "askerSessionId": fromSessionID,
			"targetAgentId": target.ID, "targetSessionId": toSession,
			"question": question,
		})
	}
```
把 `select` 改为在各分支 emit reply:
```go
	emitReply := func(answer string, timedOut bool) {
		if s.emit != nil {
			s.emit.Emit(ctx, "orch:run:reply", map[string]any{
				"runId": env.runID, "askId": askID, "answer": answer, "timedOut": timedOut,
			})
		}
	}
	select {
	case ans := <-env.reply:
		emitReply(ans, false)
		return ans, nil
	case <-ctx.Done():
		emitReply("", true)
		return "", ctx.Err()
	case <-timeAfter(s.approvalTimeout):
		emitReply("", true)
		return "", fmt.Errorf("orch.Ask: 等待 %s 回复超时", agentName)
	}
```

- [ ] **Step 4: 跑通过 + orch_svc 全包**

Run: `go test -race ./internal/service/orch_svc/...`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit internal/service/orch_svc/ask.go internal/service/orch_svc/ask_test.go \
  -m "✨ orch ask:emit orch:run:ask / orch:run:reply 事件(供前端渲染)"
```

---

## Task 4: 前端 events + `orch-run-store` 累积 ask 日志/在飞 ask

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/events.ts`
- Modify: `frontend/src/stores/orch-run-store.ts`
- Test: `frontend/src/stores/__tests__/orch-run-store.test.ts`(若无则新建)

**Interfaces:**
- Produces:
  ```ts
  // events.ts
  ask: "orch:run:ask"; reply: "orch:run:reply";
  // store
  askLog: Map<number, AskLogItem[]>;       // 时间线(append-only, 供 feed)
  activeAsks: Map<number, ActiveAsk[]>;    // 在飞(ask 无匹配 reply, 供 graph 徽标)
  // AskLogItem: { kind:"ask"|"reply"; askId; agentId; targetAgentId?; text; ts }
  // ActiveAsk: { askId; askerAgentId; targetAgentId }
  ```

- [ ] **Step 1: 写失败测试(store)**

`orch-run-store.test.ts`(对齐 deadlock 测试风格):
```ts
it("onRunEvent ask → activeAsks + askLog 增; reply → activeAsks 清、askLog 加 reply", () => {
  const s = useOrchRunStore.getState();
  s.onRunEvent("orch:run:ask", { runId: 1, askId: "k1", askerAgentId: 2, askerSessionId: 50, targetAgentId: 3, targetSessionId: 70, question: "鉴权?" } as never);
  expect(useOrchRunStore.getState().activeAsks.get(1)).toHaveLength(1);
  expect(useOrchRunStore.getState().askLog.get(1)).toHaveLength(1);
  s.onRunEvent("orch:run:reply", { runId: 1, askId: "k1", answer: "ok", timedOut: false } as never);
  expect(useOrchRunStore.getState().activeAsks.get(1) ?? []).toHaveLength(0);
  expect(useOrchRunStore.getState().askLog.get(1)).toHaveLength(2);
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-run-store.test.ts`
Expected: FAIL。

- [ ] **Step 3: events.ts 加键**

`ORCH_EVENTS` 加 `ask: "orch:run:ask"`、`reply: "orch:run:reply"`。

- [ ] **Step 4: 扩 orch-run-store**

`OrchRunState` 加 `askLog`/`activeAsks` 两 Map + 类型;初值空 Map;`onRunEvent` 加分支:
```ts
    if (name === ORCH_EVENTS.ask) {
      const p = payload as unknown as {
        runId: number; askId: string; askerAgentId: number; targetAgentId: number; question: string;
      };
      const log = new Map(get().askLog);
      const arr = [...(log.get(p.runId) ?? []), {
        kind: "ask" as const, askId: p.askId, agentId: p.askerAgentId,
        targetAgentId: p.targetAgentId, text: p.question, ts: Date.now(),
      }];
      log.set(p.runId, arr);
      const act = new Map(get().activeAsks);
      act.set(p.runId, [...(act.get(p.runId) ?? []), {
        askId: p.askId, askerAgentId: p.askerAgentId, targetAgentId: p.targetAgentId,
      }]);
      set({ askLog: log, activeAsks: act });
      return;
    }
    if (name === ORCH_EVENTS.reply) {
      const p = payload as unknown as { runId: number; askId: string; answer: string; timedOut: boolean };
      const act = new Map(get().activeAsks);
      const prevAsk = (get().activeAsks.get(p.runId) ?? []).find((a) => a.askId === p.askId);
      act.set(p.runId, (act.get(p.runId) ?? []).filter((a) => a.askId !== p.askId));
      const log = new Map(get().askLog);
      log.set(p.runId, [...(log.get(p.runId) ?? []), {
        kind: "reply" as const, askId: p.askId,
        agentId: prevAsk?.targetAgentId ?? 0, text: p.timedOut ? "" : p.answer, ts: Date.now(),
      }]);
      set({ askLog: log, activeAsks: act });
      return;
    }
```
`__reset` 清两 Map。
> `Date.now()` 仅前端时间线排序用(非 workflow 脚本,允许)。

- [ ] **Step 5: 跑通过**

Run: `cd frontend && pnpm test -- src/stores/__tests__/orch-run-store.test.ts`
Expected: PASS。

- [ ] **Step 6:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/events.ts \
  frontend/src/stores/orch-run-store.ts \
  frontend/src/stores/__tests__/orch-run-store.test.ts \
  -m "✨ orch 前端:store 累积 ask 日志/在飞 ask(orch:run:ask|reply)"
```

> ⚠ `OrchEventsHost` 已 `Object.values(ORCH_EVENTS).map(EventsOn)` 全量订阅 → 新增 ask/reply 事件**自动被订阅并路由** `onRunEvent`,无需改 `orch-events-host.tsx`。

---

## Task 5: `feed-data` 渲染 ask/reply + activity-feed 接线

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/feed-data.ts`
- Modify: `frontend/src/components/agentre/orchestration/activity-feed.tsx`
- Test: `__tests__/feed-data.test.ts`、`__tests__/activity-feed.test.tsx`

**Interfaces:**
- Produces:`buildFeed(detail, askLog?: AskLogItem[]): FeedItem[]`;新增 `FeedItem.kind` 值 `"reply"`(`"ask"` 已存在于类型)。

- [ ] **Step 1: 写失败测试(feed-data)**

`feed-data.test.ts` 追加:
```ts
it("askLog 合并进 feed: ask + reply 两条按 ts 排序", () => {
  const items = buildFeed({ tasks: [] } as never, [
    { kind: "ask", askId: "k", agentId: 2, targetAgentId: 3, text: "鉴权?", ts: 10 },
    { kind: "reply", askId: "k", agentId: 3, text: "ok", ts: 20 },
  ]);
  expect(items.map((i) => i.kind)).toEqual(["ask", "reply"]);
  expect(items[0].text).toBe("鉴权?");
});
```

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/feed-data.test.ts`
Expected: FAIL — buildFeed 不收第二参 / 无 reply 项。

- [ ] **Step 3: 实现 buildFeed 合并**

`feed-data.ts`:`FeedItem.kind` 联合加 `"reply"`;新增 `AskLogItem` 类型(与 store 一致);`buildFeed(detail, askLog = [])` 末尾把 askLog 映射进 items 后再统一 sort:
```ts
  for (const a of askLog) {
    items.push({
      id: `${a.kind}-${a.askId}`,
      kind: a.kind,
      agentId: a.agentId,
      text: a.text,
      ts: a.ts,
    });
  }
  return items.sort((a, b) => a.ts - b.ts);
```

- [ ] **Step 4: activity-feed 接线 + 渲染 ask/reply**

`activity-feed.tsx`:从 store 取该 run 的 `askLog` 传入 `buildFeed(detail, askLog)`;给 `ask`/`reply` kind 加图标/文案(走 i18n `orchestration.feed.ask`/`reply`,带 agent 名 + 动态 text 不翻译)。补/改 `activity-feed.test.tsx` 断言 ask/reply 行渲染(testid 如 `feed-ask-k` / 文案含问题)。
> `buildFeed` 既有调用方(只传 detail)因 `askLog = []` 默认值不破。

- [ ] **Step 5: 跑通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/feed-data.test.ts src/components/agentre/orchestration/__tests__/activity-feed.test.tsx`
Expected: PASS。

- [ ] **Step 6:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/feed-data.ts \
  frontend/src/components/agentre/orchestration/activity-feed.tsx \
  frontend/src/components/agentre/orchestration/__tests__/feed-data.test.ts \
  frontend/src/components/agentre/orchestration/__tests__/activity-feed.test.tsx \
  -m "✨ orch 活动流:渲染 peer ask/reply 事件"
```

---

## Task 6: 结构图「提问·等待回复」徽标(读 activeAsks)

**Files:**
- Modify: `frontend/src/components/agentre/orchestration/structure-graph.tsx`
- Test: `__tests__/structure-graph.test.tsx`

**Interfaces:**
- Consumes:`useOrchRunStore(s => s.activeAsks.get(runId))`。
- Produces:asker 节点徽标 `node-{askerAgentId}-asking`(文案 `t("orchestration.graph.askWaiting")`)。

- [ ] **Step 1: 写失败测试**

```ts
it("activeAsks 含某 asker → 其节点挂 提问·等待回复 徽标", () => {
  useOrchRunStore.setState({
    activeAsks: new Map([[1, [{ askId: "k", askerAgentId: 3, targetAgentId: 2 }]]]),
  });
  const detail = makeDetail({
    runId: 1, runStatus: "running",
    tasks: [makeTask(1, 2, "running"), makeTask(2, 3, "running", 1)],
  });
  render(<StructureGraph detail={detail} onSelectSession={vi.fn()} />);
  expect(screen.getByTestId("node-3-asking")).toBeInTheDocument();
});
```
> 注:`onSelectSession` 是 S6 后的 prop 名;若 S3 在 S6 之前执行,这里仍是 `onSelectNode`——**按实际执行顺序对齐 prop 名**(roadmap 顺序 S3 在 S6 之后则用 `onSelectSession`)。

- [ ] **Step 2: 跑确认失败**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: FAIL — `node-3-asking` 不存在。

- [ ] **Step 3: 实现徽标**

`StructureGraph` 读 `const activeAsks = useOrchRunStore((s) => (runId !== undefined ? s.activeAsks.get(runId) : undefined));`,算 `askingAgentIds = new Set(activeAsks?.map(a => a.askerAgentId))`,透传进 `NodeTree`→`NodeCard`(新增 prop `isAsking: boolean`)。`NodeCard` 头部(皇冠/×N/子代理徽标旁)加:
```tsx
        {isAsking && (
          <span
            data-testid={`node-${node.agentId}-asking`}
            className="shrink-0 rounded bg-status-waiting-bg px-1.5 py-0.5 text-xs text-status-waiting"
          >
            {t("orchestration.graph.askWaiting")}
          </span>
        )}
```
> 琥珀 `status-waiting` = 等待语义,与运行绿/死锁红区分(对齐 DESIGN.md 状态色)。

- [ ] **Step 4: 跑通过**

Run: `cd frontend && pnpm test -- src/components/agentre/orchestration/__tests__/structure-graph.test.tsx`
Expected: PASS。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/components/agentre/orchestration/structure-graph.tsx \
  frontend/src/components/agentre/orchestration/__tests__/structure-graph.test.tsx \
  -m "✨ orch 结构图:asker 节点 提问·等待回复 徽标(activeAsks)"
```

---

## Task 7: i18n + 全量校验

**Files:**
- Modify: `frontend/src/i18n/locales/{zh-CN,en}/common.json`

- [ ] **Step 1: 加 zh-CN 键**

`orchestration.feed` 内加 `"ask": "{{name}} 提问"`、`"reply": "{{name}} 回复"`;`orchestration.graph` 内加 `"askWaiting": "提问·等待回复"`。

- [ ] **Step 2: 加 en 键**

`"ask": "{{name}} asked"`、`"reply": "{{name}} replied"`;`"askWaiting": "Asking · waiting"`。

- [ ] **Step 3: i18n + 编排目录 + store 测试**

Run: `cd frontend && pnpm test -- src/__tests__/i18n.test.ts src/components/agentre/orchestration src/stores/__tests__/orch-run-store.test.ts`
Expected: PASS。

- [ ] **Step 4: 后端 + 前端 + lint 全量(看真 exit code)**

```bash
go test -race ./internal/service/orch_svc/... ./internal/app/...
cd frontend && pnpm test
cd /Users/codfrm/Code/agentre/agentre && make lint
```
Expected: 全绿。

- [ ] **Step 5:(门控)提交**

```bash
git commit frontend/src/i18n/locales/zh-CN/common.json frontend/src/i18n/locales/en/common.json \
  -m "🌐 orch:peer ask/reply feed + graph 徽标 i18n 双语键"
```

---

## Final verification (after all tasks)

- [ ] `go test -race ./internal/service/orch_svc/... ./internal/app/...` — 后端绿。
- [ ] `make test-frontend` + `make lint` — 前端绿。
- [ ] 人工对照设计稿 `Z2P0Vn`/`o6UQQ`(活动流含 peer ask/reply)+ `iBqBl`(前端·会话#1 带「向后端 提问·等待回复」徽标)。**真机**:起两个互相 ask 的 agent(一方 busy 时另一方 ask),看注入是否 steer 进当前 turn、feed/graph 是否显示(并入 S7 验证)。

## Self-review notes(写计划时已核对)

1. **Spec coverage(§5 item11 / §9 / roadmap S3)**:busy → Enqueue/steer(复用 setter)→ Task 1/2;`<peer_ask ask_id from>` XML 注入 → Task 1;feed 渲染 ask/reply → Task 4/5;结构图「提问·等待回复」徽标 → Task 6;4min 超时走既有分支 → Task 1(未改超时机制)。✅
2. **busy 检测方式(关键决策)**:不靠 `AgentStatus`(adapter 只反映上一轮完成态、测不出 running),改**先 Send 撞 `ChatSendInFlight` 再 Enqueue**——无 check-then-act 竞态,直接复用既有错误语义。adapter 用 `errors.As(*httputils.Error)` + `code.ChatSendInFlight` 映射哨兵(已核 i18n error 是 `*httputils.Error` 带导出 `Code`)。
3. **ask/reply 数据可用性(缺口已补)**:现状 ask/reply 既不在 `detail.tasks` 也不 emit 事件(只 deadlock emit)→ feed/graph 无从渲染。S3 **补 emit**(Task 3)+ store 累积(Task 4),镜像 deadlock 链路(`OrchEventsHost` 全量订阅,无需改 host)。
4. **askLog 持久性(已知限制,留 review)**:ask/reply 是 channel 临时态、不落 `orch_task`;`askLog` 在 store 内累积、`__reset`/切 Run 清空,**整页刷新会丢历史**。要 feed 时间线跨刷新持久,需后端把 ask/reply 落库(超出零后端范围)——本切片**不做**,feed 在活跃查看期内显示,够用。
5. **依赖/顺序**:Task 6 的 `onSelectSession` prop 名假设 S6 已先执行(roadmap S3 在 S6 之后);若调换顺序,Task 6 Step 1/3 用 `onSelectNode`。已在 Task 6 标注。
6. **Placeholder/类型一致性**:无 TODO;事件 payload 字段(askerAgentId/targetAgentId/askId/question/answer/timedOut)后端 emit(Task 3)与前端解析(Task 4)逐字对齐;`ErrSessionBusy`/`Enqueue` 在 deps/ask/adapter/mock 一致;testid `node-{id}-asking`、`feed-ask-{askId}` 实现与测试一致。
7. **mock 重生成**:deps.go 改接口 → 必须 `make mock`(Task 1 Step 3),否则 `mock_orch_svc` 不含 Enqueue、orch_svc 测试编译失败。
