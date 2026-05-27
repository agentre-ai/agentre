package agent_backend_svc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
)

func TestBuildClaudeCodeEnv(t *testing.T) {
	t.Run("有 gateway + LLM provider 时注入 ANTHROPIC_* + AGENTRE_GATEWAY_* + 用户 env_json", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{
			LLMProviderKey: "key-7", // 绑了 provider → claude CLI 该走 gateway 转发
			ModelRoutes:    `{"OPUS":"key-opus"}`,
			EnvJSON:        `{"ANTHROPIC_LOG":"info"}`,
		}
		env, err := buildClaudeCodeEnv(b, ProbeDeps{
			Token:      "tok-1",
			GatewayURL: "http://127.0.0.1:60080",
		})
		assert.NoError(t, err)
		assert.Equal(t, "http://127.0.0.1:60080", env["ANTHROPIC_BASE_URL"])
		assert.Equal(t, "tok-1", env["ANTHROPIC_AUTH_TOKEN"])
		// 不写 ANTHROPIC_API_KEY：x-api-key 与 OAuth 路径冲突，订阅模式下会被吞；
		// AUTH_TOKEN 走 Bearer，是 anthropic 文档专门压 OAuth 的覆盖位。
		_, hasKey := env["ANTHROPIC_API_KEY"]
		assert.False(t, hasKey, "ANTHROPIC_API_KEY 不应再写入；语义重叠且有版本会被 OAuth 抢")
		assert.Equal(t, "opus", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
		assert.Equal(t, "info", env["ANTHROPIC_LOG"])
		// PostToolUse hook 子进程的 env 通道：跟 LLMProviderKey 无关,有 gateway+token 就设。
		assert.Equal(t, "http://127.0.0.1:60080", env["AGENTRE_GATEWAY_URL"])
		assert.Equal(t, "tok-1", env["AGENTRE_GATEWAY_TOKEN"])
	})

	// OPUS/SONNET/HAIKU 三条 alias 字面量是 gateway 端识别的契约；
	// 改名（比如把字面量从 "sonnet" 改成 "sonnet-4"）应立刻让测试红。
	t.Run("OPUS/SONNET/HAIKU 三路 alias 都注入对应的字面量", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{
			LLMProviderKey: "key-1",
			ModelRoutes:    `{"OPUS":"key-opus","SONNET":"key-sonnet","HAIKU":"key-haiku"}`,
		}
		env, err := buildClaudeCodeEnv(b, ProbeDeps{
			Token:      "tok",
			GatewayURL: "http://127.0.0.1:1",
		})
		assert.NoError(t, err)
		assert.Equal(t, "opus", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
		assert.Equal(t, "sonnet", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
		assert.Equal(t, "haiku", env["ANTHROPIC_DEFAULT_HAIKU_MODEL"])
	})

	t.Run("model_routes 不含 alias 时不注入 ANTHROPIC_DEFAULT_* 系列", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{LLMProviderKey: "key-1"}
		env, err := buildClaudeCodeEnv(b, ProbeDeps{
			Token:      "tok",
			GatewayURL: "http://127.0.0.1:1",
		})
		assert.NoError(t, err)
		for _, k := range []string{
			"ANTHROPIC_DEFAULT_OPUS_MODEL",
			"ANTHROPIC_DEFAULT_SONNET_MODEL",
			"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		} {
			_, has := env[k]
			assert.Falsef(t, has, "alias 表为空时不应注入 %s", k)
		}
	})

	t.Run("无 gateway 时不写 BASE_URL/API_KEY，让 CLI 走自身登录", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{
			LLMProviderKey: "key-1",
			EnvJSON:        `{"ANTHROPIC_LOG":"debug"}`,
		}
		env, err := buildClaudeCodeEnv(b, ProbeDeps{})
		assert.NoError(t, err)
		_, hasBase := env["ANTHROPIC_BASE_URL"]
		_, hasKey := env["ANTHROPIC_API_KEY"]
		_, hasAuth := env["ANTHROPIC_AUTH_TOKEN"]
		assert.False(t, hasBase, "no gateway 不应注入 ANTHROPIC_BASE_URL")
		assert.False(t, hasKey, "no gateway 不应注入 ANTHROPIC_API_KEY")
		assert.False(t, hasAuth, "no gateway 不应注入 ANTHROPIC_AUTH_TOKEN")
		assert.Equal(t, "debug", env["ANTHROPIC_LOG"])
	})

	// 现象：用户 `claude login` 登过订阅账号后，CLI 在订阅模式下会优先用 OAuth session，
	// 单独写 ANTHROPIC_API_KEY 压不住，请求直接打到 anthropic.com 绕过 gateway。
	// 必须改用 ANTHROPIC_AUTH_TOKEN（走 `Authorization: Bearer <token>`），
	// 这才是 anthropic 文档里压 OAuth、转向自定义代理的标准开关。
	t.Run("有 gateway 时用 AUTH_TOKEN 走 Bearer 压 OAuth，不写 API_KEY 避免 header 冲突", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{LLMProviderKey: "key-1"}
		env, err := buildClaudeCodeEnv(b, ProbeDeps{
			Token:      "tok-bearer",
			GatewayURL: "http://127.0.0.1:60080",
		})
		assert.NoError(t, err)
		assert.Equal(t, "tok-bearer", env["ANTHROPIC_AUTH_TOKEN"],
			"必须设 ANTHROPIC_AUTH_TOKEN 压住订阅 OAuth；只写 API_KEY 在订阅模式下会被 CLI 忽略")
		_, hasKey := env["ANTHROPIC_API_KEY"]
		assert.False(t, hasKey,
			"不要再写 ANTHROPIC_API_KEY：与 AUTH_TOKEN 语义重叠；同时下发会让上游收到两条冲突的 auth header")
		assert.Equal(t, "http://127.0.0.1:60080", env["ANTHROPIC_BASE_URL"])
	})
}

// modelCapturingProvider 实现 provider.Provider，单纯记录 agent loop 真正发出的
// CompletionRequest.Model。一旦 builtinProber 没把 LLMProvider.Model 透传过来，
// 这里读到的就是空串，意味着真正打到 LLM 的请求会被 anthropic / openai 以 model
// 字段为空为由直接 400，等价于"看似有调用、实际啥都没跑成"的 200ms 现象。
type modelCapturingProvider struct {
	mu        sync.Mutex
	seenModel string
}

func (p *modelCapturingProvider) Name() string { return "fake" }

func (p *modelCapturingProvider) ChatCompletion(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("modelCapturingProvider: ChatCompletion not used by builtinProber")
}

func (p *modelCapturingProvider) ChatStream(_ context.Context, req *provider.CompletionRequest) (<-chan provider.StreamChunk, error) {
	p.mu.Lock()
	if p.seenModel == "" {
		p.seenModel = req.Model
	}
	p.mu.Unlock()

	ch := make(chan provider.StreamChunk, 2)
	ch <- provider.StreamChunk{ContentDelta: "pong"}
	// 直接 close → loop.go 把 partial 收成 StopEndTurn 的 assistant turn，
	// builtinProber.lastAssistantText 即能拿到 "pong"。
	close(ch)
	return ch, nil
}

func (p *modelCapturingProvider) Model() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.seenModel
}

func TestBuiltinProber_PassesProviderModelToAgent(t *testing.T) {
	// 现象：用户在前端点「测试连接」200ms 就完成、对应 LLM 后台看不到调用。
	// 根因之一：builtinProber 调 coding.New(ctx, prov, cwd) 没传 coding.WithModel(p.Model)，
	// 父 agent 的 model 字段是空字符串；anthropic / openai 收到空 model 字段会直接 400 而不
	// 计入正常调用记录。该测试通过 fake provider 捕获 agent loop 实际派发的
	// CompletionRequest.Model，验证它必须等于 LLMProvider.Model。
	t.Run("把 LLMProvider.Model 传到底层 agent 的 ChatStream req.Model", func(t *testing.T) {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		repoMock := mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl)
		llm_provider_repo.RegisterLLMProvider(repoMock)
		entity := &llm_provider_entity.LLMProvider{
			ProviderKey: "key-11",
			Type:        string(llm_provider_entity.TypeAnthropic),
			Name:        "production",
			Model:       "claude-sonnet-4-6",
			APIKey:      "sk-test",
			BaseURL:     "http://127.0.0.1:0",
			Status:      consts.ACTIVE,
		}
		repoMock.EXPECT().FindByKey(gomock.Any(), "key-11").Return(entity, nil)

		capture := &modelCapturingProvider{}
		origBuilder := providerBuilder
		providerBuilder = func(_ *llm_provider_entity.LLMProvider) (provider.Provider, error) {
			return capture, nil
		}
		t.Cleanup(func() { providerBuilder = origBuilder })

		reply, err := builtinProber{}.Run(ctx, &agent_backend_entity.AgentBackend{
			ID:             1,
			Type:           string(agent_backend_entity.TypeBuiltin),
			Name:           "默认助手",
			LLMProviderKey: "key-11",
		}, ProbeDeps{})

		assert.NoError(t, err)
		assert.Equal(t, "pong", strings.TrimSpace(reply))
		assert.Equal(t, "claude-sonnet-4-6", capture.Model(),
			"builtinProber 必须把 LLMProvider.Model 注入 coding.WithModel；空 model 会让真实 LLM 调用以 400 失败")
	})
}

func TestBuildCodexEnv(t *testing.T) {
	t.Run("有 gateway 时只注入 API_KEY + 用户 env_json", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{
			EnvJSON: `{"OPENAI_ORGANIZATION":"acme"}`,
		}
		env, err := buildCodexEnv(b, ProbeDeps{
			Token:      "tok-2",
			GatewayURL: "http://127.0.0.1:60080",
		})
		assert.NoError(t, err)
		assert.Equal(t, "tok-2", env["OPENAI_API_KEY"])
		assert.Equal(t, "acme", env["OPENAI_ORGANIZATION"])
		_, hasBase := env["OPENAI_BASE_URL"]
		assert.False(t, hasBase, "base_url 应由 Codex Config 覆盖，不靠 OPENAI_BASE_URL 环境变量")
	})

	t.Run("无 gateway 时不写 BASE_URL/API_KEY，让 CLI 走自身登录", func(t *testing.T) {
		b := &agent_backend_entity.AgentBackend{
			EnvJSON: `{"OPENAI_ORGANIZATION":"acme"}`,
		}
		env, err := buildCodexEnv(b, ProbeDeps{})
		assert.NoError(t, err)
		_, hasBase := env["OPENAI_BASE_URL"]
		_, hasKey := env["OPENAI_API_KEY"]
		assert.False(t, hasBase)
		assert.False(t, hasKey)
		assert.Equal(t, "acme", env["OPENAI_ORGANIZATION"])
	})
}
