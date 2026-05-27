package chat_svc_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/agents/provider/providertest"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/canonical"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"agentre/internal/pkg/httpgateway"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/repository/chat_repo/mock_chat_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/remote_device_svc"
	"agentre/internal/service/remote_device_svc/mock_remote_device_svc"
	"agentre/pkg/claudecode"
)

type chatMocks struct {
	agent    *mock_agent_repo.MockAgentRepo
	backend  *mock_agent_backend_repo.MockAgentBackendRepo
	provider *mock_llm_provider_repo.MockLLMProviderRepo
	session  *mock_chat_repo.MockSessionRepo
	message  *mock_chat_repo.MockMessageRepo
	dbMock   sqlmock.Sqlmock
	ctx      context.Context
	events   []recorded
	svc      chat_svc.ChatSvc
}

type recorded struct {
	Name    string
	Payload any
}

type fakeChatGateway struct {
	status httpgateway.GatewayStatus
	url    string
	token  string
}

func (f *fakeChatGateway) IssueToken(context.Context, *agent_backend_entity.AgentBackend, time.Duration) (string, error) {
	if f.token != "" {
		return f.token, nil
	}
	return "chat-token", nil
}

func (f *fakeChatGateway) RevokeToken(string) {}

func (f *fakeChatGateway) URL() string {
	if f.url != "" {
		return f.url
	}
	return "http://127.0.0.1:60080"
}

func (f *fakeChatGateway) Status() httpgateway.GatewayStatus { return f.status }

func setupChatTest(t *testing.T) *chatMocks {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	dbCtx, _, dbMock := testutils.Database(t)

	m := &chatMocks{
		agent:    mock_agent_repo.NewMockAgentRepo(ctrl),
		backend:  mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl),
		provider: mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl),
		session:  mock_chat_repo.NewMockSessionRepo(ctrl),
		message:  mock_chat_repo.NewMockMessageRepo(ctrl),
		dbMock:   dbMock,
		ctx:      dbCtx,
	}
	agent_repo.RegisterAgent(m.agent)
	agent_backend_repo.RegisterAgentBackend(m.backend)
	llm_provider_repo.RegisterLLMProvider(m.provider)
	chat_repo.RegisterSession(m.session)
	chat_repo.RegisterMessage(m.message)

	emitter := chat_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		m.events = append(m.events, recorded{Name: name, Payload: payload})
	})
	m.svc = chat_svc.NewChat(emitter)
	chat_svc.RegisterChat(m.svc)
	return m
}

func TestRegisterGatewayBeforeNewChatMakesCLIBackendsChattable(t *testing.T) {
	chat_svc.RegisterChat(nil)
	chat_svc.RegisterGateway(nil)
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
	})
	t.Cleanup(func() {
		chat_svc.RegisterGateway(nil)
		chat_svc.RegisterChat(nil)
	})

	m := setupChatTest(t)
	ctx := context.Background()

	m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
		{ID: 7, Name: "Coder", AgentBackendID: 12, Status: consts.ACTIVE},
	}, nil)
	m.backend.EXPECT().BatchFind(ctx, []int64{12}).Return(map[int64]*agent_backend_entity.AgentBackend{
		12: {ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "key-21", Status: consts.ACTIVE},
	}, nil)
	m.provider.EXPECT().BatchFindByKey(ctx, []string{"key-21"}).Return(map[string]*llm_provider_entity.LLMProvider{
		"key-21": {ID: 21, Type: string(llm_provider_entity.TypeOpenAIResponse), Status: consts.ACTIVE},
	}, nil)
	m.session.EXPECT().CountRunningByAgents(ctx, []int64{7}).Return(map[int64]int{}, nil)
	m.session.EXPECT().CountByAgents(ctx, []int64{7}).Return(map[int64]int64{}, nil)
	m.session.EXPECT().ListByAgent(ctx, int64(7), 5).Return(nil, nil)
	m.session.EXPECT().ListAttentionByAgent(ctx, int64(7), 20).Return(nil, nil)

	resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
	assert.NoError(t, err)
	if assert.Len(t, resp.Agents, 1) {
		assert.True(t, resp.Agents[0].Chattable)
		assert.Empty(t, resp.Agents[0].ChattableHint)
	}
}

func TestListAgents(t *testing.T) {
	convey.Convey("ListAgents", t, func() {
		m := setupChatTest(t)
		ctx := context.Background()

		convey.Convey("claudecode backend.DefaultPermissionMode 透出到 ChatAgentItem", func() {
			// 前端新会话场景下，pill 需要拿到 backend 管理员预设的默认 mode 才能
			// 正确显示并把它当 raw 透回 chat_svc.Send；ChatAgentItem 必须暴露此字段。
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 3, Name: "CC Eng", AgentBackendID: 17, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{17}).Return(map[int64]*agent_backend_entity.AgentBackend{
				17: {
					ID:                    17,
					Type:                  string(agent_backend_entity.TypeClaudeCode),
					LLMProviderKey:        "",
					DefaultPermissionMode: "plan",
					Status:                consts.ACTIVE,
				},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{}).Return(map[string]*llm_provider_entity.LLMProvider{}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{3}).Return(map[int64]int{}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{3}).Return(map[int64]int64{}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(3), 5).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(3), 20).Return(nil, nil)

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			if assert.Len(t, resp.Agents, 1) {
				assert.Equal(t, "plan", resp.Agents[0].DefaultPermissionMode)
				assert.Equal(t, string(agent_backend_entity.TypeClaudeCode), resp.Agents[0].BackendType)
			}
		})

		convey.Convey("非 claudecode 后端不带 DefaultPermissionMode（codex 用自己的 collaboration mode）", func() {
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 4, Name: "Codex", AgentBackendID: 18, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{18}).Return(map[int64]*agent_backend_entity.AgentBackend{
				18: {
					ID:             18,
					Type:           string(agent_backend_entity.TypeCodex),
					LLMProviderKey: "",
					Status:         consts.ACTIVE,
				},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{}).Return(map[string]*llm_provider_entity.LLMProvider{}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{4}).Return(map[int64]int{}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{4}).Return(map[int64]int64{}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(4), 5).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(4), 20).Return(nil, nil)

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			if assert.Len(t, resp.Agents, 1) {
				assert.Empty(t, resp.Agents[0].DefaultPermissionMode)
			}
		})

		convey.Convey("CEO 默认 Pinned，但无后端时 Chattable=false", func() {
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 1, Name: "CEO 助手", SystemBadge: agent_entity.SystemBadgeDefault, AgentBackendID: 0, Status: consts.ACTIVE},
				{ID: 2, Name: "工程师", AgentBackendID: 7, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{7}).Return(map[int64]*agent_backend_entity.AgentBackend{
				7: {ID: 7, Type: "builtin", LLMProviderKey: "key-11", Status: consts.ACTIVE},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{"key-11"}).Return(map[string]*llm_provider_entity.LLMProvider{
				"key-11": {ID: 11, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE},
			}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{1, 2}).Return(map[int64]int{1: 0, 2: 3}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{1, 2}).Return(map[int64]int64{1: 0, 2: 12}, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(1), 20).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(2), 20).Return([]*chat_entity.Session{
				{ID: 50, AgentID: 2, Title: "approve me", AgentStatus: "waiting", LastMessageAt: 1700000005000},
			}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(1), 5).Return(nil, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(2), 5).Return([]*chat_entity.Session{
				{ID: 99, AgentID: 2, Title: "修复 #142", AgentStatus: "running", LastMessageAt: 1700000000000},
			}, nil)

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			assert.Len(t, resp.Agents, 2)
			assert.True(t, resp.Agents[0].Pinned)
			assert.False(t, resp.Agents[0].Chattable)
			assert.True(t, resp.Agents[1].Chattable)
			assert.Equal(t, 3, resp.Agents[1].ActiveCount)
			assert.Equal(t, "修复 #142", resp.Agents[1].Sessions[0].Title)
			assert.Len(t, resp.Agents[0].AttentionSessions, 0, "CEO 没 attention session")
			if assert.Len(t, resp.Agents[1].AttentionSessions, 1) {
				assert.Equal(t, int64(50), resp.Agents[1].AttentionSessions[0].ID)
				assert.True(t, resp.Agents[1].AttentionSessions[0].NeedsAttention)
			}
		})
	})
}

func TestListAgents_PopulatesDeviceFields(t *testing.T) {
	convey.Convey("ListAgents device fields", t, func() {
		m := setupChatTest(t)
		ctx := context.Background()

		// 注入 mock remote_device_svc 并在测试结束后恢复 nil。
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockRDS := mock_remote_device_svc.NewMockRemoteDeviceSvc(ctrl)
		remote_device_svc.SetDefault(mockRDS)
		t.Cleanup(func() { remote_device_svc.SetDefault(nil) })

		convey.Convey("本地 backend (DeviceID='') → DeviceID/DeviceName/Online 均为零值", func() {
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 5, Name: "本地 Agent", AgentBackendID: 20, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{20}).Return(map[int64]*agent_backend_entity.AgentBackend{
				20: {ID: 20, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "", LLMProviderKey: "", Status: consts.ACTIVE},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{}).Return(map[string]*llm_provider_entity.LLMProvider{}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{5}).Return(map[int64]int{}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{5}).Return(map[int64]int64{}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(5), 5).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(5), 20).Return(nil, nil)
			// 本地 backend 不触发 remote_device_svc.Get

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			if assert.Len(t, resp.Agents, 1) {
				assert.Equal(t, "", resp.Agents[0].DeviceID)
				assert.Equal(t, "", resp.Agents[0].DeviceName)
				assert.False(t, resp.Agents[0].Online)
			}
		})

		convey.Convey("远端 backend + device 在线 → DeviceID/DeviceName/Online 填充", func() {
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 6, Name: "远端 Agent", AgentBackendID: 21, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{21}).Return(map[int64]*agent_backend_entity.AgentBackend{
				21: {ID: 21, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "7", LLMProviderKey: "", Status: consts.ACTIVE},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{}).Return(map[string]*llm_provider_entity.LLMProvider{}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{6}).Return(map[int64]int{}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{6}).Return(map[int64]int64{}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(6), 5).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(6), 20).Return(nil, nil)
			mockRDS.EXPECT().Get(ctx, int64(7)).Return(&remote_device_svc.DeviceView{
				ID: 7, Name: "linux-srv", Online: true,
			}, nil)

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			if assert.Len(t, resp.Agents, 1) {
				assert.Equal(t, "7", resp.Agents[0].DeviceID)
				assert.Equal(t, "linux-srv", resp.Agents[0].DeviceName)
				assert.True(t, resp.Agents[0].Online)
			}
		})

		convey.Convey("远端 backend + device 查询失败 → DeviceID 填入但 DeviceName/Online 留零值（不报错）", func() {
			m.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{
				{ID: 7, Name: "孤儿 Agent", AgentBackendID: 22, Status: consts.ACTIVE},
			}, nil)
			m.backend.EXPECT().BatchFind(ctx, []int64{22}).Return(map[int64]*agent_backend_entity.AgentBackend{
				22: {ID: 22, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "9", LLMProviderKey: "", Status: consts.ACTIVE},
			}, nil)
			m.provider.EXPECT().BatchFindByKey(ctx, []string{}).Return(map[string]*llm_provider_entity.LLMProvider{}, nil)
			m.session.EXPECT().CountRunningByAgents(ctx, []int64{7}).Return(map[int64]int{}, nil)
			m.session.EXPECT().CountByAgents(ctx, []int64{7}).Return(map[int64]int64{}, nil)
			m.session.EXPECT().ListByAgent(ctx, int64(7), 5).Return(nil, nil)
			m.session.EXPECT().ListAttentionByAgent(ctx, int64(7), 20).Return(nil, nil)
			mockRDS.EXPECT().Get(ctx, int64(9)).Return(nil, errors.New("device not found"))

			resp, err := m.svc.ListAgents(ctx, &chat_svc.ListAgentsRequest{})
			assert.NoError(t, err)
			if assert.Len(t, resp.Agents, 1) {
				assert.Equal(t, "9", resp.Agents[0].DeviceID, "DeviceID 应填入即使 device 查询失败")
				assert.Equal(t, "", resp.Agents[0].DeviceName)
				assert.False(t, resp.Agents[0].Online)
			}
		})
	})
}

func TestLoadSession(t *testing.T) {
	convey.Convey("LoadSession", t, func() {
		m := setupChatTest(t)
		ctx := context.Background()

		convey.Convey("正常返回 detail + messages 按 seq 升序", func() {
			m.session.EXPECT().Find(ctx, int64(3)).Return(&chat_entity.Session{
				ID: 3, AgentID: 7, Title: "draft", AgentStatus: "idle", LastMessageAt: 1, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, Name: "Eng", AvatarColor: "agent-2", AgentBackendID: 22, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(22)).Return(&agent_backend_entity.AgentBackend{
				ID: 22, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(3)).Return([]*chat_entity.Message{
				{ID: 10, SessionID: 3, Role: "user", BlocksJSON: `[{"type":"text","data":{"text":"hi"}}]`, Seq: 1},
				{ID: 11, SessionID: 3, Role: "assistant", BlocksJSON: `[{"type":"text","data":{"text":"hello"}}]`, Seq: 2, Model: "claude-sonnet-4-6"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 3})
			assert.NoError(t, err)
			assert.Equal(t, "Eng", resp.Session.AgentName)
			assert.Equal(t, string(agent_backend_entity.TypeClaudeCode), resp.Session.BackendType)
			assert.Len(t, resp.Messages, 2)
			assert.Equal(t, "hi", resp.Messages[0].Blocks[0].Text)
			assert.Equal(t, "claude-sonnet-4-6", resp.Messages[1].Model)
		})

		convey.Convey("session 不存在 → ChatSessionNotFound", func() {
			m.session.EXPECT().Find(ctx, int64(99)).Return(nil, nil)
			_, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 99})
			assert.Error(t, err)
		})

		convey.Convey("LLMProviderType 透传到 detail（前端按它判定 Usage 字段语义）", func() {
			convey.Convey("builtin + anthropic provider → llmProviderType=anthropic", func() {
				m.session.EXPECT().Find(ctx, int64(20)).Return(&chat_entity.Session{ID: 20, AgentID: 60, Status: consts.ACTIVE}, nil)
				m.agent.EXPECT().Find(ctx, int64(60)).Return(&agent_entity.Agent{ID: 60, AgentBackendID: 70, Status: consts.ACTIVE}, nil)
				m.backend.EXPECT().Find(ctx, int64(70)).Return(&agent_backend_entity.AgentBackend{
					ID: 70, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-80", Status: consts.ACTIVE,
				}, nil)
				m.provider.EXPECT().FindByKey(ctx, "key-80").Return(&llm_provider_entity.LLMProvider{
					ID: 80, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", Status: consts.ACTIVE,
				}, nil)
				m.message.EXPECT().List(ctx, int64(20)).Return(nil, nil)

				resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 20})
				assert.NoError(t, err)
				assert.Equal(t, string(llm_provider_entity.TypeAnthropic), resp.Session.LLMProviderType)
			})

			convey.Convey("builtin + openai-chat provider → llmProviderType=openai-chat", func() {
				m.session.EXPECT().Find(ctx, int64(21)).Return(&chat_entity.Session{ID: 21, AgentID: 61, Status: consts.ACTIVE}, nil)
				m.agent.EXPECT().Find(ctx, int64(61)).Return(&agent_entity.Agent{ID: 61, AgentBackendID: 71, Status: consts.ACTIVE}, nil)
				m.backend.EXPECT().Find(ctx, int64(71)).Return(&agent_backend_entity.AgentBackend{
					ID: 71, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-81", Status: consts.ACTIVE,
				}, nil)
				m.provider.EXPECT().FindByKey(ctx, "key-81").Return(&llm_provider_entity.LLMProvider{
					ID: 81, Type: string(llm_provider_entity.TypeOpenAIChat), Model: "gpt-4o", Status: consts.ACTIVE,
				}, nil)
				m.message.EXPECT().List(ctx, int64(21)).Return(nil, nil)

				resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 21})
				assert.NoError(t, err)
				assert.Equal(t, string(llm_provider_entity.TypeOpenAIChat), resp.Session.LLMProviderType)
			})

			convey.Convey("backend 无 provider 绑定（CLI 登录态）→ llmProviderType 留空", func() {
				m.session.EXPECT().Find(ctx, int64(22)).Return(&chat_entity.Session{ID: 22, AgentID: 62, Status: consts.ACTIVE}, nil)
				m.agent.EXPECT().Find(ctx, int64(62)).Return(&agent_entity.Agent{ID: 62, AgentBackendID: 72, Status: consts.ACTIVE}, nil)
				m.backend.EXPECT().Find(ctx, int64(72)).Return(&agent_backend_entity.AgentBackend{
					ID: 72, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
				}, nil)
				m.message.EXPECT().List(ctx, int64(22)).Return(nil, nil)

				resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 22})
				assert.NoError(t, err)
				assert.Empty(t, resp.Session.LLMProviderType)
			})
		})

		convey.Convey("provider.ContextWindow > 0 → 直接透传到 detail", func() {
			m.session.EXPECT().Find(ctx, int64(4)).Return(&chat_entity.Session{
				ID: 4, AgentID: 8, Title: "ctx", Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(8)).Return(&agent_entity.Agent{
				ID: 8, Name: "Eng", AgentBackendID: 33, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(33)).Return(&agent_backend_entity.AgentBackend{
				ID: 33, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-44", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-44").Return(&llm_provider_entity.LLMProvider{
				ID: 44, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", ContextWindow: 200000, Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(4)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 4})
			assert.NoError(t, err)
			assert.Equal(t, 200000, resp.Session.ContextWindow)
		})

		convey.Convey("provider.ContextWindow == 0 → 走 cago catalog 兜底", func() {
			m.session.EXPECT().Find(ctx, int64(5)).Return(&chat_entity.Session{
				ID: 5, AgentID: 9, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(9)).Return(&agent_entity.Agent{
				ID: 9, AgentBackendID: 34, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(34)).Return(&agent_backend_entity.AgentBackend{
				ID: 34, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-45", Status: consts.ACTIVE,
			}, nil)
			// ContextWindow 留 0；Model 取 cago 内置 catalog 已知的 claude-sonnet-4-6
			m.provider.EXPECT().FindByKey(ctx, "key-45").Return(&llm_provider_entity.LLMProvider{
				ID: 45, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(5)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 5})
			assert.NoError(t, err)
			assert.Greater(t, resp.Session.ContextWindow, 0, "应从 cago catalog 兜底解析出 contextWindow")
		})

		convey.Convey("backend 无 provider 绑定 → contextWindow 留 0", func() {
			m.session.EXPECT().Find(ctx, int64(6)).Return(&chat_entity.Session{
				ID: 6, AgentID: 10, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(10)).Return(&agent_entity.Agent{
				ID: 10, AgentBackendID: 35, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(35)).Return(&agent_backend_entity.AgentBackend{
				ID: 35, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "", Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(6)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 6})
			assert.NoError(t, err)
			assert.Equal(t, 0, resp.Session.ContextWindow)
		})

		convey.Convey("CLI login + 最新 assistant.Model → 走 catalog 解析（无 provider 也能拿到 contextWindow）", func() {
			// claudecode CLI 自身 login（LLMProviderKey=""），runner 已把 system.init.model 写回 message.Model。
			m.session.EXPECT().Find(ctx, int64(7)).Return(&chat_entity.Session{
				ID: 7, AgentID: 11, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{
				ID: 11, AgentBackendID: 40, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(40)).Return(&agent_backend_entity.AgentBackend{
				ID: 40, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(7)).Return([]*chat_entity.Message{
				{ID: 80, SessionID: 7, Role: "user", BlocksJSON: "[]", Seq: 1},
				{ID: 81, SessionID: 7, Role: "assistant", BlocksJSON: "[]", Seq: 2, Model: "claude-sonnet-4-6"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 7})
			assert.NoError(t, err)
			assert.Greater(t, resp.Session.ContextWindow, 0, "无 provider 也应从 message.Model 反查 catalog 拿 contextWindow")
		})

		convey.Convey("claudecode + provider 未显式配置 ContextWindow → 用 message.Model 反映实际运行模型", func() {
			// claudecode 后端，provider.Model=sonnet 但 ContextWindow=0（未显式配），message.Model=haiku-4-5。
			// 新优先级：第 1 级 provider.ContextWindow 未命中（=0）→ 落到第 2 级 message.Model catalog。
			m.session.EXPECT().Find(ctx, int64(8)).Return(&chat_entity.Session{ID: 8, AgentID: 12, Status: consts.ACTIVE}, nil)
			m.agent.EXPECT().Find(ctx, int64(12)).Return(&agent_entity.Agent{
				ID: 12, AgentBackendID: 41, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(41)).Return(&agent_backend_entity.AgentBackend{
				ID: 41, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-46", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-46").Return(&llm_provider_entity.LLMProvider{
				ID: 46, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", ContextWindow: 0, Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(8)).Return([]*chat_entity.Message{
				{ID: 90, SessionID: 8, Role: "assistant", BlocksJSON: "[]", Seq: 1, Model: "claude-haiku-4-5"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 8})
			assert.NoError(t, err)
			assert.Equal(t, 200000, resp.Session.ContextWindow, "应取 haiku 的 200k，而不是 sonnet 的 1M")
		})

		convey.Convey("claudecode + provider.ContextWindow > 0 → 显式配置覆盖 message.Model catalog", func() {
			// 核心新行为：LLM 供应商显式配了 500k，即使 message.Model=haiku（catalog 200k），也应取 500k。
			m.session.EXPECT().Find(ctx, int64(9)).Return(&chat_entity.Session{ID: 9, AgentID: 13, Status: consts.ACTIVE}, nil)
			m.agent.EXPECT().Find(ctx, int64(13)).Return(&agent_entity.Agent{
				ID: 13, AgentBackendID: 42, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(42)).Return(&agent_backend_entity.AgentBackend{
				ID: 42, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-47", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-47").Return(&llm_provider_entity.LLMProvider{
				ID: 47, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", ContextWindow: 500000, Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(9)).Return([]*chat_entity.Message{
				{ID: 100, SessionID: 9, Role: "assistant", BlocksJSON: "[]", Seq: 1, Model: "claude-haiku-4-5"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 9})
			assert.NoError(t, err)
			assert.Equal(t, 500000, resp.Session.ContextWindow, "provider 显式 ContextWindow 应覆盖 message.Model catalog")
		})

		convey.Convey("session.ContextWindow > 0 → runtime 上报值覆盖所有 fallback", func() {
			// codex app-server 推的 modelContextWindow 已经落到 session.context_window 列，
			// 即使 provider 配了不同的窗口、message.Model 也指向另一个模型，runtime 实测值最权威。
			m.session.EXPECT().Find(ctx, int64(10)).Return(&chat_entity.Session{
				ID: 10, AgentID: 14, ContextWindow: 258400, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(14)).Return(&agent_entity.Agent{
				ID: 14, AgentBackendID: 43, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(43)).Return(&agent_backend_entity.AgentBackend{
				ID: 43, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "key-48", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-48").Return(&llm_provider_entity.LLMProvider{
				ID: 48, Type: string(llm_provider_entity.TypeOpenAIResponse), Model: "gpt-5.4", ContextWindow: 100000, Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(10)).Return([]*chat_entity.Message{
				{ID: 110, SessionID: 10, Role: "assistant", BlocksJSON: "[]", Seq: 1, Model: "gpt-5-codex"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 10})
			assert.NoError(t, err)
			assert.Equal(t, 258400, resp.Session.ContextWindow, "runtime 上报值应优先于 provider 配置和 catalog")
		})

		convey.Convey("message.Model 带未注册的 dated 后缀 → llmcatalog.Lookup 前缀匹配兜底", func() {
			// 模拟 runner 上报 haiku 的新日期版本（cago alias 还没收录这个具体日期），
			// llmcatalog.Lookup 按前缀匹配命中 claude-haiku-4-5 的 200k 窗口。
			m.session.EXPECT().Find(ctx, int64(11)).Return(&chat_entity.Session{
				ID: 11, AgentID: 15, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(15)).Return(&agent_entity.Agent{
				ID: 15, AgentBackendID: 44, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(44)).Return(&agent_backend_entity.AgentBackend{
				ID: 44, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(11)).Return([]*chat_entity.Message{
				{ID: 120, SessionID: 11, Role: "assistant", BlocksJSON: "[]", Seq: 1, Model: "claude-haiku-4-5-20260515"},
			}, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 11})
			assert.NoError(t, err)
			assert.Equal(t, 200000, resp.Session.ContextWindow, "前缀匹配 claude-haiku-4-5 应拿到 200k 窗口")
		})
	})
}

func TestLoadSession_PopulatesDeviceFields(t *testing.T) {
	convey.Convey("LoadSession device fields", t, func() {
		m := setupChatTest(t)
		ctx := context.Background()

		// 注入 mock remote_device_svc 并在测试结束后恢复 nil。
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		mockRDS := mock_remote_device_svc.NewMockRemoteDeviceSvc(ctrl)
		remote_device_svc.SetDefault(mockRDS)
		t.Cleanup(func() { remote_device_svc.SetDefault(nil) })

		// 注入 CwdResolver 并在测试结束后清空。
		chat_svc.RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
			return "/Users/me/proj", nil
		})
		t.Cleanup(func() { chat_svc.RegisterCwdResolver(nil) })

		convey.Convey("本地 backend (DeviceID='') → DeviceID/DeviceName/Online 均为零值, Cwd 由 CwdResolver 填充", func() {
			m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
				ID: 100, AgentID: 50, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(50)).Return(&agent_entity.Agent{
				ID: 50, Name: "本地 Agent", AgentBackendID: 60, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(60)).Return(&agent_backend_entity.AgentBackend{
				ID: 60, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "", Status: consts.ACTIVE,
			}, nil)
			m.message.EXPECT().List(ctx, int64(100)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 100})
			assert.NoError(t, err)
			assert.Equal(t, "", resp.Session.DeviceID)
			assert.Equal(t, "", resp.Session.DeviceName)
			assert.False(t, resp.Session.Online)
			assert.Equal(t, "/Users/me/proj", resp.Session.Cwd)
		})

		convey.Convey("远端 backend + device 在线 → DeviceID/DeviceName/Online 填充", func() {
			m.session.EXPECT().Find(ctx, int64(101)).Return(&chat_entity.Session{
				ID: 101, AgentID: 51, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(51)).Return(&agent_entity.Agent{
				ID: 51, Name: "远端 Agent", AgentBackendID: 61, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(61)).Return(&agent_backend_entity.AgentBackend{
				ID: 61, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "7", Status: consts.ACTIVE,
			}, nil)
			mockRDS.EXPECT().Get(ctx, int64(7)).Return(&remote_device_svc.DeviceView{
				ID: 7, Name: "linux-srv", Online: true,
			}, nil)
			m.message.EXPECT().List(ctx, int64(101)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 101})
			assert.NoError(t, err)
			assert.Equal(t, "7", resp.Session.DeviceID)
			assert.Equal(t, "linux-srv", resp.Session.DeviceName)
			assert.True(t, resp.Session.Online)
		})

		convey.Convey("远端 backend + device 查询失败 → DeviceID 填入但 DeviceName/Online 留零值（不报错）", func() {
			m.session.EXPECT().Find(ctx, int64(102)).Return(&chat_entity.Session{
				ID: 102, AgentID: 52, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(52)).Return(&agent_entity.Agent{
				ID: 52, Name: "孤儿 Agent", AgentBackendID: 62, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(62)).Return(&agent_backend_entity.AgentBackend{
				ID: 62, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "9", Status: consts.ACTIVE,
			}, nil)
			mockRDS.EXPECT().Get(ctx, int64(9)).Return(nil, errors.New("device not found"))
			m.message.EXPECT().List(ctx, int64(102)).Return(nil, nil)

			resp, err := m.svc.LoadSession(ctx, &chat_svc.LoadSessionRequest{SessionID: 102})
			assert.NoError(t, err)
			assert.Equal(t, "9", resp.Session.DeviceID, "DeviceID 应填入即使 device 查询失败")
			assert.Equal(t, "", resp.Session.DeviceName)
			assert.False(t, resp.Session.Online)
		})
	})
}

func TestGetLaunchCommand(t *testing.T) {
	convey.Convey("GetLaunchCommand", t, func() {
		t.Setenv("AGENTRE_DATA_DIR", t.TempDir()) // 让 agentruntime.AgentCwd 落在临时目录

		// gateway 注入：BuildLaunchCommand 在 provider 非空时需要 gateway URL。
		chat_svc.RegisterGateway(&fakeChatGateway{
			status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		})
		t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

		m := setupChatTest(t)
		ctx := context.Background()

		convey.Convey("claudecode + provider → 单行命令含 BASE_URL、永久 token、model、--resume", func() {
			m.session.EXPECT().Find(ctx, int64(3)).Return(&chat_entity.Session{
				ID: 3, AgentID: 7, ProviderSessionID: "sess-uuid", Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 22, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(22)).Return(&agent_backend_entity.AgentBackend{
				ID: 22, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-33", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-33").Return(&llm_provider_entity.LLMProvider{
				ID: 33, Type: string(llm_provider_entity.TypeAnthropic), Model: "claude-sonnet-4-6", Status: consts.ACTIVE,
			}, nil)

			resp, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 3})
			assert.NoError(t, err)
			assert.Equal(t, string(agent_backend_entity.TypeClaudeCode), resp.BackendType)
			// 单行命令
			assert.NotContains(t, resp.Command, "\n")
			// gateway URL + fake gateway 发出的真实 token（"chat-token"）
			assert.Contains(t, resp.Command, "ANTHROPIC_BASE_URL='http://127.0.0.1:60080'")
			assert.Contains(t, resp.Command, "ANTHROPIC_AUTH_TOKEN='chat-token'")
			// 没有 <TOKEN> 占位符泄漏
			assert.NotContains(t, resp.Command, "<TOKEN>")
			// model + resume
			assert.Contains(t, resp.Command, "claude --model claude-sonnet-4-6 --resume sess-uuid")
		})

		convey.Convey("codex + provider session → 单行命令用 resume 子命令带 session id", func() {
			m.session.EXPECT().Find(ctx, int64(6)).Return(&chat_entity.Session{
				ID: 6, AgentID: 8, ProviderSessionID: "codex-thread-123", Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(ctx, int64(8)).Return(&agent_entity.Agent{
				ID: 8, AgentBackendID: 23, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(23)).Return(&agent_backend_entity.AgentBackend{
				ID: 23, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "key-34", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-34").Return(&llm_provider_entity.LLMProvider{
				ID: 34, Type: string(llm_provider_entity.TypeOpenAIResponse), Model: "gpt-5-codex", Status: consts.ACTIVE,
			}, nil)

			resp, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 6})
			assert.NoError(t, err)
			assert.Equal(t, string(agent_backend_entity.TypeCodex), resp.BackendType)
			assert.NotContains(t, resp.Command, "\n")
			assert.Contains(t, resp.Command, "OPENAI_API_KEY='chat-token'")
			assert.Contains(t, resp.Command, "codex resume")
			assert.Contains(t, resp.Command, " codex-thread-123")
			assert.Contains(t, resp.Command, `-c 'model="gpt-5-codex"'`)
		})

		convey.Convey("builtin → ChatLaunchCommandNotAvailable", func() {
			m.session.EXPECT().Find(ctx, int64(4)).Return(&chat_entity.Session{ID: 4, AgentID: 9, Status: consts.ACTIVE}, nil)
			m.agent.EXPECT().Find(ctx, int64(9)).Return(&agent_entity.Agent{ID: 9, AgentBackendID: 5, Status: consts.ACTIVE}, nil)
			m.backend.EXPECT().Find(ctx, int64(5)).Return(&agent_backend_entity.AgentBackend{
				ID: 5, Type: string(agent_backend_entity.TypeBuiltin), Status: consts.ACTIVE,
			}, nil)

			_, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 4})
			assert.Error(t, err)
		})

		convey.Convey("session 不存在 → ChatSessionNotFound", func() {
			m.session.EXPECT().Find(ctx, int64(404)).Return(nil, nil)
			_, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 404})
			assert.Error(t, err)
		})

		convey.Convey("SessionID <= 0 → InvalidParameter", func() {
			_, err := m.svc.GetLaunchCommand(ctx, &chat_svc.LaunchCommandRequest{SessionID: 0})
			assert.Error(t, err)
		})
	})
}

type recordingRunner struct {
	requests chan agentruntime.RunRequest
}

// Capabilities 返一份联合 PermissionModeMeta —— recordingRunner 会同时被
// claudecode / codex 的测试 swap 进来,所以 AllowedModes 给两边的并集,
// SwitchableDuringTurn=true 保证不会误命中"飞行中拒切"分支。
func (*recordingRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			DefaultMode:          "acceptEdits",
			SwitchableDuringTurn: true,
		},
	}
}
func (r *recordingRunner) Run(_ context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	r.requests <- req
	events := make(chan agentruntime.Event, 1)
	events <- agentruntime.TextDelta{Text: "ok"}
	close(events)
	return events, &agentruntime.RunResult{ProviderSessionID: "builtin-100"}, nil
}

type compactRecordingRunner struct {
	*recordingRunner
}

func (r *compactRecordingRunner) Capabilities() capability.Capabilities {
	base := r.recordingRunner.Capabilities()
	base.Set = map[capability.Capability]bool{
		capability.CapCompact: true,
	}
	return base
}

func (r *compactRecordingRunner) Run(_ context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	r.requests <- req
	events := make(chan agentruntime.Event, 1)
	events <- agentruntime.CompactBoundary{Trigger: "manual"}
	close(events)
	return events, &agentruntime.RunResult{ProviderSessionID: req.ProviderSessionID}, nil
}

type streamErrorRunner struct{}

func (streamErrorRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }
func (r streamErrorRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	events := make(chan agentruntime.Event, 2)
	events <- agentruntime.TextDelta{Text: "partial answer"}
	events <- agentruntime.ErrorEvent{Err: errors.New("upstream failed")}
	close(events)
	return events, &agentruntime.RunResult{}, nil
}

type streamRetryRunner struct{}

func (streamRetryRunner) Capabilities() capability.Capabilities { return capability.Capabilities{} }
func (r streamRetryRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	events := make(chan agentruntime.Event, 3)
	events <- agentruntime.Retry{
		Message: "Reconnecting... 1/5",
		Details: "high demand",
		Attempt: 1,
		Max:     5,
	}
	events <- agentruntime.TextDelta{Text: "recovered"}
	events <- agentruntime.Done{}
	close(events)
	return events, &agentruntime.RunResult{}, nil
}

type streamSteerConsumedRunner struct{}

func (streamSteerConsumedRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (r streamSteerConsumedRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	events := make(chan agentruntime.Event, 3)
	events <- agentruntime.TextDelta{Text: "before "}
	events <- agentruntime.SteerConsumed{
		Steers: []agentruntime.ConsumedSteer{{QueuedID: "qid-1", Text: "follow-up"}},
	}
	events <- agentruntime.TextDelta{Text: "after"}
	close(events)
	return events, &agentruntime.RunResult{}, nil
}

func TestSend_NewSession(t *testing.T) {
	convey.Convey("Send 新建 session 走通", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx // must carry DB for Transaction
		firstUserText := "优化一下 Edit/Write/file_change，能不能把 live 和 replay 通路统一起来，不要切走回来才正常"

		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
			PromptJSON: `["You are helpful."]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: "builtin", LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
			Model: "claude-sonnet-4-6",
		}, nil)

		fp := providertest.New().
			QueueStream(
				provider.StreamChunk{ContentDelta: "hello"},
				provider.StreamChunk{ContentDelta: "world"},
				provider.StreamChunk{FinishReason: provider.FinishStop, Usage: &provider.Usage{PromptTokens: 5, CompletionTokens: 2}},
			)
		chat_svc.SetProviderBuilderForTest(func(_ *llm_provider_entity.LLMProvider) (provider.Provider, error) {
			return fp, nil
		})
		t.Cleanup(chat_svc.ResetProviderBuilderForTest)

		m.session.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				assert.Equal(t, firstUserText, s.Title)
				s.ID = 100
				return nil
			})

		// Transaction calls: Begin + repo calls via mock + Commit
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
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).Times(1) // inside transaction
		m.dbMock.ExpectCommit()

		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", BlocksJSON: encodeText(firstUserText), Seq: 1},
			{ID: 1001, SessionID: 100, Role: "assistant", BlocksJSON: "[]", Seq: 2},
		}, nil).AnyTimes()
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes() // post-turn updates
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: firstUserText})
		assert.NoError(t, err)
		assert.Equal(t, int64(100), resp.SessionID)
		assert.NotZero(t, resp.AssistantMessageID)

		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

		var got string
		for _, ev := range m.events {
			payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
			if !ok {
				continue
			}
			if payload.Kind == chat_svc.StreamChunk {
				got += payload.Delta
			}
		}
		assert.Equal(t, "helloworld", got)
	})
}

func TestSend_ExistingSessionUsesSessionAgentBackend(t *testing.T) {
	// Given 已有会话属于 Agent 7,
	// When 前端异常传入另一个 AgentID,
	// Then Send 必须以 chat_sessions.agent_id 为准，避免 A 会话误跑 B 后端。
	m := setupChatTest(t)
	ctx := m.ctx
	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Correct", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
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

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 99, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		assert.Equal(t, int64(12), req.Backend.ID)
		assert.Equal(t, int64(7), req.AgentID)
		assert.Equal(t, int64(100), req.SessionID)
		assert.Equal(t, "hi", req.UserText)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}

func TestSend_CodexPermissionModePersistsAndStartsTurnInMode(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		PermissionMode: "default",
	}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)
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

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID:      100,
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		assert.Equal(t, "plan", req.CollaborationMode)
		assert.Equal(t, "", req.PermissionMode)
		assert.Equal(t, "plan", sess.PermissionMode)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}

func TestSend_CodexLocalDoesNotInjectGatewayDeps(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		token:  "chat-token",
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex Local", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
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

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID: 100,
		AgentID:   7,
		Text:      "hi",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		assert.Empty(t, req.GatewayURL)
		assert.Empty(t, req.GatewayToken)
		assert.Nil(t, req.Provider)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}

func TestCompact_CodexStartsCompactTurnWithoutUserMessage(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	runner := &compactRecordingRunner{recordingRunner: &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{
		ID:                100,
		AgentID:           7,
		AgentStatus:       "idle",
		Status:            consts.ACTIVE,
		ProviderSessionID: "codex-thread-123",
		PermissionMode:    "default",
	}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	var createdRoles []string
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			createdRoles = append(createdRoles, msg.Role)
			msg.ID = 1001
			return nil
		}).Times(1)
	m.dbMock.ExpectCommit()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Compact(ctx, &chat_svc.CompactRequest{SessionID: 100})
	assert.NoError(t, err)
	assert.Equal(t, int64(100), resp.SessionID)
	assert.Equal(t, int64(1001), resp.AssistantMessageID)
	assert.NotEmpty(t, resp.Stream)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	assert.Equal(t, []string{"assistant"}, createdRoles)

	select {
	case req := <-runner.requests:
		assert.True(t, req.Compact)
		assert.Empty(t, req.UserText)
		assert.Equal(t, "codex-thread-123", req.ProviderSessionID)
		assert.Equal(t, "default", req.CollaborationMode)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}

	var sawCompactBoundary bool
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok || payload.Kind != chat_svc.StreamCompactBoundary {
			continue
		}
		sawCompactBoundary = true
		require.NotNil(t, payload.Compact)
		assert.Equal(t, "manual", payload.Compact.Trigger)
	}
	assert.True(t, sawCompactBoundary, "compact turn should emit compact boundary divider")
}

func TestCompact_RequiresCodexProviderSessionAndCapability(t *testing.T) {
	t.Run("missing provider session", func(t *testing.T) {
		m := setupChatTest(t)
		ctx := context.Background()
		m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.Compact(ctx, &chat_svc.CompactRequest{SessionID: 100})
		assert.Error(t, err)
	})

	t.Run("non-codex backend", func(t *testing.T) {
		m := setupChatTest(t)
		ctx := context.Background()
		m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, ProviderSessionID: "thread-1",
		}, nil)
		m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Claude", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.Compact(ctx, &chat_svc.CompactRequest{SessionID: 100})
		assert.Error(t, err)
	})

	t.Run("codex runtime without compact capability", func(t *testing.T) {
		m := setupChatTest(t)
		ctx := context.Background()
		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(ctx, int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, ProviderSessionID: "thread-1",
		}, nil)
		m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.Compact(ctx, &chat_svc.CompactRequest{SessionID: 100})
		assert.Error(t, err)
	})
}

func TestSend_ClaudeCodeLocalKeepsGatewayDepsForHooks(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		token:  "chat-token",
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
	t.Cleanup(restore)

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
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

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID: 100,
		AgentID:   7,
		Text:      "hi",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		assert.Equal(t, "http://127.0.0.1:60080", req.GatewayURL)
		assert.Equal(t, "chat-token", req.GatewayToken)
		assert.Nil(t, req.Provider)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}

// capturingDaemonClient 抓住 remote.Runtime 通过 wire 向 daemon 发的
// runtime.run 请求体并立刻返错,让 chat_svc.runTurn 同步走到 failTurn,无需等待
// daemon 反向 notify。生产 *client.Client 是 WebSocket + JSON-RPC,这里只覆盖
// chat_svc → remote.Runtime 编码出的 wire.RunParams 字段。
type capturingDaemonClient struct {
	runParams chan wire.RunParams
}

func (c *capturingDaemonClient) Call(_ context.Context, method string, params, _ any) error {
	if method == wire.MethodRun {
		if p, ok := params.(wire.RunParams); ok {
			select {
			case c.runParams <- p:
			default:
			}
		}
		return errors.New("captured for test")
	}
	return nil
}

func (*capturingDaemonClient) Notify(_ string, _ any) error { return nil }
func (*capturingDaemonClient) Handle(_ string, _ func(context.Context, json.RawMessage) (any, error)) {
}
func (*capturingDaemonClient) Closed() <-chan struct{} { return nil }
func (*capturingDaemonClient) Close() error            { return nil }

// TestSend_ClaudeCodeRemoteSkipsClientGatewayDeps 回归用户报告:
//   - agentred 部署在 local-coding,desktop 把本机 gateway URL (127.0.0.1:52401)
//     和明文 Provider 实体发给远端 claudecode 子进程,导致子进程拨自己的 loopback
//     拿到 "API Error: Unable to connect to API (ConnectionRefused)"。
//   - 根因:chat_svc.runTurn 无脑调 signChatTokenFor + 把 prov 塞进 req.Provider,
//     不区分本地/远端。远端 daemon 自己有 ProviderLookup + Gateway,该自家解。
//   - 修法:be.IsRemote() 时跳过 signChatTokenFor、清空 req.Provider,让 daemon
//     handlers/runtime.go 走自家 Lookup → 自家 Gateway 路径。同时也防止 APIKey
//     每个 turn 越线漂移。
func TestSend_ClaudeCodeRemoteSkipsClientGatewayDeps(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		token:  "chat-token",
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	capture := &capturingDaemonClient{runParams: make(chan wire.RunParams, 1)}

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pool := mock_remote_device_svc.NewMockConnPool(ctrl)
	lease := mock_remote_device_svc.NewMockLease(ctrl)
	pool.EXPECT().Borrow(gomock.Any(), int64(7)).Return(lease, nil).AnyTimes()
	lease.EXPECT().Client().Return(capture).AnyTimes()
	lease.EXPECT().Closed().Return(make(chan struct{})).AnyTimes()
	lease.EXPECT().Release().AnyTimes()
	chat_svc.SetConnPoolForTest(m.svc, pool)
	t.Cleanup(func() { chat_svc.SetConnPoolForTest(m.svc, nil) })

	sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Claude Remote", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode),
		LLMProviderKey: "key-5", DeviceID: "7", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-5").Return(&llm_provider_entity.LLMProvider{
		ID: 5, Type: string(llm_provider_entity.TypeAnthropic), Name: "huu-glm",
		APIKey:  "secret-key-should-not-cross-the-wire",
		BaseURL: "https://huu.dqy.ink", Status: consts.ACTIVE,
	}, nil).AnyTimes()
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

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID: 100, AgentID: 7, Text: "hi",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case p := <-capture.runParams:
		// 直接 marshal wire payload 后做子串扫描:即便未来谁手痒给 wire.RunParams 加
		// 回一个 GatewayURL/Token/Provider 之类字段,只要值真的越线进了 wire,这条
		// 断言就会红。比"字段级 == 空"更耐重命名/重构。
		raw, err := json.Marshal(p)
		require.NoError(t, err)
		body := string(raw)
		assert.NotContains(t, body, "127.0.0.1",
			"remote backend wire 不应含 desktop 本机 gateway 地址")
		assert.NotContains(t, body, "chat-token",
			"remote backend wire 不应含 desktop 签的 token")
		assert.NotContains(t, body, "secret-key-should-not-cross-the-wire",
			"remote backend wire 不应含 LLM provider APIKey 明文")
		// wire 必须带 stable provider key 给 daemon 自查 keychain。
		assert.Contains(t, body, `"llmProviderKey":"key-5"`,
			"远端 backend wire 必须带 stable provider key 给 daemon 解 keychain")
		// 防回归: 老的 int id 字段不能再出现。
		assert.NotContains(t, body, `"llmProviderId"`,
			"老的 int id 不能再出现在 wire 上")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime.run RPC")
	}
}

// TestSend_NewClaudeCodeBypassSessionPersistsPermissionModeAtLaunch 回归用户报告:
//   - 新建 claudecode 会话首发选 bypass,发完第一轮 bypass pill 反而被错灰。
//   - 根因:runtime 在 spawn goroutine 里写 at_launch,前端 LoadSession 抢在它前面
//     拿到空串,之后再不 reload,permissionModeDisabledReason 看到 active+launch=""
//     就把 bypass 禁用。
//   - 修法:Send 同步路径里 chat_repo.Session().Create 时就把 PermissionModeAtLaunch
//     写成 createPermissionMode 解析值,保证 LoadSession 永远拿得到正确值。
func TestSend_NewClaudeCodeBypassSessionPersistsPermissionModeAtLaunch(t *testing.T) {
	convey.Convey("新建 claudecode 会话首发 bypass: session.Create 时 PermissionModeAtLaunch 已落库", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx
		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Claude", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, sess *chat_entity.Session) error {
				assert.Equal(t, "bypassPermissions", sess.PermissionMode)
				assert.Equal(t, "bypassPermissions", sess.PermissionModeAtLaunch,
					"at_launch 必须在 Send 同步写入,避免前端首轮 LoadSession 拿空串后 bypass 被错误禁用")
				sess.ID = 100
				return nil
			})
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
			AgentID:        7,
			Text:           "hi",
			PermissionMode: "bypassPermissions",
		})
		assert.NoError(t, err)
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

func TestSend_NewCodexSessionStoresPermissionModeBeforeFirstTurn(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
	t.Cleanup(restore)

	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, sess *chat_entity.Session) error {
			assert.Equal(t, "plan", sess.PermissionMode)
			sess.ID = 100
			return nil
		})
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	select {
	case req := <-runner.requests:
		assert.Equal(t, "plan", req.CollaborationMode)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for runtime request")
	}
}

func TestSend_CodexPlanUpdatedPersistsVisiblePlanBlock(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventPlanUpdated, Plan: []agentruntime.PlanStep{
			{Step: "Inspect files", Status: "completed"},
			{Step: "Describe next change", Status: "inProgress"},
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, PermissionMode: "plan",
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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

	var final *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			cp := *msg
			final = &cp
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID:      100,
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.NotNil(t, final)
	blocks, err := final.GetBlocks()
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	var planText string
	switch pb := blocks[0].(type) {
	case chat_svc.PlanBlock:
		planText = pb.Text
	case *chat_svc.PlanBlock:
		planText = pb.Text
	default:
		t.Fatalf("expected PlanBlock, got %T", blocks[0])
	}
	assert.Contains(t, planText, "Inspect files")
	assert.Contains(t, planText, "[>]")
}

func TestSend_CodexPlanItemTextPersistsVisiblePlanBlock(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventPlanUpdated, PlanText: "# Plan\n\n1. Inspect files\n2. Report findings\n"},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, PermissionMode: "plan",
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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

	var final *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			cp := *msg
			final = &cp
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID:      100,
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.NotNil(t, final)
	blocks, err := final.GetBlocks()
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	pb, ok := blocks[0].(chat_svc.PlanBlock)
	if !ok {
		ptr, ptrOK := blocks[0].(*chat_svc.PlanBlock)
		require.True(t, ptrOK, "expected PlanBlock, got %T", blocks[0])
		pb = *ptr
	}
	assert.Equal(t, "# Plan\n\n1. Inspect files\n2. Report findings\n", pb.Text)
}

func TestSend_ActionablePlanBlockMarksSessionWaiting(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, newEventRunner{events: []agentruntime.Event{
		agentruntime.PlanUpdated{Plan: canonical.PlanUpdate{
			Text: "# Plan\n\n1. Inspect files\n2. Report findings\n",
			Actions: []canonical.PlanAction{
				{ID: "plan.execute", Kind: canonical.PlanActionApprove},
				{ID: "plan.refine", Kind: canonical.PlanActionRefine, RequiresFeedback: true},
			},
		}},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, PermissionMode: "plan",
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)

	var sessionUpdates []*chat_entity.Session
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, sess *chat_entity.Session) error {
			cp := *sess
			sessionUpdates = append(sessionUpdates, &cp)
			return nil
		}).AnyTimes()

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

	var final *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			cp := *msg
			final = &cp
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID:      100,
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.NotEmpty(t, sessionUpdates)
	last := sessionUpdates[len(sessionUpdates)-1]
	assert.Equal(t, "waiting", last.AgentStatus)
	assert.True(t, last.NeedsAttention)

	require.NotNil(t, final)
	blocks, err := final.GetBlocks()
	require.NoError(t, err)
	require.Len(t, blocks, 1)
	pb, ok := blocks[0].(chat_svc.PlanBlock)
	if !ok {
		ptr, ptrOK := blocks[0].(*chat_svc.PlanBlock)
		require.True(t, ptrOK, "expected PlanBlock, got %T", blocks[0])
		pb = *ptr
	}
	require.Len(t, pb.Actions, 2)
	assert.Equal(t, "plan.execute", pb.Actions[0].ID)
}

func TestResolvePlanAction_CodexExecuteContinuesWaitingPlan(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	runner := captureRunRequestRunner{
		events:   []agentruntime.Event{agentruntime.TextDelta{Text: "executed"}},
		requests: make(chan agentruntime.RunRequest, 1),
	}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
	t.Cleanup(restore)

	planMsg := &chat_entity.Message{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2}
	require.NoError(t, planMsg.SetBlocks([]blocks.ContentBlock{chat_svc.PlanBlock{
		Text: "# Plan\n\n1. Execute",
		Actions: []canonical.PlanAction{
			{ID: "plan.execute", Kind: canonical.PlanActionApprove},
			{ID: "plan.refine", Kind: canonical.PlanActionRefine, RequiresFeedback: true},
		},
	}}))

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "waiting", Status: consts.ACTIVE, PermissionMode: "plan",
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
		{ID: 1000, SessionID: 100, Role: "user", Seq: 1},
		planMsg,
	}, nil).Times(2)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "default").Return(nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "user" {
				msg.ID = 1002
			} else {
				msg.ID = 1003
			}
			return nil
		}).Times(2)
	m.dbMock.ExpectCommit()
	var clearedPlanActions bool
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.ID == planMsg.ID {
				bs, err := msg.GetBlocks()
				require.NoError(t, err)
				require.Len(t, bs, 1)
				switch p := bs[0].(type) {
				case chat_svc.PlanBlock:
					clearedPlanActions = len(p.Actions) == 0
				case *chat_svc.PlanBlock:
					clearedPlanActions = p != nil && len(p.Actions) == 0
				default:
					t.Fatalf("expected PlanBlock, got %T", bs[0])
				}
			}
			return nil
		}).AnyTimes()

	resp, err := m.svc.ResolvePlanAction(ctx, &chat_svc.ResolvePlanActionRequest{
		SessionID: 100,
		ActionID:  canonical.PlanActionIDExecute,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, int64(100), resp.SessionID)
	assert.Equal(t, int64(1002), resp.UserMessageID)
	assert.Equal(t, int64(1003), resp.AssistantMessageID)
	assert.Equal(t, chat_svc.StreamName(100, 1003), resp.Stream)
	chat_svc.WaitForStreamForTest(m.svc, 1003)

	req := <-runner.requests
	assert.Equal(t, "Implement the plan.", req.UserText)
	assert.Equal(t, "default", req.CollaborationMode)
	assert.True(t, clearedPlanActions)
}

func TestSend_CodexPlanEmptyTurnPersistsFallbackText(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE, PermissionMode: "plan",
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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

	var final *chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			cp := *msg
			final = &cp
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{
		SessionID:      100,
		AgentID:        7,
		Text:           "hi",
		PermissionMode: "plan",
	})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.NotNil(t, final)
	gotBlocks, err := final.GetBlocks()
	require.NoError(t, err)
	require.Len(t, gotBlocks, 1)
	var text string
	switch tb := gotBlocks[0].(type) {
	case blocks.TextBlock:
		text = tb.Text
	case *blocks.TextBlock:
		text = tb.Text
	default:
		t.Fatalf("expected TextBlock, got %T", gotBlocks[0])
	}
	assert.Contains(t, text, "Plan mode completed")
}

func TestSend_StreamErrorEventCarriesFinalAssistantMessage(t *testing.T) {
	// Given 当前会话开始一轮回复
	// When runtime 流中断并返回 EventError
	// Then error 事件必须携带带 errorText 的最终 assistant 消息，让前端无需切换会话即可刷新当前 transcript。
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, streamErrorRunner{})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var errorEvent *chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if ok && payload.Kind == chat_svc.StreamError {
			cp := payload
			errorEvent = &cp
			break
		}
	}
	if assert.NotNil(t, errorEvent) && assert.NotNil(t, errorEvent.Message) {
		assert.Equal(t, "upstream failed", errorEvent.Error)
		assert.Equal(t, int64(1001), errorEvent.Message.ID)
		assert.Equal(t, "upstream failed", errorEvent.Message.ErrorText)
		assert.Equal(t, "partial answer", errorEvent.Message.Blocks[0].Text)
	}
}

func TestSend_StreamRetryEventIsForwardedWithoutFailingTurn(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, streamRetryRunner{})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var retryEvent *chat_svc.ChatStreamEvent
	var sawDone bool
	var sawError bool
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		switch payload.Kind {
		case chat_svc.StreamRetry:
			cp := payload
			retryEvent = &cp
		case chat_svc.StreamDone:
			sawDone = true
		case chat_svc.StreamError:
			sawError = true
		}
	}
	if assert.NotNil(t, retryEvent) {
		assert.Equal(t, 1, retryEvent.RetryAttempt)
		assert.Equal(t, 5, retryEvent.RetryMaxAttempts)
		assert.Equal(t, "Reconnecting... 1/5", retryEvent.RetryMessage)
		assert.Equal(t, "high demand", retryEvent.RetryDetails)
		assert.NotZero(t, retryEvent.RetryAt)
	}
	assert.True(t, sawDone)
	assert.False(t, sawError)
}

// TestSend_StreamUsageEventsAreForwardedAndPersisted —— turn 内每次 EventUsage 都应：
//  1. emit 一条 StreamUsage（payload 字段一致），让前端 Composer 进度条实时刷新；
//  2. patch assistantMsg 的 token 列（per-frame Update 落库）；
//  3. turn 末尾的 RunResult.Usage 仍然覆盖一次（兜底口径不变）。
func TestSend_StreamUsageEventsAreForwardedAndPersisted(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	// 一个吐两帧 EventUsage 的 fake runner —— 模拟 turn 内两次内部 API call 的边界。
	// 第二帧 token 比第一帧大，断言「最终态 = 第二帧」就能验证累积语义没颠倒。
	runner := scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventTextDelta, Text: "thinking..."},
		{Kind: agentruntime.EventUsage, Usage: &provider.Usage{
			PromptTokens: 200, CompletionTokens: 50, CachedTokens: 10000, CacheCreationTokens: 0,
		}},
		{Kind: agentruntime.EventUsage, Usage: &provider.Usage{
			PromptTokens: 50, CompletionTokens: 20, CachedTokens: 10300, CacheCreationTokens: 50,
		}},
		{Kind: agentruntime.EventDone},
	}}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

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
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()

	// 记录 assistant 消息 Update 的所有快照，确保中间帧、turn 末尾都至少写到一次最新值。
	var assistantSnaps []chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "assistant" {
				assistantSnaps = append(assistantSnaps, *msg)
			}
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	// 收到的 StreamUsage 事件：两条，分别对应两帧 EventUsage。
	var usages []chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if ok && payload.Kind == chat_svc.StreamUsage {
			usages = append(usages, payload)
		}
	}
	require.Len(t, usages, 2, "每帧 EventUsage 应当 emit 一条 StreamUsage")
	require.NotNil(t, usages[0].Usage)
	assert.Equal(t, int64(1001), usages[0].Usage.MessageID, "StreamUsage 必须带 assistantMsg.ID 让前端按消息匹配")
	assert.Equal(t, 200, usages[0].Usage.PromptTokens)
	assert.Equal(t, 10000, usages[0].Usage.CachedTokens)
	require.NotNil(t, usages[1].Usage)
	assert.Equal(t, 50, usages[1].Usage.PromptTokens)
	assert.Equal(t, 10300, usages[1].Usage.CachedTokens)
	assert.Equal(t, 50, usages[1].Usage.CacheCreationTokens)

	// assistantMsg 至少被 Update 了 (per-frame 两次 + turn 末尾一次) = 3 次。
	// 最终落库的快照必须是第二帧（更晚到达，覆盖了第一帧）。
	require.GreaterOrEqual(t, len(assistantSnaps), 3,
		"两帧 EventUsage 各 Update 一次 + turn 末尾再 Update 一次")
	final := assistantSnaps[len(assistantSnaps)-1]
	assert.Equal(t, 50, final.PromptTokens, "末态应为最后一帧 EventUsage 的 PromptTokens")
	assert.Equal(t, 10300, final.CachedTokens)
	assert.Equal(t, 50, final.CacheCreationTokens)
	assert.Equal(t, 20, final.CompletionTokens)
}

// scriptedRunner 按预设序列吐 RuntimeEvent 字面量(老 fixture 风格),内部转 NEW
// Event 喂给 chat_svc dispatcher。生产 runner 已直接发 NEW Event;这里保留
// RuntimeEvent 入参,是为了让大量老测试 fixture 字面量不必逐行重写,
// 通过 chat_svc.ConvertOldEventToNewForTest 桥接。
type scriptedRunner struct {
	events []agentruntime.RuntimeEvent
}

// Capabilities 返联合 meta(同 recordingRunner)—— scriptedRunner 也会被多个
// backend type 测试 swap 进来,统一给宽放白名单保证 chat_svc 校验放行。
func (scriptedRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			DefaultMode:          "acceptEdits",
			SwitchableDuringTurn: true,
		},
	}
}

func (r scriptedRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, len(r.events))
	for _, e := range r.events {
		if ev := chat_svc.ConvertOldEventToNewForTest(e); ev != nil {
			ch <- ev
		}
	}
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

type newEventRunner struct {
	events []agentruntime.Event
}

func (newEventRunner) Capabilities() capability.Capabilities {
	return scriptedRunner{}.Capabilities()
}

func (r newEventRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, len(r.events))
	for _, e := range r.events {
		ch <- e
	}
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

type captureRunRequestRunner struct {
	events   []agentruntime.Event
	requests chan agentruntime.RunRequest
}

func (r captureRunRequestRunner) Capabilities() capability.Capabilities {
	return scriptedRunner{}.Capabilities()
}

func (r captureRunRequestRunner) Run(_ context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event, len(r.events))
	for _, e := range r.events {
		ch <- e
	}
	close(ch)
	r.requests <- req
	return ch, &agentruntime.RunResult{}, nil
}

// captureSessionStatusPatches 抽出 emitter 收到的所有 StreamSessionStatus payload 序列。
// 单测靠它断言"等→应答"过程中 session 级 status 的翻转顺序。
func captureSessionStatusPatches(events []recorded) []chat_svc.ChatSessionStatusPatch {
	var out []chat_svc.ChatSessionStatusPatch
	for _, ev := range events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		if payload.Kind == chat_svc.StreamSessionStatus && payload.SessionStatus != nil {
			out = append(out, *payload.SessionStatus)
		}
	}
	return out
}

// standardSendMocks 把 Send 路径上必经的 Find / Create / Update mock 配好。
// turn 内 runtime 走 scriptedRunner，所以这里只关心 session/agent/backend/provider
// 元数据查询 + message create 两条（user + assistant）+ session update 系列。
//
// captured 是 session.Update 的最终落库参数序列：单测断言「最后一条 NeedsAttention=false」
// 用它。
func standardSendMocks(t *testing.T, m *chatMocks, sessionID, agentID, backendID int64, providerKey string) *[]chat_entity.Session {
	t.Helper()
	captured := standardSendMocksWithoutMessageUpdate(t, m, sessionID, agentID, backendID, providerKey)
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	return captured
}

func standardSendMocksWithoutMessageUpdate(t *testing.T, m *chatMocks, sessionID, agentID, backendID int64, providerKey string) *[]chat_entity.Session {
	t.Helper()
	m.session.EXPECT().Find(gomock.Any(), sessionID).Return(&chat_entity.Session{
		ID: sessionID, AgentID: agentID, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), agentID).Return(&agent_entity.Agent{
		ID: agentID, Name: "Eng", AgentBackendID: backendID, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), backendID).Return(&agent_backend_entity.AgentBackend{
		ID: backendID, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: providerKey, Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), providerKey).Return(&llm_provider_entity.LLMProvider{
		Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), sessionID).Return(1, nil)
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

	m.message.EXPECT().List(gomock.Any(), sessionID).Return(nil, nil).AnyTimes()

	captured := make([]chat_entity.Session, 0, 4)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
			captured = append(captured, *s)
			return nil
		}).AnyTimes()
	return &captured
}

// TestSend_AskUserQuestionFlipsSessionToWaiting:
//   - 收到 EventAskUserQuestion 应 emit StreamSessionStatus{agentStatus=waiting, needsAttention=true}
//   - 收到 EventAskUserQuestionAnswered 应 emit StreamSessionStatus{agentStatus=running, needsAttention=false}
//   - turn 收尾后 session 最终落库 AgentStatus=idle + NeedsAttention=false
func TestSend_AskUserQuestionFlipsSessionToWaiting(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventTextDelta, Text: "thinking..."},
		{Kind: agentruntime.EventAskUserQuestion, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "req-1",
			Questions: []agentruntime.AskQuestion{{
				Question: "Pick one",
				Options:  []agentruntime.AskOption{{Label: "A"}, {Label: "B"}},
			}},
		}},
		{Kind: agentruntime.EventAskUserQuestionAnswered, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "req-1",
			Answered:  true,
			Answers:   []agentruntime.AskAnswer{{QuestionIndex: 0, Labels: []string{"A"}}},
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	captured := standardSendMocks(t, m, 100, 7, 12, "key-21")

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	patches := captureSessionStatusPatches(m.events)
	require.Len(t, patches, 2, "AskUserQuestion + Answered 各 emit 一帧 StreamSessionStatus")
	assert.Equal(t, "waiting", patches[0].AgentStatus)
	assert.True(t, patches[0].NeedsAttention)
	assert.Equal(t, "running", patches[1].AgentStatus)
	assert.False(t, patches[1].NeedsAttention)

	require.NotEmpty(t, *captured, "session 至少落库一次")
	final := (*captured)[len(*captured)-1]
	assert.Equal(t, "idle", final.AgentStatus, "turn 收尾应翻 idle")
	assert.False(t, final.NeedsAttention, "turn 收尾应清掉 NeedsAttention")
}

func TestSend_AskUserQuestionCheckpointsWaitingCard(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventTextDelta, Text: "thinking..."},
		{Kind: agentruntime.EventAskUserQuestion, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "req-1",
			Questions: []agentruntime.AskQuestion{{
				Question: "Pick one",
				Options:  []agentruntime.AskOption{{Label: "A"}, {Label: "B"}},
			}},
		}},
	}})
	t.Cleanup(restore)

	_ = standardSendMocksWithoutMessageUpdate(t, m, 104, 7, 12, "key-21")

	var updates []chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "assistant" {
				updates = append(updates, *msg)
			}
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 104, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.GreaterOrEqual(t, len(updates), 2, "waiting request must checkpoint before final update")
	checkpointBlocks, err := updates[0].GetBlocks()
	require.NoError(t, err)
	require.True(t, hasBlockTypeForTest(checkpointBlocks, "user_ask"), "checkpoint must include the actionable user_ask card")
}

func TestSend_ToolPermissionCheckpointsWaitingCard(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventToolPermissionRequest, ToolPermission: &agentruntime.ToolPermissionEvent{
			RequestID: "perm-1",
			ToolName:  "Bash",
			Input:     []byte(`{"command":"ls"}`),
		}},
	}})
	t.Cleanup(restore)

	_ = standardSendMocksWithoutMessageUpdate(t, m, 105, 7, 12, "key-21")

	var updates []chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "assistant" {
				updates = append(updates, *msg)
			}
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 105, AgentID: 7, Text: "run ls"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.GreaterOrEqual(t, len(updates), 2, "tool permission request must checkpoint before final update")
	checkpointBlocks, err := updates[0].GetBlocks()
	require.NoError(t, err)
	require.True(t, hasBlockTypeForTest(checkpointBlocks, "tool_permission"), "checkpoint must include the actionable tool_permission card")
}

// TestSend_OrphanToolResultFromAskUserQuestionIsDropped:
//
//	复现 AskUserQuestion 答完后前端冒出无主 tool 条的根因 ——
//	  - runtime 翻译层（translateClaudeCodeEvent）对 EventPreToolUse 用 Tool.Name
//	    过滤掉了 AskUserQuestion 的 tool_use；
//	  - 但 pkg/claudecode/session.go 的 parseUserContent 给 EventPostToolUse 只填
//	    了 ID + Response、没填 Name；EventPostToolUse 的同名过滤拿不到 Name 就漏过；
//	  - 结果就是 chat_svc 会收到一条 ToolUseID 但 acc 里没有对应 tool_use 的孤儿
//	    EventToolResult，再继续 emit StreamToolResult，前端用默认 toolName="tool"
//	    渲染出幽灵卡，DB 里也会留下无主 ToolResultBlock。
//
//	这里模拟孤儿 EventToolResult 流过 chat_svc 事件循环，断言：
//	  1. emitter 上不会出现 StreamToolResult；
func TestSend_OrphanToolResultFromAskUserQuestionIsDropped(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	askToolID := "toolu_ask_001"
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventAskUserQuestion, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "req-orphan",
			Questions: []agentruntime.AskQuestion{{
				Question: "Pick one",
				Options:  []agentruntime.AskOption{{Label: "A"}, {Label: "B"}},
			}},
		}},
		{Kind: agentruntime.EventAskUserQuestionAnswered, AskUserQuestion: &agentruntime.AskUserQuestionEvent{
			RequestID: "req-orphan",
			Answered:  true,
			Answers:   []agentruntime.AskAnswer{{QuestionIndex: 0, Labels: []string{"A"}}},
		}},
		// translateClaudeCodeEvent 已经 drop 掉对应的 EventToolUseStart，
		// 但 EventToolResult 因 Name 为空逃过过滤漏到这里。
		{Kind: agentruntime.EventToolResult, ToolResult: &agentruntime.ToolResultEvent{
			ToolUseID: askToolID,
			Content:   `[{"label":"A"}]`,
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	_ = standardSendMocks(t, m, 102, 7, 12, "key-21")

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 102, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		if payload.Kind == chat_svc.StreamToolResult {
			t.Errorf("孤儿 tool_result 不应被 emit；payload.ToolUseID=%q toolResult=%q",
				payload.ToolUseID, payload.ToolResult)
		}
	}
}

func TestSend_CheckpointsAssistantWhenToolResultArrives(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventTextDelta, Text: "checking "},
		{Kind: agentruntime.EventToolUseStart, ToolUse: &agentruntime.ToolUseEvent{
			ID:    "toolu_1",
			Name:  "Bash",
			Input: []byte(`{"command":"pwd"}`),
		}},
		{Kind: agentruntime.EventToolResult, ToolResult: &agentruntime.ToolResultEvent{
			ToolUseID: "toolu_1",
			Content:   "/tmp/project",
		}},
		{Kind: agentruntime.EventTextDelta, Text: "done"},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(103)).Return(&chat_entity.Session{
		ID: 103, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(103)).Return(1, nil)
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

	m.message.EXPECT().List(gomock.Any(), int64(103)).Return(nil, nil).AnyTimes()
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	var updates []chat_entity.Message
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			updates = append(updates, *msg)
			return nil
		}).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 103, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	require.GreaterOrEqual(t, len(updates), 2, "tool_result checkpoint + final update should both persist assistant")
	checkpointBlocks, err := updates[0].GetBlocks()
	require.NoError(t, err)
	require.Len(t, checkpointBlocks, 3)
	assert.Equal(t, "checking ", blockTextForTest(t, checkpointBlocks[0]))
	assert.Equal(t, "toolu_1", toolUseIDForTest(t, checkpointBlocks[1]))
	assert.Equal(t, "toolu_1", toolResultIDForTest(t, checkpointBlocks[2]))

	finalBlocks, err := updates[len(updates)-1].GetBlocks()
	require.NoError(t, err)
	require.Len(t, finalBlocks, 4)
	assert.Equal(t, "done", blockTextForTest(t, finalBlocks[3]))
}

func blockTextForTest(t *testing.T, b blocks.ContentBlock) string {
	t.Helper()
	switch tb := b.(type) {
	case blocks.TextBlock:
		return tb.Text
	case *blocks.TextBlock:
		return tb.Text
	default:
		t.Fatalf("expected text block, got %T", b)
		return ""
	}
}

func hasBlockTypeForTest(bs []blocks.ContentBlock, typ string) bool {
	for _, b := range bs {
		if b != nil && b.Type() == typ {
			return true
		}
	}
	return false
}

func toolUseIDForTest(t *testing.T, b blocks.ContentBlock) string {
	t.Helper()
	switch tb := b.(type) {
	case blocks.ToolUseBlock:
		return tb.ID
	case *blocks.ToolUseBlock:
		return tb.ID
	default:
		t.Fatalf("expected tool use block, got %T", b)
		return ""
	}
}

func toolResultIDForTest(t *testing.T, b blocks.ContentBlock) string {
	t.Helper()
	switch tb := b.(type) {
	case blocks.ToolResultBlock:
		return tb.ToolUseID
	case *blocks.ToolResultBlock:
		return tb.ToolUseID
	default:
		t.Fatalf("expected tool result block, got %T", b)
		return ""
	}
}

// TestSend_ToolPermissionFlipsSessionToWaiting: ToolPermissionRequest / Resolved
// 对称走 ask 一样的 waiting → running 翻转。
func TestSend_ToolPermissionFlipsSessionToWaiting(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventToolPermissionRequest, ToolPermission: &agentruntime.ToolPermissionEvent{
			RequestID: "perm-1",
			ToolName:  "Bash",
			Input:     []byte(`{"command":"ls"}`),
		}},
		{Kind: agentruntime.EventToolPermissionResolved, ToolPermission: &agentruntime.ToolPermissionEvent{
			RequestID: "perm-1",
			ToolName:  "Bash",
			Input:     []byte(`{"command":"ls"}`),
			Resolved:  true,
			Allowed:   true,
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	captured := standardSendMocks(t, m, 101, 7, 12, "key-21")

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 101, AgentID: 7, Text: "run ls"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	patches := captureSessionStatusPatches(m.events)
	require.Len(t, patches, 2, "ToolPermission Request + Resolved 各 emit 一帧 StreamSessionStatus")
	assert.Equal(t, "waiting", patches[0].AgentStatus)
	assert.True(t, patches[0].NeedsAttention)
	assert.Equal(t, "running", patches[1].AgentStatus)
	assert.False(t, patches[1].NeedsAttention)

	require.NotEmpty(t, *captured)
	final := (*captured)[len(*captured)-1]
	assert.Equal(t, "idle", final.AgentStatus)
	assert.False(t, final.NeedsAttention)
}

func TestSend_SteerConsumedSplitsMessages(t *testing.T) {
	// Given 当前 assistant 已经流出部分内容且用户排队了一条 follow-up
	// When runtime 报告这条 follow-up 已被消费
	// Then chat_svc 把当前 assistant 收口，插入正式 user 消息，并把后续流切到新的 assistant。
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, streamSteerConsumedRunner{})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	createIDs := []int64{1000, 1001, 1002, 1003}
	createIdx := 0
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			msg.ID = createIDs[createIdx]
			createIdx++
			return nil
		}).Times(len(createIDs))

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
	m.dbMock.ExpectCommit()

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	m.dbMock.ExpectCommit()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var consumed *chat_svc.ChatStreamEvent
	var done *chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		switch payload.Kind {
		case chat_svc.StreamSteerConsumed:
			cp := payload
			consumed = &cp
		case chat_svc.StreamDone:
			cp := payload
			done = &cp
		}
	}

	if assert.NotNil(t, consumed) {
		assert.Equal(t, []string{"qid-1"}, consumed.QueuedIDs)
		if assert.NotNil(t, consumed.PreviousAssistantMessage) {
			assert.Equal(t, int64(1001), consumed.PreviousAssistantMessage.ID)
			assert.Equal(t, "before ", consumed.PreviousAssistantMessage.Blocks[0].Text)
		}
		if assert.Len(t, consumed.UserMessages, 1) {
			assert.Equal(t, int64(1002), consumed.UserMessages[0].ID)
			assert.Equal(t, "follow-up", consumed.UserMessages[0].Blocks[0].Text)
		}
		if assert.NotNil(t, consumed.AssistantMessage) {
			assert.Equal(t, int64(1003), consumed.AssistantMessage.ID)
			assert.Equal(t, 4, consumed.AssistantMessage.Seq)
		}
	}
	if assert.NotNil(t, done) && assert.NotNil(t, done.Message) {
		assert.Equal(t, int64(1003), done.Message.ID)
		assert.Equal(t, "after", done.Message.Blocks[0].Text)
	}
}

// autoContinueRunner 实现 BackendRunner + SteerDrainer：每次 Run 返一段简单
// 文本；第一次 DrainPending 返非空（模拟 turn 进行中又排了消息没被 hook 拉走），
// 之后返 nil。用来验证 chat_svc.runTurn 收尾时会取走残留并起新一轮。
type autoContinueRunner struct {
	mu             sync.Mutex
	runs           int
	pendingByRun   [][]agentruntime.ConsumedSteer
	drainCallIndex int
}

func (*autoContinueRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (r *autoContinueRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	r.mu.Lock()
	r.runs++
	idx := r.runs
	r.mu.Unlock()
	events := make(chan agentruntime.Event, 1)
	events <- agentruntime.TextDelta{Text: fmt.Sprintf("turn-%d", idx)}
	close(events)
	return events, &agentruntime.RunResult{ProviderSessionID: "auto-sid"}, nil
}

func (r *autoContinueRunner) Steer(_ context.Context, _ int64, _ string, _ string) error {
	return nil
}

func (r *autoContinueRunner) DrainPending(_ context.Context, _ int64) []agentruntime.ConsumedSteer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drainCallIndex >= len(r.pendingByRun) {
		return nil
	}
	out := r.pendingByRun[r.drainCallIndex]
	r.drainCallIndex++
	return out
}

func (r *autoContinueRunner) runCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.runs
}

// TestSend_AutoContinuesWhenSteerInboxNonEmpty 验证 turn 自然结束后，如果 runner
// 的 SteerInbox 还有未消费的排队消息（PostToolUse hook 没拉走），chat_svc 会自动
// 合并成一段 user msg 起新一轮 —— 替代旧的 Stop hook block=continue 把戏。
func TestSend_AutoContinuesWhenSteerInboxNonEmpty(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	runner := &autoContinueRunner{
		pendingByRun: [][]agentruntime.ConsumedSteer{
			{
				{QueuedID: "qid-a", Text: "follow-up-1"},
				{QueuedID: "qid-b", Text: "follow-up-2"},
			},
			nil, // 第二轮收尾 drain 返空 → 不再续
		},
	}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	// 4 个 Create：first turn user/assistant + auto-continue user/assistant。
	createIDs := []int64{1000, 1001, 1002, 1003}
	createdMessages := make([]*chat_entity.Message, 0, 4)
	createIdx := 0
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			msg.ID = createIDs[createIdx]
			createIdx++
			cloned := *msg
			createdMessages = append(createdMessages, &cloned)
			return nil
		}).Times(len(createIDs))

	// 第一轮 startTurn 事务 + 第二轮 auto-continue 事务。
	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
	m.dbMock.ExpectCommit()

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	m.dbMock.ExpectCommit()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	// 两个 turn 都跑了
	assert.Equal(t, 2, runner.runCount(), "runner should be Run twice (initial + auto-continue)")

	// 合并文本作为第二轮 user msg：默认用 \n\n 拼接
	var newUser *chat_entity.Message
	for _, m := range createdMessages {
		if m.ID == 1002 {
			cm := m
			newUser = cm
			break
		}
	}
	if assert.NotNil(t, newUser, "auto-continue should create the merged user msg id=1002") {
		assert.Equal(t, "user", newUser.Role)
		assert.Contains(t, newUser.BlocksJSON, "follow-up-1")
		assert.Contains(t, newUser.BlocksJSON, "follow-up-2")
	}

	// 事件流：StreamSteerConsumed 应至少 emit 一次（带 QueuedIDs），StreamDone 也要有
	var consumed *chat_svc.ChatStreamEvent
	doneCount := 0
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		switch payload.Kind {
		case chat_svc.StreamSteerConsumed:
			cp := payload
			consumed = &cp
		case chat_svc.StreamDone:
			doneCount++
		}
	}
	if assert.NotNil(t, consumed, "expected StreamSteerConsumed event for auto-continue") {
		assert.ElementsMatch(t, []string{"qid-a", "qid-b"}, consumed.QueuedIDs)
		if assert.NotNil(t, consumed.PreviousAssistantMessage) {
			assert.Equal(t, int64(1001), consumed.PreviousAssistantMessage.ID)
		}
		if assert.Len(t, consumed.UserMessages, 1) {
			assert.Equal(t, int64(1002), consumed.UserMessages[0].ID)
		}
		if assert.NotNil(t, consumed.AssistantMessage) {
			assert.Equal(t, int64(1003), consumed.AssistantMessage.ID)
		}
	}
	assert.GreaterOrEqual(t, doneCount, 1, "should emit StreamDone after final turn closes")
}

// TestSend_AutoContinuesMultipleLevels 验证 auto-continue 能链式接续多轮：
// turn1 结束 → 残留 [A,B] → turn2 → 残留 [C] → turn3 → 空 → 收尾。每一层都
// 必须落 user+assistant 行 + emit StreamSteerConsumed。
func TestSend_AutoContinuesMultipleLevels(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	runner := &autoContinueRunner{
		pendingByRun: [][]agentruntime.ConsumedSteer{
			{{QueuedID: "qid-a", Text: "A"}, {QueuedID: "qid-b", Text: "B"}}, // 第 1 轮后
			{{QueuedID: "qid-c", Text: "C"}},                                 // 第 2 轮后
			nil,                                                              // 第 3 轮后收尾
		},
	}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	// 6 个 Create：3 轮 × (user+assistant)。
	createIDs := []int64{1000, 1001, 1002, 1003, 1004, 1005}
	createdMessages := make([]*chat_entity.Message, 0, 6)
	createIdx := 0
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			msg.ID = createIDs[createIdx]
			createIdx++
			cloned := *msg
			createdMessages = append(createdMessages, &cloned)
			return nil
		}).Times(len(createIDs))

	// 3 段事务：初始 startTurn + 2 次 auto-continue。
	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
	m.dbMock.ExpectCommit()
	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
	m.dbMock.ExpectCommit()
	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(5, nil)
	m.dbMock.ExpectCommit()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	assert.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	assert.Equal(t, 3, runner.runCount(), "runner should be Run 3 times across the chain")

	// 第三轮的 user msg（id=1004）应当只包含 "C"，因为 [A,B] 已经在第二轮里消费过了。
	var thirdUser *chat_entity.Message
	for _, msg := range createdMessages {
		if msg.ID == 1004 {
			cm := msg
			thirdUser = cm
			break
		}
	}
	if assert.NotNil(t, thirdUser, "third turn should create the user msg id=1004") {
		assert.Equal(t, "user", thirdUser.Role)
		assert.Contains(t, thirdUser.BlocksJSON, `"C"`)
		assert.NotContains(t, thirdUser.BlocksJSON, `"A"`)
		assert.NotContains(t, thirdUser.BlocksJSON, `"B"`)
	}

	// 事件流：应当有 2 个 StreamSteerConsumed（一次链转移一个）+ 至少 1 个 StreamDone。
	consumedQueuedIDs := make([][]string, 0, 2)
	doneCount := 0
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok {
			continue
		}
		switch payload.Kind {
		case chat_svc.StreamSteerConsumed:
			consumedQueuedIDs = append(consumedQueuedIDs, payload.QueuedIDs)
		case chat_svc.StreamDone:
			doneCount++
		}
	}
	if assert.Len(t, consumedQueuedIDs, 2, "expect StreamSteerConsumed twice for two transitions") {
		assert.ElementsMatch(t, []string{"qid-a", "qid-b"}, consumedQueuedIDs[0])
		assert.ElementsMatch(t, []string{"qid-c"}, consumedQueuedIDs[1])
	}
	assert.GreaterOrEqual(t, doneCount, 1, "final turn must emit StreamDone")
}

func TestSend_Errors(t *testing.T) {
	convey.Convey("Send 错误路径", t, func() {

		convey.Convey("Agent 后端类型未知 → AgentBackendInvalidType", func() {
			m := setupChatTest(t)
			ctx := context.Background()
			m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: "definitely-not-a-real-type", Status: consts.ACTIVE,
			}, nil)
			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: "hi"})
			assert.Error(t, err)
		})

		convey.Convey("claudecode + provider 但 gateway 未起 → ChatBackendGatewayUnavailable", func() {
			m := setupChatTest(t)
			ctx := context.Background()
			m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: "claudecode", LLMProviderKey: "key-21", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "key-21").Return(&llm_provider_entity.LLMProvider{
				ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
			}, nil)
			// chat_svc 默认无 gateway → 当 LLMProviderKey != "" 时按 unavailable 拒掉。
			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: "hi"})
			assert.Error(t, err)
		})

		convey.Convey("远端 claudecode + provider 缓存明确缺 key → 发送前返回可操作错误", func() {
			m := setupChatTest(t)
			ctx := context.Background()
			chat_svc.RegisterGateway(&fakeChatGateway{
				status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
			})
			t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockRDS := mock_remote_device_svc.NewMockRemoteDeviceSvc(ctrl)
			remote_device_svc.SetDefault(mockRDS)
			t.Cleanup(func() { remote_device_svc.SetDefault(nil) })

			m.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{
				ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: "claudecode", LLMProviderKey: "missing-key", DeviceID: "42", Status: consts.ACTIVE,
			}, nil)
			m.provider.EXPECT().FindByKey(ctx, "missing-key").Return(&llm_provider_entity.LLMProvider{
				ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
			}, nil)
			mockRDS.EXPECT().ListDeviceProviders(int64(42)).Return([]remote_device_svc.ProviderSummary{
				{Key: "other-key", Name: "Other", Type: "anthropic"},
			})

			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: "hi"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "远端 agentred 未配置")
			assert.Contains(t, err.Error(), "missing-key")
		})

		// 回归:远端 claudecode dial 失败时,runTurn 必须把 selectRunner 返回的真错
		// (RemoteRunnerDialFailed) 透传给 failTurn,而不是覆写成假的 "unsupported
		// backend type: claudecode" —— claudecode runtime 早就注册了,真错是 dial。
		convey.Convey("远端 claudecode + Pool.Borrow 失败 → ErrorText 透传真错而非 'unsupported backend type'", func() {
			m := setupChatTest(t)
			ctx := m.ctx

			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			mockPool := mock_remote_device_svc.NewMockConnPool(ctrl)
			mockPool.EXPECT().Borrow(gomock.Any(), int64(42)).
				Return(nil, errors.New("dial timeout")).AnyTimes()
			chat_svc.SetConnPoolForTest(m.svc, mockPool)
			t.Cleanup(func() { chat_svc.SetConnPoolForTest(m.svc, nil) })

			m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
				ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
			}, nil)
			// LLMProviderKey="" → 不走 gateway 校验;DeviceID="42" → 走远端 borrow 路径。
			m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: "claudecode", DeviceID: "42", Status: consts.ACTIVE,
			}, nil)

			m.session.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

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

			var capturedErrText string
			var mu sync.Mutex
			m.message.EXPECT().Update(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
					if msg.ErrorText != "" {
						mu.Lock()
						capturedErrText = msg.ErrorText
						mu.Unlock()
					}
					return nil
				}).AnyTimes()

			resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
			assert.NoError(t, err)

			chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

			mu.Lock()
			got := capturedErrText
			mu.Unlock()
			assert.NotEmpty(t, got, "failTurn 应给 message 写入 ErrorText")
			assert.NotContains(t, got, "unsupported backend type",
				"claudecode runtime 已注册;真错被 chat.go:1561 fmt.Errorf 覆写")
			assert.Contains(t, got, "无法连接到远端 agentred",
				"真错应是 RemoteRunnerDialFailed,需要透传给前端方便排查")
		})

		convey.Convey("空文本 → InvalidParameter", func() {
			m := setupChatTest(t)
			ctx := context.Background()
			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: "   "})
			assert.Error(t, err)
		})

		convey.Convey("文本过长 → ChatTextTooLong", func() {
			m := setupChatTest(t)
			ctx := context.Background()
			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{AgentID: 7, Text: strings.Repeat("x", chat_entity.MessageTextMaxBytes+1)})
			assert.Error(t, err)
		})

		convey.Convey("同一 session 第二次 Send 抢锁失败 → ChatSendInFlight", func() {
			m := setupChatTest(t)
			ctx := m.ctx // must carry DB handle for Transaction

			// providerCalled is closed once the background goroutine has read
			// providerBuilder (i.e. entered runTurn and called providerBuilder(prov)).
			// We wait on it before the test returns so that the t.Cleanup that resets
			// providerBuilder doesn't race with the goroutine's read.
			providerCalled := make(chan struct{})

			// never-closing stream keeps the first turn "in flight"
			fp := providertest.New().QueueStreamFunc(func(pCtx context.Context) <-chan provider.StreamChunk {
				ch := make(chan provider.StreamChunk)
				go func() { <-pCtx.Done(); close(ch) }()
				return ch
			})
			chat_svc.SetProviderBuilderForTest(func(p *llm_provider_entity.LLMProvider) (provider.Provider, error) {
				// signal that providerBuilder has been called; safe to reset after this
				select {
				case <-providerCalled:
				default:
					close(providerCalled)
				}
				return fp, nil
			})
			t.Cleanup(chat_svc.ResetProviderBuilderForTest)

			m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
				ID: 7, AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
			}, nil).AnyTimes()
			m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: "builtin", LLMProviderKey: "key-21", Status: consts.ACTIVE,
			}, nil).AnyTimes()
			m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
				ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
			}, nil).AnyTimes()
			m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
				ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
			}, nil).AnyTimes()
			// Update outside tx (set running) — only first Send reaches this
			m.session.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

			// DB transaction for the first Send only
			m.dbMock.ExpectBegin()
			m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
			m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
					if msg.Role == "user" {
						msg.ID = 1
					} else {
						msg.ID = 2
					}
					return nil
				}).Times(2)
			m.dbMock.ExpectCommit()

			// List is called inside runTurn (before stream blocks), so mock it
			m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()

			// First Send — acquires lock, spawns goroutine that blocks on stream
			_, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
			assert.NoError(t, err)

			// Second Send — TryLock fails immediately → ChatSendInFlight
			_, err = m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
			assert.Error(t, err)

			// Wait until the background goroutine has called providerBuilder before
			// t.Cleanup resets it, preventing a data race on the package-level variable.
			select {
			case <-providerCalled:
			case <-time.After(2 * time.Second):
				t.Fatal("timed out waiting for providerBuilder to be called")
			}
			// The in-flight goroutine is now blocked on the never-closing stream channel.
			// The lock it holds will remain until process exit — acceptable for this test.
		})
	})
}

func TestRenameAndDelete(t *testing.T) {
	convey.Convey("Rename + Delete", t, func() {
		ctx := context.Background()

		convey.Convey("Rename 校验 title 长度", func() {
			m := setupChatTest(t)
			m.session.EXPECT().Find(ctx, int64(5)).Return(&chat_entity.Session{ID: 5, AgentID: 1, AgentStatus: "idle", Status: consts.ACTIVE}, nil)
			m.session.EXPECT().Update(ctx, gomock.Any()).Return(nil)
			_, err := m.svc.Rename(ctx, &chat_svc.RenameRequest{SessionID: 5, Title: "new title"})
			assert.NoError(t, err)
		})

		convey.Convey("Rename 找不到 session → ChatSessionNotFound", func() {
			m := setupChatTest(t)
			m.session.EXPECT().Find(ctx, int64(99)).Return(nil, nil)
			_, err := m.svc.Rename(ctx, &chat_svc.RenameRequest{SessionID: 99, Title: "x"})
			assert.Error(t, err)
		})

		convey.Convey("Delete 只软删 session，不清理 Agent cwd", func() {
			m := setupChatTest(t)
			m.session.EXPECT().SoftDelete(ctx, int64(5)).Return(nil)
			_, err := m.svc.Delete(ctx, &chat_svc.DeleteRequest{SessionID: 5})
			assert.NoError(t, err)
		})
	})
}

// encodeText helper: pack a text block into StoredBlock-envelope JSON for fixtures.
func encodeText(s string) string {
	m := &chat_entity.Message{}
	_ = m.SetBlocks([]blocks.ContentBlock{&blocks.TextBlock{Text: s}})
	return m.BlocksJSON
}

// ── Regenerate ──────────────────────────────────────────────────────────────

func TestRegenerate_BuiltinTruncatesAndRestartsTurn(t *testing.T) {
	convey.Convey("Regenerate(builtin) 截断后启新 turn", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
		t.Cleanup(restore)

		// session 已经有：seq1 user "hi", seq2 assistant "v1"
		// 用户在 seq2 上点重新生成 → 期望删 seq>=1，并以 "hi" 重新跑一轮。
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi")},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		// 事务里：先 DeleteFromSeq(100, 1) 干掉 user+assistant，再 NextSeq + Create×2。
		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()

		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1001})
		assert.NoError(t, err)
		assert.Equal(t, int64(100), resp.SessionID)
		assert.NotZero(t, resp.AssistantMessageID)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "hi", req.UserText, "重新生成必须用原 user 消息的文本重发")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the regenerated turn")
		}
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

// 回归：第一次 Send 走完后 builtin runner 会把 RunResult.ProviderSessionID 落到
// session（值是 "builtin-<sid>"）。如果 Regenerate 把 HasProviderSession() 误当成
// 必须有 Rewinder 的硬条件，就会在第二轮起返回 ChatRegenerateUnsupported，
// 表现就是「按钮点了没反应」。builtin 每轮历史从 chat_messages 重建，DB 截断后
// 直接重跑 turn 即可，应当与首轮无差。
func TestRegenerate_BuiltinWithProviderSessionStillRestartsTurn(t *testing.T) {
	convey.Convey("Regenerate(builtin) 即便 session 已有 ProviderSessionID 也应放行", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "builtin-100",
			AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi")},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()

		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1001})
		assert.NoError(t, err, "builtin 有 ProviderSessionID 不应当作未支持")
		assert.Equal(t, int64(100), resp.SessionID)
		assert.NotZero(t, resp.AssistantMessageID)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "hi", req.UserText, "重新生成必须用原 user 消息的文本重发")
			assert.Equal(t, "builtin-100", req.ProviderSessionID, "原 builtin convID 透传，runner 自己决定是否复用")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the regenerated turn")
		}
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

func TestRegenerate_CodexRollsBackProviderTurns(t *testing.T) {
	convey.Convey("Regenerate(codex) 按目标 user 到末尾的 turn 数生成 rollback anchor", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "cx-abc", AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("first")},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
			{ID: 1002, SessionID: 100, Role: "user", Seq: 3, BlocksJSON: encodeText("second")},
			{ID: 1003, SessionID: 100, Role: "assistant", Seq: 4, BlocksJSON: encodeText("v2")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeCodex), Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(4), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1001})
		assert.NoError(t, err)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "first", req.UserText)
			assert.Equal(t, "cx-abc", req.ProviderSessionID)
			assert.Equal(t, "2", req.ForkAnchor, "目标 user 自身和后续第二轮都要从 Codex thread rollback")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the regenerated codex turn")
		}
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

func TestRegenerate_ClaudeCodeForksViaAnchor(t *testing.T) {
	convey.Convey("Regenerate(claudecode) 走 ForkAnchor 路径，把 user msg 的 ForkAnchor 透传给 runner", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		// session 有：seq1 user "hi"（ForkAnchor 已经在上一轮被写到 "anchor-uuid"），seq2 assistant "v1"
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "cc-old", AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi"), ForkAnchor: "anchor-uuid"},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1001})
		assert.NoError(t, err)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "hi", req.UserText, "重新生成必须用原 user 消息的文本重发")
			assert.Equal(t, "anchor-uuid", req.ForkAnchor, "user msg 的 ForkAnchor 必须透传到 runner")
			assert.Equal(t, "cc-old", req.ProviderSessionID, "原 provider session id 透传，runner 会做 fork")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the regenerated turn")
		}
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

func TestRegenerate_ClaudeCodeWithoutAnchorDropsSession(t *testing.T) {
	convey.Convey("Regenerate(claudecode) 首轮 user msg 没 anchor → 丢 session 当全新 turn 处理", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		// 首轮 user msg ForkAnchor 为空（在 JSONL 里 parentUuid=null）。
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "cc-old", AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi") /* ForkAnchor: "" */},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1001})
		assert.NoError(t, err)

		select {
		case req := <-runner.requests:
			assert.Empty(t, req.ForkAnchor, "首轮无 anchor，不传 ForkAnchor")
			assert.Empty(t, req.ProviderSessionID, "首轮 anchor 缺失 → provider session 被丢弃，runner 创建新会话")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the regenerated turn")
		}
		chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)
	})
}

func TestRegenerate_RejectsNonAssistantTarget(t *testing.T) {
	convey.Convey("目标是 user 消息 → ChatRegenerateNotAssistant", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1000)).Return(&chat_entity.Message{
			ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi"),
		}, nil)

		_, err := m.svc.Regenerate(ctx, &chat_svc.RegenerateRequest{SessionID: 100, MessageID: 1000})
		assert.Error(t, err)
	})
}

// TestEdit_BuiltinTruncatesAndReplaysNewText 编辑历史 user 消息 → 截到该 user
// （含）后用 NEW 文本重跑。和 Regenerate 的差别是 user 文本被替换。
func TestEdit_BuiltinTruncatesAndReplaysNewText(t *testing.T) {
	convey.Convey("Edit(builtin) 用新文本替换历史 user 后重跑 turn", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, runner)
		t.Cleanup(restore)

		// session 已经有：seq1 user "hi", seq2 assistant "v1"。
		// 编辑 user (id=1000) 为 "yo"，期望删 seq>=1，并以 "yo" 重新跑一轮。
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1000)).Return(&chat_entity.Message{
			ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("hi"),
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{}, nil).AnyTimes()
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		resp, err := m.svc.Edit(ctx, &chat_svc.EditRequest{SessionID: 100, MessageID: 1000, Text: "yo"})
		assert.NoError(t, err)
		assert.Equal(t, int64(100), resp.SessionID)
		assert.NotZero(t, resp.AssistantMessageID)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "yo", req.UserText, "Edit 必须用 NEW 文本而不是原文回放")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the edited turn")
		}
	})
}

func TestEdit_CodexRollsBackProviderTurns(t *testing.T) {
	convey.Convey("Edit(codex) 按被编辑 user 到末尾的 turn 数生成 rollback anchor", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeCodex, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "cx-abc", AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1002)).Return(&chat_entity.Message{
			ID: 1002, SessionID: 100, Role: "user", Seq: 3, BlocksJSON: encodeText("second"),
		}, nil)
		m.message.EXPECT().List(gomock.Any(), int64(100)).Return([]*chat_entity.Message{
			{ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("first")},
			{ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1")},
			{ID: 1002, SessionID: 100, Role: "user", Seq: 3, BlocksJSON: encodeText("second")},
			{ID: 1003, SessionID: 100, Role: "assistant", Seq: 4, BlocksJSON: encodeText("v2")},
		}, nil).AnyTimes()
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeCodex), Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 3).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(3, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		_, err := m.svc.Edit(ctx, &chat_svc.EditRequest{SessionID: 100, MessageID: 1002, Text: "second edited"})
		assert.NoError(t, err)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "second edited", req.UserText)
			assert.Equal(t, "cx-abc", req.ProviderSessionID)
			assert.Equal(t, "1", req.ForkAnchor, "只需要 rollback 被编辑的最后一轮")
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the edited codex turn")
		}
	})
}

// TestEdit_ClaudeCodeForksViaTargetAnchor claudecode 编辑：直接取 target.ForkAnchor
// 当 forkAnchor，不需要先找 user anchor。
func TestEdit_ClaudeCodeForksViaTargetAnchor(t *testing.T) {
	convey.Convey("Edit(claudecode) target.ForkAnchor 透传到 RunRequest.ForkAnchor", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		runner := &recordingRunner{requests: make(chan agentruntime.RunRequest, 1)}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, ProviderSessionID: "cc-old", AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1000)).Return(&chat_entity.Message{
			ID: 1000, SessionID: 100, Role: "user", Seq: 1, BlocksJSON: encodeText("看看目录"),
			ForkAnchor: "anchor-uuid",
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		m.dbMock.ExpectBegin()
		m.message.EXPECT().DeleteFromSeq(gomock.Any(), int64(100), 1).Return(int64(2), nil)
		m.message.EXPECT().NextSeq(gomock.Any(), int64(100)).Return(1, nil)
		newIDs := []int64{2000, 2001}
		var calls int
		m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
				msg.ID = newIDs[calls]
				calls++
				return nil
			}).Times(2)
		m.dbMock.ExpectCommit()
		m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

		_, err := m.svc.Edit(ctx, &chat_svc.EditRequest{SessionID: 100, MessageID: 1000, Text: "看看xx目录"})
		assert.NoError(t, err)

		select {
		case req := <-runner.requests:
			assert.Equal(t, "看看xx目录", req.UserText, "新文本必须透传")
			assert.Equal(t, "anchor-uuid", req.ForkAnchor, "target.ForkAnchor 即 forkAnchor")
			assert.Equal(t, "cc-old", req.ProviderSessionID)
		case <-time.After(2 * time.Second):
			t.Fatal("runtime never received the edited turn")
		}
	})
}

func TestEdit_RejectsNonUserTarget(t *testing.T) {
	convey.Convey("Edit 目标是 assistant 消息 → ChatEditNotUser", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.message.EXPECT().Find(gomock.Any(), int64(1001)).Return(&chat_entity.Message{
			ID: 1001, SessionID: 100, Role: "assistant", Seq: 2, BlocksJSON: encodeText("v1"),
		}, nil)

		_, err := m.svc.Edit(ctx, &chat_svc.EditRequest{SessionID: 100, MessageID: 1001, Text: "new"})
		assert.Error(t, err)
	})
}

func TestEdit_RejectsEmptyText(t *testing.T) {
	convey.Convey("Edit 文本去空白后为空 → InvalidParameter（不应进 DB / runtime）", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		_, err := m.svc.Edit(ctx, &chat_svc.EditRequest{SessionID: 100, MessageID: 1000, Text: "   "})
		assert.Error(t, err)
	})
}

// fakePermissionRunner 实现 BackendRunner + PermissionModeSetter，让
// SetPermissionMode 测试可以注入 runtime 行为（成功 / NoActive / 其它 err）。
type fakePermissionRunner struct {
	setMode   string
	setSID    int64
	setErr    error
	setCalled bool
}

// Capabilities 返与生产 claudecode runtime 一致的 PermissionModeMeta;
// SetPermissionMode 重构后按 meta 判定支持与可切性,fake 必须给出对应值。
func (*fakePermissionRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			DefaultMode:          "acceptEdits",
			SwitchableDuringTurn: true,
		},
	}
}
func (r *fakePermissionRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func (r *fakePermissionRunner) SetPermissionMode(_ context.Context, sessionID int64, mode string) error {
	r.setCalled = true
	r.setSID = sessionID
	r.setMode = mode
	return r.setErr
}

// fakeSteerableRunner 实现 BackendRunner + Steerer (+ SteerCanceler when
// the test sets cancelable=true)，让 Enqueue / CancelQueued 测试不依赖
// Send 路径。
type fakeSteerableRunner struct {
	// Steer 捕获
	steerText string
	steerID   string
	steerErr  error

	// CancelSteer 捕获（仅在 fakeCancelableRunner 上才会被读到）
	cancelGotID string
}

func (*fakeSteerableRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (r *fakeSteerableRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func (r *fakeSteerableRunner) Steer(_ context.Context, _ int64, queuedID, text string) error {
	r.steerID = queuedID
	r.steerText = text
	return r.steerErr
}

// fakeCancelableRunner 是 fakeSteerableRunner 的超集：额外实现 SteerCanceler
// 让 chat_svc 的类型断言通过，验证 Cancellable=true / CancelQueued 转发路径。
type fakeCancelableRunner struct {
	fakeSteerableRunner

	// CancelSteer 返回值/错误注入；与基类的 cancelGotID 在同一对象上读写。
	cancelRemove []string
	cancelErr    error
}

func (r *fakeCancelableRunner) CancelSteer(_ context.Context, _ int64, queuedID string) ([]string, error) {
	r.cancelGotID = queuedID
	return r.cancelRemove, r.cancelErr
}

// nonSteerableRunner 只实现 BackendRunner，不实现 Steerer。用于验证
// Enqueue 在 type-assertion 失败时返回 ChatSteerUnsupported。
type nonSteerableRunner struct{}

func (nonSteerableRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (nonSteerableRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func TestEnqueue_RoutesToSteerer(t *testing.T) {
	convey.Convey("Enqueue 转发到 backend runner 的 Steer", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		chat_svc.RegisterGateway(&fakeChatGateway{
			status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		})
		t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

		runner := &fakeSteerableRunner{}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
		}, nil)

		resp, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 100, Text: "wait"})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Queued)
		assert.NotEmpty(t, resp.QueuedID, "Enqueue should generate a queuedID")
		assert.Equal(t, resp.QueuedID, runner.steerID, "queuedID should be passed to runner.Steer")
		assert.Equal(t, "wait", runner.steerText)
		// fakeSteerableRunner does not implement SteerCanceler.
		assert.False(t, resp.Cancellable)
	})
}

func TestEnqueue_CancellableTrueWhenRunnerImplementsSteerCanceler(t *testing.T) {
	convey.Convey("runner 实现 SteerCanceler → EnqueueResponse.Cancellable=true", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		chat_svc.RegisterGateway(&fakeChatGateway{
			status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		})
		t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

		runner := &fakeCancelableRunner{}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
		}, nil)

		resp, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 100, Text: "wait"})
		assert.NoError(t, err)
		assert.True(t, resp.Cancellable)
	})
}

func TestEnqueue_NoActiveTurn(t *testing.T) {
	convey.Convey("runner.Steer 返 ErrNoActiveTurn → ChatSteerNoActive", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		chat_svc.RegisterGateway(&fakeChatGateway{
			status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
		})
		t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

		runner := &fakeSteerableRunner{steerErr: agentruntime.ErrNoActiveTurn}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 100, Text: "wait"})
		assert.Error(t, err)
	})
}

func TestEnqueue_BackendNotSteerer(t *testing.T) {
	convey.Convey("runner 不实现 Steerer → ChatSteerUnsupported", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		// 临时把 builtin 替换成只实现 BackendRunner 的 fake，验证
		// type-assertion 失败时 Enqueue 返回错误。
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, nonSteerableRunner{})
		t.Cleanup(restore)

		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
		}, nil)
		m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
			ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 100, Text: "wait"})
		assert.Error(t, err)
	})
}

func TestEnqueue_RejectsEmptyText(t *testing.T) {
	convey.Convey("Enqueue 文本去空后为空 → InvalidParameter", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		_, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 100, Text: "   "})
		assert.Error(t, err)
	})
}

func TestEnqueue_SessionNotFound(t *testing.T) {
	convey.Convey("Enqueue 找不到 session → ChatSessionNotFound", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx
		m.session.EXPECT().Find(gomock.Any(), int64(999)).Return(nil, nil)

		_, err := m.svc.Enqueue(ctx, &chat_svc.EnqueueRequest{SessionID: 999, Text: "hi"})
		assert.Error(t, err)
	})
}

// cancelQueuedFixture 复用 Enqueue 测试的 happy-path 数据填充。每个 CancelQueued
// 用例都要 Find session → Find agent → Find backend → Find provider，抽出来让
// 用例只关注「runner 行为差异」。
func cancelQueuedFixture(t *testing.T) (m *chatMocks, ctx context.Context) {
	t.Helper()
	m = setupChatTest(t)
	ctx = m.ctx
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
	}, nil)
	return m, ctx
}

func TestCancelQueued_HitForwardsToCanceler(t *testing.T) {
	convey.Convey("CancelQueued 按 ID 命中 → 返 Removed 列表", t, func() {
		m, ctx := cancelQueuedFixture(t)
		runner := &fakeCancelableRunner{cancelRemove: []string{"qid-1"}}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		resp, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 100, QueuedID: "qid-1"})
		assert.NoError(t, err)
		assert.Equal(t, []string{"qid-1"}, resp.Removed)
		assert.Equal(t, "qid-1", runner.cancelGotID)
	})
}

func TestCancelQueued_ClearAllForwardsEmptyID(t *testing.T) {
	convey.Convey("CancelQueued QueuedID 为空 → 清空", t, func() {
		m, ctx := cancelQueuedFixture(t)
		runner := &fakeCancelableRunner{cancelRemove: []string{"a", "b"}}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		resp, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 100, QueuedID: ""})
		assert.NoError(t, err)
		assert.Equal(t, []string{"a", "b"}, resp.Removed)
		assert.Equal(t, "", runner.cancelGotID, "empty QueuedID should pass through as empty")
	})
}

func TestCancelQueued_RunnerWithoutCancelerReturnsUnsupported(t *testing.T) {
	convey.Convey("runner 不实现 SteerCanceler → ChatCancelUnsupported", t, func() {
		m, ctx := cancelQueuedFixture(t)
		// fakeSteerableRunner 只有 Steer，没有 CancelSteer。
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, &fakeSteerableRunner{})
		t.Cleanup(restore)

		_, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 100, QueuedID: "qid-1"})
		assert.Error(t, err)
	})
}

func TestCancelQueued_NotFoundError(t *testing.T) {
	convey.Convey("runner 返 ErrSteerNotFound → ChatCancelNotFound", t, func() {
		m, ctx := cancelQueuedFixture(t)
		runner := &fakeCancelableRunner{cancelErr: agentruntime.ErrSteerNotFound}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		_, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 100, QueuedID: "qid-gone"})
		assert.Error(t, err)
	})
}

func TestCancelQueued_NoActiveError(t *testing.T) {
	convey.Convey("runner 返 ErrNoActiveTurn → ChatSteerNoActive", t, func() {
		m, ctx := cancelQueuedFixture(t)
		runner := &fakeCancelableRunner{cancelErr: agentruntime.ErrNoActiveTurn}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		_, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 100, QueuedID: "qid"})
		assert.Error(t, err)
	})
}

func TestCancelQueued_SessionNotFound(t *testing.T) {
	convey.Convey("CancelQueued 找不到 session → ChatSessionNotFound", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx
		m.session.EXPECT().Find(gomock.Any(), int64(999)).Return(nil, nil)

		_, err := m.svc.CancelQueued(ctx, &chat_svc.CancelQueuedRequest{SessionID: 999, QueuedID: ""})
		assert.Error(t, err)
	})
}

func TestStop_NoActiveTurnReturnsError(t *testing.T) {
	convey.Convey("Stop 无活跃 turn → ChatStopNoActive", t, func() {
		m := setupChatTest(t)
		_, err := m.svc.Stop(m.ctx, &chat_svc.StopRequest{SessionID: 100})
		assert.Error(t, err, "activeCancels 没记录就应当返错，不去查 DB")
	})
}

func TestStop_InvalidRequestReturnsError(t *testing.T) {
	convey.Convey("Stop SessionID <= 0 → InvalidParameter", t, func() {
		m := setupChatTest(t)
		_, err := m.svc.Stop(m.ctx, &chat_svc.StopRequest{SessionID: 0})
		assert.Error(t, err)
	})
}

func TestListAgentSessions(t *testing.T) {
	convey.Convey("ListAgentSessions", t, func() {
		m := setupChatTest(t)
		ctx := m.ctx

		convey.Convey("中段分页：返回当前页 sessions、total、hasMore=true", func() {
			m.session.EXPECT().ListByAgentPaged(ctx, int64(7), 20, 20).Return([]*chat_entity.Session{
				{ID: 30, AgentID: 7, Title: "newer", AgentStatus: "idle", LastMessageAt: 1700000300000},
				{ID: 25, AgentID: 7, Title: "older", AgentStatus: "idle", LastMessageAt: 1700000250000},
			}, nil)
			m.session.EXPECT().CountByAgent(ctx, int64(7)).Return(int64(42), nil)

			resp, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 7, Offset: 20, Limit: 20,
			})
			assert.NoError(t, err)
			assert.Len(t, resp.Sessions, 2)
			assert.Equal(t, int64(42), resp.Total)
			assert.True(t, resp.HasMore, "20+2 < 42 → hasMore=true")
			assert.Equal(t, "newer", resp.Sessions[0].Title)
		})

		convey.Convey("末页：offset+len == total → hasMore=false", func() {
			m.session.EXPECT().ListByAgentPaged(ctx, int64(7), 40, 20).Return([]*chat_entity.Session{
				{ID: 2, AgentID: 7, Title: "tail-a", AgentStatus: "idle"},
				{ID: 1, AgentID: 7, Title: "tail-b", AgentStatus: "idle"},
			}, nil)
			m.session.EXPECT().CountByAgent(ctx, int64(7)).Return(int64(42), nil)

			resp, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 7, Offset: 40, Limit: 20,
			})
			assert.NoError(t, err)
			assert.Len(t, resp.Sessions, 2)
			assert.False(t, resp.HasMore)
		})

		convey.Convey("limit=0 默认走 20", func() {
			m.session.EXPECT().ListByAgentPaged(ctx, int64(7), 0, 20).Return(nil, nil)
			m.session.EXPECT().CountByAgent(ctx, int64(7)).Return(int64(0), nil)

			resp, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 7, Offset: 0, Limit: 0,
			})
			assert.NoError(t, err)
			assert.Empty(t, resp.Sessions)
			assert.False(t, resp.HasMore)
		})

		convey.Convey("limit 超上限 → 裁到 100", func() {
			m.session.EXPECT().ListByAgentPaged(ctx, int64(7), 0, 100).Return(nil, nil)
			m.session.EXPECT().CountByAgent(ctx, int64(7)).Return(int64(0), nil)

			_, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 7, Offset: 0, Limit: 999,
			})
			assert.NoError(t, err)
		})

		convey.Convey("agentID<=0 → InvalidParameter", func() {
			_, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 0, Offset: 0, Limit: 20,
			})
			assert.Error(t, err)
		})

		convey.Convey("offset<0 → InvalidParameter", func() {
			_, err := m.svc.ListAgentSessions(ctx, &chat_svc.ListAgentSessionsRequest{
				AgentID: 7, Offset: -1, Limit: 20,
			})
			assert.Error(t, err)
		})
	})
}

// setPermissionModeFixture 复用：每个用例都要 Find session（用于 DB 写），
// 走 happy 路径时还得 Find agent / backend / provider。提前注入 mocks 后
// 用例只关心「这次切到什么 mode + runtime 行为」。session 是返回的引用，
// 测试可以读 PermissionMode 字段验证 Update 入参。
func setPermissionModeFixture(t *testing.T) (*chatMocks, *chat_entity.Session) {
	t.Helper()
	m := setupChatTest(t)
	chat_svc.RegisterGateway(&fakeChatGateway{
		status: httpgateway.GatewayStatus{State: "running", URL: "http://127.0.0.1:60080"},
	})
	t.Cleanup(func() { chat_svc.RegisterGateway(nil) })

	sess := &chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE,
	}, nil)
	return m, sess
}

func TestSetPermissionMode_InvalidMode(t *testing.T) {
	convey.Convey("mode 不在白名单 → ChatPermissionModeInvalid（不查 DB / 不调 runtime）", t, func() {
		m := setupChatTest(t)
		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "wild-west",
		})
		assert.Error(t, err)
	})
}

func TestSetPermissionMode_SessionNotFound(t *testing.T) {
	convey.Convey("session 不存在 → ChatSessionNotFound", t, func() {
		m := setupChatTest(t)
		m.session.EXPECT().Find(gomock.Any(), int64(999)).Return(nil, nil)
		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 999, Mode: "plan",
		})
		assert.Error(t, err)
	})
}

func TestSetPermissionMode_RunnerUnsupported(t *testing.T) {
	convey.Convey("runner 不实现 PermissionModeSetter → Unsupported（不写 DB）", t, func() {
		m, _ := setPermissionModeFixture(t)
		// 用 fakeSteerableRunner — 没实现 PermissionModeSetter。
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, &fakeSteerableRunner{})
		t.Cleanup(restore)
		// 关键断言：session Update 不应该被调用 —— mock_chat_repo 默认严格 mock，
		// 没 EXPECT Update 就调到会失败。

		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "plan",
		})
		assert.Error(t, err)
	})
}

func TestSetPermissionMode_HappyPath(t *testing.T) {
	convey.Convey("DB 写成功 + runtime 下发成功 → Applied=true", t, func() {
		m, sess := setPermissionModeFixture(t)
		runner := &fakePermissionRunner{}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").DoAndReturn(
			func(_ context.Context, _ int64, mode string) error {
				assert.Equal(t, "plan", mode, "DB 写入的是请求里的 mode")
				assert.Equal(t, "plan", sess.PermissionMode, "内存 session 也要同步，供启动路径使用")
				return nil
			},
		)

		resp, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "plan",
		})
		assert.NoError(t, err)
		assert.True(t, resp.Applied)
		assert.Equal(t, "plan", resp.Mode)
		assert.True(t, runner.setCalled, "runtime setter 应当被调用")
		assert.Equal(t, "plan", runner.setMode)
		assert.Equal(t, int64(100), runner.setSID)
	})
}

func TestSetPermissionMode_NoActiveTurnNonFatal(t *testing.T) {
	convey.Convey("runtime 返 ErrNoActiveTurn → 不报错（DB 已持久化，下次 spawn 生效）", t, func() {
		m, sess := setPermissionModeFixture(t)
		runner := &fakePermissionRunner{setErr: agentruntime.ErrNoActiveTurn}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "acceptEdits").DoAndReturn(
			func(_ context.Context, _ int64, mode string) error {
				assert.Equal(t, "acceptEdits", mode)
				assert.Equal(t, "acceptEdits", sess.PermissionMode)
				return nil
			},
		)

		resp, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "acceptEdits",
		})
		assert.NoError(t, err, "NoActive 必须吞掉 —— 这是预启动场景的核心契约")
		assert.True(t, resp.Applied)
		assert.Equal(t, "acceptEdits", resp.Mode)
		assert.True(t, runner.setCalled)
	})
}

func TestSetPermissionMode_CodexPersistsWithoutRuntimeSetter(t *testing.T) {
	convey.Convey("codex default/plan 写 DB 后直接成功，不要求 runtime setter", t, func() {
		m := setupChatTest(t)
		sess := &chat_entity.Session{ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE}
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
		}, nil)
		m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "plan").Return(nil)

		resp, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100,
			Mode:      "plan",
		})
		assert.NoError(t, err)
		assert.True(t, resp.Applied)
		assert.Equal(t, "plan", resp.Mode)
		assert.Equal(t, "plan", sess.PermissionMode)
	})
}

func TestSetPermissionMode_CodexRejectsActiveTurn(t *testing.T) {
	for _, status := range []string{"running", "waiting"} {
		t.Run(status, func(t *testing.T) {
			m := setupChatTest(t)
			m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
				ID: 100, AgentID: 7, AgentStatus: status, Status: consts.ACTIVE,
			}, nil)
			m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
				ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE,
			}, nil)
			m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
				ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
			}, nil)

			_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
				SessionID: 100,
				Mode:      "plan",
			})
			assert.Error(t, err)
		})
	}
}

func TestSetPermissionMode_CodexRejectsClaudeOnlyMode(t *testing.T) {
	convey.Convey("codex 不接受 acceptEdits / bypassPermissions", t, func() {
		m := setupChatTest(t)
		m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
			ID: 100, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
		}, nil)
		m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
			ID: 7, Name: "Codex", AgentBackendID: 12, Status: consts.ACTIVE,
		}, nil)
		m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: "", Status: consts.ACTIVE,
		}, nil)

		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100,
			Mode:      "acceptEdits",
		})
		assert.Error(t, err)
	})
}

func TestSetPermissionMode_RuntimeOtherErrorStillReturnsError(t *testing.T) {
	convey.Convey("runtime 返非 NoActive 错 → 返错（DB 已写但调用方应当知道）", t, func() {
		m, _ := setPermissionModeFixture(t)
		runner := &fakePermissionRunner{setErr: errors.New("stdin broken")}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "bypassPermissions").Return(nil)

		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "bypassPermissions",
		})
		assert.Error(t, err)
	})
}

func TestSetPermissionMode_DBWriteFailure(t *testing.T) {
	convey.Convey("DB 写失败 → ChatPermissionModeInternal（runtime 不应被调）", t, func() {
		m, _ := setPermissionModeFixture(t)
		runner := &fakePermissionRunner{}
		restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
		t.Cleanup(restore)

		m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "default").Return(errors.New("db down"))

		_, err := m.svc.SetPermissionMode(m.ctx, &chat_svc.SetPermissionModeRequest{
			SessionID: 100, Mode: "default",
		})
		assert.Error(t, err)
		assert.False(t, runner.setCalled, "DB 失败后不应再调 runtime")
	})
}

// providerSessionGoneRunner 模拟"claudecode CLI resume 失效"：runner.Run 直接
// 返 wrapping ErrSessionNotFound 的 err（acquireSession 早退），或者通过
// result.StopErr 抬到上层（0-frame fallback）。两个分支都要让 chat_svc 识别 +
// 清掉 sess.ProviderSessionID + emit i18n 错误。
type providerSessionGoneRunner struct {
	mode string // "early" → Run 直接返 err；"stop" → result.StopErr
}

func (providerSessionGoneRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (r providerSessionGoneRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	wrapped := fmt.Errorf("%w: No conversation found with session ID: gone", claudecode.ErrSessionNotFound)
	switch r.mode {
	case "early":
		return nil, nil, wrapped
	case "stop":
		events := make(chan agentruntime.Event)
		close(events)
		return events, &agentruntime.RunResult{StopErr: wrapped}, nil
	}
	panic("providerSessionGoneRunner: unknown mode " + r.mode)
}

// TestSend_ClaudeCodeProviderSessionGoneEarlyClearsAndSurfacesI18n 用户报告的核心
// 修复点 ① —— runner.Run 直接返 wrapping ErrSessionNotFound 的 err（OpenSession
// 健康检查窗口拿到 stderr 后 acquireSession 早退）时，chat_svc 必须：
//   - 把 sess.ProviderSessionID 清空并持久化（下一轮 Send 才能 spawn 全新 CLI 会话）
//   - 把错误替换成 i18n.NewError(ChatProviderSessionGone) 的人话文案，让前端
//     看到「CLI 会话已过期 …」而不是英文 stderr。
func TestSend_ClaudeCodeProviderSessionGoneEarlyClearsAndSurfacesI18n(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	runner := providerSessionGoneRunner{mode: "early"}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, ProviderSessionID: "cc-gone", AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
	}, nil)

	var (
		clearedMu  sync.Mutex
		clearedSID bool
	)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
		if s.ID == 100 && s.ProviderSessionID == "" {
			clearedMu.Lock()
			clearedSID = true
			clearedMu.Unlock()
		}
		return nil
	}).AnyTimes()

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

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var errorEvent *chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if ok && payload.Kind == chat_svc.StreamError {
			cp := payload
			errorEvent = &cp
			break
		}
	}
	require.NotNil(t, errorEvent, "ErrSessionNotFound 路径必须 emit StreamError")
	assert.Contains(t, errorEvent.Error, "CLI 会话已过期",
		"StreamError.Error 必须是 ChatProviderSessionGone 的 i18n 中文文案")
	assert.NotContains(t, errorEvent.Error, "No conversation found",
		"i18n 替换之后不应当再回退到英文 stderr")

	clearedMu.Lock()
	defer clearedMu.Unlock()
	assert.True(t, clearedSID,
		"必须在 DB 上把 ProviderSessionID 置空，否则下一轮 Send 还会再撞同一个失效 id")
}

// TestSend_ClaudeCodeProviderSessionGoneViaStopErrClearsAndSurfacesI18n 修复点 ②
// —— 0-frame fallback 路径：runner 正常返回 events，但 result.StopErr 抬着
// ErrSessionNotFound（CLI spawn 起来后才命中 stderr → ExitErr → StopErr）。
// chat_svc 的处理必须和 early err 路径完全一致。
func TestSend_ClaudeCodeProviderSessionGoneViaStopErrClearsAndSurfacesI18n(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx

	runner := providerSessionGoneRunner{mode: "stop"}
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, runner)
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(&chat_entity.Session{
		ID: 100, AgentID: 7, ProviderSessionID: "cc-gone", AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), Status: consts.ACTIVE,
	}, nil)

	var (
		clearedMu  sync.Mutex
		clearedSID bool
	)
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
		if s.ID == 100 && s.ProviderSessionID == "" {
			clearedMu.Lock()
			clearedSID = true
			clearedMu.Unlock()
		}
		return nil
	}).AnyTimes()

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

	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var errorEvent *chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if ok && payload.Kind == chat_svc.StreamError {
			cp := payload
			errorEvent = &cp
			break
		}
	}
	require.NotNil(t, errorEvent, "StopErr 路径必须 emit StreamError")
	assert.Contains(t, errorEvent.Error, "CLI 会话已过期",
		"StopErr 走的也是 i18n 替换分支，文案应当一致")

	clearedMu.Lock()
	defer clearedMu.Unlock()
	assert.True(t, clearedSID, "StopErr 命中 ErrSessionNotFound 时同样必须清空 ProviderSessionID")
}

// passivePermissionModeRunner 模拟 claudecode 后端在 turn 中途 emit
// EventPermissionModeChanged（被动 ExitPlanMode 流程），让 chat_svc.runTurn
// 走 mode 同步分支。
type passivePermissionModeRunner struct {
	emitMode  string
	emitTwice bool
}

func (passivePermissionModeRunner) Capabilities() capability.Capabilities {
	return capability.Capabilities{}
}
func (r passivePermissionModeRunner) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	events := make(chan agentruntime.Event, 4)
	events <- agentruntime.TextDelta{Text: "preface"}
	events <- agentruntime.PermissionModeChanged{Mode: r.emitMode}
	if r.emitTwice {
		// 同 mode 再发一遍:service 应当幂等,不再二次写 DB / 二次推 patch。
		events <- agentruntime.PermissionModeChanged{Mode: r.emitMode}
	}
	close(events)
	return events, &agentruntime.RunResult{}, nil
}

// TestSend_PassivePermissionModeChangePersistsAndEmitsPatch 验证 CLI 自身切换
// permission mode 之后：
//   - chat_sessions.permission_mode 通过 UpdatePermissionMode 落到 DB；
//   - 内存里的 sess.PermissionMode 同步到新值；
//   - 推一条 StreamSessionStatus 给前端，payload 里携带新 permissionMode。
//
// 同时验证幂等：CLI 重复发同一个 mode 不会触发二次写 DB / 二次 emit。
func TestSend_PassivePermissionModeChangePersistsAndEmitsPatch(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeClaudeCode, passivePermissionModeRunner{
		emitMode:  "default",
		emitTwice: true, // 验证幂等
	})
	t.Cleanup(restore)

	sess := &chat_entity.Session{
		ID: 100, AgentID: 7, AgentStatus: "running", Status: consts.ACTIVE,
		PermissionMode: "plan",
	}
	m.session.EXPECT().Find(gomock.Any(), int64(100)).Return(sess, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Claude", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "", Status: consts.ACTIVE,
	}, nil)
	// 幂等期望：UpdatePermissionMode("default") 必须只调一次，即便事件被发了两次。
	m.session.EXPECT().UpdatePermissionMode(gomock.Any(), int64(100), "default").Return(nil).Times(1)
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
	m.message.EXPECT().List(gomock.Any(), int64(100)).Return(nil, nil).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 100, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	// 1) 内存 session 已经同步到新 mode（runTurn 改的是 *sess 上的字段）
	assert.Equal(t, "default", sess.PermissionMode, "sess.PermissionMode 必须翻到 CLI 通报的新值")

	// 2) emitter 上恰好有一条 StreamSessionStatus 带 permissionMode：default
	var modePatches []chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok || payload.Kind != chat_svc.StreamSessionStatus {
			continue
		}
		if payload.SessionStatus != nil && payload.SessionStatus.PermissionMode != "" {
			modePatches = append(modePatches, payload)
		}
	}
	require.Len(t, modePatches, 1, "StreamSessionStatus 携带 permissionMode 必须且只能 emit 一次（幂等）")
	require.NotNil(t, modePatches[0].SessionStatus)
	assert.Equal(t, "default", modePatches[0].SessionStatus.PermissionMode)
}

// TestSend_StreamToolUseCarriesCanonical 断言 emit 出去的 Edit / Write tool_use
// 事件带 Canonical (FileEdit / FileWrite),前端 CanonicalToolRouter 据此分发到
// canonical-tool/<kind>/card.tsx 渲染。旧 ToolDiff/ToolWrite sidecar 已删 — 由
// runtime translator 算出 canonical 后透传到 dispatcher_emitter,不再分两路计算。
func TestSend_StreamToolUseCarriesCanonical(t *testing.T) {
	m := setupChatTest(t)
	ctx := m.ctx
	restore := agentruntime.SwapRuntimeForTest(agent_backend_entity.TypeBuiltin, scriptedRunner{events: []agentruntime.RuntimeEvent{
		{Kind: agentruntime.EventToolUseStart, ToolUse: &agentruntime.ToolUseEvent{
			ID:    "toolu_edit",
			Name:  "Edit",
			Input: []byte(`{"file_path":"/x.go","old_string":"a\n","new_string":"b\n"}`),
		}},
		{Kind: agentruntime.EventToolResult, ToolResult: &agentruntime.ToolResultEvent{
			ToolUseID: "toolu_edit",
			Content:   "ok",
		}},
		{Kind: agentruntime.EventToolUseStart, ToolUse: &agentruntime.ToolUseEvent{
			ID:    "toolu_write",
			Name:  "Write",
			Input: []byte(`{"file_path":"/y.go","content":"hello\n"}`),
		}},
		{Kind: agentruntime.EventToolResult, ToolResult: &agentruntime.ToolResultEvent{
			ToolUseID: "toolu_write",
			Content:   "ok",
		}},
		{Kind: agentruntime.EventDone},
	}})
	t.Cleanup(restore)

	m.session.EXPECT().Find(gomock.Any(), int64(201)).Return(&chat_entity.Session{
		ID: 201, AgentID: 7, AgentStatus: "idle", Status: consts.ACTIVE,
	}, nil)
	m.agent.EXPECT().Find(gomock.Any(), int64(7)).Return(&agent_entity.Agent{
		ID: 7, Name: "Eng", AgentBackendID: 12, Status: consts.ACTIVE, PromptJSON: `[]`,
	}, nil)
	m.backend.EXPECT().Find(gomock.Any(), int64(12)).Return(&agent_backend_entity.AgentBackend{
		ID: 12, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "key-21", Status: consts.ACTIVE,
	}, nil)
	m.provider.EXPECT().FindByKey(gomock.Any(), "key-21").Return(&llm_provider_entity.LLMProvider{
		ID: 21, Type: string(llm_provider_entity.TypeAnthropic), Status: consts.ACTIVE, Model: "claude-sonnet-4-6",
	}, nil)

	m.dbMock.ExpectBegin()
	m.message.EXPECT().NextSeq(gomock.Any(), int64(201)).Return(1, nil)
	m.message.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, msg *chat_entity.Message) error {
			if msg.Role == "user" {
				msg.ID = 2000
			} else {
				msg.ID = 2001
			}
			return nil
		}).Times(2)
	m.dbMock.ExpectCommit()
	m.message.EXPECT().List(gomock.Any(), int64(201)).Return(nil, nil).AnyTimes()
	m.session.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()
	m.message.EXPECT().Update(gomock.Any(), gomock.Any()).AnyTimes()

	resp, err := m.svc.Send(ctx, &chat_svc.SendRequest{SessionID: 201, AgentID: 7, Text: "hi"})
	require.NoError(t, err)
	chat_svc.WaitForStreamForTest(m.svc, resp.AssistantMessageID)

	var editEv, writeEv *chat_svc.ChatStreamEvent
	for _, ev := range m.events {
		payload, ok := ev.Payload.(chat_svc.ChatStreamEvent)
		if !ok || payload.Kind != chat_svc.StreamToolUse {
			continue
		}
		switch payload.ToolUseID {
		case "toolu_edit":
			ev := payload
			editEv = &ev
		case "toolu_write":
			ev := payload
			writeEv = &ev
		}
	}

	require.NotNil(t, editEv, "Edit tool_use 事件必须 emit")
	require.NotNil(t, editEv.Canonical, "Edit 事件必须携带 Canonical FileEdit,前端 live 才能走 FileEditCard")
	assert.Equal(t, "file.edit", string(editEv.Canonical.Kind))
	require.NotNil(t, editEv.Canonical.FileEdit)
	require.Len(t, editEv.Canonical.FileEdit.Files, 1)
	assert.Equal(t, "/x.go", editEv.Canonical.FileEdit.Files[0].Path)

	require.NotNil(t, writeEv, "Write tool_use 事件必须 emit")
	require.NotNil(t, writeEv.Canonical, "Write 事件必须携带 Canonical FileWrite,前端 live 才能走 FileWriteCard")
	assert.Equal(t, "file.write", string(writeEv.Canonical.Kind))
	require.NotNil(t, writeEv.Canonical.FileWrite)
	assert.Equal(t, "/y.go", writeEv.Canonical.FileWrite.Path)
	assert.Equal(t, "hello\n", writeEv.Canonical.FileWrite.Content)
}
