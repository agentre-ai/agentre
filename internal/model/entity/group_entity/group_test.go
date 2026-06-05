package group_entity_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/group_entity"
)

func TestGroup_Check(t *testing.T) {
	ctx := context.Background()
	Convey("Check 校验 nil / 空标题 / 缺协调者 / 合法", t, func() {
		cases := []struct {
			name string
			g    *group_entity.Group
			ok   bool
		}{
			{"nil receiver", nil, false},
			{"blank title", &group_entity.Group{Title: "   ", CoordinatorAgentID: 1}, false},
			{"missing coordinator", &group_entity.Group{Title: "x", CoordinatorAgentID: 0}, false},
			{"negative coordinator", &group_entity.Group{Title: "x", CoordinatorAgentID: -1}, false},
			{"valid", &group_entity.Group{Title: "x", CoordinatorAgentID: 1}, true},
		}
		for _, tc := range cases {
			Convey(tc.name, func() {
				err := tc.g.Check(ctx)
				if tc.ok {
					So(err, ShouldBeNil)
				} else {
					So(err, ShouldNotBeNil)
				}
			})
		}
	})
}

func TestGroupCanAdvance(t *testing.T) {
	Convey("CanAdvance 仅在 run_status 允许推进时为真(无轮数上限)", t, func() {
		So((&group_entity.Group{RunStatus: group_entity.RunIdle}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunRunning}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunWaitingUser}).CanAdvance(), ShouldBeTrue)
		So((&group_entity.Group{RunStatus: group_entity.RunPaused}).CanAdvance(), ShouldBeFalse)
		So((&group_entity.Group{RunStatus: group_entity.RunError}).CanAdvance(), ShouldBeFalse)
	})
}

func TestGroupMessageRecipientsRoundTrip(t *testing.T) {
	Convey("SetRecipients/Recipients 应 json round-trip", t, func() {
		m := &group_entity.GroupMessage{}
		m.SetRecipients([]int64{3, 7, 9})
		So(m.Recipients(), ShouldResemble, []int64{3, 7, 9})
	})
	Convey("空收件人返回空切片而非 nil-panic", t, func() {
		So((&group_entity.GroupMessage{}).Recipients(), ShouldBeEmpty)
	})
}

func TestGroupMemberIsCoordinator(t *testing.T) {
	Convey("IsCoordinator 看 role", t, func() {
		So((&group_entity.GroupMember{Role: group_entity.RoleCoordinator}).IsCoordinator(), ShouldBeTrue)
		So((&group_entity.GroupMember{Role: group_entity.RoleMember}).IsCoordinator(), ShouldBeFalse)
	})
}

func TestGroup_PinnedField(t *testing.T) {
	Convey("Group.Pinned 字段", t, func() {
		So((&group_entity.Group{Pinned: true}).Pinned, ShouldBeTrue)
		So((&group_entity.Group{}).Pinned, ShouldBeFalse)
	})
}
