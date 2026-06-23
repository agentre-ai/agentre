package chat_svc_test

import (
	"context"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// 设计问题⑥ 回归(端到端接线):用户直接对群成员 backing session(GroupID>0)发起 Send
// (不经 group_svc.launchDelivery,SendRequest 不带群 MCP/后缀)时,startTurn 必须经
// 注册的 TurnExtrasProvider 补齐群上下文,并让 group_send MCP + 群 system-prompt 后缀
// 真正进到 runtime 的 RunRequest。钉死「startTurn 调 fillGroupTurnExtras」这条接线,
// 防被误删后群上下文再次悄悄丢失。
func TestSend_GroupBackingSessionInjectsGroupContextViaProvider(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		token:  "chat-token",
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	const suffix = "\n\n## 群聊「队」\n你是本群的成员。"
	chat_svc.ResetTurnExtrasProviders()
	t.Cleanup(chat_svc.ResetTurnExtrasProviders)
	chat_svc.RegisterTurnExtrasProvider(func(_ context.Context, _ *agent_entity.Agent, _, groupID int64) ([]agentruntime.MCPServerSpec, string, bool) {
		if groupID <= 0 {
			return nil, "", false
		}
		return []agentruntime.MCPServerSpec{{
			Name:  "group",
			URL:   "http://127.0.0.1:60080/mcp/group/",
			Tools: []string{"group_send", "group_task_create"},
		}}, suffix, true
	})

	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
	t.Cleanup(restore)

	// GroupID=5 → 这是一条群成员 backing session。
	sess := &chat_entity.Session{ID: 100, AgentID: 7, GroupID: 5, AgentStatus: "idle", Status: consts.ACTIVE}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Claude Local", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
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
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	// 直接发起:SendRequest 不带 MCPServers/SystemPromptSuffix(模拟用户在 backing session
	// tab 里直接发言)。
	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID: 100,
		AgentID:   7,
		Text:      "hi",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		// group_send MCP 经 provider 补齐进 extras.mcpServers → RunRequest.MCPServers。
		var hasGroupSend bool
		for _, srv := range req.MCPServers {
			for _, tool := range srv.Tools {
				if tool == "group_send" {
					hasGroupSend = true
				}
			}
		}
		assert.True(t, hasGroupSend, "群成员 backing session 直接发起轮应注入 group_send MCP")
		// 群 system-prompt 后缀拼到 RunRequest.SystemPrompt。
		assert.Contains(t, req.SystemPrompt, "群聊「队」", "应拼上群 system-prompt 后缀")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}
