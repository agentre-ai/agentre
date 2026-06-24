package app

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
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

func TestToTaskDTO_MapsAllFields(t *testing.T) {
	Convey("toTaskDTO 应把 Task 所有字段映射到 TaskDTO", t, func() {
		tk := &orch_entity.Task{
			ID:           99,
			RunID:        42,
			AgentID:      7,
			SessionID:    500,
			ParentTaskID: 3,
			Kind:         orch_entity.TaskKindDispatch,
			Status:       orch_entity.TaskRunning,
			Brief:        "实现登录接口",
			Result:       "已完成",
			CallSeq:      2,
			Refs:         `["task:1"]`,
			Createtime:   3000,
			Updatetime:   4000,
		}
		dto := toTaskDTO(tk)
		So(dto.ID, ShouldEqual, tk.ID)
		So(dto.RunID, ShouldEqual, tk.RunID)
		So(dto.AgentID, ShouldEqual, tk.AgentID)
		So(dto.SessionID, ShouldEqual, tk.SessionID)
		So(dto.ParentTaskID, ShouldEqual, tk.ParentTaskID)
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

func TestToTaskDTO_ZeroValues(t *testing.T) {
	Convey("toTaskDTO 对零值 Task 返回零值 DTO", t, func() {
		tk := &orch_entity.Task{}
		dto := toTaskDTO(tk)
		So(dto.ID, ShouldEqual, 0)
		So(dto.Kind, ShouldEqual, "")
		So(dto.CallSeq, ShouldEqual, 0)
	})
}
