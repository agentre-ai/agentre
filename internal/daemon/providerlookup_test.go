package daemon

import (
	"context"
	"testing"

	"agentre/internal/daemon/state"
	"agentre/internal/model/entity/llm_provider_entity"

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
	assert.Equal(t, "claude-sonnet-4-6", p.Model)
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
