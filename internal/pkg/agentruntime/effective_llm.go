package agentruntime

// EffectiveLLMConfigMode 是解析目标模式（ModelTarget 契约的派生形态）。
//
// 只持久化 providerKey / modelKey 的组合决定 mode：
//   - 两个 key 都为空 → native（CLI 自身登录态）；
//   - ProviderKey 非空且 ModelKey 为空 → provider-default（每轮解析当前默认模型）；
//   - 两个 key 都非空 → fixed-model（解析指定 Model 记录）。
//
// v1 阶段三种 mode 都会从 chat_svc 产出：native（无供应商）、provider-default
// （ProviderKey 非空 + ModelKey 空）与 fixed-model（Backend 或 Session 钉了固定
// ModelKey）。
type EffectiveLLMConfigMode string

const (
	// EffectiveModeNative 无 Agentre 供应商：CLI 自身登录态。
	EffectiveModeNative EffectiveLLMConfigMode = "native"
	// EffectiveModeProviderDefault ProviderKey 非空、ModelKey 空：每轮解析 Provider 当前默认模型。
	EffectiveModeProviderDefault EffectiveLLMConfigMode = "provider-default"
	// EffectiveModeFixedModel ProviderKey + ModelKey 都非空：解析指定 Model 记录。
	EffectiveModeFixedModel EffectiveLLMConfigMode = "fixed-model"
)

// EffectiveLLMConfig 是执行侧唯一解析结果（EffectiveLLMConfig v1 seam）。
//
// chat_svc 通过 llm_provider_svc.ResolveTarget 解析产生，随 RunRequest / GoalRequest
// 下发；runtime 与 gateway 只消费它决定实际 Provider 与 Model，不得各自重新拼装
// Backend / Session / Provider 的优先级。
//
// 秘密边界：APIKey 只存在于执行侧契约里（与 llm_provider_svc.ResolvedModel 同口径）；
// 展示侧永远走脱敏 DTO，不进入本结构体所在的日志 / IPC 路径。
type EffectiveLLMConfig struct {
	// Mode 目标模式（见 EffectiveLLMConfigMode）。
	Mode EffectiveLLMConfigMode
	// ProviderKey / ModelKey 是持久化目标的稳定身份（来自 ModelTarget）。
	ProviderKey string
	ModelKey    string
	// ProviderType 是 Provider 的类型（anthropic / openai-chat / openai-response）。
	ProviderType string
	// ProviderName 是供应商展示名（仅日志/展示，不含凭证）。
	ProviderName string
	// ModelID 是实际发给上游的模型 id（provider-default 每轮解析当前默认）。
	ModelID string
	// ContextWindow / MaxOutput 是解析出模型的元数据（供上下文窗口展示 / provider 扩展）。
	ContextWindow int
	MaxOutput     int
	// BaseURL / APIKey / HasAPIKey 是执行侧连接信息。
	BaseURL   string
	APIKey    string
	HasAPIKey bool
}

// EffectiveModelID 返回解析出的实际模型 id；native（无供应商）时为空串。
func (c *EffectiveLLMConfig) EffectiveModelID() string {
	if c == nil {
		return ""
	}
	return c.ModelID
}

// EffectiveProviderKey 返回供应商稳定 key；native 时为空串。
func (c *EffectiveLLMConfig) EffectiveProviderKey() string {
	if c == nil {
		return ""
	}
	return c.ProviderKey
}
