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

func TestReadTask_ReturnsSettledResult(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, Status: orch_entity.DispatchDone, Summary: "小结", Result: "完整正文见 src/x.go"}, nil)

	Convey("read 返回 settled 任务的 Summary + 完整 Result", t, func() {
		out, err := orch_svc.Default().ReadDispatch(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(strings.Contains(out, "完整正文见 src/x.go"), ShouldBeTrue)
		So(strings.Contains(out, "小结"), ShouldBeTrue)
	})
}

func TestReadTask_RunningPeek(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, SessionID: 800, Status: orch_entity.DispatchRunning}, nil)
	chat.EXPECT().LatestAssistantText(gomock.Any(), int64(800)).Return("我正在写测试", nil)

	Convey("read 撞 running 任务 → 取会话当前最新 assistant 文本(peek)", t, func() {
		out, err := orch_svc.Default().ReadDispatch(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, "我正在写测试")
	})
}

func TestReadTask_RunningPeek_Empty(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, SessionID: 800, Status: orch_entity.DispatchRunning}, nil)
	chat.EXPECT().LatestAssistantText(gomock.Any(), int64(800)).Return("", nil)

	Convey("read 撞 running 且尚无输出 → 兜底文案", t, func() {
		out, err := orch_svc.Default().ReadDispatch(context.Background(), 500, 11)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, "尚无输出")
	})
}

func TestReadTask_RejectsForeignRunTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(77)).Return(
		&orch_entity.Dispatch{ID: 77, RunID: 999, Status: orch_entity.DispatchDone}, nil) // 别的 Run

	Convey("read 跨 Run 任务 → 拒绝", t, func() {
		_, err := orch_svc.Default().ReadDispatch(context.Background(), 500, 77)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in this run")
	})
}
