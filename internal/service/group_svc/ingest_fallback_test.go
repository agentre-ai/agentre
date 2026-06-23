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

// fallbackHarness 装配「三成员群 + 可观察 Send/Create」的标准场景:
// member 1/2/3 (backing 11/12/13), 群消息历史可注入(驱动旧版 lastSenderMemberID)。
type fallbackHarness struct {
	svc     group_svc.GroupSvc
	sent    chan int64
	created chan *group_entity.GroupMessage
}

func newFallbackHarness(t *testing.T, ctrl *gomock.Controller, history []*group_entity.GroupMessage) *fallbackHarness {
	t.Helper()
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
		{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Role: group_entity.RoleHost, Status: group_entity.MemberActive},
		{ID: 2, GroupID: 5, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		{ID: 3, GroupID: 5, AgentID: 3, BackingSessionID: 13, Status: group_entity.MemberActive},
	}
	memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(members, nil).AnyTimes()
	for _, m := range members {
		memberRepo.EXPECT().Find(gomock.Any(), m.ID).Return(m, nil).AnyTimes()
	}
	msgRepo.EXPECT().NextSeq(gomock.Any(), int64(5)).Return(1, nil).AnyTimes()
	msgRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(history, nil).AnyTimes()
	// launchDelivery → buildGroupSystemPrompt → openTaskSnapshot 读任务卡(本 harness 无任务)。
	taskRepo := mock_group_repo.NewMockGroupTaskRepo(ctrl)
	group_repo.RegisterTask(taskRepo)
	taskRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()
	// 主持人 prompt → recruitableRoster 读招募池(全部 active agent;本 harness 无可招募对象)。
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	agent_repo.RegisterAgent(agentRepo)
	agentRepo.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()

	h := &fallbackHarness{
		sent:    make(chan int64, 8),
		created: make(chan *group_entity.GroupMessage, 8),
	}
	msgRepo.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, m *group_entity.GroupMessage) error {
			h.created <- m
			return nil
		}).AnyTimes()

	for _, sid := range []int64{11, 12, 13} {
		ch := make(chan chat_svc.TurnResult, 1)
		gw.EXPECT().ObserveTurn(sid).Return((<-chan chat_svc.TurnResult)(ch), func() {}).AnyTimes()
	}
	gw.EXPECT().Send(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
			h.sent <- req.SessionID
			return &chat_svc.SendResponse{SessionID: req.SessionID}, nil
		}).AnyTimes()

	h.svc = group_svc.NewForTestWithNames(gw, map[int64]string{1: "林队", 2: "后端", 3: "前端"})
	return h
}

func (h *fallbackHarness) waitSent(t *testing.T, want int64) {
	t.Helper()
	select {
	case sid := <-h.sent:
		So(sid, ShouldEqual, want)
	case <-time.After(2 * time.Second):
		t.Fatalf("session %d 的投递未起", want)
	}
}

// waitAgentMessage 等到下一条 SenderKindAgent 的落库消息(跳过 user 消息)。
func (h *fallbackHarness) waitAgentMessage(t *testing.T) *group_entity.GroupMessage {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case m := <-h.created:
			if m.SenderKind == group_entity.SenderKindAgent {
				return m
			}
		case <-deadline:
			t.Fatal("agent 消息未落库")
			return nil
		}
	}
}

// 回归(dev group-3 设计缺陷): 成员回复无 mention 时, 原 fallback 回「全群最近一个
// 发言成员」(用户消息 sender_member_id=0 被跳过) → 回复可能路由到无关 agent。
// 改为回「触发本轮的来源」: 用户触发 → toUser; 成员触发 → 该成员。
func TestIngest_NoMentionFallsBackToTriggeringUser(t *testing.T) {
	Convey("用户触发成员 1 的轮, 成员 1 无 mention 回复 → 回用户, 而非更早发言的成员 2", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		// 历史里成员 2 曾发言 —— 旧逻辑会把成员 1 的回复路由给它。
		h := newFallbackHarness(t, ctrl, []*group_entity.GroupMessage{
			{GroupID: 5, Seq: 1, SenderKind: group_entity.SenderKindAgent, SenderMemberID: 2, Content: "早安"},
		})
		ctx := context.Background()

		So(h.svc.SendGroupMessage(ctx, &group_svc.SendGroupMessageRequest{
			GroupID: 5, Text: "干活", RecipientMemberIDs: []int64{1},
		}), ShouldBeNil)
		h.waitSent(t, 11) // 成员 1 的 turn 在飞

		So(h.svc.IngestAgentMessage(ctx, 1, "做完了", nil), ShouldBeNil)
		m := h.waitAgentMessage(t)
		So(m.ToUser, ShouldBeTrue)
		So(m.Recipients(), ShouldBeEmpty)
	})
}

func TestIngest_NoMentionFallsBackToTriggeringMember(t *testing.T) {
	Convey("成员 1 触发成员 2 的轮, 成员 2 无 mention 回复 → 回成员 1, 而非更早发言的成员 3", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		// 历史里成员 3 曾发言 —— 旧逻辑会把成员 2 的回复路由给它。
		h := newFallbackHarness(t, ctrl, []*group_entity.GroupMessage{
			{GroupID: 5, Seq: 1, SenderKind: group_entity.SenderKindAgent, SenderMemberID: 3, Content: "早安"},
		})
		ctx := context.Background()

		// 成员 1 @后端(成员 2) → 成员 2 的 turn 在飞(来源=成员 1)。
		So(h.svc.IngestAgentMessage(ctx, 1, "@后端 看看", []string{"后端"}), ShouldBeNil)
		_ = h.waitAgentMessage(t) // 成员 1 的消息落库
		h.waitSent(t, 12)

		So(h.svc.IngestAgentMessage(ctx, 2, "好的, 看完了", nil), ShouldBeNil)
		m := h.waitAgentMessage(t)
		So(m.ToUser, ShouldBeFalse)
		So(m.Recipients(), ShouldResemble, []int64{1})
	})
}

// 防御路径: 成员不在跑(无 turn 来源可查, 理论上 group_send 只发生在 turn 中)时,
// 保留旧的 lastSenderMemberID 回退链。
func TestIngest_NoMentionWithoutInflightKeepsLegacyFallback(t *testing.T) {
	Convey("成员 1 不在跑却 ingest 无 mention 消息 → 回退到最近发言的成员 2", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		h := newFallbackHarness(t, ctrl, []*group_entity.GroupMessage{
			{GroupID: 5, Seq: 1, SenderKind: group_entity.SenderKindAgent, SenderMemberID: 2, Content: "早安"},
		})
		ctx := context.Background()

		So(h.svc.IngestAgentMessage(ctx, 1, "补一句", nil), ShouldBeNil)
		m := h.waitAgentMessage(t)
		So(m.ToUser, ShouldBeFalse)
		So(m.Recipients(), ShouldResemble, []int64{2})
	})
}

func TestIngest_MentionedMemberIsPrependedWhenBodyHasNoAt(t *testing.T) {
	Convey("MCP group_send 带 mentions 但正文没有 @ → 落库正文前缀补 @成员", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		h := newFallbackHarness(t, ctrl, nil)
		ctx := context.Background()

		So(h.svc.IngestAgentMessage(ctx, 1, "做完了", []string{"前端"}), ShouldBeNil)
		m := h.waitAgentMessage(t)
		So(m.Content, ShouldEqual, "@前端 做完了")
		So(m.Recipients(), ShouldResemble, []int64{3})
	})

	Convey("正文已经包含 @ → 不重复补前缀", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		h := newFallbackHarness(t, ctrl, nil)
		ctx := context.Background()

		So(h.svc.IngestAgentMessage(ctx, 1, "@前端 做完了", []string{"前端"}), ShouldBeNil)
		m := h.waitAgentMessage(t)
		So(m.Content, ShouldEqual, "@前端 做完了")
		So(m.Recipients(), ShouldResemble, []int64{3})
	})
}
