package group_svc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

// registerCreateMocks 注册 group_create 测试共用的 mock 仓储(全部零 EXPECT,由用例自配,
// 防止上一个用例的全局注册残留把误调用记到错误的 controller 上)。
func registerCreateMocks(ctrl *gomock.Controller) (
	*mock_chat_repo.MockSessionRepo, *mock_agent_repo.MockAgentRepo,
	*mock_group_repo.MockGroupRepo, *mock_group_repo.MockGroupMemberRepo,
	*mock_group_repo.MockGroupMessageRepo, *mock_group_repo.MockGroupTaskRepo,
) {
	sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
	chat_repo.RegisterSession(sessRepo)
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	agent_repo.RegisterAgent(agentRepo)
	groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
	group_repo.RegisterGroup(groupRepo)
	memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
	group_repo.RegisterMember(memberRepo)
	msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
	group_repo.RegisterMessage(msgRepo)
	taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
	group_repo.RegisterTask(taskRepo)
	return sessRepo, agentRepo, groupRepo, memberRepo, msgRepo, taskRepo
}

// enabledCreateAgent 造一个已开启 group_create 工具的发起 agent(过实时复查门控)。
func enabledCreateAgent(id int64, name string) *agent_entity.Agent {
	a := &agent_entity.Agent{ID: id, Name: name, Status: consts.ACTIVE}
	a.SetTools([]agent_entity.AgentToolItem{{Key: "group_create", Enabled: true}})
	return a
}

func TestHandleGroupCreate_ApprovedExecutes(t *testing.T) {
	Convey("批准 → 解析成员名建群(项目继承自发起会话)+ system 拉起消息 + brief 投主持人;result 文本含 group id", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		sessRepo, agentRepo, groupRepo, memberRepo, msgRepo, taskRepo := registerCreateMocks(ctrl)

		// 发起会话:agent 7 的普通单聊(GroupID=0),项目 3
		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, ProjectID: 3, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		// 成员名解析池
		agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
			{ID: 7, Name: "部门负责人", Status: consts.ACTIVE},
			{ID: 8, Name: "开发", Status: consts.ACTIVE},
		}, nil).AnyTimes()
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).
			Return(enabledCreateAgent(7, "部门负责人"), nil).AnyTimes()
		// CreateGroup 路径(host + 1 成员;backendSupportsGroup 走 gw)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), gomock.Any(), capability.CapMCPTools).Return(true, nil).AnyTimes()
		var createdGroup group_entity.Group
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, g *group_entity.Group) error {
			g.ID = 12
			createdGroup = *g
			return nil
		})
		groupRepo.EXPECT().Find(gomock.Any(), int64(12)).
			Return(&group_entity.Group{ID: 12, Title: "新功能开发组", HostAgentID: 7, ProjectID: 3, Status: consts.ACTIVE}, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(12), gomock.Any()).Return(nil, nil).AnyTimes()
		var hostMemberID int64
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *group_entity.GroupMember) error {
			m.ID = 100 + m.AgentID
			if m.Role == group_entity.RoleHost {
				hostMemberID = m.ID
			}
			return nil
		}).Times(2) // host + 开发
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(12)).Return([]*group_entity.GroupMember{
			{ID: 107, GroupID: 12, AgentID: 7, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
			{ID: 108, GroupID: 12, AgentID: 8, Status: group_entity.MemberActive},
		}, nil).AnyTimes()
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(12)).Return(1, nil).AnyTimes()
		// 两条消息:system 拉起 + brief(user → host)
		var persisted []*group_entity.GroupMessage
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, m *group_entity.GroupMessage) error {
			persisted = append(persisted, m)
			return nil
		}).Times(2)

		// 审批:Begin 返回测试持有的 channel,push true 模拟批准(经 chat_svc.AnswerToolApproval)。
		apvCh := make(chan bool, 1)
		var begunBlk *blocks.ToolApprovalBlock
		gw.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error) {
				begunBlk = blk
				return apvCh, nil
			})
		var finishedResult string
		gw.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).DoAndReturn(
			func(_ context.Context, _ int64, _, _, result string) error {
				finishedResult = result
				return nil
			})

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{7: "部门负责人", 8: "开发"})
		done := make(chan struct{})
		var text string
		var err error
		go func() {
			defer close(done)
			text, err = svc.HandleGroupCreate(context.Background(), 7, 99, "新功能开发组", []string{"开发"}, "按设计稿重构 UI,验收:e2e 通过", 5, nil)
		}()
		apvCh <- true
		<-done

		So(err, ShouldBeNil)
		So(begunBlk.ToolKey, ShouldEqual, "group_create")
		So(begunBlk.ToolName, ShouldEqual, "group_create")
		So(begunBlk.Status, ShouldEqual, "pending")
		So(createdGroup.Title, ShouldEqual, "新功能开发组")
		So(createdGroup.HostAgentID, ShouldEqual, 7)
		So(createdGroup.ProjectID, ShouldEqual, 3)                  // 项目继承自发起会话
		So(createdGroup.WorkflowID, ShouldEqual, 5)                 // workflowId 透传到群实体
		So(begunBlk.ToolInput["workflowId"], ShouldEqual, int64(5)) // 审批卡展示绑定的流程
		So(text, ShouldContainSubstring, "group created: id=12")
		So(text, ShouldContainSubstring, "新功能开发组")
		// 审批卡 result 与返回给 CLI 的文本同一契约(前端 GroupCreateCard 解析依据)。
		So(finishedResult, ShouldContainSubstring, "group created: id=12")
		So(hostMemberID, ShouldNotEqual, 0)
		So(len(persisted), ShouldEqual, 2)
		So(persisted[0].SenderKind, ShouldEqual, group_entity.SenderKindSystem)
		So(persisted[0].Content, ShouldContainSubstring, "部门负责人")
		So(persisted[1].SenderKind, ShouldEqual, group_entity.SenderKindUser)
		So(persisted[1].Content, ShouldContainSubstring, "按设计稿重构 UI")
	})
}

func TestHandleGroupCreate_Denied(t *testing.T) {
	Convey("拒绝 → 不建群(无 group Create),返回拒绝文案,Finish(denied)", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		// groupRepo / msgRepo 等零 EXPECT:拒绝路径任何建群副作用 → 调用即 fail。
		sessRepo, agentRepo, _, _, _, _ := registerCreateMocks(ctrl)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, ProjectID: 3, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		agentRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(enabledCreateAgent(7, "部门负责人"), nil)
		agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
			{ID: 8, Name: "开发", Status: consts.ACTIVE},
		}, nil)

		apvCh := make(chan bool, 1)
		gw.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).Return((<-chan bool)(apvCh), nil)
		gw.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "denied", "").Return(nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{7: "部门负责人", 8: "开发"})
		done := make(chan struct{})
		var text string
		var err error
		go func() {
			defer close(done)
			text, err = svc.HandleGroupCreate(context.Background(), 7, 99, "新功能开发组", []string{"开发"}, "brief", 0, nil)
		}()
		apvCh <- false // 经 chat_svc 返回的 channel 模拟前端拒绝
		<-done

		So(err, ShouldBeNil)
		So(text, ShouldContainSubstring, "用户拒绝")
	})
}

func TestHandleGroupCreate_Timeout(t *testing.T) {
	Convey("超时 → Finish(expired),返回超时文案,不建群", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		sessRepo, agentRepo, _, _, _, _ := registerCreateMocks(ctrl)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, ProjectID: 3, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		agentRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(enabledCreateAgent(7, "部门负责人"), nil)
		agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
			{ID: 8, Name: "开发", Status: consts.ACTIVE},
		}, nil)
		gw.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).Return((<-chan bool)(make(chan bool)), nil)
		gw.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "expired", "").Return(nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{7: "部门负责人", 8: "开发"})
		group_svc.SetApprovalTimeoutForTest(svc, 50*time.Millisecond)

		text, err := svc.HandleGroupCreate(context.Background(), 7, 99, "新功能开发组", []string{"开发"}, "brief", 0, nil)
		So(err, ShouldBeNil)
		So(text, ShouldContainSubstring, "审批超时")
	})
}

func TestHandleGroupCreate_ToolDisabled(t *testing.T) {
	Convey("发起 agent 未开 group_create 工具 → GroupCreateToolDisabled,且不解析成员、不 Begin", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)                // 零 EXPECT:Begin 调用即 fail
		sessRepo, agentRepo, _, _, _, _ := registerCreateMocks(ctrl) // agentRepo.List 零 EXPECT:不应解析成员

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, ProjectID: 3, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		// 实时复查(token 无状态):发起 agent 已在设置里关掉 group_create。
		off := &agent_entity.Agent{ID: 7, Name: "部门负责人", Status: consts.ACTIVE}
		off.SetTools([]agent_entity.AgentToolItem{{Key: "group_create", Enabled: false}})
		agentRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(off, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleGroupCreate(context.Background(), 7, 99, "t", []string{"开发"}, "b", 0, nil)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupCreateToolDisabled)
	})
}

func TestHandleGroupCreate_Guards(t *testing.T) {
	Convey("群成员轮(session.GroupID>0)→ GroupCreateNested,且不 Begin", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl) // 零 EXPECT:Begin 调用即 fail
		sessRepo, _, _, _, _, _ := registerCreateMocks(ctrl)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, GroupID: 5, Status: consts.ACTIVE,
		}, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleGroupCreate(context.Background(), 7, 99, "t", []string{"开发"}, "b", 0, nil)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupCreateNested)
	})

	Convey("会话不存在 / agent 不匹配 → GroupCreateSessionInvalid", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		sessRepo, _, _, _, _, _ := registerCreateMocks(ctrl)
		svc := group_svc.NewForTest(gw)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
		_, err := svc.HandleGroupCreate(context.Background(), 7, 99, "t", nil, "b", 0, nil)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupCreateSessionInvalid)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 8, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		_, err = svc.HandleGroupCreate(context.Background(), 7, 99, "t", nil, "b", 0, nil)
		So(err, ShouldNotBeNil)
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupCreateSessionInvalid)
	})

	Convey("成员名解析不到 → GroupCreateMemberUnknown(err 含名字),且不 Begin", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl) // 零 EXPECT:Begin 调用即 fail
		sessRepo, agentRepo, _, _, _, _ := registerCreateMocks(ctrl)

		sessRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(&chat_entity.Session{
			ID: 99, AgentID: 7, GroupID: 0, Status: consts.ACTIVE,
		}, nil)
		agentRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(enabledCreateAgent(7, "部门负责人"), nil)
		agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
			{ID: 8, Name: "开发", Status: consts.ACTIVE},
		}, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleGroupCreate(context.Background(), 7, 99, "t", []string{"测试"}, "b", 0, nil)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupCreateMemberUnknown)
		So(err.Error(), ShouldContainSubstring, "测试")
	})
}
