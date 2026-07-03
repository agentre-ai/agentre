package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestCreateRun_BuildsRunRootSessionAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)

	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "架构师"}, nil)
	// 先建 Run（拿 RunID）→ 建根会话 → 建根 Task → 回填 RootTaskID。
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		So(in.RunID, ShouldEqual, 100)
		So(in.ParentSessionID, ShouldEqual, 0)
		So(in.AgentID, ShouldEqual, 2)
		// 根会话标题 = Run 目标, 避免侧栏显示「(未命名会话)」。
		So(in.Title, ShouldEqual, "做个登录页")
		return 500, nil
	})
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.SessionID, ShouldEqual, 500)
		So(tk.Kind, ShouldEqual, orch_entity.TaskKindDispatch)
		tk.ID = 9
		return nil
	})
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	// 用流程注入触发 Leader 首轮。
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("CreateRun 建 Run + 根会话 + 根 Task 并触发 Leader 首轮", t, func() {
		got, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "做个登录页", LeaderAgentID: 2, FlowContent: "先拆分再并行",
		})
		So(err, ShouldBeNil)
		So(got.Run.ID, ShouldEqual, 100)
		So(got.Run.RootTaskID, ShouldEqual, 9)
		So(got.Run.Status, ShouldEqual, orch_entity.RunRunning)
	})
}

func TestCreateRun_PersistsAllowedAgentIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "L"}, nil)
	runs.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, r *orch_entity.OrchestrationRun) error {
		So(r.AllowedAgentIDs, ShouldEqual, "[3,4]") // 去重剔零后 JSON
		r.ID = 100
		return nil
	})
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(500), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error { tk.ID = 9; return nil })
	runs.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(500), gomock.Any()).Return(nil)

	Convey("CreateRun 把 allowedAgentIds 去重剔零后落库", t, func() {
		_, err := orch_svc.Default().CreateRun(context.Background(), &orch_svc.CreateRunRequest{
			Goal: "g", LeaderAgentID: 2, AllowedAgentIDs: []int64{3, 4, 3, 0},
		})
		So(err, ShouldBeNil)
	})
}
