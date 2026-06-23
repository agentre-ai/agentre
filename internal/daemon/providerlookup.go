// Package daemon assembles the agentred daemon: state, gateway, rpc server,
// handlers, notifier. Lives one level up from sub-packages to avoid import
// cycles (state/handlers/rpc don't depend on daemon).
package daemon

import (
	"context"
	"fmt"

	"github.com/cago-frame/cago/pkg/consts"

	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
)

// ProviderLookup implements httpgateway.ProviderLookup: given a stable provider key,
// return the full LLMProvider entity from agentred state.
type ProviderLookup struct {
	state *state.State
}

// NewProviderLookup constructs a ProviderLookup backed by the given state.
func NewProviderLookup(s *state.State) *ProviderLookup {
	return &ProviderLookup{state: s}
}

// FindByKey satisfies httpgateway.ProviderLookup and handlers.LLMProviderLookupPort.
// It errors when the key has no metadata in state.
func (l *ProviderLookup) FindByKey(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error) {
	snap := l.state.Snapshot()
	meta, ok := snap.LLMProviders[key]
	if !ok {
		return nil, fmt.Errorf("provider %q not configured", key)
	}
	if meta.APIKey == "" {
		return nil, fmt.Errorf("provider %q apiKey not configured", key)
	}
	return &llm_provider_entity.LLMProvider{
		ProviderKey: key,
		Type:        meta.Type,
		Name:        meta.Name,
		APIKey:      meta.APIKey,
		BaseURL:     meta.BaseURL,
		Model:       meta.Model,
		Status:      consts.ACTIVE,
	}, nil
}
