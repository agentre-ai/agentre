package orch_svc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

// parseAskID 从注入消息里抽出 ask_id(消息形如 "【收到提问 ask_id=<id>】...")。
func parseAskID(msg string) string {
	const k = "ask_id="
	i := strings.Index(msg, k)
	if i < 0 {
		return ""
	}
	rest := msg[i+len(k):]
	if j := strings.IndexAny(rest, "】\" "); j >= 0 {
		return rest[:j]
	}
	return rest
}

func TestAsk_InjectLiveSessionThenReplyResolves(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	// 王 在该 Run 已有「活会话」700 → 问题注入 700(保留王的上下文)。
	// AnyTimes：Ask 内部调两次 ListByRun（resolveOrCreateAgentSession + detectAskCycle）。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		injCh <- msg
		return nil
	})

	Convey("ask 注入活会话, 目标用 reply(ask_id) 解开阻塞", t, func() {
		done := make(chan string, 1)
		go func() {
			ans, _ := orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权用什么?")
			done <- ans
		}()
		askID := parseAskID(<-injCh) // 拿到注入消息里的 ask_id(模拟王读到它)
		So(askID, ShouldNotBeBlank)
		// 王(agentID=1)按 ask_id 回复。
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "用 session+cookie"), ShouldBeNil)
		select {
		case ans := <-done:
			So(ans, ShouldEqual, "用 session+cookie")
		case <-time.After(time.Second):
			t.Fatal("ask 未在超时内返回")
		}
	})
}

func TestReply_RejectsForeignReplier(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1}, nil)
	// AnyTimes：Ask 内部调两次 ListByRun（resolveOrCreateAgentSession + detectAskCycle）。
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error { injCh <- m; return nil })

	Convey("非接收者(agentID=2)不能 reply 别人的 ask", t, func() {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() { _, _ = orch_svc.Default().Ask(ctx, 500, "王", "q") }()
		askID := parseAskID(<-injCh)
		So(orch_svc.Default().Reply(context.Background(), 2, askID, "乱答"), ShouldNotBeNil)
	})
}
