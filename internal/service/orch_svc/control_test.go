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

func TestStopRun_CascadeCancels(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, emit)
	t.Cleanup(func() { orch_svc.Default().ResetSchedulersForTest() })

	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, Status: orch_entity.RunRunning}, nil)
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunStopped)
		return nil
	})
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, Status: orch_entity.TaskRunning}, {ID: 2, Status: orch_entity.TaskDone},
	}, nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ID, ShouldEqual, 1) // 只取消活任务，不动已 done
		So(tk.Status, ShouldEqual, orch_entity.TaskCanceled)
		return nil
	})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:stopped", gomock.Any())

	Convey("StopRun 级联取消活任务", t, func() {
		So(orch_svc.Default().StopRun(context.Background(), 100), ShouldBeNil)
	})
}

func TestPauseRun_SetsPausedAndGates(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, nil, nil, emit)
	orch_svc.Default().ResetSchedulersForTest()
	t.Cleanup(func() { orch_svc.Default().ResetSchedulersForTest() })

	// PauseRun: Find → running, Update → paused, emit paused
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, Status: orch_entity.RunRunning}, nil)
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunPaused)
		return nil
	})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:paused", gomock.Any())

	// ResumeRun: Find → paused (or running), Update → running, emit resumed
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, Status: orch_entity.RunPaused}, nil)
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.Status, ShouldEqual, orch_entity.RunRunning)
		return nil
	})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:resumed", gomock.Any())

	Convey("PauseRun sets paused flag; ResumeRun clears it", t, func() {
		So(orch_svc.Default().PauseRun(context.Background(), 100), ShouldBeNil)
		So(orch_svc.Default().SchedulerPausedForTest(100), ShouldBeTrue)

		So(orch_svc.Default().ResumeRun(context.Background(), 100), ShouldBeNil)
		So(orch_svc.Default().SchedulerPausedForTest(100), ShouldBeFalse)
	})
}

func TestPauseRun_RunNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, nil, nil, nil)

	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(nil, nil)

	Convey("PauseRun returns errRunNotActive when run not found", t, func() {
		err := orch_svc.Default().PauseRun(context.Background(), 100)
		So(err, ShouldNotBeNil)
	})
}
