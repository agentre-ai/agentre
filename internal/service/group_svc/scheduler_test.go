package group_svc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/repository/group_repo"
	"agentre/internal/repository/group_repo/mock_group_repo"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/group_svc"
	"agentre/internal/service/group_svc/mock_group_svc"
)

func TestScheduler_FanOutThenToolRoute(t *testing.T) {
	Convey("用户发给[后端,前端] → 两 turn 并发; 后端 group_send @前端 → 前端二次投递", t, func() {
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

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
			{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
			{ID: 3, GroupID: 5, AgentID: 3, BackingSessionID: 13, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(members[1], nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		ch12 := make(chan chat_svc.TurnResult, 1)
		ch13 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(12)).Return((<-chan chat_svc.TurnResult)(ch12), func() {}).AnyTimes()
		gw.EXPECT().ObserveTurn(int64(13)).Return((<-chan chat_svc.TurnResult)(ch13), func() {}).AnyTimes()
		sent := make(chan int64, 8)
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				So(len(req.MCPServers), ShouldBeGreaterThan, 0)
				So(req.SystemPromptSuffix, ShouldNotBeBlank)
				sent <- req.SessionID
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端", 3: "前端"})
		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "开工", RecipientMemberIDs: []int64{2, 3}}), ShouldBeNil)

		got := map[int64]bool{}
		for i := 0; i < 2; i++ {
			select {
			case sid := <-sent:
				got[sid] = true
			case <-time.After(2 * time.Second):
				t.Fatal("fan-out 投递不足")
			}
		}
		So(got[12] && got[13], ShouldBeTrue)

		ch13 <- chat_svc.TurnResult{SessionID: 13} // 前端 turn 结束, 释放槽
		time.Sleep(50 * time.Millisecond)
		So(svc.IngestAgentMessage(ctx, 2, "做好了", []string{"前端"}), ShouldBeNil)
		select {
		case sid := <-sent:
			So(sid, ShouldEqual, 13)
		case <-time.After(2 * time.Second):
			t.Fatal("tool 路由二次投递未发生")
		}
	})
}

func TestScheduler_QuiesceToWaitingUser(t *testing.T) {
	Convey("单成员投递跑完且无新 pending → run_status 静默到 waiting_user", t, func() {
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

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		ch11 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch11), func() {}).AnyTimes()
		launched := make(chan struct{}, 1)
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				select {
				case launched <- struct{}{}:
				default:
				}
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		runStatusCh := make(chan string, 8)
		group_svc.SetEmitterForTest(svc, runStatusEmitter(runStatusCh))
		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1}}), ShouldBeNil)
		select {
		case <-launched:
		case <-time.After(2 * time.Second):
			t.Fatal("投递未起")
		}

		ch11 <- chat_svc.TurnResult{SessionID: 11} // turn 结束, 无新 pending
		// run_status 在 gogo goroutine 里翻成 waiting_user, 经 emitter 事件(已同步)观察。
		So(waitForRunStatus(runStatusCh, group_entity.RunWaitingUser, time.Second), ShouldBeTrue)
	})
}

// runStatusEmitter 把 transitionRunStatus emit 的 run_status 事件抽出 runStatus 字符串投到 ch。
func runStatusEmitter(ch chan<- string) group_svc.EmitterFunc {
	return func(_ context.Context, _ string, payload any) {
		if p, ok := payload.(map[string]any); ok && p["kind"] == "run_status" {
			if rs, ok := p["runStatus"].(string); ok {
				select {
				case ch <- rs:
				default:
				}
			}
		}
	}
}

// waitForRunStatus 在 timeout 内从 ch 等到目标 run_status(经 emitter 同步, 不裸读共享指针)。
func waitForRunStatus(ch <-chan string, want string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case rs := <-ch:
			if rs == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestScheduler_TurnErrorReleasesSlot(t *testing.T) {
	Convey("成员 turn 以 Err 结束 → 释放槽(后续投递可再起), 且不为出错 turn 落消息", t, func() {
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

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(members[0], nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		// 关键负面断言: 整个测试里恰好落 1 条消息(下面那次显式投递的 user 消息),
		// handleTurnResult 不得为出错 turn 调 Create。
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).Times(1)
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).Times(1)

		ch11 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch11), func() {}).AnyTimes()
		sent := make(chan int64, 4)
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				sent <- req.SessionID
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		runStatusCh := make(chan string, 8)
		group_svc.SetEmitterForTest(svc, runStatusEmitter(runStatusCh))
		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1}}), ShouldBeNil)
		select {
		case sid := <-sent:
			So(sid, ShouldEqual, 11)
		case <-time.After(2 * time.Second):
			t.Fatal("首投未起")
		}

		ch11 <- chat_svc.TurnResult{SessionID: 11, Err: errors.New("boom")} // turn 出错
		// quiesce 到 waiting_user(经 emitter 同步)说明出错 turn 的槽已释放。
		So(waitForRunStatus(runStatusCh, group_entity.RunWaitingUser, time.Second), ShouldBeTrue)

		group_svc.EnqueueForTest(svc, 5, []int64{1}, "再来一次", "你")
		group_svc.KickForTest(svc, ctx, 5)
		select {
		case sid := <-sent:
			So(sid, ShouldEqual, 11) // 槽已释放, 二次投递再起
		case <-time.After(2 * time.Second):
			t.Fatal("出错后槽未释放, 二次投递未起")
		}
	})
}
