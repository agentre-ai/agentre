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

func TestCancelTask_CascadesAndAborts(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(2)).Return(
		&orch_entity.Task{ID: 2, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning, ParentTaskID: 1}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 2, RunID: 100, SessionID: 800, Status: orch_entity.TaskRunning, ParentTaskID: 1, Kind: "dispatch"},
		{ID: 3, RunID: 100, SessionID: 801, Status: orch_entity.TaskRunning, ParentTaskID: 2, Kind: "dispatch"}, // 子孙
		{ID: 4, RunID: 100, SessionID: 802, Status: orch_entity.TaskDone, ParentTaskID: 2, Kind: "dispatch"},    // 已终态,跳过
	}, nil)
	// #2 #3 被标 canceled + AbortTurn;#4 已 done 跳过。
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	chat.EXPECT().AbortTurn(gomock.Any(), int64(800)).Return(nil)
	chat.EXPECT().AbortTurn(gomock.Any(), int64(801)).Return(nil)
	emit.EXPECT().Emit(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()

	Convey("cancel 级联标记子孙活任务 + 尽力硬打断,返回取消数", t, func() {
		n, err := orch_svc.Default().CancelTask(context.Background(), 500, 2)
		So(err, ShouldBeNil)
		So(n, ShouldEqual, 2)
	})
}

func TestCancelTask_RejectsForeignRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(77)).Return(
		&orch_entity.Task{ID: 77, RunID: 999}, nil)

	Convey("cancel 跨 Run 任务 → 拒绝", t, func() {
		_, err := orch_svc.Default().CancelTask(context.Background(), 500, 77)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in this run")
	})
}
