package group_svc

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

// fakeRosterGW 是内部测试用的最小 ChatGateway(避免 internal 测试 import mock_group_svc
// 造成 import cycle)。只让 AgentBackendHasCapability 恒返回 true。
type fakeRosterGW struct{}

func (fakeRosterGW) EnsureSession(context.Context, *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error) {
	return nil, nil
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
func (fakeRosterGW) DeleteSession(context.Context, int64) error {
	return nil
}
func (fakeRosterGW) AgentBackendHasCapability(context.Context, int64, capability.Capability) (bool, error) {
	return true, nil
}
func (fakeRosterGW) BeginToolApproval(context.Context, int64, *chatblocks.ToolApprovalBlock) (<-chan bool, error) {
	return nil, nil
}
func (fakeRosterGW) FinishToolApproval(context.Context, int64, string, string, string) error {
	return nil
}

func TestBuildGroupMCP_HostGetsInvite(t *testing.T) {
	Convey("主持人 spec.Tools 含 group_invite, 普通成员不含", t, func() {
		s := newGroupSvc(nil, nil)
		g := &group_entity.Group{ID: 5}
		host := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 1, Role: group_entity.RoleHost})
		member := s.buildGroupMCP(g, &group_entity.GroupMember{ID: 2, Role: group_entity.RoleMember})
		So(host[0].Tools, ShouldContain, "group_send")
		So(host[0].Tools, ShouldContain, "group_invite")
		So(member[0].Tools, ShouldContain, "group_send")
		So(member[0].Tools, ShouldNotContain, "group_invite")
		// 任务三件套对所有成员放行(跨成员派活/交接一律走任务卡)。
		for _, tool := range []string{"group_task_create", "group_task_complete", "group_task_cancel"} {
			So(host[0].Tools, ShouldContain, tool)
			So(member[0].Tools, ShouldContain, tool)
		}
	})
}

// 回归(dev group-3): prompt 只说「@用户 = 回复人类」而投递抬头写「(来自 你)」,
// 词汇对不上 → codex 完成任务后把汇报对象选成 host(mentions:["claude glm"]),
// 未被 @ 的成员被卷入, agent 互聊不止。prompt 必须给出明确的回复路由规则。
func TestBuildGroupSystemPrompt_ReplyToSourceGuidance(t *testing.T) {
	Convey("成员 prompt 含「默认回复消息来源 / 来自用户时 mentions:[\"用户\"] / 勿随意 @ 其他成员」", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(
			&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()
		// buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		s := newGroupSvc(fakeRosterGW{}, nil)
		g := &group_entity.Group{ID: 5, Title: "队"}
		members := []*group_entity.GroupMember{
			{ID: 1, AgentID: 1, Role: group_entity.RoleHost},
			{ID: 2, AgentID: 2, Role: group_entity.RoleMember},
		}
		prompt := s.buildGroupSystemPrompt(g, members, members[1])
		So(prompt, ShouldContainSubstring, `(来自 X)`)
		So(prompt, ShouldContainSubstring, `mentions:["用户"]`)
		So(prompt, ShouldContainSubstring, "不要主动 @ 其他成员")
	})
}

func TestBuildGroupSystemPrompt_HostRoster(t *testing.T) {
	Convey("主持人 prompt 含 group_invite 用法(可跨部门) + 可招募 roster(全部 active 中未进群的支持 agent)", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)

		// 招募池=全部 active agent:Bob(已进群) + Carol(未进群,可招募)。
		agentRepo.EXPECT().List(gomock.Any()).Return(
			[]*agent_entity.Agent{
				{ID: 2, Name: "Bob", Status: consts.ACTIVE},
				{ID: 3, Name: "Carol", Status: consts.ACTIVE},
			}, nil).AnyTimes()
		// s.names(当前成员列表)走 agent_repo.Find。
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(
			&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()
		// buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		s := newGroupSvc(fakeRosterGW{}, nil)
		g := &group_entity.Group{ID: 5, Title: "队", DepartmentID: 42, HostAgentID: 1}
		members := []*group_entity.GroupMember{
			{ID: 1, AgentID: 1, Role: group_entity.RoleHost},
			{ID: 2, AgentID: 2, Role: group_entity.RoleMember},
		}
		coordPrompt := s.buildGroupSystemPrompt(g, members, members[0])
		memberPrompt := s.buildGroupSystemPrompt(g, members, members[1])
		So(coordPrompt, ShouldContainSubstring, "group_invite")
		So(coordPrompt, ShouldContainSubstring, "可跨部门")      // 招募池放宽措辞
		So(coordPrompt, ShouldContainSubstring, "可招募：Carol") // 可招募(去掉「同事」)
		So(memberPrompt, ShouldNotContainSubstring, "group_invite")

		// 修订 06-03:无部门(DepartmentID==0)的群同样可招募(早退已删)。
		noDeptPrompt := s.buildGroupSystemPrompt(
			&group_entity.Group{ID: 5, Title: "队", HostAgentID: 1}, members, members[0])
		So(noDeptPrompt, ShouldContainSubstring, "Carol")
	})
}
