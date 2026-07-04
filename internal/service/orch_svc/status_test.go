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

func TestFormatRunStatus(t *testing.T) {
	tasks := []*orch_entity.Task{
		{ID: 1, AgentID: 10, Kind: "dispatch", Status: orch_entity.TaskAwaitingChildren, Brief: "根", CallSeq: 1, ParentTaskID: 0},
		{ID: 2, AgentID: 11, Kind: "dispatch", Status: orch_entity.TaskRunning, Brief: "前端", CallSeq: 1, ParentTaskID: 1, NodeRef: "FE"},
		{ID: 3, AgentID: 12, Kind: "dispatch", Status: orch_entity.TaskDone, Brief: "后端", CallSeq: 1, ParentTaskID: 1, Summary: "完工"},
	}
	names := map[int64]string{10: "组长", 11: "小前", 12: "小后"}

	Convey("status JSON 覆盖任务树 + agent 名 + has_summary + blocked_on", t, func() {
		out := orch_svc.FormatRunStatusForTest(tasks, names)
		So(out, ShouldContainSubstring, `"task_id":2`)
		So(out, ShouldContainSubstring, `"agent":"小前"`)
		So(out, ShouldContainSubstring, `"node":"FE"`)
		So(out, ShouldContainSubstring, `"has_summary":true`) // task#3 有 Summary
		So(out, ShouldContainSubstring, `"blocked_on":[2]`)   // task#1 awaiting-children,活跃子=#2
	})
}

func TestRunStatus_ScopesToCallerRun(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 1, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{
		{ID: 1, RunID: 100, AgentID: 10, Kind: "dispatch", Status: orch_entity.TaskRunning, Brief: "根"},
	}, nil)
	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{{ID: 10, Name: "组长"}}, nil)

	Convey("status 定位调用者 Run 并渲染任务树", t, func() {
		out, err := orch_svc.Default().RunStatus(context.Background(), 500)
		So(err, ShouldBeNil)
		So(out, ShouldContainSubstring, `"agent":"组长"`)
	})
}
