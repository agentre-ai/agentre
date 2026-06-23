package chat_svc_test

import (
	"context"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// sendPassthroughFixture 用 builtin 后端跑一遍 Send,捕获交给 runtime 的 RunRequest。
// 复用 chat_test.go 的 recordingRunner / setupChatTest。
func sendPassthroughFixture(t *testing.T) (*chatMocks, *recordingRunner, *chat_entity.Session) {
	t.Helper()
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	m := setupChatTest(t)

	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
	backend := &agent_backend_entity.AgentBackend{
		ID:             12,
		Type:           string(agent_backend_entity.TypeBuiltin),
		LLMProviderKey: "key-11",
		Status:         consts.ACTIVE,
	}
	// PromptJSON 非空,这样 GetPrompt() 拼出来的原始 SystemPrompt 也非空,
	// 才能验证 "原始 prompt + suffix" 的拼接行为而不是从空串拼。
	agent := &agent_entity.Agent{
		ID: 7, Name: "Eva", AgentBackendID: 12, Status: consts.ACTIVE,
		PromptJSON: `["You are Eva.","Be concise."]`,
	}
	provider := &llm_provider_entity.LLMProvider{
		ID: 11, Type: string(llm_provider_entity.TypeAnthropic), Model: "m", Status: consts.ACTIVE,
	}

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(agent, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(backend, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-11").Return(provider, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "user" {
				msg.ID = 1000
			} else {
				msg.ID = 1001
			}
			return nil
		}).Times(2)
	m.dbMock.ExpectCommit()

	return m, runner, sess
}

func captureSendRunRequest(t *testing.T, m *chatMocks, runner *recordingRunner, req *chat_svc.SendRequest) agentruntime.RunRequest {
	t.Helper()
	resp, err := m.svc.Send(m.ctx, req)
	require.NoError(t, err)
	var got agentruntime.RunRequest
	select {
	case got = <-runner.requests:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	return got
}

func TestSend_PassthroughMCPAndSystemPromptSuffix(t *testing.T) {
	convey.Convey("SendRequest 透传 MCPServers / SystemPromptSuffix 到 RunRequest", t, func() {
		convey.Convey("Given Send 携带 MCPServers + SystemPromptSuffix, When Send, Then RunRequest.SystemPrompt = 原始 prompt + suffix 且 MCPServers 原样转发", func() {
			m, runner, _ := sendPassthroughFixture(t)

			mcp := []agentruntime.MCPServerSpec{{
				Name:    "group",
				URL:     "http://127.0.0.1:60080/mcp/group/",
				Headers: map[string]string{"Authorization": "Bearer tok"},
			}}
			const suffix = "\n\n[群上下文] 你在群 #42 里,roster: Eva, Bob。"

			got := captureSendRunRequest(t, m, runner, &chat_svc.SendRequest{
				SessionID:          100,
				AgentID:            7,
				Text:               "hi",
				MCPServers:         mcp,
				SystemPromptSuffix: suffix,
			})

			// 原始 prompt 来自 GetPrompt() join("\n");suffix 原样追加(raw concat)。
			assert.Equal(t, "You are Eva.\nBe concise."+suffix, got.SystemPrompt)
			assert.Equal(t, mcp, got.MCPServers)
		})

		convey.Convey("Given Send 不带这两个字段(回归), When Send, Then SystemPrompt 与今日逐字节一致且 MCPServers 为空", func() {
			m, runner, _ := sendPassthroughFixture(t)

			got := captureSendRunRequest(t, m, runner, &chat_svc.SendRequest{
				SessionID: 100,
				AgentID:   7,
				Text:      "hi",
			})

			// 空 suffix ⇒ 与原始 prompt 逐字节一致,无尾随分隔符/垃圾。
			assert.Equal(t, "You are Eva.\nBe concise.", got.SystemPrompt)
			assert.Empty(t, got.MCPServers)
		})
	})
}

func findRecordedEvent(m *chatMocks, name string) *recorded {
	for i := range m.events {
		if m.events[i].Name == name {
			return &m.events[i]
		}
	}
	return nil
}

func TestSend_EmitsTurnStartedBypass(t *testing.T) {
	convey.Convey("EmitTurnStartedBypass 经会话级旁路把 per-turn 流名推给已打开的查看者", t, func() {
		convey.Convey("Given Send 带 EmitTurnStartedBypass(群成员轮), When Send, Then emit chat:autonomous:<sid> 带 StreamAutonomousStarted + per-turn stream + 新 assistant 行", func() {
			m, runner, _ := sendPassthroughFixture(t)

			captureSendRunRequest(t, m, runner, &chat_svc.SendRequest{
				SessionID:             100,
				AgentID:               7,
				Text:                  "hi",
				EmitTurnStartedBypass: true,
			})

			ev := findRecordedEvent(m, chat_svc.AutonomousStreamName(100))
			require.NotNil(t, ev, "应 emit 会话级旁路事件 %s", chat_svc.AutonomousStreamName(100))
			payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
			require.True(t, ok, "payload 应为 ChatStreamEvent")
			assert.Equal(t, chat_svc.StreamAutonomousStarted, payload.Kind)
			// 旁路携带 per-turn 流名(assistant msg id=1001 见 fixture)+ 新 assistant 行,
			// 前端 onAutonomousEvent 据此 openStream + 插行。
			assert.Equal(t, chat_svc.StreamName(100, 1001), payload.Stream)
			require.NotNil(t, payload.AssistantMessage)
			assert.Equal(t, int64(1001), payload.AssistantMessage.ID)
		})

		convey.Convey("Given Send 不带该标志(普通前端 Send 回归), When Send, Then 不 emit 会话级旁路(避免发起者双开流)", func() {
			m, runner, _ := sendPassthroughFixture(t)

			captureSendRunRequest(t, m, runner, &chat_svc.SendRequest{
				SessionID: 100,
				AgentID:   7,
				Text:      "hi",
			})

			convey.So(findRecordedEvent(m, chat_svc.AutonomousStreamName(100)), convey.ShouldBeNil)
		})
	})
}
