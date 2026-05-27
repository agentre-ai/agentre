package llm_provider_svc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cago-frame/agents/provider/models"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/google/uuid"

	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/pkg/llmcatalog"
	"agentre/internal/repository/llm_provider_repo"
)

// 默认 endpoint。BaseURL 留空时使用。
const (
	defaultAnthropicBaseURL = "https://api.anthropic.com"
	defaultOpenAIBaseURL    = "https://api.openai.com/v1"
	testConnectionPrompt    = "hi"
	testConnectionMaxTokens = 16
	// anthropicVersion Anthropic Messages / Models API 必填的版本头。
	// 与 cago agents/provider/anthropics 当前 SDK 使用的版本对齐。
	anthropicVersion = "2023-06-01"
)

// httpDoer 抽象 http.Client，方便在单测里替换实现。
type httpDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// LLMProviderSvc LLM 供应商应用服务。
type LLMProviderSvc interface {
	List(ctx context.Context, req *ListProvidersRequest) (*ListProvidersResponse, error)
	Create(ctx context.Context, req *CreateProviderRequest) (*CreateProviderResponse, error)
	Update(ctx context.Context, req *UpdateProviderRequest) (*UpdateProviderResponse, error)
	Delete(ctx context.Context, req *DeleteProviderRequest) (*DeleteProviderResponse, error)
	ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error)
	PreviewModels(ctx context.Context, req *PreviewModelsRequest) (*PreviewModelsResponse, error)
	TestConnection(ctx context.Context, req *TestConnectionRequest) (*TestConnectionResponse, error)
	LookupModel(ctx context.Context, req *LookupModelRequest) (*LookupModelResponse, error)
}

type llmProviderSvc struct {
	http httpDoer
	now  func() int64
}

var defaultLLMProvider LLMProviderSvc = &llmProviderSvc{
	http: &http.Client{Timeout: 15 * time.Second},
	now:  func() int64 { return time.Now().Unix() },
}

// LLMProvider 取默认服务单例。
func LLMProvider() LLMProviderSvc { return defaultLLMProvider }

func (s *llmProviderSvc) List(ctx context.Context, _ *ListProvidersRequest) (*ListProvidersResponse, error) {
	rows, err := llm_provider_repo.LLMProvider().List(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*ProviderItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toItem(row))
	}
	return &ListProvidersResponse{Items: items}, nil
}

func (s *llmProviderSvc) Create(ctx context.Context, req *CreateProviderRequest) (*CreateProviderResponse, error) {
	now := s.now()
	p := &llm_provider_entity.LLMProvider{
		Type:          strings.TrimSpace(req.Type),
		Name:          strings.TrimSpace(req.Name),
		ProviderKey:   uuid.NewString(),
		APIKey:        strings.TrimSpace(req.APIKey),
		BaseURL:       strings.TrimSpace(req.BaseURL),
		Model:         strings.TrimSpace(req.Model),
		MaxOutput:     clampTokens(req.MaxOutput),
		ContextWindow: clampTokens(req.ContextWindow),
		Status:        consts.ACTIVE,
		Createtime:    now,
		Updatetime:    now,
	}
	if err := p.Check(ctx); err != nil {
		return nil, err
	}

	exist, err := llm_provider_repo.LLMProvider().FindByName(ctx, p.Name)
	if err != nil {
		return nil, err
	}
	if exist != nil {
		return nil, i18n.NewError(ctx, code.LLMProviderNameDuplicated)
	}

	if err := llm_provider_repo.LLMProvider().Create(ctx, p); err != nil {
		return nil, err
	}
	return &CreateProviderResponse{Item: toItem(p)}, nil
}

func (s *llmProviderSvc) Update(ctx context.Context, req *UpdateProviderRequest) (*UpdateProviderResponse, error) {
	p, err := llm_provider_repo.LLMProvider().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, i18n.NewError(ctx, code.LLMProviderNotFound)
	}

	newName := strings.TrimSpace(req.Name)
	if newName != p.Name {
		exist, err := llm_provider_repo.LLMProvider().FindByName(ctx, newName)
		if err != nil {
			return nil, err
		}
		if exist != nil && exist.ID != p.ID {
			return nil, i18n.NewError(ctx, code.LLMProviderNameDuplicated)
		}
	}

	p.Name = newName
	p.BaseURL = strings.TrimSpace(req.BaseURL)
	if newKey := strings.TrimSpace(req.APIKey); newKey != "" {
		p.APIKey = newKey
	}
	p.Model = strings.TrimSpace(req.Model)
	p.MaxOutput = clampTokens(req.MaxOutput)
	p.ContextWindow = clampTokens(req.ContextWindow)
	p.Updatetime = s.now()

	if err := p.Check(ctx); err != nil {
		return nil, err
	}
	if err := llm_provider_repo.LLMProvider().Update(ctx, p); err != nil {
		return nil, err
	}
	return &UpdateProviderResponse{Item: toItem(p)}, nil
}

func (s *llmProviderSvc) Delete(ctx context.Context, req *DeleteProviderRequest) (*DeleteProviderResponse, error) {
	p, err := llm_provider_repo.LLMProvider().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, i18n.NewError(ctx, code.LLMProviderNotFound)
	}
	if err := llm_provider_repo.LLMProvider().Delete(ctx, p.ID); err != nil {
		return nil, err
	}
	return &DeleteProviderResponse{}, nil
}

func (s *llmProviderSvc) ListModels(ctx context.Context, req *ListModelsRequest) (*ListModelsResponse, error) {
	p, err := llm_provider_repo.LLMProvider().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, i18n.NewError(ctx, code.LLMProviderNotFound)
	}

	ids, err := s.fetchModelIDs(ctx, p)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", i18n.NewError(ctx, code.LLMProviderFetchModels), err)
	}

	items := make([]*ModelInfo, 0, len(ids))
	for _, id := range ids {
		items = append(items, enrichModel(id, p))
	}
	return &ListModelsResponse{Items: items}, nil
}

func (s *llmProviderSvc) LookupModel(_ context.Context, req *LookupModelRequest) (*LookupModelResponse, error) {
	id := strings.TrimSpace(req.ID)
	if id == "" {
		return &LookupModelResponse{}, nil
	}
	info, ok := llmcatalog.Lookup(id)
	if !ok {
		return &LookupModelResponse{}, nil
	}
	return &LookupModelResponse{
		Known:         true,
		Vendor:        string(info.Vendor),
		ContextWindow: info.ContextWindow,
		MaxOutput:     info.MaxOutput,
	}, nil
}

func (s *llmProviderSvc) PreviewModels(ctx context.Context, req *PreviewModelsRequest) (*PreviewModelsResponse, error) {
	probe := &llm_provider_entity.LLMProvider{
		Type:    strings.TrimSpace(req.Type),
		APIKey:  strings.TrimSpace(req.APIKey),
		BaseURL: strings.TrimSpace(req.BaseURL),
	}
	ids, err := s.fetchModelIDs(ctx, probe)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", i18n.NewError(ctx, code.LLMProviderFetchModels), err)
	}
	items := make([]*ModelInfo, 0, len(ids))
	for _, id := range ids {
		items = append(items, enrichModel(id, probe))
	}
	return &PreviewModelsResponse{Items: items}, nil
}

func (s *llmProviderSvc) TestConnection(ctx context.Context, req *TestConnectionRequest) (*TestConnectionResponse, error) {
	p, err := s.providerForTest(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := s.sendTestMessage(ctx, p); err != nil {
		// 测试连通性时上游错误属于"用户可读结果"，不上抛 i18n error，让前端
		// 拿 message 展示。nilerr 的 lint 由此豁免。
		return &TestConnectionResponse{OK: false, Message: err.Error()}, nil //nolint:nilerr
	}
	return &TestConnectionResponse{OK: true, Message: "模型调用成功"}, nil
}

func (s *llmProviderSvc) providerForTest(ctx context.Context, req *TestConnectionRequest) (*llm_provider_entity.LLMProvider, error) {
	var saved *llm_provider_entity.LLMProvider
	if req.ID > 0 {
		p, err := llm_provider_repo.LLMProvider().Find(ctx, req.ID)
		if err != nil {
			return nil, err
		}
		if p == nil {
			return nil, i18n.NewError(ctx, code.LLMProviderNotFound)
		}
		saved = p
		if !req.UseDraft {
			return saved, nil
		}
	}
	return mergeTestDraft(saved, req), nil
}

func mergeTestDraft(saved *llm_provider_entity.LLMProvider, req *TestConnectionRequest) *llm_provider_entity.LLMProvider {
	out := &llm_provider_entity.LLMProvider{}
	if saved != nil {
		*out = *saved
	}
	if typ := strings.TrimSpace(req.Type); typ != "" {
		out.Type = typ
	}
	if key := strings.TrimSpace(req.APIKey); key != "" || saved == nil {
		out.APIKey = key
	}
	out.BaseURL = strings.TrimSpace(req.BaseURL)
	out.Model = strings.TrimSpace(req.Model)
	return out
}

// fetchModelIDs 调 provider 的 /v1/models endpoint，返回原始 id 列表。
// openai-chat 与 openai-response 共用 /v1/models —— OpenAI 的 models 接口不区分
// 是给 chat 还是 responses API 用的。
func (s *llmProviderSvc) fetchModelIDs(ctx context.Context, p *llm_provider_entity.LLMProvider) ([]string, error) {
	switch llm_provider_entity.ProviderType(p.Type) {
	case llm_provider_entity.TypeAnthropic:
		return s.fetchAnthropicModels(ctx, p)
	case llm_provider_entity.TypeOpenAIChat, llm_provider_entity.TypeOpenAIResponse:
		return s.fetchOpenAIModels(ctx, p)
	default:
		return nil, i18n.NewError(ctx, code.LLMProviderInvalidType)
	}
}

func (s *llmProviderSvc) fetchAnthropicModels(ctx context.Context, p *llm_provider_entity.LLMProvider) ([]string, error) {
	base := strings.TrimRight(firstNonEmpty(p.BaseURL, defaultAnthropicBaseURL), "/")
	return s.fetchModelList(ctx, base+"/v1/models", func(h http.Header) {
		h.Set("x-api-key", p.APIKey)
		h.Set("anthropic-version", anthropicVersion)
	})
}

func (s *llmProviderSvc) fetchOpenAIModels(ctx context.Context, p *llm_provider_entity.LLMProvider) ([]string, error) {
	base := strings.TrimRight(firstNonEmpty(p.BaseURL, defaultOpenAIBaseURL), "/")
	return s.fetchModelList(ctx, base+"/models", func(h http.Header) {
		if p.APIKey != "" {
			h.Set("Authorization", "Bearer "+p.APIKey)
		}
	})
}

// sendTestMessage 发送一条最小用户消息，验证默认模型不只是凭证可列模型，而是真的能完成一次 LLM 调用。
func (s *llmProviderSvc) sendTestMessage(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	if strings.TrimSpace(p.Model) == "" {
		return errors.New("请先选择默认模型")
	}
	switch llm_provider_entity.ProviderType(p.Type) {
	case llm_provider_entity.TypeAnthropic:
		return s.sendAnthropicTestMessage(ctx, p)
	case llm_provider_entity.TypeOpenAIChat:
		return s.sendOpenAITestMessage(ctx, p)
	case llm_provider_entity.TypeOpenAIResponse:
		return s.sendOpenAIResponseTestMessage(ctx, p)
	default:
		return i18n.NewError(ctx, code.LLMProviderInvalidType)
	}
}

func (s *llmProviderSvc) sendAnthropicTestMessage(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	base := strings.TrimRight(firstNonEmpty(p.BaseURL, defaultAnthropicBaseURL), "/")
	payload := struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model:     p.Model,
		MaxTokens: testConnectionMaxTokens,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: testConnectionPrompt},
		},
	}
	req, err := newJSONRequest(ctx, base+"/v1/messages", payload)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", p.APIKey)
	req.Header.Set("anthropic-version", anthropicVersion)

	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}
	if err := s.doJSON(req, &resp); err != nil {
		return err
	}
	if len(resp.Content) == 0 && resp.StopReason == "" {
		return errors.New("empty completion response")
	}
	return nil
}

func (s *llmProviderSvc) sendOpenAITestMessage(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	base := strings.TrimRight(firstNonEmpty(p.BaseURL, defaultOpenAIBaseURL), "/")
	payload := struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Model: p.Model,
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "user", Content: testConnectionPrompt},
		},
	}
	req, err := newJSONRequest(ctx, base+"/chat/completions", payload)
	if err != nil {
		return err
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Role    string `json:"role"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := s.doJSON(req, &resp); err != nil {
		return err
	}
	if len(resp.Choices) == 0 {
		return errors.New("empty completion choices")
	}
	return nil
}

// sendOpenAIResponseTestMessage 走 /v1/responses，验证 openai-response 凭证 + 模型可用。
// 请求体只带 model + input（字符串形式），最大输出限到 testConnectionMaxTokens 减少花费。
// 响应里 output[].content[].text 是模型回答；空回也认为成功（part of empty 200）。
func (s *llmProviderSvc) sendOpenAIResponseTestMessage(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	base := strings.TrimRight(firstNonEmpty(p.BaseURL, defaultOpenAIBaseURL), "/")
	payload := struct {
		Model           string `json:"model"`
		Input           string `json:"input"`
		MaxOutputTokens int    `json:"max_output_tokens"`
	}{
		Model:           p.Model,
		Input:           testConnectionPrompt,
		MaxOutputTokens: testConnectionMaxTokens,
	}
	req, err := newJSONRequest(ctx, base+"/responses", payload)
	if err != nil {
		return err
	}
	if p.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.APIKey)
	}

	var resp struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := s.doJSON(req, &resp); err != nil {
		return err
	}
	// 200 + 空 output 也认为联通；只要 doJSON 没抛 http error，凭证 + 模型就 OK。
	return nil
}

// fetchModelList Anthropic 与 OpenAI 的 /models 接口同享 `{"data":[{"id":"..."}]}`
// 形状，差异仅在 endpoint 与认证头；setAuth 负责注入特定 provider 需要的请求头。
func (s *llmProviderSvc) fetchModelList(ctx context.Context, url string, setAuth func(http.Header)) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	setAuth(req.Header)

	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := s.doJSON(req, &payload); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.ID != "" {
			out = append(out, m.ID)
		}
	}
	return out, nil
}

func newJSONRequest(ctx context.Context, url string, payload any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

func (s *llmProviderSvc) doJSON(req *http.Request, out any) error {
	resp, err := s.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(body), 200))
	}
	if len(body) == 0 {
		return errors.New("empty response body")
	}
	return json.Unmarshal(body, out)
}

// enrichModel 用 cago agents 内置目录补全已知模型的元数据；命中失败时只携带 id。
func enrichModel(id string, p *llm_provider_entity.LLMProvider) *ModelInfo {
	out := &ModelInfo{ID: id}
	if info, ok := llmcatalog.Lookup(id); ok {
		out.Vendor = string(info.Vendor)
		out.ContextWindow = info.ContextWindow
		out.MaxOutput = info.MaxOutput
		out.Modalities = toStrings(info.Modalities)
		out.Thinking = info.Thinking
		out.KnownInCago = true
		return out
	}
	// 未命中目录：vendor 退而用 provider type 推断。
	switch llm_provider_entity.ProviderType(p.Type) {
	case llm_provider_entity.TypeAnthropic:
		out.Vendor = string(models.VendorAnthropic)
	case llm_provider_entity.TypeOpenAIChat, llm_provider_entity.TypeOpenAIResponse:
		out.Vendor = string(models.VendorOpenAI)
	}
	return out
}

func toItem(p *llm_provider_entity.LLMProvider) *ProviderItem {
	return &ProviderItem{
		ID:            p.ID,
		Type:          p.Type,
		ProviderKey:   p.ProviderKey,
		Name:          p.Name,
		BaseURL:       p.BaseURL,
		MaskedAPIKey:  p.MaskedAPIKey(),
		HasAPIKey:     p.APIKey != "",
		Model:         p.Model,
		MaxOutput:     p.MaxOutput,
		ContextWindow: p.ContextWindow,
		Createtime:    p.Createtime,
		Updatetime:    p.Updatetime,
	}
}

// clampTokens 把负值视作未指定（0），其余保持原值。
func clampTokens(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

func toStrings(ms []models.Modality) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		out = append(out, string(m))
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
