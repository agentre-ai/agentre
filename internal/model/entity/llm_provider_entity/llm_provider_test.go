package llm_provider_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCheck(t *testing.T) {
	ctx := context.Background()

	t.Run("nil receiver", func(t *testing.T) {
		var p *LLMProvider
		assert.Error(t, p.Check(ctx))
	})

	t.Run("empty name", func(t *testing.T) {
		p := &LLMProvider{Type: string(TypeOpenAIChat)}
		assert.Error(t, p.Check(ctx))
	})

	t.Run("unsupported type", func(t *testing.T) {
		p := &LLMProvider{Type: "google", Name: "x"}
		assert.Error(t, p.Check(ctx))
	})

	t.Run("valid anthropic", func(t *testing.T) {
		p := &LLMProvider{Type: string(TypeAnthropic), Name: "prod"}
		assert.NoError(t, p.Check(ctx))
	})

	t.Run("valid openai-chat", func(t *testing.T) {
		p := &LLMProvider{Type: string(TypeOpenAIChat), Name: "prod"}
		assert.NoError(t, p.Check(ctx))
	})

	t.Run("valid openai-response", func(t *testing.T) {
		p := &LLMProvider{Type: string(TypeOpenAIResponse), Name: "prod"}
		assert.NoError(t, p.Check(ctx))
	})

	t.Run("legacy openai value rejected", func(t *testing.T) {
		// 旧的 "openai" 由迁移转成 "openai-chat"；运行时再看到 "openai" 视为非法。
		p := &LLMProvider{Type: "openai", Name: "x"}
		assert.Error(t, p.Check(ctx))
	})
}

func TestProviderTypeHelpers(t *testing.T) {
	chat := &LLMProvider{Type: string(TypeOpenAIChat)}
	resp := &LLMProvider{Type: string(TypeOpenAIResponse)}
	ant := &LLMProvider{Type: string(TypeAnthropic)}

	assert.True(t, chat.IsOpenAIChat())
	assert.False(t, chat.IsOpenAIResponse())
	assert.True(t, chat.IsOpenAICompatible())

	assert.False(t, resp.IsOpenAIChat())
	assert.True(t, resp.IsOpenAIResponse())
	assert.True(t, resp.IsOpenAICompatible())

	assert.True(t, ant.IsAnthropic())
	assert.False(t, ant.IsOpenAICompatible())
}

func TestMaskedAPIKey(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{in: "", want: ""},
		{in: "short", want: "•••••"},
		{in: "sk-ant-1234abcd5678", want: "sk-a••••••5678"},
	}
	for _, tc := range cases {
		p := &LLMProvider{APIKey: tc.in}
		assert.Equal(t, tc.want, p.MaskedAPIKey(), "key=%q", tc.in)
	}
}
