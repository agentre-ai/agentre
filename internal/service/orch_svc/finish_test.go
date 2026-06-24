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

func TestFinish_RootCollapsesRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, ParentTaskID: 0, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.Status, ShouldEqual, orch_entity.TaskDone)
		So(tk.Result, ShouldEqual, "全部完成,已交付")
		return nil
	})
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunDone)
		return nil
	})

	Convey("根 finish 收口整个 Run", t, func() {
		So(orch_svc.Default().Finish(context.Background(), 500, "全部完成,已交付"), ShouldBeNil)
	})
}

func TestFinish_RootEmitsRunDoneEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, ParentTaskID: 0, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.Status, ShouldEqual, orch_entity.TaskDone)
		return nil
	})
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunDone)
		return nil
	})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:done", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, payload any) {
		m, ok := payload.(map[string]any)
		So(ok, ShouldBeTrue)
		So(m["runId"], ShouldEqual, int64(100))
	})

	Convey("根 finish 发出 orch:run:done 事件并携带 runId", t, func() {
		So(orch_svc.Default().Finish(context.Background(), 500, "全部完成,已交付"), ShouldBeNil)
	})
}

func TestFinish_NonRootReportsToParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, nil)

	// FindBySession → non-root task (ID:11 ≠ RootTaskID:9)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, ParentTaskID: 9, SessionID: 600, Status: orch_entity.TaskRunning},
		nil,
	)
	// runs.Find → run with RootTaskID=9 (≠ tk.ID=11, so NOT root)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning},
		nil,
	)
	// tasks.Update called once to mark the non-root task done
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.Status, ShouldEqual, orch_entity.TaskDone)
		return nil
	})
	// runs.Update must NOT be called (non-root finish does NOT collapse the Run)
	// gomock strict controller enforces this: any unexpected call will fail the test.

	// reportToParent: fetch parent task
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskAwaitingChildren},
		nil,
	)
	// reportToParent: check all children settled
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return(
		[]*orch_entity.Task{
			{ID: 11, ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskDone},
		},
		nil,
	)
	// allChildrenSettled=true → parent flips to running (2nd tasks.Update)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ID, ShouldEqual, int64(9))
		So(tk.Status, ShouldEqual, orch_entity.TaskRunning)
		return nil
	})
	// reportToParent sends message to parent session containing the summary
	var capturedMsg string
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		capturedMsg = msg
		return nil
	})

	Convey("非根 finish → 回报父会话, Run 不收口", t, func() {
		err := orch_svc.Default().Finish(context.Background(), 600, "子任务完成小结")
		So(err, ShouldBeNil)
		So(capturedMsg, ShouldContainSubstring, "子任务完成小结")
	})
}
