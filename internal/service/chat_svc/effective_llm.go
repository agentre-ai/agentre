package chat_svc

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/llm_provider_svc"
)

// effective_llm.go 是 EffectiveLLMConfig v1 解析口（spec「Effective configuration,
// Gateway and Runtime」决策 8）：所有执行入口（Send / Regenerate / Edit / Compact /
// Goal / 复制启动命令 / 网关 token 路由）通过唯一的 EffectiveLLMConfig 决定实际
// Provider 与 Model，Runtime / Gateway / 展示路径不得各自重新拼装优先级。

// effectiveLLMForTurn 在 turn 入口已解析出的有效 Provider（含 #39 回退，见
// sessionProviderOverride）之上，通过 llm_provider_svc.ResolveTarget 解析其模型，
// 组装执行侧 EffectiveLLMConfig v1。
//
//   - prov == nil（CLI 登录态 / 无任何供应商）→ native 配置（所有模型字段为空）；
//   - prov != nil && modelKey == "" → provider-default：经 ResolveTarget 解析 Provider 当前默认模型；
//   - prov != nil && modelKey != "" → fixed-model：经 ResolveTarget 解析指定 Model 记录（Backend 固定模型）。
//
// 解析失败（Provider 存在但没有合法启用默认模型 / 固定模型不存在或停用等配置损坏）返回 error，
// 由调用方阻止本轮，不静默降级（spec 决策 7）。ResolveTarget 是 Go 内部解析口，携带明文 APIKey /
// BaseURL 供执行侧使用，不通过 Wails 绑定暴露给前端。
func (s *chatSvc) effectiveLLMForTurn(ctx context.Context, prov *llm_provider_entity.LLMProvider, modelKey string) (*agentruntime.EffectiveLLMConfig, error) {
	if prov == nil {
		return &agentruntime.EffectiveLLMConfig{Mode: agentruntime.EffectiveModeNative}, nil
	}
	target := llm_provider_svc.ModelTarget{ProviderKey: prov.ProviderKey, ModelKey: strings.TrimSpace(modelKey)}
	resolved, err := llm_provider_svc.LLMProvider().ResolveTarget(ctx, target)
	if err != nil {
		return nil, err
	}
	mode := agentruntime.EffectiveModeProviderDefault
	if target.ModelKey != "" {
		mode = agentruntime.EffectiveModeFixedModel
	}
	return &agentruntime.EffectiveLLMConfig{
		Mode:          mode,
		ProviderKey:   resolved.ProviderKey,
		ModelKey:      resolved.ModelKey,
		ProviderType:  resolved.ProviderType,
		ProviderName:  prov.Name,
		ModelID:       resolved.ModelID,
		ContextWindow: resolved.ContextWindow,
		MaxOutput:     resolved.MaxOutput,
		BaseURL:       resolved.BaseURL,
		APIKey:        resolved.APIKey,
		HasAPIKey:     resolved.HasAPIKey,
	}, nil
}

// effectiveLLMForNonRemoteTurn 是 turn 入口用变体：远端 backend 由 daemon 自家解析
// （desktop 本地 provider 表反映不了 daemon 配置，wire 只透传 key），所以返回 nil、
// 不做本地模型解析，也不让本地模型配置损坏阻塞远端轮。非远端后端直接委托
// effectiveLLMForTurn，并带上 backend 的固定 ModelKey（Backend fixed-model，spec
// 决策 5/10）：非空时解析指定模型，为空则走 provider-default。
func (s *chatSvc) effectiveLLMForNonRemoteTurn(ctx context.Context, be *agent_backend_entity.AgentBackend, prov *llm_provider_entity.LLMProvider) (*agentruntime.EffectiveLLMConfig, error) {
	if be != nil && be.IsRemote() {
		return nil, nil
	}
	return s.effectiveLLMForTurn(ctx, prov, backendModelKeyFor(be, prov))
}

// backendModelKeyFor 返回 backend 固定模型的 ModelKey；仅当 effective provider 就是
// backend 自家绑定的 provider 时才适用。会话把 provider 覆盖到别家时，backend 的固定
// 模型不适用于那家（避免把 fixed model 拿去解析另一家，造成 ModelNotOwned 硬失败）。
func backendModelKeyFor(be *agent_backend_entity.AgentBackend, prov *llm_provider_entity.LLMProvider) string {
	if be == nil || prov == nil {
		return ""
	}
	if be.LLMProviderKey != "" && be.LLMProviderKey == prov.ProviderKey {
		return be.LLMModelKey
	}
	return ""
}
