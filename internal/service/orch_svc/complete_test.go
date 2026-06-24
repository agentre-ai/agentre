package orch_svc_test

import (
	"context"
	"testing"

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
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	// capture values set by watchCompletion so we can assert them in Convey context.
	var capturedChildStatus, capturedChildResult string
	var capturedSendMsg string

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().ObserveTurn(int64(600)).Return((<-chan orch_svc.TurnDone)(turnCh), func() {})
	chat.EXPECT().AgentStatus(gomock.Any(), int64(600)).Return("idle", nil)
	chat.EXPECT().FinalAssistantText(gomock.Any(), int64(600)).Return("登录表单已实现,见 src/login.tsx", nil)

	child := &orch_entity.Task{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentTaskID: 9, CallSeq: 1, Status: orch_entity.TaskRunning}

	// 子任务标 done + 写 Result。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		if tk.ID == 11 {
			capturedChildStatus = tk.Status
			capturedChildResult = tk.Result
		}
		return nil
	}).AnyTimes()
	// 取父任务(用于唤醒 + 状态翻回)。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskAwaitingChildren}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 11, ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
	}, nil)
	// 报告注入父会话，唤醒决策轮。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedSendMsg = msg
		return nil
	})

	Convey("子任务完成 → 标 done + 报告回报父会话续轮", t, func() {
		done := make(chan struct{})
		go func() { orch_svc.Default().WatchCompletionForTest(context.Background(), child); close(done) }()
		turnCh <- orch_svc.TurnDone{SessionID: 600, OK: true}
		close(turnCh)
		<-done

		So(capturedChildStatus, ShouldEqual, orch_entity.TaskDone)
		So(capturedChildResult, ShouldContainSubstring, "登录表单已实现")
		So(capturedSendMsg, ShouldContainSubstring, "登录表单已实现")
	})
}

// TestAllChildrenSettled_Boundaries — allChildrenSettled 边界用例（通过 export_test.go 包内包装）。
func TestAllChildrenSettled_Boundaries(t *testing.T) {
	parent := &orch_entity.Task{ID: 9, RunID: 100}

	Convey("Case A: 全部 dispatch 子任务已终态 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
			{ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})

	Convey("Case B: 有一个未终态 dispatch 子任务 → false", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
			{ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning},
			{ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeFalse)
		ctrl.Finish()
	})

	Convey("Case C: 未终态 dispatch 子任务属于不同父任务 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
			{ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
			{ParentTaskID: 7, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})

	Convey("Case D: 未终态子任务为非 dispatch 类型 → true", t, func() {
		ctrl := gomock.NewController(t)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
		orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
			{ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
			{ParentTaskID: 9, Kind: orch_entity.TaskKindAsk, Status: orch_entity.TaskRunning},
		}, nil)
		So(orch_svc.Default().AllChildrenSettledForTest(context.Background(), parent), ShouldBeTrue)
		ctrl.Finish()
	})
}

// TestWatchCompletion_TechnicalErrorEscalates — AgentStatus 返回 "error" 时子任务标 error 并上抛父会话。
func TestWatchCompletion_TechnicalErrorEscalates(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	var capturedChildStatus string
	var capturedSendMsg string

	turnCh := make(chan orch_svc.TurnDone, 1)
	chat.EXPECT().ObserveTurn(int64(601)).Return((<-chan orch_svc.TurnDone)(turnCh), func() {})
	chat.EXPECT().AgentStatus(gomock.Any(), int64(601)).Return("error", nil)

	child := &orch_entity.Task{ID: 12, RunID: 200, AgentID: 4, SessionID: 601, ParentTaskID: 20, CallSeq: 1, Status: orch_entity.TaskRunning}

	// 子任务标 error。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		if tk.ID == 12 {
			capturedChildStatus = tk.Status
		}
		return nil
	}).AnyTimes()
	// reportToParent: 取父任务。
	tasks.EXPECT().Find(gomock.Any(), int64(20)).Return(&orch_entity.Task{ID: 20, RunID: 200, SessionID: 700, Status: orch_entity.TaskRunning}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(200)).Return([]*orch_entity.Task{
		{ID: 12, ParentTaskID: 20, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskError},
	}, nil)
	// 错误上抛消息包含"技术中断"。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedSendMsg = msg
		return nil
	})

	Convey("子任务 error → 标 error + 上抛技术中断消息到父会话", t, func() {
		done := make(chan struct{})
		go func() { orch_svc.Default().WatchCompletionForTest(context.Background(), child); close(done) }()
		turnCh <- orch_svc.TurnDone{SessionID: 601, OK: false}
		close(turnCh)
		<-done

		So(capturedChildStatus, ShouldEqual, orch_entity.TaskError)
		So(capturedSendMsg, ShouldContainSubstring, "技术中断")
	})
}
