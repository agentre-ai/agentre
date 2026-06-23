package group_svc_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

// registerTaskMocks 注册任务域测试共用的 mock 仓储,返回各 mock。
func registerTaskMocks(_ *testing.T, ctrl *gomock.Controller) (*mock_group_repo.MockGroupRepo, *mock_group_repo.MockGroupMemberRepo, *mock_group_repo.MockGroupMessageRepo, *mock_group_repo.MockGroupTaskRepo) {
	groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
	memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
	msgRepo := mock_group_repo.NewMockGroupMessageRepo(ctrl)
	taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
	group_repo.RegisterGroup(groupRepo)
	group_repo.RegisterMember(memberRepo)
	group_repo.RegisterMessage(msgRepo)
	group_repo.RegisterTask(taskRepo)
	// 主持人 launchDelivery → recruitableRoster 读招募池(全部 active agent;本 harness 无可招募对象)。
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	agent_repo.RegisterAgent(agentRepo)
	agentRepo.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()
	return groupRepo, memberRepo, msgRepo, taskRepo
}

func TestHandleTaskCreate(t *testing.T) {
	Convey("主持人建卡 → 落卡+落 created 消息+投递给执行人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		taskRepo.EXPECT().NextTaskNo(gomock.Any(), int64(5)).Return(1, nil)
		taskRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.TaskNo, ShouldEqual, 1)
				So(x.CreatorMemberID, ShouldEqual, 100)
				So(x.AssigneeMemberID, ShouldEqual, 101)
				So(x.Status, ShouldEqual, group_entity.TaskStatusOpen)
				So(x.ParentTaskNo, ShouldEqual, 0)
				x.ID = 77
				return nil
			})
		// 任务快照注入会在 launch 路径再读一次任务列表(Task 11 接线) → 容忍。
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCreated)
				So(m.TaskID, ShouldEqual, 77)
				So(m.Recipients(), ShouldResemble, []int64{101})
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindAgent)
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		// 建卡即派活:执行人必须被 kick 起轮 —— Send 恰好 1 次,投给其 backing session(12)
		// 且正文携带任务卡内容(钉死 enqueueDeliveries+kick 副作用,防实现退化为只落卡)。
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				So(req.SessionID, ShouldEqual, 12)
				So(strings.Contains(req.Text, "任务 #1"), ShouldBeTrue)
				return &chat_svc.SendResponse{}, nil
			}).Times(1)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		task, err := svc.HandleTaskCreate(ctx, 100, "前端工程师", "重构设置页", "按新稿来", 0)
		So(err, ShouldBeNil)
		So(task.TaskNo, ShouldEqual, 1)
	})

	Convey("assignee 不在群 → GroupMemberNotFound", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, _ := registerTaskMocks(t, ctrl)

		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host}, nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		_, err := svc.HandleTaskCreate(ctx, 100, "不存在的人", "t", "b", 0)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupMemberNotFound)
	})

	Convey("给自己派活 → GroupTaskSelfAssign(防自循环,模型可读懂并自行执行)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, _ := registerTaskMocks(t, ctrl)

		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host}, nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		_, err := svc.HandleTaskCreate(ctx, 100, "林队", "给自己的活", "b", 0)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupTaskSelfAssign)
	})
}

func TestHandleTaskComplete(t *testing.T) {
	Convey("执行人交付 → 卡置 done + completed 消息投回建卡人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		// RunStatus 必须可推进(idle), 否则 kick 直接返回、Send .Times(1) 钉不住。
		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(dev, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.Status, ShouldEqual, group_entity.TaskStatusDone)
				So(x.Result, ShouldEqual, "改了 12 个文件,测试通过")
				return nil
			})
		// 任务快照注入会在 launch 路径再读一次任务列表(Task 11 接线) → 容忍。
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCompleted)
				So(m.TaskID, ShouldEqual, 77)
				So(m.Recipients(), ShouldResemble, []int64{100}) // 回建卡人
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		// 钉死「交付即回投」:建卡人(backing session 11)被投递起轮(Send .Times(1))。
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				So(req.SessionID, ShouldEqual, 11)
				So(strings.Contains(req.Text, "任务 #1 已完成"), ShouldBeTrue)
				return &chat_svc.SendResponse{}, nil
			}).Times(1)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		done, err := svc.HandleTaskComplete(ctx, 101, 1, "改了 12 个文件,测试通过")
		So(err, ShouldBeNil)
		So(done.Status, ShouldEqual, group_entity.TaskStatusDone)
	})

	Convey("自交付(creator==caller) → 不投自己, 走回退链改投用户", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl) // 不设 Send 期望:投回自己会 fail
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host}, nil).AnyTimes()

		selfTask := &group_entity.GroupTask{ID: 79, GroupID: 5, TaskNo: 3, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 100, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 3).Return(selfTask, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		// applyFallback 回退链:无在跑来源 → 查最近发言者(无) → 回用户。
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCompleted)
				So(m.Recipients(), ShouldResemble, []int64{}) // 不含 caller 自己
				So(m.ToUser, ShouldBeTrue)
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		done, err := svc.HandleTaskComplete(ctx, 100, 3, "自己建卡自己做完")
		So(err, ShouldBeNil)
		So(done.Status, ShouldEqual, group_entity.TaskStatusDone)
	})

	Convey("建卡人已离群 → 不投离群成员, 走回退链改投用户", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl) // 不设 Send 期望:投给离群建卡人会 fail
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		// 建卡人(100)已离群 → activeMemberByID 必须跳过它(钉死 IsActive 过滤)。
		creatorLeft := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberLeft}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}

		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(dev, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{creatorLeft, dev}, nil).AnyTimes()

		open := &group_entity.GroupTask{ID: 80, GroupID: 5, TaskNo: 4, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 4).Return(open, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		// applyFallback 回退链:无在跑来源 → 查最近发言者(无) → 回用户。
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil)
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCompleted)
				So(m.Recipients(), ShouldResemble, []int64{}) // 不含已离群建卡人
				So(m.ToUser, ShouldBeTrue)
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		done, err := svc.HandleTaskComplete(ctx, 101, 4, "做完了, 但建卡人已离群")
		So(err, ShouldBeNil)
		So(done.Status, ShouldEqual, group_entity.TaskStatusDone)
	})

	Convey("非执行人 complete → GroupTaskForbidden;空 result → GroupTaskResultRequired;关单再 complete → GroupTaskClosed", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, _, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil).AnyTimes()
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()

		Convey("空 result 在查库前就拒绝", func() {
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 1, "   ")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskResultRequired)
		})
		Convey("非执行人", func() {
			open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 1, "我替他交了")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskForbidden)
		})
		Convey("已关单", func() {
			closed := &group_entity.GroupTask{ID: 78, GroupID: 5, TaskNo: 2, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 100, Status: group_entity.TaskStatusDone}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 2).Return(closed, nil)
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 2, "再交一次")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskClosed)
		})
		Convey("任务不存在", func() {
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 9).Return(nil, nil)
			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskComplete(ctx, 100, 9, "交个不存在的")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskNotFound)
		})
	})
}

func TestHandleTaskCancel(t *testing.T) {
	Convey("建卡人取消 → 卡置 canceled + canceled 消息投给执行人", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupMember{host, dev}, nil).AnyTimes()

		open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
				return nil
			})
		// 任务快照注入会在 launch 路径再读一次任务列表(Task 11 接线) → 容忍。
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
				So(m.Recipients(), ShouldResemble, []int64{101}) // 通知执行人
				return nil
			})
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				So(req.SessionID, ShouldEqual, 12)
				So(strings.Contains(req.Text, "任务 #1 已取消"), ShouldBeTrue)
				return &chat_svc.SendResponse{}, nil
			}).Times(1)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
		got, err := svc.HandleTaskCancel(ctx, 100, 1, "需求变了")
		So(err, ShouldBeNil)
		So(got.Status, ShouldEqual, group_entity.TaskStatusCanceled)
	})

	Convey("权限与状态门:无关成员/主持人/已关单", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		g := &group_entity.Group{ID: 5, Status: consts.ACTIVE, RunStatus: group_entity.RunIdle}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()

		Convey("无关成员(非 creator 非 host)取消 → GroupTaskForbidden", func() {
			outsider := &group_entity.GroupMember{ID: 103, GroupID: 5, AgentID: 4, Status: group_entity.MemberActive}
			memberRepo.EXPECT().Find(gomock.Any(), int64(103)).Return(outsider, nil)
			open := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 1).Return(open, nil)

			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskCancel(ctx, 103, 1, "我看不顺眼")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskForbidden)
		})

		Convey("主持人(非 creator)取消 → 成功, 通知执行人与建卡人", func() {
			host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
			dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
			dev2 := &group_entity.GroupMember{ID: 102, GroupID: 5, AgentID: 3, BackingSessionID: 13, Status: group_entity.MemberActive}
			memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
			memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
				Return([]*group_entity.GroupMember{host, dev, dev2}, nil).AnyTimes()

			open := &group_entity.GroupTask{ID: 88, GroupID: 5, TaskNo: 2, Title: "t",
				CreatorMemberID: 102, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 2).Return(open, nil)
			taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, x *group_entity.GroupTask) error {
					So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
					return nil
				})
			taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
			msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
			msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, m *group_entity.GroupMessage) error {
					So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
					So(m.Recipients(), ShouldResemble, []int64{101, 102}) // 执行人+建卡人, 去掉 caller
					return nil
				})
			groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			ch := make(chan chat_svc.TurnResult, 2)
			gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
			gw.EXPECT().ObserveTurn(int64(13)).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
			gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
					So(req.SessionID == 12 || req.SessionID == 13, ShouldBeTrue)
					So(strings.Contains(req.Text, "任务 #2 已取消"), ShouldBeTrue)
					return &chat_svc.SendResponse{}, nil
				}).Times(2)

			svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师", 3: "后端工程师"})
			got, err := svc.HandleTaskCancel(ctx, 100, 2, "方向调整")
			So(err, ShouldBeNil)
			So(got.Status, ShouldEqual, group_entity.TaskStatusCanceled)
		})

		Convey("执行人已离群 → 不投递任何人(notify 过滤已离群成员)", func() {
			host := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
			devLeft := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberLeft}
			memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(host, nil)
			memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
				Return([]*group_entity.GroupMember{host, devLeft}, nil).AnyTimes()

			open := &group_entity.GroupTask{ID: 90, GroupID: 5, TaskNo: 3, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 3).Return(open, nil)
			taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, x *group_entity.GroupTask) error {
					So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
					return nil
				})
			taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
			msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
			msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, m *group_entity.GroupMessage) error {
					So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
					So(m.Recipients(), ShouldResemble, []int64{}) // 已离群执行人被过滤
					return nil
				})
			groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
			// 不设 gw.Send 期望:任何投递都会 fail(零投递钉死)。

			svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "前端工程师"})
			got, err := svc.HandleTaskCancel(ctx, 100, 3, "人都走了")
			So(err, ShouldBeNil)
			So(got.Status, ShouldEqual, group_entity.TaskStatusCanceled)
		})

		Convey("已关单再取消 → GroupTaskClosed(建卡人也不行)", func() {
			creator := &group_entity.GroupMember{ID: 100, GroupID: 5, AgentID: 1, Role: group_entity.RoleHost, Status: group_entity.MemberActive}
			memberRepo.EXPECT().Find(gomock.Any(), int64(100)).Return(creator, nil)
			closed := &group_entity.GroupTask{ID: 78, GroupID: 5, TaskNo: 2, Title: "t",
				CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusDone}
			taskRepo.EXPECT().FindByGroupAndNo(gomock.Any(), int64(5), 2).Return(closed, nil)

			svc := group_svc.NewForTest(gw)
			_, err := svc.HandleTaskCancel(ctx, 100, 2, "想撤回")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTaskClosed)
		})
	})
}

func TestRemoveGroupMember_CancelsOpenTasks(t *testing.T) {
	Convey("成员离群 → 其名下 open 任务级联取消 + 落 system 消息", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo, memberRepo, msgRepo, taskRepo := registerTaskMocks(t, ctrl)

		dev := &group_entity.GroupMember{ID: 101, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(101)).Return(dev, nil)
		memberRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		gw.EXPECT().DeleteSession(gomock.Any(), int64(12)).Return(nil)
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).
			Return(&group_entity.Group{ID: 5, Status: consts.ACTIVE}, nil).AnyTimes()

		mine := &group_entity.GroupTask{ID: 77, GroupID: 5, TaskNo: 1, Title: "t",
			CreatorMemberID: 100, AssigneeMemberID: 101, Status: group_entity.TaskStatusOpen}
		others := &group_entity.GroupTask{ID: 78, GroupID: 5, TaskNo: 2, Title: "x",
			CreatorMemberID: 100, AssigneeMemberID: 100, Status: group_entity.TaskStatusOpen}
		taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).
			Return([]*group_entity.GroupTask{mine, others}, nil)
		taskRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, x *group_entity.GroupTask) error {
				So(x.ID, ShouldEqual, 77) // 只取消该成员名下的
				So(x.Status, ShouldEqual, group_entity.TaskStatusCanceled)
				return nil
			})
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(9, nil)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldEqual, group_entity.SenderKindSystem)
				So(m.TaskEvent, ShouldEqual, group_entity.TaskEventCanceled)
				return nil
			})

		svc := group_svc.NewForTest(gw)
		So(svc.RemoveGroupMember(ctx, 101), ShouldBeNil)
	})
}
