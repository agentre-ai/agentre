package group_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/group_repo"
	"agentre/internal/repository/group_repo/mock_group_repo"
	"agentre/internal/service/group_svc"
	"agentre/internal/service/group_svc/mock_group_svc"
)

func TestGroupSvc_CreateGroup_AddsCoordinatorMember(t *testing.T) {
	Convey("建群应建协调者成员 + backing session", t, func() {
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

		// 协调者后端通过 CapMCPTools 门控 → 放行建群。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(1), capability.CapMCPTools).Return(true, nil)
		groupRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, g *group_entity.Group) error { g.ID = 5; return nil })
		// ensureMember: no existing row → create path
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(5), int64(1)).Return(nil, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(1), int64(0), int64(5)).Return(int64(11), nil)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.Role, ShouldEqual, group_entity.RoleCoordinator)
				So(m.BackingSessionID, ShouldEqual, 11)
				So(m.Status, ShouldEqual, group_entity.MemberActive)
				return nil
			})
		// LoadGroup tail
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&group_entity.Group{ID: 5}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)

		svc := group_svc.NewForTest(gw)
		detail, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", CoordinatorAgentID: 1})
		So(err, ShouldBeNil)
		So(detail.Group.ID, ShouldEqual, 5)
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
			&group_entity.Group{ID: 7, ProjectID: 3, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(
			[]*group_entity.GroupMember{{ID: 1}}, nil)
		// 限额检查通过后才走后端门控; CapMCPTools 放行 → 继续 ensureMember。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(9), capability.CapMCPTools).Return(true, nil)
		// FindByGroupAndAgent 返回一条 left 行(status-agnostic)。
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(7), int64(9)).Return(
			&group_entity.GroupMember{ID: 42, GroupID: 7, AgentID: 9, Status: group_entity.MemberLeft}, nil)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(9), int64(3), int64(7)).Return(int64(99), nil)
		// 复活走 Update 且带刷新后的 session + active 状态; Create 不应被调用(无 EXPECT → 触发即失败)。
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				So(m.ID, ShouldEqual, 42)
				So(m.Status, ShouldEqual, group_entity.MemberActive)
				So(m.BackingSessionID, ShouldEqual, 99)
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
	Convey("协调者后端不支持群聊应返回 GroupBackendUnsupported 且不建群", t, func() {
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
		_, err := svc.CreateGroup(ctx, &group_svc.CreateGroupRequest{Title: "支付小队", CoordinatorAgentID: 1})
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
			memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
			group_repo.RegisterMember(memberRepo)

			memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(
				&group_entity.GroupMember{ID: 42, Status: group_entity.MemberActive}, nil)
			memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, m *group_entity.GroupMember) error {
					So(m.Status, ShouldEqual, group_entity.MemberLeft)
					return nil
				})

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
