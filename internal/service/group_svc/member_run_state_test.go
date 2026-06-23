package group_svc_test

import (
	"context"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

type memberRunStateEvent struct {
	memberID int64
	runState string
}

// memberRunStateEmitter 抽出 member_run_state 事件的 (memberID, runState) 投到 ch。
func memberRunStateEmitter(ch chan<- memberRunStateEvent) group_svc.EmitterFunc {
	return func(_ context.Context, _ string, payload any) {
		p, ok := payload.(map[string]any)
		if !ok || p["kind"] != "member_run_state" {
			return
		}
		mid, _ := p["memberID"].(int64)
		rs, _ := p["runState"].(string)
		select {
		case ch <- memberRunStateEvent{memberID: mid, runState: rs}:
		default:
		}
	}
}

func waitForMemberRunState(ch <-chan memberRunStateEvent, memberID int64, want string, timeout time.Duration) bool {
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			if e.memberID == memberID && e.runState == want {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

func TestScheduler_EmitsMemberRunStateRunningThenIdle(t *testing.T) {
	Convey("成员 turn 起 → 推 member_run_state running; turn 结束 → 推 idle", t, func() {
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
		// launchDelivery → buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		// 主持人 prompt → recruitableRoster 读招募池(全部 active agent;本测试无可招募对象)。
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		ch11 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch11), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		runStateCh := make(chan memberRunStateEvent, 16)
		group_svc.SetEmitterForTest(svc, memberRunStateEmitter(runStateCh))

		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1}}), ShouldBeNil)
		So(waitForMemberRunState(runStateCh, 1, "running", 2*time.Second), ShouldBeTrue)

		ch11 <- chat_svc.TurnResult{SessionID: 11}
		So(waitForMemberRunState(runStateCh, 1, "idle", 2*time.Second), ShouldBeTrue)
	})
}

// namedEvent 连频道名一起记录(侧栏全局频道断言用)。
type namedEvent struct {
	name    string
	payload map[string]any
}

func recordingEmitter(ch chan<- namedEvent) group_svc.EmitterFunc {
	return func(_ context.Context, name string, payload any) {
		if p, ok := payload.(map[string]any); ok {
			select {
			case ch <- namedEvent{name: name, payload: p}:
			default:
			}
		}
	}
}

func waitNamedEvent(ch <-chan namedEvent, timeout time.Duration, match func(namedEvent) bool) bool {
	deadline := time.After(timeout)
	for {
		select {
		case e := <-ch:
			if match(e) {
				return true
			}
		case <-deadline:
			return false
		}
	}
}

// 回归(侧栏 running 不亮): member_run_state / run_status 原先只 emit 到
// per-group 频道(group:event:<id>),只有打开的群页订阅得到;侧栏(群行 +
// 成员 backing session 行)需要一个常驻可订阅的全局频道,且 member_run_state
// 必须带 backingSessionID 才能映射到会话行。
func TestScheduler_MemberRunStateAlsoEmitsGlobalWithBackingSession(t *testing.T) {
	Convey("成员 turn 起/止与 run_status 转移同步发到全局 groups:run_state 频道", t, func() {
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
		// launchDelivery → buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		// 主持人 prompt → recruitableRoster 读招募池(全部 active agent;本测试无可招募对象)。
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		ch11 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch11), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		events := make(chan namedEvent, 32)
		group_svc.SetEmitterForTest(svc, recordingEmitter(events))

		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1}}), ShouldBeNil)
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "member_run_state" &&
				e.payload["groupID"] == int64(5) &&
				e.payload["memberID"] == int64(1) &&
				e.payload["runState"] == "running" &&
				e.payload["backingSessionID"] == int64(11)
		}), ShouldBeTrue)

		ch11 <- chat_svc.TurnResult{SessionID: 11}
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "member_run_state" &&
				e.payload["memberID"] == int64(1) &&
				e.payload["runState"] == "idle"
		}), ShouldBeTrue)
		// turn 全部结束 → transitionRunStatus(waiting_user) 也要发到全局频道(带 groupID)。
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "run_status" &&
				e.payload["groupID"] == int64(5) &&
				e.payload["runStatus"] == group_entity.RunWaitingUser
		}), ShouldBeTrue)
	})
}

// 回归: stopAll 清 inflight 却不发 member_run_state idle —— 停止群后侧栏/roster
// 的成员行卡 running 直到 reload。
func TestStopGroup_EmitsIdleForInflightMembers(t *testing.T) {
	Convey("StopGroup 时每个在跑成员收到 member_run_state idle + 全局 run_status idle", t, func() {
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
		// launchDelivery → buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本测试无任务)。
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		// 主持人 prompt → recruitableRoster 读招募池(全部 active agent;本测试无可招募对象)。
		agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
		agent_repo.RegisterAgent(agentRepo)
		agentRepo.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		ch11 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(11)).Return((<-chan chat_svc.TurnResult)(ch11), func() {}).AnyTimes()
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()
		gw.EXPECT().Stop(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		events := make(chan namedEvent, 32)
		group_svc.SetEmitterForTest(svc, recordingEmitter(events))

		So(svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1}}), ShouldBeNil)
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.payload["kind"] == "member_run_state" && e.payload["runState"] == "running"
		}), ShouldBeTrue)

		So(svc.StopGroup(ctx, 5), ShouldBeNil)
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "member_run_state" &&
				e.payload["memberID"] == int64(1) &&
				e.payload["runState"] == "idle" &&
				e.payload["backingSessionID"] == int64(11)
		}), ShouldBeTrue)
		So(waitNamedEvent(events, 2*time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "run_status" &&
				e.payload["groupID"] == int64(5) &&
				e.payload["runStatus"] == group_entity.RunIdle
		}), ShouldBeTrue)
	})
}

// 用户控制路径(Pause 等经 setRunStatus)同样要把 run_status 发到全局频道。
func TestRunStatusAlsoEmitsGlobal(t *testing.T) {
	Convey("PauseGroup → 全局频道收到 {kind:run_status, groupID, runStatus:paused}", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, nil)
		events := make(chan namedEvent, 8)
		group_svc.SetEmitterForTest(svc, recordingEmitter(events))

		So(svc.PauseGroup(ctx, 5), ShouldBeNil)
		So(waitNamedEvent(events, time.Second, func(e namedEvent) bool {
			return e.name == group_svc.GroupRunStateEventName &&
				e.payload["kind"] == "run_status" &&
				e.payload["groupID"] == int64(5) &&
				e.payload["runStatus"] == group_entity.RunPaused
		}), ShouldBeTrue)
	})
}

func TestLoadGroup_MemberRunStatesReflectInflight(t *testing.T) {
	Convey("LoadGroup 的 MemberRunStates 跟随调度器在跑态: 在跑→running, 否则 idle", t, func() {
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
		members := []*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
			{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
		taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
		group_repo.RegisterTask(taskRepo)
		taskRepo.EXPECT().ListByGroup(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端"})
		group_svc.MarkInflightForTest(svc, 5, 2)

		d, err := svc.LoadGroup(ctx, 5)
		So(err, ShouldBeNil)
		So(d.MemberRunStates[1], ShouldEqual, "idle")
		So(d.MemberRunStates[2], ShouldEqual, "running")
	})
}
