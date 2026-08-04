package handlers

import (
	"context"
	"sort"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// HealthPingResult 是 health.ping 的返回。客户端用来探活，不修改 daemon 状态。
//
// DBSizeBytes 是这台 daemon 通知日志所在库的体量(规格「安全、隐私、兼容性与可访问性 /
// 磁盘增长」):远端盒子上的 transcript 是档案,体量得看得见,用户才判断得了何时该清理。
// 没有库统计口时省略,而不是报 0 ——「不知道」与「库是空的」在界面上必须是两回事。
//
// **库文件的路径不在这里**。它是绝对路径,通常带着宿主机的 OS 用户名,而这条应答会发给
// 每一个已配对的对端。规格要的是「路径与体量在 daemon 状态查询里可见」,那份查询是本机
// IPC(/local/status 与 `agentred status`)—— 路径在那里,给的是这台机器前面的人。
type HealthPingResult struct {
	InstanceUUID string                 `json:"instanceUUID"`
	ServerTimeMs int64                  `json:"serverTimeMs"`
	Providers    []wire.ProviderSummary `json:"providers,omitempty"`
	DBSizeBytes  int64                  `json:"dbSizeBytes,omitempty"`
}

// HealthHandlers groups health.* RPC methods.
type HealthHandlers struct {
	instanceUUID string
	state        *state.State
	dbStat       DBStatPort
}

// NewHealthHandlers 注入当前 daemon 的 instance uuid、state 与库统计口。
// dbStat 传 nil 表示这台 daemon 不报库信息(应答里两个字段一并省略)。
func NewHealthHandlers(instanceUUID string, st *state.State, dbStat DBStatPort) *HealthHandlers {
	return &HealthHandlers{instanceUUID: instanceUUID, state: st, dbStat: dbStat}
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
	res := HealthPingResult{
		InstanceUUID: h.instanceUUID,
		ServerTimeMs: time.Now().UnixMilli(),
		Providers:    providers,
	}
	if h.dbStat != nil {
		res.DBSizeBytes = h.dbStat.DBStat().SizeBytes
	}
	return res, nil
}
