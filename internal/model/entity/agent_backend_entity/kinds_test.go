package agent_backend_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/model/entity/llm_provider_entity"
)

func TestKindForReturnsCorrectKind(t *testing.T) {
	cases := []struct {
		name      string
		input     BackendType
		wantType  BackendType
		wantNilOK bool
	}{
		{"builtin", TypeBuiltin, TypeBuiltin, false},
		{"claudecode", TypeClaudeCode, TypeClaudeCode, false},
		{"codex", TypeCodex, TypeCodex, false},
		{"unknown", BackendType("foo"), "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := KindFor(tc.input)
			if tc.wantNilOK {
				assert.Nil(t, k)
				return
			}
			if assert.NotNil(t, k) {
				assert.Equal(t, tc.wantType, k.Type())
			}
		})
	}
}

func TestProviderTypeMatch(t *testing.T) {
	cases := []struct {
		name      string
		kind      BackendKind
		provType  llm_provider_entity.ProviderType
		wantMatch bool
	}{
		{"builtin matches anything", builtinKind{}, llm_provider_entity.TypeAnthropic, true},
		{"builtin matches openai-chat", builtinKind{}, llm_provider_entity.TypeOpenAIChat, true},
		{"claudecode matches anthropic", claudeCodeKind{}, llm_provider_entity.TypeAnthropic, true},
		{"claudecode rejects openai-chat", claudeCodeKind{}, llm_provider_entity.TypeOpenAIChat, false},
		{"claudecode rejects openai-response", claudeCodeKind{}, llm_provider_entity.TypeOpenAIResponse, false},
		{"codex rejects openai-chat", codexKind{}, llm_provider_entity.TypeOpenAIChat, false},
		{"codex matches openai-response", codexKind{}, llm_provider_entity.TypeOpenAIResponse, true},
		{"codex rejects anthropic", codexKind{}, llm_provider_entity.TypeAnthropic, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantMatch, tc.kind.ProviderTypeMatch(tc.provType))
		})
	}
}

func TestKnownAliases(t *testing.T) {
	assert.Empty(t, builtinKind{}.KnownAliases())
	assert.Equal(t, []string{"OPUS", "SONNET", "HAIKU"}, claudeCodeKind{}.KnownAliases())
	assert.Empty(t, codexKind{}.KnownAliases())
}

func TestAllowsCLIPath(t *testing.T) {
	assert.False(t, builtinKind{}.AllowsCLIPath())
	assert.True(t, claudeCodeKind{}.AllowsCLIPath())
	assert.True(t, codexKind{}.AllowsCLIPath())
}

func TestIsReservedEnvKey(t *testing.T) {
	cases := []struct {
		key  string
		want bool
	}{
		{"ANTHROPIC_BASE_URL", true},
		{"ANTHROPIC_API_KEY", true},
		{"ANTHROPIC_AUTH_TOKEN", true},
		{"ANTHROPIC_MODEL", true},
		{"ANTHROPIC_DEFAULT_OPUS_MODEL", true},
		{"ANTHROPIC_DEFAULT_SONNET_MODEL", true},
		{"ANTHROPIC_DEFAULT_HAIKU_MODEL", true},
		{"OPENAI_API_KEY", true},
		{"OPENAI_BASE_URL", true},
		{"OPENAI_API_BASE", true},
		{"ANTHROPIC_LOG", false},
		{"OPENAI_ORGANIZATION", false},
		{"FOO", false},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			assert.Equal(t, tc.want, IsReservedEnvKey(tc.key))
		})
	}
}

func TestValidateSandboxEnum(t *testing.T) {
	ctx := context.Background()
	for _, v := range []string{"", "read-only", "workspace-write", "danger-full-access"} {
		assert.NoError(t, validateSandbox(ctx, v), v)
	}
	for _, v := range []string{"  ", "weird", "Full Access"} {
		// 含空白的字符串去空白后变空 → 合法；其它必须报错
		if v == "  " {
			assert.NoError(t, validateSandbox(ctx, v))
			continue
		}
		assert.Error(t, validateSandbox(ctx, v))
	}
}

func TestValidateApprovalEnum(t *testing.T) {
	ctx := context.Background()
	for _, v := range []string{"", "untrusted", "on-failure", "on-request", "never"} {
		assert.NoError(t, validateApproval(ctx, v), v)
	}
	for _, v := range []string{"maybe", "Yes"} {
		assert.Error(t, validateApproval(ctx, v))
	}
}

func TestParseModelRoutes_StringValues(t *testing.T) {
	t.Run("正常 UUID 值", func(t *testing.T) {
		got, err := ParseModelRoutes(`{"OPUS":"4f8c-1234","SONNET":"a2bc"}`)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"OPUS": "4f8c-1234", "SONNET": "a2bc"}, got)
	})
	t.Run("空对象", func(t *testing.T) {
		got, err := ParseModelRoutes(`{}`)
		require.NoError(t, err)
		assert.Empty(t, got)
	})
	t.Run("小写别名自动 ToUpper", func(t *testing.T) {
		got, err := ParseModelRoutes(`{"opus":"4f8c","sonnet":"a2bc"}`)
		require.NoError(t, err)
		assert.Equal(t, map[string]string{"OPUS": "4f8c", "SONNET": "a2bc"}, got)
	})
	t.Run("空 value 被拒绝", func(t *testing.T) {
		_, err := ParseModelRoutes(`{"OPUS":""}`)
		assert.Error(t, err, "空 value 应该被拒绝")
	})
}

func TestParseEnvJSON(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{"empty", "", map[string]string{}, false},
		{"empty object", "{}", map[string]string{}, false},
		{"single", `{"K":"V"}`, map[string]string{"K": "V"}, false},
		{"malformed", `{not json`, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseEnvJSON(tc.input)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			if assert.NoError(t, err) {
				assert.Equal(t, tc.want, got)
			}
		})
	}
}
