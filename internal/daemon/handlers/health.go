package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// HealthPingResult 是 health.ping 的返回。客户端用来探活，不修改 daemon 状态。
type HealthPingResult struct {
	InstanceUUID string                 `json:"instanceUUID"`
	ServerTimeMs int64                  `json:"serverTimeMs"`
	Providers    []wire.ProviderSummary `json:"providers,omitempty"`
}

// HealthHandlers groups health.* RPC methods.
type HealthHandlers struct {
	instanceUUID string
	state        *state.State
}

// NewHealthHandlers 注入当前 daemon 的 instance uuid 和 state。
func NewHealthHandlers(instanceUUID string, st *state.State) *HealthHandlers {
	return &HealthHandlers{instanceUUID: instanceUUID, state: st}
}

// Ping 无副作用心跳；watcher 用它判活 + 推 last_seen_at。
// 顺带返回 daemon 已配置的 LLM provider 列表（key + name + type），
// 让 desktop watcher 渲染 "provider synced" 状态，无需单独 RPC。
func (h *HealthHandlers) Ping(_ context.Context) (HealthPingResult, error) {
	snap := h.state.Snapshot()
	providers := make([]wire.ProviderSummary, 0, len(snap.LLMProviders))
	for k, v := range snap.LLMProviders {
		providers = append(providers, wire.ProviderSummary{
			Key:  k,
			Name: v.Name,
			Type: v.Type,
		})
	}
	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Key < providers[j].Key
	})
	return HealthPingResult{
		InstanceUUID: h.instanceUUID,
		ServerTimeMs: time.Now().UnixMilli(),
		Providers:    providers,
	}, nil
}
