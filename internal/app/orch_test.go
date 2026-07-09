package app

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

func TestToRunItem_MapsAllFields(t *testing.T) {
	Convey("toRunItem 应把 OrchestrationRun 所有字段映射到 RunItemDTO", t, func() {
		r := &orch_entity.OrchestrationRun{
			ID:            42,
			Goal:          "做个登录页",
			LeaderAgentID: 7,
			Status:        orch_entity.RunRunning,
			ProjectID:     10,
			FlowID:        5,
			FlowContent:   "步骤一…",
			RootTaskID:    99,
			Createtime:    1000,
			Updatetime:    2000,
		}
		dto := toRunItem(r)
		So(dto.ID, ShouldEqual, r.ID)
		So(dto.Goal, ShouldEqual, r.Goal)
		So(dto.LeaderAgentID, ShouldEqual, r.LeaderAgentID)
		So(dto.Status, ShouldEqual, r.Status)
		So(dto.ProjectID, ShouldEqual, r.ProjectID)
		// 新增字段断言
		So(dto.FlowID, ShouldEqual, r.FlowID)
		So(dto.FlowContent, ShouldEqual, r.FlowContent)
		So(dto.RootTaskID, ShouldEqual, r.RootTaskID)
		So(dto.Createtime, ShouldEqual, r.Createtime)
		So(dto.Updatetime, ShouldEqual, r.Updatetime)
	})
}

func TestToDispatchDTO_MapsAllFields(t *testing.T) {
	Convey("toDispatchDTO 应把 Dispatch 所有字段映射到 DispatchDTO", t, func() {
		tk := &orch_entity.Dispatch{
			ID:               99,
			RunID:            42,
			AgentID:          7,
			SessionID:        500,
			ParentDispatchID: 3,
			Kind:             orch_entity.DispatchKindDispatch,
			Status:           orch_entity.DispatchRunning,
			Brief:            "实现登录接口",
			Result:           "已完成",
			CallSeq:          2,
			Refs:             `["task:1"]`,
			Createtime:       3000,
			Updatetime:       4000,
		}
		dto := toDispatchDTO(tk)
		So(dto.ID, ShouldEqual, tk.ID)
		So(dto.RunID, ShouldEqual, tk.RunID)
		So(dto.AgentID, ShouldEqual, tk.AgentID)
		So(dto.SessionID, ShouldEqual, tk.SessionID)
		So(dto.ParentDispatchID, ShouldEqual, tk.ParentDispatchID)
		So(dto.Kind, ShouldEqual, tk.Kind)
		So(dto.Status, ShouldEqual, tk.Status)
		So(dto.Brief, ShouldEqual, tk.Brief)
		So(dto.Result, ShouldEqual, tk.Result)
		So(dto.CallSeq, ShouldEqual, tk.CallSeq)
		// 新增字段断言
		So(dto.Refs, ShouldEqual, tk.Refs)
		So(dto.Createtime, ShouldEqual, tk.Createtime)
		So(dto.Updatetime, ShouldEqual, tk.Updatetime)
	})
}

func TestToRunItem_ZeroValues(t *testing.T) {
	Convey("toRunItem 对零值 Run 返回零值 DTO", t, func() {
		r := &orch_entity.OrchestrationRun{}
		dto := toRunItem(r)
		So(dto.ID, ShouldEqual, 0)
		So(dto.Goal, ShouldEqual, "")
		So(dto.Status, ShouldEqual, "")
	})
}

func TestToDispatchDTO_ZeroValues(t *testing.T) {
	Convey("toDispatchDTO 对零值 Dispatch 返回零值 DTO", t, func() {
		tk := &orch_entity.Dispatch{}
		dto := toDispatchDTO(tk)
		So(dto.ID, ShouldEqual, 0)
		So(dto.Kind, ShouldEqual, "")
		So(dto.CallSeq, ShouldEqual, 0)
	})
}

func TestToTaskItemDTO_MapsAllFields(t *testing.T) {
	Convey("toTaskItemDTO 应把 Task 所有字段映射到 TaskItemDTO", t, func() {
		tk := &orch_entity.Task{
			ID:              42,
			RunID:           100,
			Seq:             3,
			Text:            "写测试",
			Status:          orch_entity.TaskStatusInProgress,
			AssigneeAgentID: 7,
			Createtime:      1000,
			Updatetime:      2000,
		}
		dto := toTaskItemDTO(tk)
		So(dto.ID, ShouldEqual, tk.ID)
		So(dto.RunID, ShouldEqual, tk.RunID)
		So(dto.Seq, ShouldEqual, tk.Seq)
		So(dto.Text, ShouldEqual, tk.Text)
		So(dto.Status, ShouldEqual, tk.Status)
		So(dto.AssigneeAgentID, ShouldEqual, tk.AssigneeAgentID)
		So(dto.Createtime, ShouldEqual, tk.Createtime)
		So(dto.Updatetime, ShouldEqual, tk.Updatetime)
	})
}

func TestRunLoad_PopulatesTasksFromListTasks(t *testing.T) {
	Convey("RunLoad 返回的 RunDetailDTO.Tasks 含待办清单条目(经 orch_svc.ListTasks)", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		chat := mock_orch_svc.NewMockChatGateway(ctrl)
		agents := mock_orch_svc.NewMockAgentLookup(ctrl)
		runs := mock_orch_repo.NewMockRunRepo(ctrl)
		dispatches := mock_orch_repo.NewMockDispatchRepo(ctrl)
		tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
		orch_svc.Default().RegisterDeps(chat, agents, runs, dispatches, nil, nil)
		orch_svc.Default().RegisterTodoRepo(tasks)

		run := &orch_entity.OrchestrationRun{ID: 42, Goal: "做个登录页", Status: orch_entity.RunRunning}
		dispatch := &orch_entity.Dispatch{ID: 1, RunID: 42, Kind: orch_entity.DispatchKindDispatch}
		todo := &orch_entity.Task{ID: 9, RunID: 42, Seq: 1, Text: "写测试", Status: orch_entity.TaskStatusPending}

		runs.EXPECT().Find(gomock.Any(), int64(42)).Return(run, nil)
		dispatches.EXPECT().ListByRun(gomock.Any(), int64(42)).Return([]*orch_entity.Dispatch{dispatch}, nil)
		tasks.EXPECT().ListByRun(gomock.Any(), int64(42)).Return([]*orch_entity.Task{todo}, nil)

		a := &App{ctx: context.Background()}
		got, err := a.RunLoad(42)
		So(err, ShouldBeNil)
		So(got, ShouldNotBeNil)
		So(len(got.Dispatches), ShouldEqual, 1)
		So(len(got.Tasks), ShouldEqual, 1)
		So(got.Tasks[0].ID, ShouldEqual, int64(9))
		So(got.Tasks[0].Text, ShouldEqual, "写测试")
	})
}
