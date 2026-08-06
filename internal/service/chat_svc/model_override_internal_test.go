package chat_svc

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
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
)

func TestModelDeviationNotice_OnlyReportsNonEmptyMismatch(t *testing.T) {
	tests := []struct {
		name     string
		override string
		actual   string
		wantText string
		wantNil  bool
	}{
		{name: "mismatch", override: "selected", actual: "actual", wantText: "所选模型 selected 未生效，实际使用 actual"},
		{name: "equal", override: "same", actual: "same", wantNil: true},
		{name: "empty override", actual: "actual", wantNil: true},
		{name: "empty actual", override: "selected", wantNil: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := modelDeviationNotice(tt.override, tt.actual)
			if tt.wantNil {
				assert.Nil(t, got)
				return
			}
			if assert.NotNil(t, got) {
				assert.Equal(t, "info", got.Level)
				assert.Equal(t, tt.wantText, got.Text)
			}
		})
	}
}

type directModelOverrideRunner struct {
	request     chan agentruntime.RunRequest
	actualModel string
}

func (*directModelOverrideRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}

func (r *directModelOverrideRunner) Run(_ context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	r.request <- req
	events := make(chan agentruntime.Event)
	close(events)
	return events, &agentruntime.RunResult{Model: r.actualModel}, nil
}

type directModelOverrideMocks struct {
	agent    *mock_agent_repo.MockAgentRepo
	backend  *mock_agent_backend_repo.MockAgentBackendRepo
	provider *mock_llm_provider_repo.MockLLMProviderRepo
	session  *mock_chat_repo.MockSessionRepo
	message  *mock_chat_repo.MockMessageRepo
}

func setupDirectModelOverrideTest(t *testing.T) (*chatSvc, *directModelOverrideMocks, context.Context) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	m := &directModelOverrideMocks{
		agent:    mock_agent_repo.NewMockAgentRepo(ctrl),
		backend:  mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl),
		provider: mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl),
		session:  mock_chat_repo.NewMockSessionRepo(ctrl),
		message:  mock_chat_repo.NewMockMessageRepo(ctrl),
	}
	agent_repo.RegisterAgent(m.agent)
	agent_backend_repo.RegisterAgentBackend(m.backend)
	llm_provider_repo.RegisterLLMProvider(m.provider)
	chat_repo.RegisterSession(m.session)
	chat_repo.RegisterMessage(m.message)
	return NewChat(NoopEmitter{}).(*chatSvc), m, context.Background()
}

func TestRunTurn_ModelOverrideReachesRunnerAndPersistsDeviationNotice(t *testing.T) {
	s, m, ctx := setupDirectModelOverrideTest(t)
	var streamEvents []ChatStreamEvent
	s = NewChat(EmitterFunc(func(_ context.Context, _ string, payload any) {
		if event, ok := payload.(ChatStreamEvent); ok {
			streamEvents = append(streamEvents, event)
		}
	})).(*chatSvc)
	runner := &directModelOverrideRunner{
		request:     make(chan agentruntime.RunRequest, 1),
		actualModel: "actual-model",
	}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{
		ID: 100, AgentID: 7, ModelOverride: "selected-model", Status: consts.ACTIVE,
	}
	a := &agent_entity.Agent{ID: 7, AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`}
	be := &agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "provider-key", Status: consts.ACTIVE,
	}
	prov := &llm_provider_entity.LLMProvider{
		ProviderKey: "provider-key", Type: string(llm_provider_entity.TypeAnthropic), Model: "provider-default", Status: consts.ACTIVE,
	}
	assistant := &chat_entity.Message{ID: 1001, SessionID: 100, Role: "assistant", BlocksJSON: "[]"}

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	var persisted *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msg *chat_entity.Message) error {
			copy := *msg
			persisted = &copy
			return nil
		}).AnyTimes()

	s.runTurn(ctx, sess, a, be, prov, nil, assistant, "stream", "", false, turnExtras{})

	select {
	case req := <-runner.request:
		assert.Equal(t, "selected-model", req.ModelOverride)
	default:
		t.Fatal("runtime did not receive a request")
	}
	require.NotNil(t, persisted)
	persistedBlocks, err := persisted.GetBlocks()
	require.NoError(t, err)
	require.Len(t, persistedBlocks, 1)
	notice, ok := persistedBlocks[0].(blocks.NoticeBlock)
	require.True(t, ok)
	assert.Equal(t, "info", notice.Level)
	assert.Contains(t, notice.Text, "selected-model")
	assert.Contains(t, notice.Text, "actual-model")

	var liveNotice bool
	for _, event := range streamEvents {
		if event.Kind != StreamDone || event.Message == nil {
			continue
		}
		for _, block := range event.Message.Blocks {
			if block.Type == "notice" && block.Text == notice.Text {
				liveNotice = true
			}
		}
	}
	assert.True(t, liveNotice, "the completed stream must carry the persisted notice")
}
