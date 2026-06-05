package group_svc

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/model/entity/group_entity"
)

func TestBuildGroupMCP_CoordinatorGetsInvite(t *testing.T) {
	Convey("协调者 spec.Tools 含 group_invite, 普通成员不含", t, func() {
		s := newGroupSvc(nil, nil)
		g := &group_entity.Group{ID: 5}
		coord := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 1, Role: group_entity.RoleCoordinator})
		member := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 2, Role: group_entity.RoleMember})
		So(coord[0].Tools, ShouldContain, "group_send")
		So(coord[0].Tools, ShouldContain, "group_invite")
		So(member[0].Tools, ShouldContain, "group_send")
		So(member[0].Tools, ShouldNotContain, "group_invite")
	})
}
