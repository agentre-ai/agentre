package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/service/chat_svc"
)

// fakeRosterGW 是内部测试用的最小 ChatGateway(避免 internal 测试 import mock_group_svc
// 造成 import cycle)。只让 AgentBackendHasCapability 恒返回 true。
type fakeRosterGW struct{}

func (fakeRosterGW) EnsureGroupMemberSession(context.Context, int64, int64, int64) (int64, error) {
	return 0, nil
}
func (fakeRosterGW) Send(context.Context, *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
	return nil, nil
}
func (fakeRosterGW) ObserveTurn(int64) (<-chan chat_svc.TurnResult, func()) {
	return nil, func() {}
}
func (fakeRosterGW) Stop(context.Context, *chat_svc.StopRequest) (*chat_svc.StopResponse, error) {
	return nil, nil
}
func (fakeRosterGW) AgentBackendHasCapability(context.Context, int64, capability.Capability) (bool, error) {
	return true, nil
}

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

func TestBuildGroupSystemPrompt_CoordinatorRoster(t *testing.T) {
	Convey("协调者 prompt 含 group_invite 用法 + 可招募 roster(部门内未进群的支持 agent)", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)

		// 部门 42:Bob(已进群) + Carol(未进群,可招募)。
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(42)).Return(
			[]*agent_entity.Agent{
				{ID: 2, Name: "Bob", Status: consts.ACTIVE},
				{ID: 3, Name: "Carol", Status: consts.ACTIVE},
			}, nil).AnyTimes()
		// s.names(当前成员列表)走 agent_repo.Find。
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(
			&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		s := newGroupSvc(fakeRosterGW{}, nil)
		g := &group_entity.Group{ID: 5, Title: "队", DepartmentID: 42, CoordinatorAgentID: 1}
		members := []*group_entity.GroupMember{
			{ID: 1, AgentID: 1, Role: group_entity.RoleCoordinator},
			{ID: 2, AgentID: 2, Role: group_entity.RoleMember},
		}
		coordPrompt := s.buildGroupSystemPrompt(g, members, members[0])
		memberPrompt := s.buildGroupSystemPrompt(g, members, members[1])
		So(coordPrompt, ShouldContainSubstring, "group_invite")
		So(coordPrompt, ShouldContainSubstring, "Carol") // 可招募
		So(memberPrompt, ShouldNotContainSubstring, "group_invite")
	})
}
