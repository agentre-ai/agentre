package orch_svc_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestWatchCompletion_ReportsToParentAndMarksDone(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	// capture values set by watchCompletion so we can assert them in Convey context.
	var capturedChildStatus, capturedChildResult string
	var capturedSendMsg string

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().AgentStatus(gomock.Any(), int64(600)).Return("idle", nil)
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(600)).Return("登录表单已实现,见 src/login.tsx", nil)

	child := &orch_entity.Dispatch{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentDispatchID: 9, CallSeq: 1, Status: orch_entity.DispatchRunning}

	// idle 分支重读子任务:无显式 finish 小结(Result:"", Summary:"")→ 退回 FinalAssistantText 走 ping。
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, SessionID: 600, Status: orch_entity.DispatchRunning, Result: "", Summary: ""}, nil)
	// 子任务标 done + 写 Result。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		if tk.ID == 11 {
			capturedChildStatus = tk.Status
			capturedChildResult = tk.Result
		}
		return nil
	}).AnyTimes()
	// 取父任务(用于唤醒 + 状态翻回)。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.DispatchAwaitingChildren}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
		{ID: 11, ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
	}, nil)
	// 报告注入父会话，唤醒决策轮。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedSendMsg = msg
		return nil
	})

	Convey("子任务完成 → 标 done + 报告回报父会话续轮", t, func() {
		done := make(chan struct{})
		go func() {
			orch_svc.Default().WatchCompletionForTest(context.Background(), child, (<-chan orch_svc.TurnDone)(turnCh), func() {})
			close(done)
		}()
		turnCh <- orch_svc.TurnDone{SessionID: 600, OK: true}
		close(turnCh)
		<-done

		So(capturedChildStatus, ShouldEqual, orch_entity.DispatchDone)
		So(capturedChildResult, ShouldContainSubstring, "登录表单已实现")
		So(capturedSendMsg, ShouldContainSubstring, "登录表单已实现")
		So(capturedSendMsg, ShouldContainSubstring, "<task_done")
		So(capturedSendMsg, ShouldContainSubstring, "read(task_id=11)")
	})
}

// TestAllChildrenSettled_Boundaries — allChildrenSettled 边界用例（通过 export_test.go 包内包装）。
func TestAllChildrenSettled_Boundaries(t *testing.T) {
	parent := &orch_entity.Dispatch{ID: 9, RunID: 100}

	Convey("Case A: 全部 dispatch 子任务已终态 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})

	Convey("Case B: 有一个未终态 dispatch 子任务 → false", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchRunning},
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeFalse)
		ctrl.Finish()
	})

	Convey("Case C: 未终态 dispatch 子任务属于不同父任务 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
			{ParentDispatchID: 7, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchRunning},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})

	Convey("Case D: 未终态子任务为非 dispatch 类型 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
			{ParentDispatchID: 9, Kind: orch_entity.DispatchKindAsk, Status: orch_entity.DispatchRunning},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})
}

// TestWatchCompletion_ReleasesSlotOnChannelClose — channel 关闭但无 idle/error 事件时槽仍被释放（Fix 1 回归）。
// 与 cap=1 配合：若槽未释放，第二个任务永远不能发射，sendCh 的第二次读将永久阻塞。
func TestWatchCompletion_ReleasesSlotOnChannelClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(1)
	t.Cleanup(func() {
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	sendCh := make(chan struct{}, 4)
	chat.EXPECT().SendAndForget(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ int64, _ string) error {
			sendCh <- struct{}{}
			return nil
		}).AnyTimes()

	// ObserveTurn 返回已关闭的 channel：watchCompletion 的 range 立即退出，没有 idle/error 事件。
	closed := make(chan orch_svc.TurnDone)
	close(closed)
	chat.EXPECT().ObserveTurn(gomock.Any()).
		Return((<-chan orch_svc.TurnDone)(closed), func() {}).AnyTimes()

	// Update 可能不被调用（channel-close 路径跳过了 idle/error 分支），AnyTimes 保持松散。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	Convey("channel 关闭（无 idle/error）→ 槽释放，第二个任务可发射", t, func() {
		orch_svc.Default().EnqueueRunForTest(200,
			&orch_entity.Dispatch{ID: 1, RunID: 200, SessionID: 701},
			"go")
		orch_svc.Default().EnqueueRunForTest(200,
			&orch_entity.Dispatch{ID: 2, RunID: 200, SessionID: 702},
			"go")

		// 两个任务都必须发射：先等第一个 SendAndForget（任务 A），
		// 它的 watchCompletion 因 closed channel 立即退出释放槽，然后任务 B 也发射。
		for i := range 2 {
			select {
			case <-sendCh:
			case <-time.After(time.Second):
				t.Fatalf("task %d did not launch within 1s — slot was likely leaked", i+1)
			}
		}
	})
}

// TestWatchCompletion_TechnicalErrorEscalates — AgentStatus 返回 "error" 时子任务标 error 并上抛父会话。
func TestWatchCompletion_TechnicalErrorEscalates(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	var capturedChildStatus string
	var capturedSendMsg string

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().AgentStatus(gomock.Any(), int64(601)).Return("error", nil)

	child := &orch_entity.Dispatch{ID: 12, RunID: 200, AgentID: 4, SessionID: 601, ParentDispatchID: 20, CallSeq: 1, Status: orch_entity.DispatchRunning}

	// 子任务标 error。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		if tk.ID == 12 {
			capturedChildStatus = tk.Status
		}
		return nil
	}).AnyTimes()
	// error 分支让位守卫:重读子任务,非 canceled → 继续标 error。
	tasks.EXPECT().Find(gomock.Any(), int64(12)).Return(&orch_entity.Dispatch{ID: 12, RunID: 200, Status: orch_entity.DispatchRunning}, nil)
	// reportToParent: 取父任务。
	tasks.EXPECT().Find(gomock.Any(), int64(20)).Return(&orch_entity.Dispatch{ID: 20, RunID: 200, SessionID: 700, Status: orch_entity.DispatchRunning}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(200)).Return([]*orch_entity.Dispatch{
		{ID: 12, ParentDispatchID: 20, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchError},
	}, nil)
	// 错误上抛消息包含"技术中断"。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedSendMsg = msg
		return nil
	})

	Convey("子任务 error → 标 error + 上抛技术中断消息到父会话", t, func() {
		done := make(chan struct{})
		go func() {
			orch_svc.Default().WatchCompletionForTest(context.Background(), child, (<-chan orch_svc.TurnDone)(turnCh), func() {})
			close(done)
		}()
		turnCh <- orch_svc.TurnDone{SessionID: 601, OK: false}
		close(turnCh)
		<-done

		So(capturedChildStatus, ShouldEqual, orch_entity.DispatchError)
		So(capturedSendMsg, ShouldContainSubstring, "<task_error")
		So(capturedSendMsg, ShouldContainSubstring, "运行时崩溃")
	})
}

// TestWatchCompletion_PrefersFinishSummary — 子任务已被 agent 显式 finish(Summary 已落库)时,
// watcher 的 idle 分支据 Summary 内联 task_report 作为回报正文(Result 始终落末条正文供 read);
// FinalAssistantText 不作为回报正文(C1:finish 与 watcher 不再各回报一次,watcher 是唯一回报者且认显式小结)。
func TestWatchCompletion_PrefersFinishSummary(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	const finishSummary = "已完成登录页并自测通过(finish 显式小结)"
	var capturedResult, capturedSendMsg string

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().AgentStatus(gomock.Any(), int64(600)).Return("idle", nil)
	// FinalAssistantText 始终落 Result(供 read),但本例 fresh.Summary 非空 → 回报正文取 Summary。
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(600)).Return("末条 assistant 正文(不应被采用)", nil).AnyTimes()
	// idle 分支重读子任务:Summary 已由先前 finish 写入。
	tasks.EXPECT().Find(gomock.Any(), int64(13)).Return(
		&orch_entity.Dispatch{ID: 13, RunID: 100, SessionID: 600, Status: orch_entity.DispatchDone, Summary: finishSummary}, nil)

	child := &orch_entity.Dispatch{ID: 13, RunID: 100, AgentID: 3, SessionID: 600, ParentDispatchID: 9, CallSeq: 1, Status: orch_entity.DispatchRunning}

	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		if tk.ID == 13 {
			capturedResult = tk.Result
		}
		return nil
	}).AnyTimes()
	// reportToParent: 取父 + 判定全部 settled。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.DispatchAwaitingChildren}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
		{ID: 13, ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
	}, nil)
	// watcher 恰好回报一次,正文为 finish 小结。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedSendMsg = msg
		return nil
	}).Times(1)

	Convey("idle 时优先采用已落库的 finish 小结内联 task_report 回报", t, func() {
		done := make(chan struct{})
		go func() {
			orch_svc.Default().WatchCompletionForTest(context.Background(), child, (<-chan orch_svc.TurnDone)(turnCh), func() {})
			close(done)
		}()
		turnCh <- orch_svc.TurnDone{SessionID: 600, OK: true}
		close(turnCh)
		<-done

		// Result 始终落末条正文(供 read);Summary 决定回报正文。
		So(capturedResult, ShouldContainSubstring, "末条 assistant 正文")
		So(capturedSendMsg, ShouldContainSubstring, "<task_report")
		So(capturedSendMsg, ShouldContainSubstring, "final=\"true\"")
		So(capturedSendMsg, ShouldContainSubstring, finishSummary)
		So(capturedSendMsg, ShouldNotContainSubstring, "不应被采用")
	})
}

// TestWatchCompletion_ParentFlipEmitsRunUpdated — done 分支触发父翻转（awaiting-children → running）
// 时,emitRunUpdated 必须在父 Update 之后再次发出,让前端能取到最新的父状态。
// 本测试使用真实 MockEmitter 断言父翻转路径确实发出了 orch:run:updated。
// TDD: 先跑见 RED(父翻转后无 Emit 调用, MinTimes(2) 不满足)，补发 emit 后见 GREEN。
func TestWatchCompletion_ParentFlipEmitsRunUpdated(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	// 注入真实 emit mock，断言父翻转路径的 emit 调用。
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, emit)

	turnCh := make(chan orch_svc.TurnDone, 1)
	child := &orch_entity.Dispatch{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentDispatchID: 9, CallSeq: 1, Status: orch_entity.DispatchRunning}

	chat.EXPECT().AgentStatus(gomock.Any(), int64(600)).Return("idle", nil)
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(600)).Return("登录表单已实现", nil)
	// idle 分支重读子任务：Result:"", Summary:"" → 退回 FinalAssistantText 走 ping。
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, SessionID: 600, Status: orch_entity.DispatchRunning, Result: "", Summary: ""}, nil)
	// 子任务标 done。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	// 父任务翻转：awaiting-children → running（父翻转路径触发条件）。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.DispatchAwaitingChildren}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Dispatch{
		{ID: 11, ParentDispatchID: 9, Kind: orch_entity.DispatchKindDispatch, Status: orch_entity.DispatchDone},
	}, nil)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	// 期望 emit 至少被调用两次：
	//   1. idle 分支本身的 emitRunUpdated（complete.go:43）
	//   2. 父翻转成功后的 emitRunUpdated（Finding 1 修复点）
	// 不能在 DoAndReturn 中调用 So（goroutine 外无 Convey 上下文），改用原子计数捕获。
	var emitCount int64
	emit.EXPECT().Emit(gomock.Any(), "orch:run:updated", gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, payload any) {
			atomic.AddInt64(&emitCount, 1)
		}).MinTimes(2)

	Convey("父翻转（awaiting-children→running）后额外发出 orch:run:updated", t, func() {
		done := make(chan struct{})
		go func() {
			orch_svc.Default().WatchCompletionForTest(context.Background(), child, (<-chan orch_svc.TurnDone)(turnCh), func() {})
			close(done)
		}()
		turnCh <- orch_svc.TurnDone{SessionID: 600, OK: true}
		close(turnCh)
		<-done
		// 父翻转路径应触发至少 2 次 emit（子 done 1 次 + 父翻转后 1 次）。
		So(atomic.LoadInt64(&emitCount), ShouldBeGreaterThanOrEqualTo, 2)
	})
}

// TestWatchCompletion_YieldsToCanceled — watcher 见任务已被取消 → 让位:不覆盖状态、不回报父。
func TestWatchCompletion_YieldsToCanceled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().SetSchedulerCapForTest(1)
	t.Cleanup(func() { orch_svc.Default().ResetSchedulersForTest(); orch_svc.Default().SetSchedulerCapForTest(0) })

	task := &orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 800, Status: orch_entity.DispatchRunning}
	// 会话 abort → 状态 error;watcher 重读 fresh 发现已被取消 → 让位。
	chat.EXPECT().AgentStatus(gomock.Any(), int64(800)).Return("error", nil)
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, Status: orch_entity.DispatchCanceled}, nil)
	// 关键:被取消 → 不得 Update、不得 injectToParent(无其它 mock EXPECT 即验证)。

	ch := make(chan orch_svc.TurnDone, 1)
	ch <- orch_svc.TurnDone{SessionID: 800, OK: false}
	close(ch)

	Convey("watcher 见任务已取消 → 让位:不覆盖状态、不回报父", t, func() {
		orch_svc.Default().WatchCompletionForTest(context.Background(), task, ch, func() {})
		// 无 tasks.Update / chat.SendAndForget 期望被调用即通过(gomock 严格模式会在多余调用时 fail)。
	})
}

// TestWatchCompletion_SubscribeBeforeSend — 走真实 kick 路径,断言 ObserveTurn 的订阅
// 早于 SendAndForget(I1:ObserveTurn 契约要求订阅先于 turn 启动,否则快 turn 终态回执丢失)。
func TestWatchCompletion_SubscribeBeforeSend(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(1)
	t.Cleanup(func() {
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	var mu sync.Mutex
	var order []string
	done := make(chan struct{})

	// 已关闭的 channel:watchCompletion 的 range 立即退出(无 idle/error),不触碰 tasks。
	closed := make(chan orch_svc.TurnDone)
	close(closed)
	chat.EXPECT().ObserveTurn(int64(800)).DoAndReturn(func(_ int64) (<-chan orch_svc.TurnDone, func()) {
		mu.Lock()
		order = append(order, "observe")
		mu.Unlock()
		return (<-chan orch_svc.TurnDone)(closed), func() {}
	})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(800), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, _ string) error {
		mu.Lock()
		order = append(order, "send")
		mu.Unlock()
		close(done)
		return nil
	})

	Convey("kick 路径下 ObserveTurn 订阅早于 SendAndForget", t, func() {
		orch_svc.Default().EnqueueRunForTest(300,
			&orch_entity.Dispatch{ID: 1, RunID: 300, SessionID: 800}, "go")
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("SendAndForget 未在 1s 内发生")
		}
		mu.Lock()
		defer mu.Unlock()
		So(len(order), ShouldEqual, 2)
		So(order[0], ShouldEqual, "observe")
		So(order[1], ShouldEqual, "send")
	})
}
