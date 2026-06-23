package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
)

// BuildSendTurnExtras 实现 chat_svc.TurnExtrasProvider:用户直接对群成员 backing session
// 发起 Send/Edit/Regenerate(不经 scheduler.launchDelivery)时,补齐群上下文(group_send
// MCP + 群 system-prompt 后缀),修设计问题⑥。与 launchDelivery 共用 buildGroupMCP /
// buildGroupSystemPrompt。
func TestBuildSendTurnExtras(t *testing.T) {
	Convey("群成员 backing session 直接发起轮:provider 补齐群上下文", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Title: "队", HostAgentID: 1}, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost},
			{ID: 2, GroupID: 5, AgentID: 2, Role: group_entity.RoleMember},
		}, nil).AnyTimes()
		// buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		// memberDisplayName → s.names → agent_repo.Find(取 agent 名)。
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).
			Return(&agent_entity.Agent{Status: consts.ACTIVE, Name: "甲"}, nil).AnyTimes()

		s := newGroupSvc(fakeRosterGW{}, nil)
		s.SetGatewayBaseURL("http://127.0.0.1:1234")

		Convey("成员(AgentID=2)→ ok=true, mcp 含 group_send, suffix 含群名", func() {
			mcp, suffix, ok := s.BuildSendTurnExtras(context.Background(), &agent_entity.Agent{ID: 2}, 42, 5)
			So(ok, ShouldBeTrue)
			So(mcp, ShouldHaveLength, 1)
			So(mcp[0].Tools, ShouldContain, "group_send")
			So(suffix, ShouldContainSubstring, "群聊「队」")
		})

		Convey("该 agent 不是本群成员 → ok=false", func() {
			_, _, ok := s.BuildSendTurnExtras(context.Background(), &agent_entity.Agent{ID: 99}, 42, 5)
			So(ok, ShouldBeFalse)
		})
	})

	Convey("非群会话(groupID<=0)/ 网关未就绪 / agent 为 nil → ok=false", t, func() {
		s := newGroupSvc(fakeRosterGW{}, nil)
		s.SetGatewayBaseURL("http://127.0.0.1:1234")
		_, _, ok := s.BuildSendTurnExtras(context.Background(), &agent_entity.Agent{ID: 2}, 42, 0)
		So(ok, ShouldBeFalse)

		_, _, okNil := s.BuildSendTurnExtras(context.Background(), nil, 42, 5)
		So(okNil, ShouldBeFalse)

		s2 := newGroupSvc(fakeRosterGW{}, nil) // gatewayBaseURL 空
		_, _, ok2 := s2.BuildSendTurnExtras(context.Background(), &agent_entity.Agent{ID: 2}, 42, 5)
		So(ok2, ShouldBeFalse)
	})
}
