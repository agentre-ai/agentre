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

// parseAskID 从注入消息里抽出 ask_id。
// 支持 XML 属性格式 ask_id="<id>" 和旧格式 ask_id=xxx。
func parseAskID(msg string) string {
	_, after, found := strings.Cut(msg, `ask_id="`)
	if !found {
		// 兼容旧格式 ask_id=xxx
		_, after, found = strings.Cut(msg, "ask_id=")
		if !found {
			return ""
		}
		if j := strings.IndexAny(after, "】\" "); j >= 0 {
			return after[:j]
		}
		return after
	}
	if j := strings.IndexByte(after, '"'); j >= 0 {
		return after[:j]
	}
	return after
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
	agents.EXPECT().Find(gomock.Any(), int64(0)).Return(nil, nil).AnyTimes()
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

func TestAsk_BusyTargetSteersIntoCurrentTurn(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: "前端"}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	// 目标 busy:SendAndForget 返 ErrSessionBusy → 必须回退 Enqueue
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).Return(orch_svc.ErrSessionBusy)
	injCh := make(chan string, 1)
	chat.EXPECT().Enqueue(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		injCh <- msg
		return nil
	})

	Convey("busy 目标:Ask 回退 Enqueue/steer, 注入 <peer_ask> XML", t, func() {
		done := make(chan string, 1)
		go func() { ans, _ := orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权?"); done <- ans }()
		msg := <-injCh
		So(msg, ShouldContainSubstring, "<peer_ask")
		So(msg, ShouldContainSubstring, `from="前端"`)
		askID := parseAskID(msg)
		So(askID, ShouldNotBeBlank)
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "session+cookie"), ShouldBeNil)
		select {
		case ans := <-done:
			So(ans, ShouldEqual, "session+cookie")
		case <-time.After(time.Second):
			t.Fatal("timeout")
		}
	})
}

func TestAsk_EscapesXMLInQuestionAndAskerName(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	// askerName 含 < > " 以覆盖属性转义
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{ID: 2, Name: `前端 <"x">`}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, msg string) error {
		injCh <- msg
		return nil
	})

	Convey("Ask 注入消息:question/askerName 中的 < > & \" 必须被 HTML 转义", t, func() {
		done := make(chan string, 1)
		// question 含 <script> 和 "双引号"
		go func() {
			ans, _ := orch_svc.Default().Ask(context.Background(), 500, "王", `用 <script> 吗? & "yes"`)
			done <- ans
		}()
		msg := <-injCh
		// 信封标签本身不应被转义
		So(msg, ShouldContainSubstring, "<peer_ask")
		So(msg, ShouldContainSubstring, "</peer_ask>")
		// question 中的 < 必须转义为 &lt;
		So(msg, ShouldNotContainSubstring, "<script")
		So(msg, ShouldContainSubstring, "&lt;script")
		// askerName 中的 < 也必须转义
		So(msg, ShouldNotContainSubstring, `前端 <"x">`)
		So(msg, ShouldContainSubstring, "前端 &lt;")
		// Reply 让 goroutine 退出，防 goroutine 泄漏
		askID := parseAskID(msg)
		So(askID, ShouldNotBeBlank)
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "已转义"), ShouldBeNil)
		select {
		case ans := <-done:
			So(ans, ShouldEqual, "已转义")
		case <-time.After(time.Second):
			t.Fatal("ask 未在超时内返回")
		}
	})
}

func TestAsk_EmitsAskAndReplyEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "王").Return(&agent_entity.Agent{ID: 1, Name: "王"}, nil)
	agents.EXPECT().Find(gomock.Any(), gomock.Any()).Return(&agent_entity.Agent{ID: 2, Name: "前端"}, nil).AnyTimes()
	tasks.EXPECT().ListByRun(gomock.Any(), int64(100)).Return([]*orch_entity.Task{{ID: 8, AgentID: 1, SessionID: 700, Status: orch_entity.TaskRunning}}, nil).AnyTimes()
	injCh := make(chan string, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(700), gomock.Any()).DoAndReturn(func(_ context.Context, _ int64, m string) error { injCh <- m; return nil })
	askEvt := make(chan struct{}, 1)
	emit.EXPECT().Emit(gomock.Any(), "orch:run:ask", gomock.Any()).Do(func(_ context.Context, _ string, _ any) { askEvt <- struct{}{} })
	emit.EXPECT().Emit(gomock.Any(), "orch:run:reply", gomock.Any())
	emit.EXPECT().Emit(gomock.Any(), "orch:run:deadlock", gomock.Any()).AnyTimes()

	Convey("Ask emit ask 事件, reply 后 emit reply 事件", t, func() {
		go func() { _, _ = orch_svc.Default().Ask(context.Background(), 500, "王", "鉴权?") }()
		<-askEvt
		askID := parseAskID(<-injCh)
		So(orch_svc.Default().Reply(context.Background(), 1, askID, "ok"), ShouldBeNil)
		time.Sleep(50 * time.Millisecond) // 让 select 收到 reply 并 emit
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
	agents.EXPECT().Find(gomock.Any(), int64(0)).Return(nil, nil).AnyTimes()
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
