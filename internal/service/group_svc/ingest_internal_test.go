package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/group_entity"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/repository/group_repo"
	"agentre/internal/repository/group_repo/mock_group_repo"
)

// 退役 @mention 自动招募回归:协调者 @ 一个未进群的部门同事,resolveMentionNames
// 不再把 ta 招进群,因此返回的收件 ids 为空(改用 group_invite 显式邀请)。
func TestResolveMentionNames_NoAutoRecruit(t *testing.T) {
	Convey("协调者 @ 未进群同事 → 不招募, 返回空 ids", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		// s.names(成员列表)走 Find;协调者 agent 1 → "Coord"。
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(
			&agent_entity.Agent{Name: "Coord", Status: consts.ACTIVE}, nil).AnyTimes()
		// 招募(若仍在)会查部门池找到 Stranger,并建 member(Create 赋 ID=999)。
		// 退役后这些都不该影响结果(ids 仍为空)。AnyTimes 放行,让断言落在 ids 上。
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), gomock.Any()).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Stranger", Status: consts.ACTIVE}}, nil).AnyTimes()
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error { m.ID = 999; return nil }).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), gomock.Any()).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		s := newGroupSvc(fakeRosterGW{}, nil)
		g := &group_entity.Group{ID: 5, DepartmentID: 42}
		sender := &group_entity.GroupMember{ID: 100, AgentID: 1, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}
		ids, toUser := s.resolveMentionNames(context.Background(), g, []*group_entity.GroupMember{sender}, sender, []string{"Stranger"})
		So(ids, ShouldBeEmpty)
		So(toUser, ShouldBeFalse)
	})
}
