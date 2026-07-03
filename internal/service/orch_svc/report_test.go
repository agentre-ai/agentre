package orch_svc_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestReport_InjectsInterimReportToParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	// 调用者子任务(有父 9)。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Task{ID: 11, RunID: 100, AgentID: 3, SessionID: 600, ParentTaskID: 9, CallSeq: 2, Status: orch_entity.TaskRunning}, nil)
	// injectToParent 取父 + 判定 settled(此处父仍 running,不翻转)。
	tasks.EXPECT().Find(gomock.Any(), int64(9)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 11, ParentTaskID: 9, Kind: orch_entity.TaskKindDispatch, Status: orch_entity.TaskRunning},
	}, nil).AnyTimes()

	var msg string
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error {
		msg = m
		return nil
	})

	Convey("report 中途向父注入 task_report(final=false),不改状态", t, func() {
		err := orch_svc.Default().Report(context.Background(), 600, "进度:表单已搭好,正在接接口")
		So(err, ShouldBeNil)
		So(strings.Contains(msg, `<task_report`), ShouldBeTrue)
		So(strings.Contains(msg, `final="false"`), ShouldBeTrue)
		So(strings.Contains(msg, "正在接接口"), ShouldBeTrue)
	})
}
