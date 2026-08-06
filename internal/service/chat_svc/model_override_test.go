package chat_svc_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

func setupChatTestWithoutDatabase(t *testing.T) *chatMocks {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	m := &chatMocks{
		agent:    mock_agent_repo.NewMockAgentRepo(ctrl),
		backend:  mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl),
		provider: mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl),
		session:  mock_chat_repo.NewMockSessionRepo(ctrl),
		message:  mock_chat_repo.NewMockMessageRepo(ctrl),
		ctx:      context.Background(),
	}
	agent_repo.RegisterAgent(m.agent)
	agent_backend_repo.RegisterAgentBackend(m.backend)
	llm_provider_repo.RegisterLLMProvider(m.provider)
	chat_repo.RegisterSession(m.session)
	chat_repo.RegisterMessage(m.message)
	m.svc = chat_svc.NewChat(chat_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		m.events = append(m.events, recorded{Name: name, Payload: payload})
	}))
	chat_svc.RegisterChat(m.svc)
	return m
}

func TestRunRequestModelOverrideJSON(t *testing.T) {
	withoutOverride, err := json.Marshal(agentruntime.RunRequest{})
	require.NoError(t, err)
	assert.NotContains(t, string(withoutOverride), "modelOverride")

	withOverride, err := json.Marshal(agentruntime.RunRequest{ModelOverride: "custom-model"})
	require.NoError(t, err)
	assert.Contains(t, string(withOverride), `"modelOverride":"custom-model"`)
}

func TestSetSessionModel_PersistsTrimmedValueAndClears(t *testing.T) {
	t.Run("stores an arbitrary model id without provider-list validation", func(t *testing.T) {
		m := setupChatTestWithoutDatabase(t)
		sess := &chat_entity.Session{ID: 100, AgentID: 7, ModelOverride: "old-model", Status: consts.ACTIVE}
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
		var updated *chat_entity.Session
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got *chat_entity.Session) error {
				updated = got
				return nil
			})

		err := m.svc.SetSessionModel(100, "  model-not-in-provider-list  ")
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, "model-not-in-provider-list", updated.ModelOverride)
	})

	t.Run("empty clears the override", func(t *testing.T) {
		m := setupChatTestWithoutDatabase(t)
		sess := &chat_entity.Session{ID: 100, AgentID: 7, ModelOverride: "old-model", Status: consts.ACTIVE}
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, got *chat_entity.Session) error {
				assert.Empty(t, got.ModelOverride)
				return nil
			})

		require.NoError(t, m.svc.SetSessionModel(100, ""))
	})
}

func TestSetSessionModel_RejectsBlankModel(t *testing.T) {
	m := setupChatTestWithoutDatabase(t)
	err := m.svc.SetSessionModel(100, " \t\n ")
	assert.Error(t, err)
}

func TestLoadSession_ProvidesModelOverrideAndProviderDefault(t *testing.T) {
	m := setupChatTestWithoutDatabase(t)
	ctx := m.ctx
	m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, ModelOverride: "selected-model", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "provider-key", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(ctx, "provider-key").Return(&llm_provider_entity.LLMProvider{
		ProviderKey: "provider-key", Type: string(llm_provider_entity.TypeAnthropic), Model: "provider-default", Status: consts.ACTIVE,
	}, nil)
	m.message.EXPECT().List(ctx, int64(100)).Return(nil, nil)

	resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 100})
	require.NoError(t, err)
	assert.Equal(t, "selected-model", resp.Session.ModelOverride)
	assert.Equal(t, "provider-default", resp.Session.ProviderDefaultModel)
}
