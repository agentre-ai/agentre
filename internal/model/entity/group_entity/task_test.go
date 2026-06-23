package group_entity_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
)

func validTask() *group_entity.GroupTask {
	return &group_entity.GroupTask{
		GroupID: 1, TaskNo: 1, Title: "重构设置页",
		CreatorMemberID: 100, AssigneeMemberID: 101,
		Status: group_entity.TaskStatusOpen,
	}
}

func TestGroupTask_Check(t *testing.T) {
	ctx := context.Background()
	Convey("Check 校验必填与状态枚举", t, func() {
		So(validTask().Check(ctx), ShouldBeNil)

		Convey("nil receiver", func() {
			var t0 *group_entity.GroupTask
			So(t0.Check(ctx), ShouldNotBeNil)
		})
		Convey("blank title", func() {
			x := validTask()
			x.Title = "  "
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("missing group", func() {
			x := validTask()
			x.GroupID = 0
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("missing assignee", func() {
			x := validTask()
			x.AssigneeMemberID = 0
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("missing creator", func() {
			x := validTask()
			x.CreatorMemberID = 0
			So(x.Check(ctx), ShouldNotBeNil)
		})
		Convey("bad status", func() {
			x := validTask()
			x.Status = "doing"
			So(x.Check(ctx), ShouldNotBeNil)
		})
	})
}

func TestGroupTask_Permissions(t *testing.T) {
	Convey("IsOpen / CanComplete / CanCancel", t, func() {
		x := validTask()
		So(x.IsOpen(), ShouldBeTrue)
		So(x.CanComplete(101), ShouldBeTrue)      // assignee
		So(x.CanComplete(100), ShouldBeFalse)     // creator 不是执行人
		So(x.CanCancel(100, false), ShouldBeTrue) // creator
		So(x.CanCancel(999, true), ShouldBeTrue)  // 主持人
		So(x.CanCancel(999, false), ShouldBeFalse)

		x.Status = group_entity.TaskStatusDone
		So(x.IsOpen(), ShouldBeFalse)
		So(x.CanComplete(101), ShouldBeFalse) // 关单后不可再操作
		So(x.CanCancel(100, true), ShouldBeFalse)

		x.Status = group_entity.TaskStatusCanceled
		So(x.IsOpen(), ShouldBeFalse)
	})
}
