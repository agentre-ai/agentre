package canonical

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestKindEnum(t *testing.T) {
	Convey("Kind 字符串值与 wire 协议对齐", t, func() {
		So(string(KindFileWrite), ShouldEqual, "file.write")
		So(string(KindFileEdit), ShouldEqual, "file.edit")
		So(string(KindUserAsk), ShouldEqual, "user.ask")
		So(string(KindPlanUpdate), ShouldEqual, "plan.update")
		So(string(KindPlanApproveRequest), ShouldEqual, "plan.approve_request")
		So(string(KindAgentSpawn), ShouldEqual, "agent.spawn")
	})
}

func TestKindOfNilSafe(t *testing.T) {
	Convey("KindOf(nil) 返空串", t, func() {
		So(KindOf(nil), ShouldEqual, Kind(""))
	})
}

func TestSubtypesImplementCanonicalTool(t *testing.T) {
	Convey("每个 canonical 子类型实现 CanonicalTool 并返对应 Kind", t, func() {
		var w CanonicalTool = FileWrite{Path: "/a", Content: "x"}
		var e CanonicalTool = FileEdit{Files: []FileEditPatch{{Path: "/a", Kind: ChangeCreated}}}
		var u CanonicalTool = UserAsk{RequestID: "r"}
		var pu CanonicalTool = PlanUpdate{Steps: []PlanStep{{Step: "s", Status: StepPending}}}
		var pa CanonicalTool = PlanApproveRequest{RequestID: "r"}
		var as CanonicalTool = AgentSpawn{TaskID: "t", Status: "running"}

		So(KindOf(w), ShouldEqual, KindFileWrite)
		So(KindOf(e), ShouldEqual, KindFileEdit)
		So(KindOf(u), ShouldEqual, KindUserAsk)
		So(KindOf(pu), ShouldEqual, KindPlanUpdate)
		So(KindOf(pa), ShouldEqual, KindPlanApproveRequest)
		So(KindOf(as), ShouldEqual, KindAgentSpawn)
	})
}

func TestFileChangeKindAndDiffOpStrings(t *testing.T) {
	Convey("wire 字符串值固定", t, func() {
		So(string(ChangeCreated), ShouldEqual, "created")
		So(string(ChangeModified), ShouldEqual, "modified")
		So(string(ChangeDeleted), ShouldEqual, "deleted")
		So(string(OpContext), ShouldEqual, " ")
		So(string(OpAdd), ShouldEqual, "+")
		So(string(OpRemove), ShouldEqual, "-")
	})
}

func TestPlanStepStatusStrings(t *testing.T) {
	Convey("PlanStepStatus wire 字符串", t, func() {
		So(string(StepPending), ShouldEqual, "pending")
		So(string(StepInProgress), ShouldEqual, "inProgress")
		So(string(StepCompleted), ShouldEqual, "completed")
		So(string(StepCancelled), ShouldEqual, "canceled")
	})
}
