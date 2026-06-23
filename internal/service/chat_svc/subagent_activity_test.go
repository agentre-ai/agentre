package chat_svc_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/mock_agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// launchMessageWithSubagentState 构造一条带 subagent_state(parent_tool_call_id=toolUseID)
// 的发起 assistant 消息,模拟后台 subagent 派遣卡所在的消息。
func launchMessageWithSubagentState(id, sessionID int64, toolUseID string) *chat_entity.Message {
	blocksJSON := `[` +
		`{"type":"tool_use","data":{"id":"` + toolUseID + `","name":"Task","input":{"description":"run something"}}},` +
		`{"type":"subagent_state","data":{"parent_tool_call_id":"` + toolUseID + `","kind":"local_agent","description":"run something","status":"running","nested_tool_call_ids":[]}}` +
		`]`
	return &chat_entity.Message{ID: id, SessionID: sessionID, Role: "assistant", BlocksJSON: blocksJSON, Seq: 4}
}

// TestDriveSubagentActivity_NestsChildrenAndPersists 是 Task 5 基石:一轮后台 subagent
// 内部活动流被嵌套渲染回发起卡(emit StreamSubagentActivityStarted),实时 stream,并把
// 新嵌套块跨消息落库进发起消息(AppendSubagentChildren)。
func TestDriveSubagentActivity_NestsChildrenAndPersists(t *testing.T) {
	convey.Convey("后台 subagent 活动流嵌套渲染回发起卡 + 跨消息落库", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		const sid = int64(100)
		const launchID = int64(2001)
		const toolUseID = "toolu_agent"
		be := &agent_backend_entity.AgentBackend{ID: 12, Type: "claudecode"}

		sess := &chat_entity.Session{ID: sid, AgentID: 7, AgentStatus: "idle", ProviderSessionID: "sess-abc"}
		m.session.EXPECT().Find(gomock.Any(), sid).Return(sess, nil).AnyTimes()

		// 发起消息定位:返回带 subagent_state{parent_tool_call_id:toolu_agent} 的消息。
		launchMsg := launchMessageWithSubagentState(launchID, sid, toolUseID)
		m.message.EXPECT().
			FindAssistantBySubagentToolUseID(gomock.Any(), sid, toolUseID).
			Return(launchMsg, nil).Times(1)

		// 关键断言:收尾把新嵌套子块跨消息落库进发起消息。
		var gotChildJSON string
		var gotChildIDs []string
		m.message.EXPECT().
			AppendSubagentChildren(gomock.Any(), sid, toolUseID, gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, _ int64, _, childJSON string, childIDs []string) error {
				gotChildJSON = childJSON
				gotChildIDs = childIDs
				return nil
			}).Times(1)

		// 活动事件流:一个嵌套 ToolCall + 嵌套 ToolResult,然后 close。
		evs := make(chan agentruntime.Event, 2)
		evs <- agentruntime.ToolCall{ID: "sub_bash", Name: "Bash", ParentToolCallID: toolUseID, Input: json.RawMessage(`{"command":"ls"}`)}
		evs <- agentruntime.ToolResult{ToolCallID: "sub_bash", Content: "SUBAGENT_DONE", ParentToolCallID: toolUseID}
		close(evs)
		act := agentruntime.SubagentActivity{ToolUseID: toolUseID, Events: evs}

		chat_svc.DriveSubagentActivityForTest(ctx, m.svc, sid, be, act)

		var (
			sawStarted    bool
			startedName   string
			startedStream string
			startedTUID   string
			startedLaunch int64
			sawDone       bool
			doneStream    string
		)
		launchStream := chat_svc.StreamName(sid, launchID)
		for _, ev := range m.events {
			p, ok := ev.Payload.(chat_svc.ChatStreamEvent)
			if !ok {
				continue
			}
			switch p.Kind {
			case chat_svc.StreamSubagentActivityStarted:
				sawStarted = true
				startedName = ev.Name
				startedStream = p.Stream
				startedTUID = p.ToolUseID
				startedLaunch = p.LaunchMessageID
			case chat_svc.StreamDone:
				if ev.Name == launchStream {
					sawDone = true
					doneStream = ev.Name
				}
			}
		}

		convey.Convey("emit 会话级 StreamSubagentActivityStarted(带发起消息 id + tool_use_id)", func() {
			assert.True(t, sawStarted, "应 emit StreamSubagentActivityStarted")
			assert.Equal(t, chat_svc.AutonomousStreamName(sid), startedName)
			assert.Equal(t, launchStream, startedStream)
			assert.Equal(t, toolUseID, startedTUID)
			assert.Equal(t, launchID, startedLaunch)
		})

		convey.Convey("新嵌套子块跨消息落库(含 sub_bash + childIDs)", func() {
			require.NotEmpty(t, gotChildJSON, "应落库子块 JSON")
			assert.Contains(t, gotChildJSON, "sub_bash")
			assert.Contains(t, gotChildJSON, "nested_tool_use")
			assert.Equal(t, []string{"sub_bash"}, gotChildIDs)
		})

		convey.Convey("收尾 emit StreamDone(发起卡 stream)", func() {
			assert.True(t, sawDone, "应在发起卡 stream 上 emit StreamDone")
			assert.Equal(t, launchStream, doneStream)
		})

		convey.Convey("session 保持 idle(后台活动不翻 running)", func() {
			assert.Equal(t, "idle", sess.AgentStatus)
		})
	})
}

// TestDriveSubagentActivity_NoLaunchMessageDrains 验证发起消息找不到时:不落库、不 emit
// started,但仍把 act.Events 抽干(别让 Session reader 阻塞)。
func TestDriveSubagentActivity_NoLaunchMessageDrains(t *testing.T) {
	convey.Convey("发起消息找不到时抽干事件不落库", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		const sid = int64(100)
		const toolUseID = "toolu_missing"
		be := &agent_backend_entity.AgentBackend{ID: 12, Type: "claudecode"}

		m.message.EXPECT().
			FindAssistantBySubagentToolUseID(gomock.Any(), sid, toolUseID).
			Return(nil, nil).Times(1)
		// 关键:不调 AppendSubagentChildren(无 EXPECT → 调用即 ctrl.Finish 失败)。

		evs := make(chan agentruntime.Event, 2)
		evs <- agentruntime.ToolCall{ID: "sub_bash", Name: "Bash", ParentToolCallID: toolUseID}
		evs <- agentruntime.ToolResult{ToolCallID: "sub_bash", Content: "x", ParentToolCallID: toolUseID}
		close(evs)
		act := agentruntime.SubagentActivity{ToolUseID: toolUseID, Events: evs}

		chat_svc.DriveSubagentActivityForTest(ctx, m.svc, sid, be, act)

		convey.Convey("事件被抽干(channel 已空)", func() {
			_, open := <-evs
			assert.False(t, open, "act.Events 应被抽干并 close")
		})

		convey.Convey("不 emit StreamSubagentActivityStarted", func() {
			for _, ev := range m.events {
				if p, ok := ev.Payload.(chat_svc.ChatStreamEvent); ok {
					assert.NotEqual(t, chat_svc.StreamSubagentActivityStarted, p.Kind)
				}
			}
		})
	})
}

// TestStartSubagentActivityWatcher_DedupesAndExitsOnClose 验证 watcher 生命周期:每会话
// 只起一个(去重),底层 SubagentActivity channel close 后干净退出并清去重位。
func TestStartSubagentActivityWatcher_DedupesAndExitsOnClose(t *testing.T) {
	convey.Convey("subagent-activity watcher 每会话一个 + channel close 后退出", t, func() {
		m := setupChatTest(t)
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		src := mock_agentruntime.NewMockSubagentActivitySource(ctrl)
		be := &agent_backend_entity.AgentBackend{ID: 12, Type: "claudecode"}

		ch := make(chan agentruntime.SubagentActivity) // 不带值,保持 open
		called := make(chan struct{})
		// Times(1) 即验证去重:第二次 start 不再订阅。
		src.EXPECT().SubagentActivity(int64(100)).
			DoAndReturn(func(int64) <-chan agentruntime.SubagentActivity {
				out := make(chan agentruntime.SubagentActivity)
				go func() {
					defer close(out)
					close(called)
					for a := range ch {
						out <- a
					}
				}()
				return out
			}).Times(1)

		chat_svc.StartSubagentActivityWatcherForTest(m.svc, 100, be, src)
		<-called // watcher goroutine 已订阅,去重位已占
		assert.True(t, chat_svc.IsSubagentActivityWatcherActiveForTest(m.svc, 100))

		// 第二次 start:被去重,不再调 SubagentActivity(Times(1) 验证)。
		chat_svc.StartSubagentActivityWatcherForTest(m.svc, 100, be, src)

		close(ch) // 让底层 channel close → watcher 退出
		require.Eventually(t, func() bool {
			return !chat_svc.IsSubagentActivityWatcherActiveForTest(m.svc, 100)
		}, time.Second, 5*time.Millisecond, "watcher 应在 channel close 后退出并清去重位")
	})
}
