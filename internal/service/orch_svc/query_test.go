package orch_svc_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestListRuns(t *testing.T) {
	Convey("ListRuns 返回所有 Run", t, func() {
		// mock 依赖与 EXPECT 均在 Convey 闭包内，与 TestLoadRun 风格保持一致。
		ctrl := gomock.NewController(t)

		chat := mock_orch_svc.NewMockChatGateway(ctrl)
		agents := mock_orch_svc.NewMockAgentLookup(ctrl)
		runs := mock_orch_repo.NewMockRunRepo(ctrl)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)

		orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

		run1 := &orch_entity.OrchestrationRun{ID: 1, Goal: "目标A", Status: orch_entity.RunRunning, ProjectID: 10}
		run2 := &orch_entity.OrchestrationRun{ID: 2, Goal: "目标B", Status: orch_entity.RunDone, ProjectID: 10}

		runs.EXPECT().List(gomock.Any()).Return([]*orch_entity.OrchestrationRun{run1, run2}, nil)

		got, err := orch_svc.Default().ListRuns(context.Background())
		So(err, ShouldBeNil)
		So(len(got), ShouldEqual, 2)
		So(got[0].ID, ShouldEqual, 1)
		So(got[1].ID, ShouldEqual, 2)

		ctrl.Finish()
	})
}

func TestListAllowedAgents_FiltersToAllowedSetPlusLeader(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
		{ID: 2, Name: "L"}, {ID: 3, Name: "A"}, {ID: 4, Name: "B"}, {ID: 9, Name: "X"},
	}, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{RunID: 100}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2, AllowedAgentIDs: "[3,4]"}, nil)

	Convey("agent_list 只回 allowed∪{leader}", t, func() {
		got, err := orch_svc.Default().ListAllowedAgents(context.Background(), 500)
		So(err, ShouldBeNil)
		ids := []int64{}
		for _, a := range got {
			ids = append(ids, a.ID)
		}
		So(ids, ShouldResemble, []int64{2, 3, 4}) // 保 List 顺序，排除 9
	})
}

func TestListAllowedAgents_EmptySetReturnsAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{{ID: 3}, {ID: 9}}, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{RunID: 100}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, AllowedAgentIDs: ""}, nil)

	Convey("空集合 → 回全部", t, func() {
		got, err := orch_svc.Default().ListAllowedAgents(context.Background(), 500)
		So(err, ShouldBeNil)
		So(len(got), ShouldEqual, 2)
	})
}

func TestLoadRun(t *testing.T) {
	Convey("LoadRun", t, func() {
		Convey("找到 Run 时返回 Run+Tasks", func() {
			ctrl := gomock.NewController(t)

			chat := mock_orch_svc.NewMockChatGateway(ctrl)
			agents := mock_orch_svc.NewMockAgentLookup(ctrl)
			runs := mock_orch_repo.NewMockRunRepo(ctrl)
			tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
			orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

			run := &orch_entity.OrchestrationRun{ID: 42, Goal: "做个登录页", Status: orch_entity.RunRunning}
			task1 := &orch_entity.Task{ID: 1, RunID: 42, Kind: orch_entity.TaskKindDispatch}
			task2 := &orch_entity.Task{ID: 2, RunID: 42, Kind: orch_entity.TaskKindAsk}

			runs.EXPECT().Find(gomock.Any(), int64(42)).Return(run, nil)
			tasks.EXPECT().ListByRun(gomock.Any(), int64(42)).Return([]*orch_entity.Task{task1, task2}, nil)

			got, err := orch_svc.Default().LoadRun(context.Background(), 42)
			So(err, ShouldBeNil)
			So(got, ShouldNotBeNil)
			So(got.Run.ID, ShouldEqual, 42)
			So(len(got.Tasks), ShouldEqual, 2)

			ctrl.Finish()
		})

		Convey("Run 不存在时返回 errRunNotActive", func() {
			ctrl := gomock.NewController(t)

			chat := mock_orch_svc.NewMockChatGateway(ctrl)
			agents := mock_orch_svc.NewMockAgentLookup(ctrl)
			runs := mock_orch_repo.NewMockRunRepo(ctrl)
			tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
			orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

			runs.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)

			got, err := orch_svc.Default().LoadRun(context.Background(), 99)
			So(got, ShouldBeNil)
			So(errors.Is(err, orch_svc.ErrRunNotActive), ShouldBeTrue)

			ctrl.Finish()
		})
	})
}
