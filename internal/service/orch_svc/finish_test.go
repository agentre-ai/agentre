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
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, ParentDispatchID: 0, SessionID: 500, Status: orch_entity.DispatchRunning}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		So(tk.Status, ShouldEqual, orch_entity.DispatchDone)
		So(tk.Summary, ShouldEqual, "全部完成,已交付")
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
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Dispatch{ID: 9, RunID: 100, ParentDispatchID: 0, SessionID: 500, Status: orch_entity.DispatchRunning}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		So(tk.Status, ShouldEqual, orch_entity.DispatchDone)
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

func TestFinish_NonRootRecordsResultNoReport(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, runs, tasks, nil, nil)

	// FindBySession → 非根任务(ID:11 ≠ RootTaskID:9)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, ParentDispatchID: 9, SessionID: 600, Status: orch_entity.DispatchRunning},
		nil,
	)
	// runs.Find → RootTaskID=9(≠ tk.ID=11,即非根)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, Status: orch_entity.RunRunning},
		nil,
	)
	// 非根 finish 只「记录」显式小结:Update 写入 Summary + done,且只调用一次。
	var capturedStatus, capturedSummary string
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Dispatch) error {
		So(tk.ID, ShouldEqual, int64(11))
		capturedStatus = tk.Status
		capturedSummary = tk.Summary
		return nil
	})

	// 关键回归:非根 finish 不得回报父(watcher 才是唯一回报者),也不得收口 Run。
	// gomock 严格控制器会因任何未声明的调用(SendAndForget/runs.Update/Find/ListByRun)而失败。

	Convey("非根 finish → 只记录 Summary+done,不回报父、不收口 Run", t, func() {
		err := orch_svc.Default().Finish(context.Background(), 600, "子任务完成小结")
		So(err, ShouldBeNil)
		So(capturedStatus, ShouldEqual, orch_entity.DispatchDone)
		So(capturedSummary, ShouldEqual, "子任务完成小结")
	})
}
