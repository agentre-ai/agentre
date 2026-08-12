package daemon

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderLookup_FindByKey_HappyPath(t *testing.T) {
	// (1) happy path: state has metadata and API key → assembled entity returned.
	const key = "prov-uuid-1"
	dir := t.TempDir()
	st, err := state.Load(dir)
	require.NoError(t, err)
	st.Mutate(func(s *state.State) {
		s.LLMProviders[key] = state.LLMProviderMeta{ //nolint:gosec // credential-shaped API key is a test fixture.
			Name: "anthropic-main", Type: "anthropic",
			BaseURL: "https://api.anthropic.com",
			APIKey:  "fixture-ant-key",
			Model:   "claude-sonnet-4-6",
		}
	})

	lookup := NewProviderLookup(st)
	p, err := lookup.FindByKey(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, p)
	// entity ID is left zero — daemon doesn't track desktop int id
	assert.Equal(t, int64(0), p.ID)
	assert.Equal(t, key, p.ProviderKey)
	assert.Equal(t, "fixture-ant-key", p.APIKey)
	assert.Equal(t, "https://api.anthropic.com", p.BaseURL)
	assert.Equal(t, "anthropic-main", p.Name)
	// daemon 尚无 Models 目录（task 6 才迁移），单模型 meta.Model 不能冒充 DefaultModelKey：
	// 实体不再携带 Model 字段，也不凭空填 default_model_key。
	assert.Empty(t, p.DefaultModelKey)
	assert.True(t, p.IsActive())
	assert.Equal(t, llm_provider_entity.TypeAnthropic, llm_provider_entity.ProviderType(p.Type))
}

func TestProviderLookup_FindByKey_StateMiss(t *testing.T) {
	// (2) state has no entry for key → error
	dir := t.TempDir()
	st, _ := state.Load(dir)
	lookup := NewProviderLookup(st)
	_, err := lookup.FindByKey(context.Background(), "prov-uuid-missing")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestProviderLookup_FindByKey_APIKeyMiss(t *testing.T) {
	const key = "prov-uuid-1"
	dir := t.TempDir()
	st, _ := state.Load(dir)
	st.Mutate(func(s *state.State) {
		s.LLMProviders[key] = state.LLMProviderMeta{Name: "x", Type: "anthropic"}
	})
	lookup := NewProviderLookup(st)
	_, err := lookup.FindByKey(context.Background(), key)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiKey not configured")
}

// TestProviderLookup_ResolveModel 钉死决策 11 的 daemon 侧模型解析（task 6 多模型目录）：
// fixed-model 精确匹配（缺失/停用拒绝），provider-default 取当前默认（缺省回落旧单模型）。
func TestProviderLookup_ResolveModel(t *testing.T) {
	const key = "prov-uuid-1"
	st, _ := state.Load(t.TempDir())
	st.Mutate(func(s *state.State) {
		s.LLMProviders[key] = state.LLMProviderMeta{ //nolint:gosec // credential-shaped API key is a test fixture.
			Name:            "anthropic-main",
			Type:            "anthropic",
			APIKey:          "fixture-ant-key",
			Model:           "claude-sonnet-legacy", // 旧单模型字段
			DefaultModelKey: "model-default",
			Models: []state.LLMModelMeta{
				{ModelKey: "model-default", ModelID: "claude-sonnet-4-6", Enabled: true},
				{ModelKey: "model-opus", ModelID: "claude-opus-4-5", Enabled: true},
				{ModelKey: "model-disabled", ModelID: "claude-haiku-gone", Enabled: false},
			},
		}
	})
	lookup := NewProviderLookup(st)
	ctx := context.Background()

	t.Run("fixed-model resolves exact model", func(t *testing.T) {
		eff, err := lookup.ResolveModel(ctx, key, "model-opus")
		require.NoError(t, err)
		assert.Equal(t, "model-opus", eff.ModelKey)
		assert.Equal(t, "claude-opus-4-5", eff.ModelID)
	})

	t.Run("fixed-model missing rejects", func(t *testing.T) {
		_, err := lookup.ResolveModel(ctx, key, "model-gone")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "model-gone")
	})

	t.Run("fixed-model disabled rejects (no silent downgrade)", func(t *testing.T) {
		_, err := lookup.ResolveModel(ctx, key, "model-disabled")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "disabled")
	})

	t.Run("provider-default resolves DefaultModelKey", func(t *testing.T) {
		eff, err := lookup.ResolveModel(ctx, key, "")
		require.NoError(t, err)
		assert.Equal(t, "model-default", eff.ModelKey)
		assert.Equal(t, "claude-sonnet-4-6", eff.ModelID)
	})

	t.Run("provider-default falls back to legacy single model", func(t *testing.T) {
		legacyKey := "prov-legacy"
		st.Mutate(func(s *state.State) {
			s.LLMProviders[legacyKey] = state.LLMProviderMeta{
				Name: "old", Type: "anthropic", APIKey: "k", Model: "claude-opus-4",
			}
		})
		eff, err := lookup.ResolveModel(ctx, legacyKey, "")
		require.NoError(t, err)
		assert.Equal(t, "", eff.ModelKey)
		assert.Equal(t, "claude-opus-4", eff.ModelID)
	})

	t.Run("provider-default no model at all returns empty", func(t *testing.T) {
		bareKey := "prov-bare"
		st.Mutate(func(s *state.State) {
			s.LLMProviders[bareKey] = state.LLMProviderMeta{Name: "bare", Type: "anthropic", APIKey: "k"}
		})
		eff, err := lookup.ResolveModel(ctx, bareKey, "")
		require.NoError(t, err)
		assert.Equal(t, "", eff.ModelID)
	})

	t.Run("unknown provider rejects", func(t *testing.T) {
		_, err := lookup.ResolveModel(ctx, "prov-nope", "")
		require.Error(t, err)
	})
}
