// Package llm_provider_svc 暴露 LLM 供应商的应用服务接口与请求/响应类型。
//
// 类型定义直接被 Wails 绑定层引用，会被 wails dev / wails build 提取成 TypeScript
// 类型暴露给前端，因此字段名要稳定、json tag 要明确。
package llm_provider_svc

// ProviderItem 单条供应商配置（脱敏后）。
//
// 注意：apiKey 仅在创建 / 更新请求中由前端传入；List 接口返回 maskedApiKey
// 替代，避免把明文 key 暴露到日志、IPC trace 或 React DevTools 中。
type ProviderItem struct {
	ID            int64  `json:"id"`
	Type          string `json:"type"`
	ProviderKey   string `json:"providerKey"`
	Name          string `json:"name"`
	BaseURL       string `json:"baseUrl"`
	MaskedAPIKey  string `json:"maskedApiKey"`
	HasAPIKey     bool   `json:"hasApiKey"`
	Model         string `json:"model"`
	MaxOutput     int    `json:"maxOutput"`
	ContextWindow int    `json:"contextWindow"`
	Createtime    int64  `json:"createtime"`
	Updatetime    int64  `json:"updatetime"`
}

// ListProvidersRequest 入参占位。
type ListProvidersRequest struct{}

// ListProvidersResponse 列出全部启用的供应商。
type ListProvidersResponse struct {
	Items []*ProviderItem `json:"items"`
}

// CreateProviderRequest 新建供应商。
type CreateProviderRequest struct {
	Type          string `json:"type" binding:"required"`
	Name          string `json:"name" binding:"required"`
	APIKey        string `json:"apiKey"`
	BaseURL       string `json:"baseUrl"`
	Model         string `json:"model"`
	MaxOutput     int    `json:"maxOutput"`
	ContextWindow int    `json:"contextWindow"`
}

// CreateProviderResponse 返回创建后的实体。
type CreateProviderResponse struct {
	Item *ProviderItem `json:"item"`
}

// UpdateProviderRequest 更新供应商。APIKey 留空表示沿用既有值。
type UpdateProviderRequest struct {
	ID            int64  `json:"id" binding:"required"`
	Name          string `json:"name" binding:"required"`
	APIKey        string `json:"apiKey"`
	BaseURL       string `json:"baseUrl"`
	Model         string `json:"model"`
	MaxOutput     int    `json:"maxOutput"`
	ContextWindow int    `json:"contextWindow"`
}

// UpdateProviderResponse 返回更新后的实体。
type UpdateProviderResponse struct {
	Item *ProviderItem `json:"item"`
}

// DeleteProviderRequest 软删除供应商。
type DeleteProviderRequest struct {
	ID int64 `json:"id" binding:"required"`
}

// DeleteProviderResponse 占位返回。
type DeleteProviderResponse struct{}

// ModelInfo 模型在前端的展示元数据。
//
// 字段并集来自 provider /v1/models 返回 + cago agents/provider/models 内置目录：
//   - id        provider 侧实际可调用的模型 id；
//   - 其余字段尽量从内置目录命中后回填，命中失败则保持零值/默认。
type ModelInfo struct {
	ID            string   `json:"id"`
	Vendor        string   `json:"vendor"`
	ContextWindow int      `json:"contextWindow"`
	MaxOutput     int      `json:"maxOutput"`
	Modalities    []string `json:"modalities"`
	Thinking      bool     `json:"thinking"`
	KnownInCago   bool     `json:"knownInCago"`
}

// ListModelsRequest 触发实时拉取模型列表。
type ListModelsRequest struct {
	ID int64 `json:"id" binding:"required"`
}

// ListModelsResponse 模型列表。
type ListModelsResponse struct {
	Items []*ModelInfo `json:"items"`
}

// TestConnectionRequest 用默认模型发送一条 hi，校验供应商是否能完成真实 LLM 调用。
//
// ID > 0 且 UseDraft=false 时测试已保存配置；UseDraft=true 或 ID=0 时测试
// 表单草稿配置。编辑草稿中 APIKey 留空会沿用已保存 key。
type TestConnectionRequest struct {
	ID       int64  `json:"id"`
	UseDraft bool   `json:"useDraft"`
	Type     string `json:"type"`
	APIKey   string `json:"apiKey"`
	BaseURL  string `json:"baseUrl"`
	Model    string `json:"model"`
}

// TestConnectionResponse 报告测试结果。OK = false 时 Message 携带原因；OK = true 时 Message 携带成功说明。
type TestConnectionResponse struct {
	OK         bool   `json:"ok"`
	Message    string `json:"message"`
	ModelCount int    `json:"modelCount"`
}

// LookupModelRequest 仅按模型 id 查询 cago 内置目录元数据，不发出 HTTP 请求。
type LookupModelRequest struct {
	ID string `json:"id" binding:"required"`
}

// LookupModelResponse 命中目录则 Known=true 并填充上下文 / 最大输出；未命中也返回成功，Known=false。
type LookupModelResponse struct {
	Known         bool   `json:"known"`
	Vendor        string `json:"vendor"`
	ContextWindow int    `json:"contextWindow"`
	MaxOutput     int    `json:"maxOutput"`
}

// PreviewModelsRequest 在 provider 尚未落库时按用户填写的临时凭证拉取模型列表。
//
// 用途：新建表单里点「获取模型」时，立刻给用户一个可选择的下拉。
type PreviewModelsRequest struct {
	Type    string `json:"type" binding:"required"`
	APIKey  string `json:"apiKey"`
	BaseURL string `json:"baseUrl"`
}

// PreviewModelsResponse 同 ListModelsResponse。
type PreviewModelsResponse struct {
	Items []*ModelInfo `json:"items"`
}
