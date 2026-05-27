// Package llm_provider_entity 维护 LLM 供应商的充血实体。
//
// 一个 LLMProvider = 一次"我可以调谁家的 API"的凭证 + 配置：
//   - Type    决定走哪个 cago agents/provider 实现（anthropic / openai-chat / openai-response）；
//   - APIKey  / BaseURL 是请求 LLM 实际需要的凭证；BaseURL 留空时由 service 层填默认值。
package llm_provider_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

// ProviderType 供应商实现类型。值与 cago agents/provider.Provider.Name() 对齐。
type ProviderType string

const (
	// TypeAnthropic 走 anthropic-sdk-go；Base URL 留空走 https://api.anthropic.com。
	TypeAnthropic ProviderType = "anthropic"
	// TypeOpenAIChat 走 cago provider/openai（基于 sashabaranov/go-openai）打 /v1/chat/completions。
	// OpenAI 兼容端（Ollama / vLLM / Azure）目前都走这条，BaseURL 留空时使用 https://api.openai.com/v1。
	TypeOpenAIChat ProviderType = "openai-chat"
	// TypeOpenAIResponse 走 cago provider/openai_response（基于官方 openai/openai-go）打 /v1/responses。
	// 适用于 o-series、gpt-5-codex 等仅支持 Responses API 的 OpenAI 模型；
	// 多数 OpenAI 兼容端尚未实现此协议，请优先选 TypeOpenAIChat。
	TypeOpenAIResponse ProviderType = "openai-response"
)

// LLMProvider 一条供应商配置记录。
//
// Model / MaxOutput / ContextWindow 是用户为该 provider 选择的默认模型与采样上限：
//   - Model         默认调用的模型 id，可留空（让上层每次显式指定）；
//   - MaxOutput     单次响应最大输出 token 数，0 表示沿用 cago 内置 catalog 默认；
//   - ContextWindow 上下文窗口 token 数，0 表示沿用 cago 内置 catalog 默认。
type LLMProvider struct {
	ID            int64  `gorm:"column:id;primaryKey;autoIncrement"`
	ProviderKey   string `gorm:"column:provider_key;type:text;not null;uniqueIndex:uniq_llm_providers_provider_key;default:''" json:"providerKey"`
	Type          string `gorm:"column:type;type:text;not null"`
	Name          string `gorm:"column:name;type:text;not null"`
	APIKey        string `gorm:"column:api_key;type:text;not null;default:''"`
	BaseURL       string `gorm:"column:base_url;type:text;not null;default:''"`
	Model         string `gorm:"column:model;type:text;not null;default:''"`
	MaxOutput     int    `gorm:"column:max_output;type:int;not null;default:0"`
	ContextWindow int    `gorm:"column:context_window;type:int;not null;default:0"`
	Status        int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime    int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime    int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

// TableName 绑定表名。
func (*LLMProvider) TableName() string { return "llm_providers" }

// IsActive 是否处于启用态（未被软删除）。
func (p *LLMProvider) IsActive() bool { return p != nil && p.Status == consts.ACTIVE }

// Check 校验关键字段。空 Name / 不支持的 Type 直接返回业务错误。
func (p *LLMProvider) Check(ctx context.Context) error {
	if p == nil {
		return i18n.NewError(ctx, code.LLMProviderNotFound)
	}
	if strings.TrimSpace(p.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	switch ProviderType(p.Type) {
	case TypeAnthropic, TypeOpenAIChat, TypeOpenAIResponse:
		return nil
	default:
		return i18n.NewError(ctx, code.LLMProviderInvalidType)
	}
}

// IsAnthropic 是否走 Anthropic provider。
func (p *LLMProvider) IsAnthropic() bool {
	return ProviderType(p.Type) == TypeAnthropic
}

// IsOpenAIChat 是否走 OpenAI Chat Completions（/v1/chat/completions）。
func (p *LLMProvider) IsOpenAIChat() bool {
	return ProviderType(p.Type) == TypeOpenAIChat
}

// IsOpenAIResponse 是否走 OpenAI Responses API（/v1/responses）。
func (p *LLMProvider) IsOpenAIResponse() bool {
	return ProviderType(p.Type) == TypeOpenAIResponse
}

// IsOpenAICompatible 是否属于 OpenAI 系列（chat 或 responses）。
// service 层判断「能复用 /v1/models 列模型 / 用 OpenAI vendor 富化元数据」时用。
func (p *LLMProvider) IsOpenAICompatible() bool {
	return p.IsOpenAIChat() || p.IsOpenAIResponse()
}

// MaskedAPIKey 用于前端只读展示，不暴露原始 key。
func (p *LLMProvider) MaskedAPIKey() string {
	key := p.APIKey
	if key == "" {
		return ""
	}
	if len(key) <= 8 {
		return strings.Repeat("•", len(key))
	}
	return key[:4] + strings.Repeat("•", 6) + key[len(key)-4:]
}
