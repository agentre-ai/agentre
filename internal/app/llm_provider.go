package app

import (
	"agentre/internal/service/llm_provider_svc"
)

// ListLLMProviders 列出全部 LLM 供应商配置（脱敏）。
func (a *App) ListLLMProviders() (*llm_provider_svc.ListProvidersResponse, error) {
	return llm_provider_svc.LLMProvider().List(a.ctx, &llm_provider_svc.ListProvidersRequest{})
}

// CreateLLMProvider 新建 LLM 供应商。
func (a *App) CreateLLMProvider(req *llm_provider_svc.CreateProviderRequest) (*llm_provider_svc.CreateProviderResponse, error) {
	return llm_provider_svc.LLMProvider().Create(a.ctx, req)
}

// UpdateLLMProvider 更新 LLM 供应商配置；apiKey 留空保留原值。
func (a *App) UpdateLLMProvider(req *llm_provider_svc.UpdateProviderRequest) (*llm_provider_svc.UpdateProviderResponse, error) {
	return llm_provider_svc.LLMProvider().Update(a.ctx, req)
}

// DeleteLLMProvider 软删除 LLM 供应商。
func (a *App) DeleteLLMProvider(req *llm_provider_svc.DeleteProviderRequest) (*llm_provider_svc.DeleteProviderResponse, error) {
	return llm_provider_svc.LLMProvider().Delete(a.ctx, req)
}

// ListLLMModels 实时拉取指定供应商的可用模型，并用 cago 内置目录补全元数据。
func (a *App) ListLLMModels(req *llm_provider_svc.ListModelsRequest) (*llm_provider_svc.ListModelsResponse, error) {
	return llm_provider_svc.LLMProvider().ListModels(a.ctx, req)
}

// PreviewLLMModels 在 provider 尚未落库时按表单凭证拉取模型列表，供「新建供应商」时下拉选择。
func (a *App) PreviewLLMModels(req *llm_provider_svc.PreviewModelsRequest) (*llm_provider_svc.PreviewModelsResponse, error) {
	return llm_provider_svc.LLMProvider().PreviewModels(a.ctx, req)
}

// LookupLLMModel 按模型 id 查 cago 内置 catalog 的默认上下文 / 最大输出，无网络请求。
func (a *App) LookupLLMModel(req *llm_provider_svc.LookupModelRequest) (*llm_provider_svc.LookupModelResponse, error) {
	return llm_provider_svc.LLMProvider().LookupModel(a.ctx, req)
}

// TestLLMProvider 用默认模型发送 hi，测试 LLM 供应商是否可真实调用。
func (a *App) TestLLMProvider(req *llm_provider_svc.TestConnectionRequest) (*llm_provider_svc.TestConnectionResponse, error) {
	return llm_provider_svc.LLMProvider().TestConnection(a.ctx, req)
}
