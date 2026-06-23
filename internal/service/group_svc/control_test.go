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

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

func TestStopGroup_AbortsInflightAndClearsQueue(t *testing.T) {
	Convey("StopGroup 对每个在跑成员调 chat_svc.Stop + 清队列 + run_status=idle", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 2, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		group_svc.MarkInflightForTest(svc, 5, 2)
		gw.EXPECT().Stop(gomock.Any(), &chat_svc.StopRequest{SessionID: 12}).Return(&chat_svc.StopResponse{Stopped: true}, nil)

		So(svc.StopGroup(ctx, 5), ShouldBeNil)
		So(g.RunStatus, ShouldEqual, group_entity.RunIdle)
	})
}

func TestStopGroup_GroupNotFound(t *testing.T) {
	Convey("StopGroup 群不存在 → GroupNotFound 且不调 Stop/Update", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(nil, nil)
		// 无 Update / Stop 的 EXPECT → 被调用即失败。

		svc := group_svc.NewForTest(gw)
		err := svc.StopGroup(ctx, 5)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupNotFound)
	})
}

func TestPauseGroup_SetsRunStatusPaused(t *testing.T) {
	Convey("PauseGroup → run_status=paused, 不中止在跑 turn", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		// 无 gw.Stop 的 EXPECT → 被调用即失败(Pause 不中止在跑 turn)。

		svc := group_svc.NewForTest(gw)
		So(svc.PauseGroup(ctx, 5), ShouldBeNil)
		So(g.RunStatus, ShouldEqual, group_entity.RunPaused)
	})
}

func TestResumeGroup_SetsRunningAndKicks(t *testing.T) {
	Convey("ResumeGroup → 先发 run_status=running 事件, 再 kick 填槽", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunPaused, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		// setRunStatus 的 Update + kick 内部 transitionRunStatus 可能再 Update → AnyTimes。
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		// kick 会 ListByGroup(无 pending → 不起 turn, 随后 quiesce 到 waiting_user)。
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Status: group_entity.MemberActive},
		}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		// 经 emitter 同步观察 run_status 转移(不裸读共享指针, 防 -race)。
		// setRunStatus 先发 running; kick 无 work → 再 quiesce 到 waiting_user。
		runStatusCh := make(chan string, 8)
		group_svc.SetEmitterForTest(svc, runStatusEmitter(runStatusCh))
		So(svc.ResumeGroup(ctx, 5), ShouldBeNil)
		So(waitForRunStatus(runStatusCh, group_entity.RunRunning, time.Second), ShouldBeTrue)
	})
}

func TestRenameGroup(t *testing.T) {
	Convey("RenameGroup", t, func() {
		ctx := context.Background()

		Convey("有效标题 → Update 且写入新 Title", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			g := &group_entity.Group{ID: 5, Title: "旧名", Status: consts.ACTIVE}
			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
			groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, gg *group_entity.Group) error {
					So(gg.Title, ShouldEqual, "新名")
					return nil
				})

			svc := group_svc.NewForTest(gw)
			So(svc.RenameGroup(ctx, 5, "新名"), ShouldBeNil)
			So(g.Title, ShouldEqual, "新名")
		})

		Convey("空白标题 → GroupTitleRequired 且不 Update", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
				&group_entity.Group{ID: 5, Title: "旧名", Status: consts.ACTIVE}, nil)
			// 无 Update 的 EXPECT → 被调用即失败。

			svc := group_svc.NewForTest(gw)
			err := svc.RenameGroup(ctx, 5, "   ")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTitleRequired)
		})

		Convey("群不存在 → GroupNotFound", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(nil, nil)

			svc := group_svc.NewForTest(gw)
			err := svc.RenameGroup(ctx, 5, "新名")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupNotFound)
		})
	})
}

// TestDeleteGroup_DeleteSessionsFalse_KeepsSessions 锁住"保留会话"分支: deleteSessions=false
// 时只软删群行(status=DELETE), 绝不删成员 backing session。
func TestDeleteGroup_DeleteSessionsFalse_KeepsSessions(t *testing.T) {
	Convey("DeleteGroup(deleteSessions=false) → status=DELETE 但保留 backing session(不调 gw.DeleteSession)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, gg *group_entity.Group) error {
				So(gg.Status, ShouldEqual, consts.DELETE)
				return nil
			})
		// 成员有 backing session, 但 deleteSessions=false → 绝不调 gw.DeleteSession(未设 EXPECT, 调到即失败)。
		// ListByGroup 仍被 stopAll 调用(无在跑成员 → 不调 gw.Stop)。
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		So(svc.DeleteGroup(ctx, 5, false), ShouldBeNil)
		So(g.Status, ShouldEqual, consts.DELETE)
	})
}

// TestStopGroup_TokenSurvivesStop 回归(报障 mcp__group__group_send 报
// "MCP server group requires re-authorization (token expired)"):停止只暂停 turn,
// 绝不能吊销活跃成员的 token —— 否则用户恢复发言时,被复用的常驻 CLI 子进程仍持停止前签发
// 的 token(--mcp-config 只在 spawn 时注入、复用轮不会重发),group_send 拿到 401 被 CLI
// 误报为"需要重新授权"。token 现为无状态签名 + 按 DB 成员资格鉴权:群仍 ACTIVE、成员仍 active
// → 停止后仍可发言。
func TestStopGroup_TokenSurvivesStop(t *testing.T) {
	Convey("StopGroup 后,活跃群的活跃成员 token 仍可发 group_send(停止不吊销)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(
			&group_entity.GroupMember{ID: 2, GroupID: 5, Status: group_entity.MemberActive}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		tok := group_svc.MintTokenForTest(svc, 5, 2)
		So(group_svc.TokenValidForTest(svc, tok), ShouldBeTrue)

		So(svc.StopGroup(ctx, 5), ShouldBeNil)
		So(group_svc.TokenValidForTest(svc, tok), ShouldBeTrue) // 停止不吊销 → 恢复后仍可发言
	})
}

// TestRemoveGroupMember_RevokesViaAuthz 锁住离群失权:RemoveGroupMember 把成员置 left 后,
// memberCanPost(按 DB 现状)即拒绝其 group_send —— 取代旧的内存 token 吊销, 且跨重启仍生效。
func TestRemoveGroupMember_RevokesViaAuthz(t *testing.T) {
	Convey("RemoveGroupMember 后该成员 token 失权(经 authz, 非删 token)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		// 同一成员指针: RemoveGroupMember 把它置 left, 之后的鉴权 Find 取到的就是 left。
		m := &group_entity.GroupMember{ID: 42, GroupID: 7, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(m, nil).AnyTimes()
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil).AnyTimes()
		// 离群级联取消会读任务列表(无任务 → 无级联副作用)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		tok := group_svc.MintTokenForTest(svc, 7, 42)
		So(group_svc.TokenValidForTest(svc, tok), ShouldBeTrue) // 离群前: active 成员有发言权

		So(svc.RemoveGroupMember(ctx, 42), ShouldBeNil)
		So(group_svc.TokenValidForTest(svc, tok), ShouldBeFalse) // 离群后(status=left): authz 拒绝
	})
}

// TestRemoveGroupMember_DeletesBackingSession 锁住离群清理: 成员有 backing session 时,
// RemoveGroupMember 必须软删它 —— 否则它以 group_id>0 的 ACTIVE 会话残留, 经 ListAgents
// 的 IncludingGroups 变体继续出现在该 agent 侧栏 recent/attention/计数里。
func TestRemoveGroupMember_DeletesBackingSession(t *testing.T) {
	Convey("成员有 backing session → RemoveGroupMember 软删它", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		m := &group_entity.GroupMember{ID: 42, GroupID: 7, AgentID: 9, BackingSessionID: 88, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(m, nil).AnyTimes()
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		gw.EXPECT().DeleteSession(gomock.Any(), int64(88)).Return(nil)
		// 离群级联取消会读群+任务列表(无任务 → 无级联副作用)。
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil).AnyTimes()
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		So(svc.RemoveGroupMember(ctx, 42), ShouldBeNil)
	})

	Convey("成员无 backing session(=0)→ 不调 DeleteSession", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		m := &group_entity.GroupMember{ID: 42, GroupID: 7, AgentID: 9, BackingSessionID: 0, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(42)).Return(m, nil).AnyTimes()
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		// gw.DeleteSession 不应被调用(BackingSessionID=0)。
		// 离群级联取消会读群+任务列表(无任务 → 无级联副作用)。
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(
			&group_entity.Group{ID: 7, Status: consts.ACTIVE}, nil).AnyTimes()
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		So(svc.RemoveGroupMember(ctx, 42), ShouldBeNil)
	})
}

// TestDeleteGroup_DeleteSessionsTrue_DeletesBackingSessions 锁住删除清理: deleteSessions=true 时
// 删除全群成员的 backing session, 只删有 BackingSessionID 的, 跳过尚未起轮(=0)的成员。
func TestDeleteGroup_DeleteSessionsTrue_DeletesBackingSessions(t *testing.T) {
	Convey("DeleteGroup(deleteSessions=true) 删除全群成员 backing session(跳过 BackingSessionID=0)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Status: group_entity.MemberActive},
			{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
			{ID: 3, GroupID: 5, AgentID: 3, BackingSessionID: 0, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		gw.EXPECT().DeleteSession(gomock.Any(), int64(11)).Return(nil)
		gw.EXPECT().DeleteSession(gomock.Any(), int64(12)).Return(nil)
		// member 3 BackingSessionID=0 → 不应调 DeleteSession。

		svc := group_svc.NewForTest(gw)
		So(svc.DeleteGroup(ctx, 5, true), ShouldBeNil)
		So(g.Status, ShouldEqual, consts.DELETE)
	})
}
