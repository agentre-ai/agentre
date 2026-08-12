// Package daemon assembles the agentred daemon: state, gateway, rpc server,
// handlers, notifier. Lives one level up from sub-packages to avoid import
// cycles (state/handlers/rpc don't depend on daemon).
package daemon

import (
	"context"
	"fmt"

	"github.com/cago-frame/cago/pkg/consts"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
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
		ProviderKey:     key,
		Type:            meta.Type,
		Name:            meta.Name,
		APIKey:          meta.APIKey,
		BaseURL:         meta.BaseURL,
		DefaultModelKey: meta.DefaultModelKey,
		Status:          consts.ACTIVE,
	}, nil
}

// ResolveModel satisfies handlers.LLMProviderLookupPort: resolve the provider's
// execution model from the daemon's own catalog (decision 11).
//
//   - modelKey 空 → provider-default：provider 当前默认模型。优先 DefaultModelKey
//     精确查 Models；未同步多模型（旧单模型状态）时回落 meta.Model。两者都没有 → 空。
//   - modelKey 非空 → fixed-model：精确查 Models 里启用模型，缺失/停用返回 error，
//     由调用方严格阻止本轮 —— 绝不静默降级为默认模型。
func (l *ProviderLookup) ResolveModel(ctx context.Context, providerKey, modelKey string) (handlers.EffectiveModel, error) {
	snap := l.state.Snapshot()
	meta, ok := snap.LLMProviders[providerKey]
	if !ok {
		return handlers.EffectiveModel{}, fmt.Errorf("provider %q not configured", providerKey)
	}
	if modelKey == "" {
		// provider-default：默认模型优先 DefaultModelKey，缺省回落旧单模型字段。
		mk := meta.DefaultModelKey
		if mk == "" {
			return handlers.EffectiveModel{ModelID: meta.Model}, nil
		}
		for _, m := range meta.Models {
			if m.ModelKey == mk && m.Enabled {
				return handlers.EffectiveModel{ModelKey: m.ModelKey, ModelID: m.ModelID}, nil
			}
		}
		// 默认模型缺失/停用：与「无默认模型」同语义（旧状态回落到旧单模型字段）。
		return handlers.EffectiveModel{ModelID: meta.Model}, nil
	}
	// fixed-model：精确匹配，缺失/停用一律拒绝。
	for _, m := range meta.Models {
		if m.ModelKey == modelKey {
			if !m.Enabled {
				return handlers.EffectiveModel{}, fmt.Errorf("model %q disabled on provider %q", modelKey, providerKey)
			}
			return handlers.EffectiveModel{ModelKey: m.ModelKey, ModelID: m.ModelID}, nil
		}
	}
	return handlers.EffectiveModel{}, fmt.Errorf("model %q not configured on provider %q", modelKey, providerKey)
}
