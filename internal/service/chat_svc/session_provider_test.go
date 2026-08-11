package chat_svc_test

import (
	"context"
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_model_entity"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// 本文件覆盖 docs/specs/2026-08-10-session-provider-switch.md 的会话级供应商切换
// （决策 1 / 11、「切换流程与生效时机」的写入与校验半边）与「有效供应商解析（唯一
// 口径）」在展示侧的两个消费点（LoadSession 展示字段、复制启动命令）。

// expectSwitchBackend 搭一条「会话 → agent → backend」的解析链：切换入口按会话钉住
// 的那一档（没钉住则 agent 默认档）校验供应商与后端 kind 是否匹配。
func expectSwitchBackend(
	m *chatMocks,
	ctx context.Context,
	sess *chat_entity.Session,
	backendType agent_backend_entity.BackendType,
	agentProviderKey string,
) {
	m.session.EXPECT().Find(ctx, sess.ID).Return(sess, nil)
	m.agent.EXPECT().Find(ctx, sess.AgentID).Return(&agent_entity.Agent{
		ID: sess.AgentID, AgentBackendID: 12, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(backendType), LLMProviderKey: agentProviderKey, Status: consts.ACTIVE,
	}, nil)
}

// noticeTextOf 取出一条消息里唯一那个 notice 块的 Text（持久化的结构化负载原文）。
func noticeTextOf(t *testing.T, msg *chat_entity.Message) string {
	t.Helper()
	require.NotNil(t, msg, "应该新建了一条承载 notice 的消息")
	bs, err := msg.GetBlocks()
	require.NoError(t, err)
	require.Len(t, bs, 1, "切换 notice 消息只带一个 notice 块")
	nb, ok := bs[0].(blocks.NoticeBlock)
	require.True(t, ok, "块类型应为 NoticeBlock")
	return nb.Text
}

// TestSetChatSessionProvider_PersistsAndAppendsNotice：切换成功 → 单列写 provider_key
// （决策 1）+ 向 transcript 追加一条持久 notice（决策 9），notice 是结构化负载而不是
// 现成文案（前端走 t() 渲染）。
func TestSetChatSessionProvider_PersistsAndAppendsNotice(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "running", Status: consts.ACTIVE}
	expectSwitchBackend(m, ctx, sess, agent_backend_entity.TypeClaudeCode, "agent-bound")
	m.provider.EXPECT().FindByKey(ctx, "session-key").Return(&llm_provider_entity.LLMProvider{
		ID: 33, ProviderKey: "session-key", Type: string(llm_provider_entity.TypeAnthropic),
		Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdateProviderKey(ctx, int64(100), "session-key").Return(nil)
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(7, nil)
	var created *chat_entity.Message
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msg *chat_entity.Message) error {
			created = msg
			return nil
		})

	resp, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{
		SessionID: 100, ProviderKey: "session-key",
	})
	require.NoError(t, err)
	assert.Equal(t, "session-key", resp.ProviderKey)
	assert.Equal(t, "agent-bound", resp.AgentProviderKey, "回传 agent 绑定 key 供 pill 渲染回落标签")
	assert.Equal(t, 7, created.Seq)
	assert.Equal(t, `{"providerKey":"session-key","kind":"switch"}`, noticeTextOf(t, created))
}

// TestSetChatSessionProvider_NoticeCarriesProviderDisplayName 钉死设计决策 1：切换
// notice 负载带上供应商显示名（后端产出时已解析到的实体，非前端按 key 查表）——
// transcript 要读得懂「改用了哪个供应商」，而不是一串 UUID。
func TestSetChatSessionProvider_NoticeCarriesProviderDisplayName(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "running", Status: consts.ACTIVE}
	expectSwitchBackend(m, ctx, sess, agent_backend_entity.TypeClaudeCode, "agent-bound")
	m.provider.EXPECT().FindByKey(ctx, "session-key").Return(&llm_provider_entity.LLMProvider{
		ID: 33, ProviderKey: "session-key", Name: "中转 · GLM 5.2", Type: string(llm_provider_entity.TypeAnthropic),
		Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdateProviderKey(ctx, int64(100), "session-key").Return(nil)
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(7, nil)
	var created *chat_entity.Message
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msg *chat_entity.Message) error {
			created = msg
			return nil
		})

	_, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{
		SessionID: 100, ProviderKey: "session-key",
	})
	require.NoError(t, err)
	assert.Equal(t,
		`{"providerKey":"session-key","providerName":"中转 · GLM 5.2","kind":"switch"}`,
		noticeTextOf(t, created))
}

// TestSetChatSessionProvider_ClearsBackToAgentBinding：空串 = 改回跟随 agent 绑定
// （CLI 登录态双向可切，决策 7）；不查供应商，notice 说明的是「跟随绑定」。
func TestSetChatSessionProvider_ClearsBackToAgentBinding(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	sess := &chat_entity.Session{ID: 100, AgentID: 7, Status: consts.ACTIVE, ProviderKey: "session-key"}
	expectSwitchBackend(m, ctx, sess, agent_backend_entity.TypeClaudeCode, "")
	m.session.EXPECT().UpdateProviderKey(ctx, int64(100), "").Return(nil)
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	var created *chat_entity.Message
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msg *chat_entity.Message) error {
			created = msg
			return nil
		})

	resp, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{
		SessionID: 100, ProviderKey: "",
	})
	require.NoError(t, err)
	assert.Empty(t, resp.ProviderKey)
	assert.Equal(t, `{"kind":"switch"}`, noticeTextOf(t, created))
}

// TestSetChatSessionProvider_NoOpWhenUnchanged：选中当前已生效的那一项（弹层里点已选中
// 的行）不写库、也不追加 notice —— 否则每点一次就往 transcript 里塞一条「已改用 X」，
// 而实际上什么都没变。mock 上没有 UpdateProviderKey / NextSeq / Create 期望。
func TestSetChatSessionProvider_NoOpWhenUnchanged(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	sess := &chat_entity.Session{ID: 100, AgentID: 7, Status: consts.ACTIVE, ProviderKey: "session-key"}
	expectSwitchBackend(m, ctx, sess, agent_backend_entity.TypeClaudeCode, "agent-bound")

	resp, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{
		SessionID: 100, ProviderKey: "session-key",
	})
	require.NoError(t, err)
	assert.Equal(t, "session-key", resp.ProviderKey)
	assert.Equal(t, "agent-bound", resp.AgentProviderKey)
}

// TestSetChatSessionProvider_RejectsUnusableProvider：决策 11 —— 复用新建会话那套校验，
// 不通过一律拒绝写库（会话保持原供应商），也不产出 notice。mock 上没有 UpdateProviderKey
// / Create 期望，真写了就会失败。
func TestSetChatSessionProvider_RejectsUnusableProvider(t *testing.T) {
	cases := []struct {
		name     string
		backend  agent_backend_entity.BackendType
		provider *llm_provider_entity.LLMProvider
	}{
		{name: "供应商不存在", backend: agent_backend_entity.TypeClaudeCode, provider: nil},
		{
			name:    "供应商已停用",
			backend: agent_backend_entity.TypeClaudeCode,
			provider: &llm_provider_entity.LLMProvider{
				ID: 33, ProviderKey: "session-key", Type: string(llm_provider_entity.TypeAnthropic),
				Status: consts.DELETE,
			},
		},
		{
			name:    "供应商类型与后端 kind 不兼容",
			backend: agent_backend_entity.TypeClaudeCode,
			provider: &llm_provider_entity.LLMProvider{
				ID: 34, ProviderKey: "session-key", Type: string(llm_provider_entity.TypeOpenAIResponse),
				Status: consts.ACTIVE,
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := setupChatTest(t)
			ctx := context.Background()

			sess := &chat_entity.Session{ID: 100, AgentID: 7, Status: consts.ACTIVE, ProviderKey: "old-key"}
			expectSwitchBackend(m, ctx, sess, tc.backend, "agent-bound")
			m.provider.EXPECT().FindByKey(ctx, "session-key").Return(tc.provider, nil)

			_, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{
				SessionID: 100, ProviderKey: "session-key",
			})
			assert.Error(t, err)
		})
	}
}

// TestSetChatSessionProvider_SessionNotFound / InvalidParameter：边界。
func TestSetChatSessionProvider_Boundaries(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	_, err := m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{SessionID: 0})
	assert.Error(t, err, "SessionID <= 0 → InvalidParameter")

	m.session.EXPECT().Find(ctx, int64(404)).Return(nil, nil)
	_, err = m.svc.SetChatSessionProvider(ctx, &chat_svc.SetSessionProviderRequest{SessionID: 404})
	assert.Error(t, err, "会话不存在 → ChatSessionNotFound")
}

// TestLoadSession_DisplaysEffectiveProvider：展示口径 —— 会话选了供应商时，供应商类型
// 与上下文窗口按 effective provider（会话 > agent 绑定）算，而不是 agent 绑定；同时把
// 会话 key 与 agent 绑定 key 一并回传给前端渲染 pill。
func TestLoadSession_DisplaysEffectiveProvider(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, Status: consts.ACTIVE, ProviderKey: "session-key",
	}, nil)
	m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
		ID: 7, AgentBackendID: 12, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "agent-bound", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(ctx, "agent-bound").Return(&llm_provider_entity.LLMProvider{
		ID: 33, ProviderKey: "agent-bound", Type: string(llm_provider_entity.TypeOpenAIChat),
		Enabled: llm_provider_entity.EnabledOn, DefaultModelKey: "mk-agent-bound", Status: consts.ACTIVE,
	}, nil).AnyTimes()
	m.provider.EXPECT().FindModelByKey(ctx, "mk-agent-bound").Return(
		&llm_provider_model_entity.LLMProviderModel{ModelKey: "mk-agent-bound", ModelID: "gpt-5", ContextWindow: 111_000, Enabled: llm_provider_model_entity.EnabledOn, Status: consts.ACTIVE},
		nil).AnyTimes()
	m.provider.EXPECT().FindByKey(ctx, "session-key").Return(&llm_provider_entity.LLMProvider{
		ID: 34, ProviderKey: "session-key", Type: string(llm_provider_entity.TypeAnthropic),
		Enabled: llm_provider_entity.EnabledOn, DefaultModelKey: "mk-session-key", Status: consts.ACTIVE,
	}, nil).AnyTimes()
	m.provider.EXPECT().FindModelByKey(ctx, "mk-session-key").Return(
		&llm_provider_model_entity.LLMProviderModel{ModelKey: "mk-session-key", ModelID: "claude-sonnet-4-6", ContextWindow: 222_000, Enabled: llm_provider_model_entity.EnabledOn, Status: consts.ACTIVE},
		nil).AnyTimes()
	m.message.EXPECT().List(ctx, int64(100)).Return(nil, nil)

	resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 100})
	require.NoError(t, err)
	assert.Equal(t, string(llm_provider_entity.TypeAnthropic), resp.Session.LLMProviderType,
		"用量口径必须按会话实际会用的供应商算")
	assert.Equal(t, 222_000, resp.Session.ContextWindow, "上下文窗口同样按 effective provider 算")
	assert.Equal(t, "session-key", resp.Session.ProviderKey)
	assert.Equal(t, "agent-bound", resp.Session.AgentProviderKey)
}

// TestLoadSession_FallsBackToAgentBindingForDisplay：会话 provider_key 为空时展示完全
// 不变（硬不变量 1）—— 仍按 agent 绑定解析。
func TestLoadSession_FallsBackToAgentBindingForDisplay(t *testing.T) {
	m := setupChatTest(t)
	ctx := context.Background()

	expectLoadSessionBackend(m, ctx, 100, 7, 12, agent_backend_entity.TypeClaudeCode,
		&llm_provider_entity.LLMProvider{
			ID: 33, ProviderKey: "agent-bound", Type: string(llm_provider_entity.TypeAnthropic),
			Status: consts.ACTIVE,
		})

	resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 100})
	require.NoError(t, err)
	assert.Equal(t, string(llm_provider_entity.TypeAnthropic), resp.Session.LLMProviderType)
	assert.Equal(t, 111_000, resp.Session.ContextWindow)
	assert.Empty(t, resp.Session.ProviderKey, "未选具体供应商 → 会话 key 为空，前端显示 agent 绑定名")
	assert.Equal(t, "agent-bound", resp.Session.AgentProviderKey)
}

// TestGetLaunchCommand_UsesEffectiveProvider：复制启动命令按 effective provider 解析，
// 复制出的命令与该会话实际执行一致（问题 5）。
func TestGetLaunchCommand_UsesEffectiveProvider(t *testing.T) {
	t.Setenv("AGENTRE_DATA_DIR", t.TempDir())
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	m := setupChatTest(t)
	ctx := context.Background()

	m.session.EXPECT().Find(ctx, int64(3)).Return(&chat_entity.Session{
		ID: 3, AgentID: 7, ProviderSessionID: "sess-uuid", Status: consts.ACTIVE, ProviderKey: "session-key",
	}, nil)
	m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
		ID: 7, AgentBackendID: 22, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(22)).Return(&agent_backend_entity.AgentBackend{
		ID: 22, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "agent-bound", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(ctx, "agent-bound").Return(&llm_provider_entity.LLMProvider{
		ID: 33, ProviderKey: "agent-bound", Type: string(llm_provider_entity.TypeAnthropic),
		Enabled: llm_provider_entity.EnabledOn, DefaultModelKey: "mk-agent-bound", Status: consts.ACTIVE,
	}, nil).AnyTimes()
	m.provider.EXPECT().FindModelByKey(ctx, "mk-agent-bound").Return(
		&llm_provider_model_entity.LLMProviderModel{ModelKey: "mk-agent-bound", ModelID: "claude-opus-4-1", Enabled: llm_provider_model_entity.EnabledOn, Status: consts.ACTIVE},
		nil).AnyTimes()
	m.provider.EXPECT().FindByKey(ctx, "session-key").Return(&llm_provider_entity.LLMProvider{
		ID: 34, ProviderKey: "session-key", Type: string(llm_provider_entity.TypeAnthropic),
		Enabled: llm_provider_entity.EnabledOn, DefaultModelKey: "mk-session-key", Status: consts.ACTIVE,
	}, nil).AnyTimes()
	m.provider.EXPECT().FindModelByKey(ctx, "mk-session-key").Return(
		&llm_provider_model_entity.LLMProviderModel{ModelKey: "mk-session-key", ModelID: "claude-sonnet-4-6", Enabled: llm_provider_model_entity.EnabledOn, Status: consts.ACTIVE},
		nil).AnyTimes()

	resp, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 3})
	require.NoError(t, err)
	assert.Contains(t, resp.Command, "--model claude-sonnet-4-6", "命令要用会话所选供应商的模型")
	assert.NotContains(t, resp.Command, "claude-opus-4-1")
}
