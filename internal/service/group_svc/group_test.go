package group_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

func TestGroupSvc_CreateGroup_AddsHostMember(t *testing.T) {
	Convey("建群应建主持人成员且不提前创建 backing session", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		// 主持人后端通过 CapMCPTools 门控 → 放行建群。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		// ensureMember: no existing row → create member only.
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleHost)
				So(m.BackingSessionID, ShouldEqual, 0)
				So(m.Status, ShouldEqual, group_entity.MemberActive)
				return nil
			})
		// LoadGroup tail
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "主持人"})
		detail, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", HostAgentID: 1})
		So(err, ShouldBeNil)
		So(detail.Group.ID, ShouldEqual, 5)
	})
}

func TestCreateGroup_PersistsWorkflowID(t *testing.T) {
	Convey("建群带 WorkflowID → 落到 groups.workflow_id", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		group_repo.RegisterTask(taskRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error {
				So(g.WorkflowID, ShouldEqual, 3) // ← 请求的 WorkflowID 必须落到实体
				g.ID = 5
				return nil
			})
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		// LoadGroup tail
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5, WorkflowID: 3}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "主持人"})
		detail, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", HostAgentID: 1, WorkflowID: 3})
		So(err, ShouldBeNil)
		So(detail.Group.WorkflowID, ShouldEqual, 3)
	})
}

func TestLoadGroup_IncludesTasks(t *testing.T) {
	Convey("LoadGroup 返回 Tasks(任务 tab 与历史卡片状态回写的数据源)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		group_repo.RegisterTask(taskRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupTask{{ID: 77, TaskNo: 1, Title: "写测试", Status: group_entity.TaskStatusOpen}}, nil)

		svc := group_svc.NewForTest(gw)
		detail, err := svc.LoadGroup(ctx, 5)
		So(err, ShouldBeNil)
		So(detail.Tasks, ShouldHaveLength, 1)
		So(detail.Tasks[0].ID, ShouldEqual, 77)
		So(detail.Tasks[0].TaskNo, ShouldEqual, 1)
	})
}

func TestGroupSvc_AddGroupMember_RejoinReactivates(t *testing.T) {
	Convey("重新入群应复活既有 left 成员(Update 而非 Create)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		// 群存在且 active, 成员数未达上限。
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Title: "支付小队", ProjectID: 3, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(
			[]*group_entity.GroupMember{{ID: 1}}, nil)
		// 限额检查通过后才走后端门控; CapMCPTools 放行 → 继续 ensureMember。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(9), capability.CapMCPTools).Return(true, nil)
		// FindByGroupAndAgent 返回一条 left 行(status-agnostic)。
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(7), int64(9)).Return(
			&group_entity.GroupMember{ID: 42, GroupID: 7, AgentID: 9, Status: group_entity.MemberLeft}, nil)
		// 复活走 Update; 不提前创建 backing session; Create 不应被调用(无 EXPECT → 触发即失败)。
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.ID, ShouldEqual, 42)
				So(m.Status, ShouldEqual, group_entity.MemberActive)
				So(m.BackingSessionID, ShouldEqual, 0)
				So(m.Role, ShouldEqual, group_entity.RoleMember)
				return nil
			})

		svc := group_svc.NewForTest(gw)
		m, err := svc.AddGroupMember(ctx, 7, 9)
		So(err, ShouldBeNil)
		So(m.ID, ShouldEqual, 42)
		So(m.Status, ShouldEqual, group_entity.MemberActive)
	})
}

func TestGroupSvc_AddGroupMember_LeavesActiveMemberSessionLazy(t *testing.T) {
	Convey("Given an active group member without a backing session, When adding the same agent, Then it keeps the member session lazy", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Title: "支付小队", ProjectID: 3, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(
			[]*group_entity.GroupMember{{ID: 42, GroupID: 7, AgentID: 9, Status: group_entity.MemberActive}}, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(9), capability.CapMCPTools).Return(true, nil)
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(7), int64(9)).Return(
			&group_entity.GroupMember{ID: 42, GroupID: 7, AgentID: 9, Role: group_entity.RoleMember, Status: group_entity.MemberActive, BackingSessionID: 0}, nil)

		svc := group_svc.NewForTest(gw)
		m, err := svc.AddGroupMember(ctx, 7, 9)
		So(err, ShouldBeNil)
		So(m.BackingSessionID, ShouldEqual, 0)
	})
}

func TestGroupSvc_AddGroupMember_MemberLimit(t *testing.T) {
	Convey("成员数达上限应返回 GroupMemberLimit 且不建 session/成员", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil)
		full := make([]*group_entity.GroupMember, 8) // maxMembers
		for i := range full {
			full[i] = &group_entity.GroupMember{ID: int64(i + 1)}
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(full, nil)
		// 无 EnsureGroupMemberSession / Create / FindByGroupAndAgent 的 EXPECT → 被调用即失败。

		svc := group_svc.NewForTest(gw)
		_, err := svc.AddGroupMember(ctx, 7, 9)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupMemberLimit)
	})
}

func TestGroupSvc_AddGroupMember_BackendUnsupported(t *testing.T) {
	Convey("成员后端不支持群聊应返回 GroupBackendUnsupported 且不建 session/成员", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		// 群存在且 active, 成员数未达上限 → 进入后端门控。
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(
			[]*group_entity.GroupMember{{ID: 1}}, nil)
		// 后端缺 CapMCPTools → 拒绝入群。
		// 无 FindByGroupAndAgent / EnsureGroupMemberSession / Create 的 EXPECT → 被调用即失败。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(9), capability.CapMCPTools).Return(false, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.AddGroupMember(ctx, 7, 9)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupBackendUnsupported)
	})
}

func TestGroupSvc_CreateGroup_BackendUnsupported(t *testing.T) {
	Convey("主持人后端不支持群聊应返回 GroupBackendUnsupported 且不建群", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)

		// 门控在 Create 之前; 后端缺 CapMCPTools → 拒绝, 不应建群。
		// 无 groupRepo.Create / EnsureGroupMemberSession 的 EXPECT → 被调用即失败。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(false, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", HostAgentID: 1})
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupBackendUnsupported)
	})
}

func TestGroupSvc_RemoveGroupMember(t *testing.T) {
	Convey("移除成员", t, func() {
		ctx := context.Background()

		Convey("成员存在 → 置 left 并 Update", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)
			group_repo.RegisterMember(memberRepo)

			memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(
				&group_entity.GroupMember{ID: 42, GroupID: 7, Status: group_entity.MemberActive}, nil)
			memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, m *group_entity.GroupMember) error {
					So(m.Status, ShouldEqual, group_entity.MemberLeft)
					return nil
				})
			// 离群级联取消会读群+任务列表(无任务 → 无级联副作用)。
			groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
				&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil).AnyTimes()
			taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
			group_repo.RegisterTask(taskRepo)
			taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(nil, nil).AnyTimes()

			svc := group_svc.NewForTest(gw)
			So(svc.RemoveGroupMember(ctx, 42), ShouldBeNil)
		})

		Convey("成员不存在 → GroupMemberNotFound", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			gw := mock_group_svc.NewMockChatGateway(ctrl)
			memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
			group_repo.RegisterMember(memberRepo)

			memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(nil, nil)

			svc := group_svc.NewForTest(gw)
			err := svc.RemoveGroupMember(ctx, 42)
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupMemberNotFound)
		})
	})
}

func TestGroupSvc_CreateGroup_AddsInitialMembers(t *testing.T) {
	Convey("建群带初始成员：主持人 + 每个成员都建 member", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		// 主持人(1) + 成员(2) 都过能力门控。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		// 主持人 ensureMember
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleHost)
				So(m.BackingSessionID, ShouldEqual, 0)
				return nil
			})
		// 成员(2) ensureMember
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(2)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleMember)
				So(m.BackingSessionID, ShouldEqual, 0)
				return nil
			})
		// LoadGroup tail
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "支付小队", HostAgentID: 1, MemberAgentIDs: []int64{2},
		})
		So(err, ShouldBeNil)
	})

	Convey("成员后端不支持群聊 → GroupBackendUnsupported", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		// 成员(7) 门控失败
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(7), capability.CapMCPTools).Return(false, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "X", HostAgentID: 1, MemberAgentIDs: []int64{7},
		})
		So(err, ShouldNotBeNil)
	})

	Convey("初始成员超过 maxMembers → GroupMemberLimit", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{Status: consts.ACTIVE}, nil).AnyTimes()

		// 主持人 + 8 个成员 = 9 > maxMembers(8)。前 7 个成员入群(连主持人 8 个)后,
		// 第 8 个成员触发上限。重复的入群调用用 AnyTimes 放行,只断言最终报错码。
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), gomock.Any(), capability.CapMCPTools).
			Return(true, nil).AnyTimes()
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, nil).AnyTimes()
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		// LoadGroup tail：仅在「没有上限拦截」的回归态会被走到(那时本测试应失败);
		// 修好后在拦截处返回,这几条不会被调用。AnyTimes 让两种路径都不报多余调用。
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "满员", HostAgentID: 1, MemberAgentIDs: []int64{2, 3, 4, 5, 6, 7, 8, 9},
		})
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupMemberLimit)
	})

	Convey("DepartmentID==0 时从主持人 agent 派生部门", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		// 主持人(1) 属于部门 42 → 派生到群。门控在派生之前放行。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		agentRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(
			&agent_entity.Agent{ID: 1, Name: "主持人", DepartmentID: 42, Status: consts.ACTIVE}, nil).AnyTimes()
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error {
				So(g.DepartmentID, ShouldEqual, 42) // ← 派生断言
				g.ID = 5
				return nil
			})
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{
			Title: "支付小队", HostAgentID: 1, DepartmentID: 0,
		})
		So(err, ShouldBeNil)
	})
}

func TestGroupSvc_HandleInvite(t *testing.T) {
	Convey("主持人邀请可用 agent → 入群 + 落 system 消息 + 返回结果", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		// caller(member 100, agent 1) 是主持人。
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleHost}}, nil).AnyTimes()
		// 招募池=全部 active agent(spec §7 修订 06-03 部门池决策)。
		agentRepo.EXPECT().List(gomock.Any()).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Bob", Status: consts.ACTIVE}}, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), capability.CapMCPTools).Return(true, nil)
		// ensureMember(2) → 新建。
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(2)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		// system "Bob 加入了群聊" 消息落库。
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindSystem)
				return nil
			})

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{2: "Bob"})
		results, err := svc.HandleInvite(ctx, 100, []string{"Bob"}, nil, "需要后端支援")
		So(err, ShouldBeNil)
		So(len(results), ShouldEqual, 1)
		So(results[0].AgentID, ShouldEqual, 2)
		So(results[0].Name, ShouldEqual, "Bob")
	})

	Convey("非主持人调用 → GroupInviteForbidden", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(
			&group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 9, Role: group_entity.RoleMember, Status: group_entity.MemberActive}, nil)

		svc := group_svc.NewForTest(gw)
		_, err := svc.HandleInvite(ctx, 101, []string{"Bob"}, nil, "")
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupInviteForbidden)
	})

	Convey("跨部门邀请:被邀请 agent 部门与群不同 → 仍入群成功", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		group_repo.RegisterMessage(msgRepo)
		agent_repo.RegisterAgent(agentRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleHost}}, nil).AnyTimes()
		// Dana 属于部门 7(≠群的 42):部门仅组织单位,跨部门招人直接成立。
		agentRepo.EXPECT().List(gomock.Any()).Return(
			[]*agent_entity.Agent{{ID: 3, Name: "Dana", DepartmentID: 7, Status: consts.ACTIVE}}, nil)
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(3), capability.CapMCPTools).Return(true, nil)
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).Return(nil, nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{3: "Dana"})
		results, err := svc.HandleInvite(ctx, 100, []string{"Dana"}, nil, "跨部门支援")
		So(err, ShouldBeNil)
		So(len(results), ShouldEqual, 1)
		So(results[0].AgentID, ShouldEqual, 3)
		So(results[0].Name, ShouldEqual, "Dana")
	})

	Convey("被邀请人不在招募池 → 跳过,返回空", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)
		agent_repo.RegisterAgent(agentRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(
			&group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
			&group_entity.Group{ID: 5, DepartmentID: 42, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(
			[]*group_entity.GroupMember{{ID: 100, AgentID: 1, Role: group_entity.RoleHost}}, nil).AnyTimes()
		agentRepo.EXPECT().List(gomock.Any()).Return(
			[]*agent_entity.Agent{{ID: 2, Name: "Bob", Status: consts.ACTIVE}}, nil)

		svc := group_svc.NewForTest(gw)
		results, err := svc.HandleInvite(ctx, 100, []string{"Stranger"}, nil, "")
		So(err, ShouldBeNil)
		So(len(results), ShouldEqual, 0)
	})
}

func TestGroupSvc_SetGroupPinned(t *testing.T) {
	Convey("SetGroupPinned 透传到 repo", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		Convey("存在的群置顶", func() {
			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
			groupRepo.EXPECT().SetPinned(gomock.Any(), int64(5), true).Return(nil)

			svc := group_svc.NewForTest(gw)
			So(svc.SetGroupPinned(ctx, 5, true), ShouldBeNil)
		})

		Convey("不存在的群 → GroupNotFound", func() {
			groupRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)

			svc := group_svc.NewForTest(gw)
			So(svc.SetGroupPinned(ctx, 99, true), ShouldNotBeNil)
		})
	})
}
