package group_svc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/group_entity"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
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

func TestScheduler_CoordinatorRecruits(t *testing.T) {
	Convey("协调者 @ 部门内未进群且支持 CapMCPTools 的同事 → 招募 + 系统消息 + 投递", t, func() {
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

		g := &group_entity.Group{ID: 7, DepartmentID: 99, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		coordinator := &group_entity.GroupMember{ID: 1, GroupID: 7, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}
		// roster 招募后会增长; kick 在后台 goroutine 读, Create 在前台写 → mutex 保护防 -race。
		var rosterMu sync.Mutex
		roster := []*group_entity.GroupMember{coordinator}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).DoAndReturn(
			func(_ context.Context, _ int64) ([]*group_entity.GroupMember, error) {
				rosterMu.Lock()
				defer rosterMu.Unlock()
				snapshot := make([]*group_entity.GroupMember, len(roster))
				copy(snapshot, roster)
				return snapshot, nil
			}).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(coordinator, nil).AnyTimes()
		// 招募的同事尚未进群 → FindByGroupAndAgent 返回 nil。
		memberRepo.EXPECT().FindByGroupAndAgent(gomock.Any(), int64(7), int64(2)).Return(nil, nil).AnyTimes()
		// ListByDepartment 暴露候选 agent(后端, id=2)。
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(99)).Return([]*agent_entity.Agent{
			{ID: 2, Name: "后端", DepartmentID: 99, Status: consts.ACTIVE},
		}, nil).AnyTimes()
		// 后端支持 CapMCPTools → 可招募。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), gomock.Any()).Return(true, nil).AnyTimes()
		// 招募会建 backing session。
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), int64(2), gomock.Any(), int64(7)).Return(int64(22), nil)
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(7)).Return(1, nil).AnyTimes()

		joinSeen := make(chan struct{}, 1)
		newMemberID := make(chan int64, 1)
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMember) error {
				m.ID = 2 // 模拟自增主键
				rosterMu.Lock()
				roster = append(roster, m) // 招募成员进 roster, 后续 ListByGroup 可见
				rosterMu.Unlock()
				newMemberID <- m.ID
				return nil
			})
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				if m.SenderKind == group_entity.SenderKindSystem {
					So(m.Content, ShouldContainSubstring, "后端")
					So(m.Content, ShouldContainSubstring, "加入")
					select {
					case joinSeen <- struct{}{}:
					default:
					}
				}
				return nil
			}).AnyTimes()

		ch22 := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(int64(22)).Return((<-chan chat_svc.TurnResult)(ch22), func() {}).AnyTimes()
		sent := make(chan int64, 4)
		gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
				sent <- req.SessionID
				return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
			}).AnyTimes()

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		// 协调者 member1 调 group_send @后端(尚未进群) → 招募。
		So(svc.IngestAgentMessage(ctx, 1, "后端来帮忙", []string{"后端"}), ShouldBeNil)

		select {
		case <-joinSeen:
		case <-time.After(2 * time.Second):
			t.Fatal("系统加入消息未落库")
		}
		select {
		case mid := <-newMemberID:
			So(mid, ShouldEqual, 2)
		case <-time.After(2 * time.Second):
			t.Fatal("招募成员未建 member")
		}
		select {
		case sid := <-sent:
			So(sid, ShouldEqual, 22) // 新成员 backing session 收到投递
		case <-time.After(2 * time.Second):
			t.Fatal("招募成员未收到投递")
		}
	})
}

func TestScheduler_RecruitDeniedWhenBackendUnsupported(t *testing.T) {
	Convey("协调者 @ 部门内同事但后端不支持 CapMCPTools → 不招募, 无 Create/EnsureSession", t, func() {
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

		g := &group_entity.Group{ID: 7, DepartmentID: 99, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		coordinator := &group_entity.GroupMember{ID: 1, GroupID: 7, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleCoordinator, Status: group_entity.MemberActive}
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return([]*group_entity.GroupMember{coordinator}, nil).AnyTimes()
		memberRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(coordinator, nil).AnyTimes()
		agentRepo.EXPECT().ListByDepartment(gomock.Any(), int64(99)).Return([]*agent_entity.Agent{
			{ID: 2, Name: "后端", DepartmentID: 99, Status: consts.ACTIVE},
		}, nil).AnyTimes()
		// 后端 *不* 支持 CapMCPTools → 不可招募。
		gw.EXPECT().AgentBackendHasCapability(gomock.Any(), int64(2), gomock.Any()).Return(false, nil).AnyTimes()
		msgRepo.EXPECT().NextSeq(gomock.Any(), int64(7)).Return(1, nil).AnyTimes()
		msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(7)).Return(nil, nil).AnyTimes()
		// 没有 agent 收件人, 也没上一个发送者 → 回退用户(quiesce), 落一条 user 消息即可。
		msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, m *group_entity.GroupMessage) error {
				So(m.SenderKind, ShouldNotEqual, group_entity.SenderKindSystem)
				return nil
			}).AnyTimes()
		// 关键负面断言: 不建成员, 不建 backing session。
		memberRepo.EXPECT().Create(gomock.Any(), gomock.Any()).Times(0)
		gw.EXPECT().EnsureGroupMemberSession(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队"})
		So(svc.IngestAgentMessage(ctx, 1, "找个不支持群聊的", []string{"后端"}), ShouldBeNil)
		time.Sleep(50 * time.Millisecond) // 给后台 goroutine(若有)跑机会, 触发任何意外的 Times(0) 调用
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
