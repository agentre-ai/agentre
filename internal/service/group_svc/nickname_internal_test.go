package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
)

func nicknameSvc() *groupSvc {
	s := newGroupSvc(fakeRosterGW{}, nil)
	s.names = func(_ context.Context, id int64) string {
		return map[int64]string{1: "技术主管", 2: "Claude Code"}[id]
	}
	return s
}

func nicknameMembers() []*group_entity.GroupMember {
	return []*group_entity.GroupMember{
		{ID: 1, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		{ID: 2, AgentID: 2, Role: group_entity.RoleMember, Status: group_entity.MemberActive, Nickname: "前端工程师"},
	}
}

func TestEffectiveName_MentionMatchesNickname(t *testing.T) {
	Convey("@mention 用群昵称命中成员(agent 全局名被群内有效名遮蔽)", t, func() {
		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE}
		members := nicknameMembers()
		ids, toUser := nicknameSvc().resolveMentionNames(context.Background(), g, members, members[0], []string{"前端工程师"})
		So(toUser, ShouldBeFalse)
		So(ids, ShouldResemble, []int64{2})
	})
}

func TestEffectiveName_PromptRosterUsesNickname(t *testing.T) {
	Convey("群 system prompt 的成员名单用群昵称, 不漏 agent 全局名", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		g := &group_entity.Group{ID: 5, Title: "支付小队", Status: consts.ACTIVE}
		members := nicknameMembers()
		suffix := nicknameSvc().buildGroupSystemPrompt(g, members, members[1])
		So(suffix, ShouldContainSubstring, "前端工程师")
		So(suffix, ShouldNotContainSubstring, "Claude Code")
	})
}
