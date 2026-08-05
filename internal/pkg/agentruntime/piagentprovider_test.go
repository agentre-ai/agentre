package agentruntime

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
)

// testProviderKey 是 UUID 形态的稳定键：带 '-'，env 键名里必须去掉。
const testProviderKey = "9a3b2c1d-4e5f-6a7b-8c9d-0e1f2a3b4c5d"

func TestPiAgentProviderEnvKey_SanitizesProviderKey(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"uuid 去 '-'", testProviderKey, "AGENTRE_PI_API_KEY_9a3b2c1d4e5f6a7b8c9d0e1f2a3b4c5d"},
		{"空 key 退化为前缀", "", "AGENTRE_PI_API_KEY_"},
		{"非字母数字全部剔除", "A-B!C_d.1", "AGENTRE_PI_API_KEY_ABCd1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, PiAgentProviderEnvKey(tc.key))
		})
	}
}

func TestPiAgentProviderModelName_AllTypes(t *testing.T) {
	cases := []struct {
		name    string
		typ     string
		model   string
		want    string
		wantErr bool
	}{
		{"anthropic", string(llm_provider_entity.TypeAnthropic), "claude-sonnet-4", "agentre-" + testProviderKey + "/claude-sonnet-4", false},
		{"openai-chat", string(llm_provider_entity.TypeOpenAIChat), "gpt-4o", "agentre-" + testProviderKey + "/gpt-4o", false},
		{"openai-response", string(llm_provider_entity.TypeOpenAIResponse), "o3", "agentre-" + testProviderKey + "/o3", false},
		{"未知 Type 报错", "deepseek", "gpt-4o", "", true},
		{"Model 为空报错", string(llm_provider_entity.TypeOpenAIChat), "", "", true},
		{"Model 纯空白报错", string(llm_provider_entity.TypeOpenAIChat), "  ", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := &llm_provider_entity.LLMProvider{ProviderKey: testProviderKey, Type: tc.typ, Model: tc.model}
			got, err := PiAgentProviderModelName(p)
			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, got)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestPiAgentProviderModelName_NilProvider(t *testing.T) {
	_, err := PiAgentProviderModelName(nil)
	require.Error(t, err)
}

func TestPiAgentProviderExtension_AllThreeTypesAPIMapping(t *testing.T) {
	cases := []struct {
		name string
		typ  string
		api  string
	}{
		{"anthropic → anthropic-messages", string(llm_provider_entity.TypeAnthropic), "anthropic-messages"},
		{"openai-chat → openai-completions", string(llm_provider_entity.TypeOpenAIChat), "openai-completions"},
		{"openai-response → openai-responses", string(llm_provider_entity.TypeOpenAIResponse), "openai-responses"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, err := PiAgentProviderExtension(&llm_provider_entity.LLMProvider{
				ProviderKey:   testProviderKey,
				Type:          tc.typ,
				Name:          "Compat",
				BaseURL:       "https://proxy.example.com",
				Model:         "m-1",
				ContextWindow: 200000,
				MaxOutput:     8192,
			})
			require.NoError(t, err)
			assert.True(t, strings.HasPrefix(src, "export default function (pi) { "))
			assert.Contains(t, src, `pi.registerProvider("agentre-`+testProviderKey+`", {`)
			assert.Contains(t, src, `api: "`+tc.api+`"`)
		})
	}
}

func TestPiAgentProviderExtension_FullShape(t *testing.T) {
	src, err := PiAgentProviderExtension(&llm_provider_entity.LLMProvider{
		ProviderKey:   testProviderKey,
		Type:          string(llm_provider_entity.TypeAnthropic),
		Name:          "My Anthropic Compat",
		BaseURL:       "https://proxy.example.com",
		APIKey:        "sk-plaintext-secret",
		Model:         "claude-sonnet-4",
		ContextWindow: 200000,
		MaxOutput:     8192,
	})
	require.NoError(t, err)
	assert.Contains(t, src, `name: "My Anthropic Compat"`)
	assert.Contains(t, src, `baseUrl: "https://proxy.example.com"`)
	assert.Contains(t, src, `api: "anthropic-messages"`)
	assert.Contains(t, src, `apiKey: "$AGENTRE_PI_API_KEY_9a3b2c1d4e5f6a7b8c9d0e1f2a3b4c5d"`)
	assert.Contains(t, src, `models: [{ id: "claude-sonnet-4", name: "claude-sonnet-4", reasoning: true, input: ["text","image"], contextWindow: 200000, maxTokens: 8192 }]`)
	// 密钥只进 env，绝不落进扩展源。
	assert.NotContains(t, src, "sk-plaintext-secret")
}

func TestPiAgentProviderExtension_ZeroWindowTokensOmitted(t *testing.T) {
	src, err := PiAgentProviderExtension(&llm_provider_entity.LLMProvider{
		ProviderKey: testProviderKey,
		Type:        string(llm_provider_entity.TypeOpenAIChat),
		Name:        "Local",
		BaseURL:     "http://localhost:8080/v1",
		Model:       "qwen",
	})
	require.NoError(t, err)
	assert.Contains(t, src, `models: [{ id: "qwen", name: "qwen", reasoning: true, input: ["text","image"] }]`)
	assert.NotContains(t, src, "contextWindow")
	assert.NotContains(t, src, "maxTokens")
}

func TestPiAgentProviderExtension_Errors(t *testing.T) {
	base := &llm_provider_entity.LLMProvider{ProviderKey: testProviderKey, Type: string(llm_provider_entity.TypeAnthropic), Model: "m"}

	t.Run("未知 Type", func(t *testing.T) {
		p := *base
		p.Type = "deepseek"
		_, err := PiAgentProviderExtension(&p)
		require.Error(t, err)
	})

	t.Run("Model 为空", func(t *testing.T) {
		p := *base
		p.Model = ""
		_, err := PiAgentProviderExtension(&p)
		require.Error(t, err)
	})

	t.Run("nil provider", func(t *testing.T) {
		_, err := PiAgentProviderExtension(nil)
		require.Error(t, err)
	})
}

func TestBuildPiAgentProviderEnv(t *testing.T) {
	base := map[string]string{"FOO": "bar", "AGENTRE_PI_MCP_CONFIG": "/tmp/cfg.json"}
	env := BuildPiAgentProviderEnv(base, &llm_provider_entity.LLMProvider{
		ProviderKey: testProviderKey,
		APIKey:      "sk-secret-123",
	})
	// 含 base 全部键，新增 env 键 → APIKey。
	assert.Equal(t, "bar", env["FOO"])
	assert.Equal(t, "/tmp/cfg.json", env["AGENTRE_PI_MCP_CONFIG"])
	assert.Equal(t, "sk-secret-123", env["AGENTRE_PI_API_KEY_9a3b2c1d4e5f6a7b8c9d0e1f2a3b4c5d"])
	// 不改入参 map。
	assert.NotContains(t, base, "AGENTRE_PI_API_KEY_9a3b2c1d4e5f6a7b8c9d0e1f2a3b4c5d")

	// nil provider 退化为纯副本，不注入 env 键。
	envNil := BuildPiAgentProviderEnv(base, nil)
	assert.Equal(t, "bar", envNil["FOO"])
	assert.NotContains(t, envNil, "AGENTRE_PI_API_KEY_")
}
