package llm_provider_svc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
)

type fakeDoer struct {
	last   *http.Request
	status int
	body   string
	err    error
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.last = req
	if f.err != nil {
		return nil, f.err
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Body:       io.NopCloser(bytes.NewBufferString(f.body)),
		Header:     make(http.Header),
	}, nil
}

func (f *fakeDoer) respond(status int, body string) {
	f.status = status
	f.body = body
}

func setupSvcTest(t *testing.T) (
	context.Context,
	*mock_llm_provider_repo.MockLLMProviderRepo,
	*fakeDoer,
	*llmProviderSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	mockRepo := mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl)
	llm_provider_repo.RegisterLLMProvider(mockRepo)

	doer := &fakeDoer{}
	svc := &llmProviderSvc{http: doer, now: func() int64 { return 1234567890 }}
	return context.Background(), mockRepo, doer, svc
}

func TestCreateProvider(t *testing.T) {
	convey.Convey("Create LLM provider", t, func() {
		ctx, mockRepo, _, svc := setupSvcTest(t)

		convey.Convey("成功创建 Anthropic 供应商", func() {
			mockRepo.EXPECT().FindByName(gomock.Any(), "production").Return(nil, nil)
			mockRepo.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
				DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
					p.ID = 7
					return nil
				})

			resp, err := svc.Create(ctx, &CreateProviderRequest{
				Type:   "anthropic",
				Name:   "production",
				APIKey: "test-ant-key-1234",
			})
			assert.NoError(t, err)
			assert.NotNil(t, resp)
			assert.Equal(t, int64(7), resp.Item.ID)
			assert.True(t, resp.Item.HasAPIKey)
			// masked api key 应当不暴露完整 key
			assert.NotContains(t, resp.Item.MaskedAPIKey, "test-key")
		})

		convey.Convey("Create 时自动 mint UUIDv4 并在响应中返回", func() {
			var capturedEntity *llm_provider_entity.LLMProvider
			mockRepo.EXPECT().FindByName(gomock.Any(), "uuid-test").Return(nil, nil)
			mockRepo.EXPECT().Create(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
				DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
					capturedEntity = p
					p.ID = 42
					return nil
				})

			resp, err := svc.Create(ctx, &CreateProviderRequest{ //nolint:gosec // credential-shaped API key is a test fixture.
				Type:   "anthropic",
				Name:   "uuid-test",
				APIKey: "test-ant-uuid",
			})
			assert.NoError(t, err)
			assert.NotNil(t, resp)
			// ProviderKey must be a valid UUIDv4
			_, parseErr := uuid.Parse(resp.Item.ProviderKey)
			assert.NoError(t, parseErr, "ProviderKey should be a valid UUID")
			assert.NotEmpty(t, resp.Item.ProviderKey)
			// The entity that hit the repo must have the same key
			assert.NotNil(t, capturedEntity)
			assert.Equal(t, resp.Item.ProviderKey, capturedEntity.ProviderKey)
		})

		convey.Convey("名称重复返回错误", func() {
			mockRepo.EXPECT().FindByName(gomock.Any(), "dup").Return(&llm_provider_entity.LLMProvider{
				ID: 1, Name: "dup",
			}, nil)
			_, err := svc.Create(ctx, &CreateProviderRequest{Type: "openai-chat", Name: "dup", APIKey: "k"})
			assert.Error(t, err)
		})

		convey.Convey("不支持的类型被拒绝", func() {
			_, err := svc.Create(ctx, &CreateProviderRequest{Type: "google", Name: "x"})
			assert.Error(t, err)
		})
	})
}

func TestUpdateProvider(t *testing.T) {
	convey.Convey("Update LLM provider", t, func() {
		ctx, mockRepo, _, svc := setupSvcTest(t)

		convey.Convey("APIKey 为空时保留原值", func() {
			existing := &llm_provider_entity.LLMProvider{
				ID: 3, Type: "openai-chat", Name: "old", APIKey: "old-key", Status: 1,
			}
			mockRepo.EXPECT().Find(gomock.Any(), int64(3)).Return(existing, nil)
			mockRepo.EXPECT().FindByName(gomock.Any(), "new").Return(nil, nil)
			mockRepo.EXPECT().Update(gomock.Any(), gomock.AssignableToTypeOf(&llm_provider_entity.LLMProvider{})).
				DoAndReturn(func(_ context.Context, p *llm_provider_entity.LLMProvider) error {
					assert.Equal(t, "old-key", p.APIKey)
					assert.Equal(t, "new", p.Name)
					return nil
				})
			_, err := svc.Update(ctx, &UpdateProviderRequest{ID: 3, Name: "new"})
			assert.NoError(t, err)
		})

		convey.Convey("供应商不存在", func() {
			mockRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
			_, err := svc.Update(ctx, &UpdateProviderRequest{ID: 99, Name: "x"})
			assert.Error(t, err)
		})
	})
}

func TestListModelsAnthropic(t *testing.T) {
	convey.Convey("ListModels for Anthropic", t, func() {
		ctx, mockRepo, doer, svc := setupSvcTest(t)

		mockRepo.EXPECT().Find(gomock.Any(), int64(1)).Return(&llm_provider_entity.LLMProvider{
			ID: 1, Type: "anthropic", APIKey: "test-ant-key", Status: 1,
		}, nil)
		doer.respond(200, `{"data":[{"id":"claude-opus-4-7"},{"id":"unknown-model"}]}`)

		resp, err := svc.ListModels(ctx, &ListModelsRequest{ID: 1})
		assert.NoError(t, err)
		assert.Equal(t, 2, len(resp.Items))

		// 第一个模型命中 cago 内置目录，元数据应被填充
		known := resp.Items[0]
		assert.Equal(t, "claude-opus-4-7", known.ID)
		assert.True(t, known.KnownInCago)
		assert.Equal(t, "anthropic", known.Vendor)
		assert.Greater(t, known.ContextWindow, 0)

		// 未知模型保留 id + 根据 provider type 推断的 vendor
		unknown := resp.Items[1]
		assert.False(t, unknown.KnownInCago)
		assert.Equal(t, "anthropic", unknown.Vendor)

		// 校验 HTTP 请求头
		assert.Equal(t, "test-ant-key", doer.last.Header.Get("x-api-key"))
		assert.NotEmpty(t, doer.last.Header.Get("anthropic-version"))
		assert.True(t, strings.HasSuffix(doer.last.URL.Path, "/v1/models"))
	})
}

func TestListModelsOpenAIUsesCustomBaseURL(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(&llm_provider_entity.LLMProvider{
		ID: 5, Type: "openai-chat", APIKey: "test-openai-key", BaseURL: "http://localhost:11434/v1", Status: 1,
	}, nil)
	doer.respond(200, `{"data":[{"id":"gpt-5.5"}]}`)

	resp, err := svc.ListModels(ctx, &ListModelsRequest{ID: 5})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(resp.Items))
	assert.Equal(t, "http://localhost:11434/v1/models", doer.last.URL.String())
	assert.Equal(t, "Bearer test-openai-key", doer.last.Header.Get("Authorization"))
}

func TestListModelsUpstreamError(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(&llm_provider_entity.LLMProvider{
		ID: 2, Type: "openai-chat", APIKey: "bad", Status: 1,
	}, nil)
	doer.respond(401, `{"error":"invalid api key"}`)
	_, err := svc.ListModels(ctx, &ListModelsRequest{ID: 2})
	assert.Error(t, err)
}

func TestTestConnectionSendsHiToOpenAI(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(9)).Return(&llm_provider_entity.LLMProvider{
		ID:      9,
		Type:    "openai-chat",
		APIKey:  "test-openai-key",
		BaseURL: "http://localhost:11434/v1",
		Model:   "gpt-4o",
		Status:  1,
	}, nil)
	doer.respond(200, `{"choices":[{"message":{"role":"assistant","content":"hi there"},"finish_reason":"stop"}]}`)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{ID: 9})
	assert.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, http.MethodPost, doer.last.Method)
	assert.Equal(t, "http://localhost:11434/v1/chat/completions", doer.last.URL.String())
	assert.Equal(t, "Bearer test-openai-key", doer.last.Header.Get("Authorization"))
	assert.Equal(t, "application/json", doer.last.Header.Get("Content-Type"))

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	assert.NoError(t, json.NewDecoder(doer.last.Body).Decode(&payload))
	assert.Equal(t, "gpt-4o", payload.Model)
	assert.Len(t, payload.Messages, 1)
	assert.Equal(t, "user", payload.Messages[0].Role)
	assert.Equal(t, "hi", payload.Messages[0].Content)
}

func TestTestConnectionSendsHiToOpenAIResponse(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(13)).Return(&llm_provider_entity.LLMProvider{
		ID:      13,
		Type:    "openai-response",
		APIKey:  "test-response-key",
		BaseURL: "https://api.openai.com/v1",
		Model:   "gpt-5-codex",
		Status:  1,
	}, nil)
	doer.respond(200, `{"status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}]}`)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{ID: 13})
	assert.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, http.MethodPost, doer.last.Method)
	assert.Equal(t, "https://api.openai.com/v1/responses", doer.last.URL.String())
	assert.Equal(t, "Bearer test-response-key", doer.last.Header.Get("Authorization"))
	assert.Equal(t, "application/json", doer.last.Header.Get("Content-Type"))

	var payload struct {
		Model           string `json:"model"`
		Input           string `json:"input"`
		MaxOutputTokens int    `json:"max_output_tokens"`
	}
	assert.NoError(t, json.NewDecoder(doer.last.Body).Decode(&payload))
	assert.Equal(t, "gpt-5-codex", payload.Model)
	assert.Equal(t, "hi", payload.Input)
	assert.Equal(t, 16, payload.MaxOutputTokens)
}

func TestTestConnectionSendsHiToAnthropic(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(10)).Return(&llm_provider_entity.LLMProvider{ //nolint:gosec // credential-shaped API key is a test fixture.
		ID:     10,
		Type:   "anthropic",
		APIKey: "test-anthropic-key",
		Model:  "claude-sonnet-4-6",
		Status: 1,
	}, nil)
	doer.respond(200, `{"content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn"}`)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{ID: 10})
	assert.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, http.MethodPost, doer.last.Method)
	assert.Equal(t, "https://api.anthropic.com/v1/messages", doer.last.URL.String())
	assert.Equal(t, "test-anthropic-key", doer.last.Header.Get("x-api-key"))
	assert.NotEmpty(t, doer.last.Header.Get("anthropic-version"))
	assert.Equal(t, "application/json", doer.last.Header.Get("Content-Type"))

	var payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Messages  []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	assert.NoError(t, json.NewDecoder(doer.last.Body).Decode(&payload))
	assert.Equal(t, "claude-sonnet-4-6", payload.Model)
	assert.Equal(t, 16, payload.MaxTokens)
	assert.Len(t, payload.Messages, 1)
	assert.Equal(t, "user", payload.Messages[0].Role)
	assert.Equal(t, "hi", payload.Messages[0].Content)
}

func TestTestConnectionSendsHiWithCreateDraft(t *testing.T) {
	ctx, _, doer, svc := setupSvcTest(t)
	doer.respond(200, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{
		UseDraft: true,
		Type:     "openai-chat",
		APIKey:   "test-draft-key",
		BaseURL:  "http://localhost:11434/v1",
		Model:    "llama3.2",
	})
	assert.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "http://localhost:11434/v1/chat/completions", doer.last.URL.String())
	assert.Equal(t, "Bearer test-draft-key", doer.last.Header.Get("Authorization"))

	var payload struct {
		Model    string `json:"model"`
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	assert.NoError(t, json.NewDecoder(doer.last.Body).Decode(&payload))
	assert.Equal(t, "llama3.2", payload.Model)
	assert.Equal(t, "hi", payload.Messages[0].Content)
}

func TestTestConnectionDraftEditKeepsSavedAPIKey(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(12)).Return(&llm_provider_entity.LLMProvider{
		ID:      12,
		Type:    "openai-chat",
		APIKey:  "test-saved-key",
		BaseURL: "http://old.example/v1",
		Model:   "old-model",
		Status:  1,
	}, nil)
	doer.respond(200, `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{
		ID:       12,
		UseDraft: true,
		Type:     "openai-chat",
		BaseURL:  "http://new.example/v1",
		Model:    "new-model",
	})
	assert.NoError(t, err)
	assert.True(t, resp.OK)
	assert.Equal(t, "http://new.example/v1/chat/completions", doer.last.URL.String())
	assert.Equal(t, "Bearer test-saved-key", doer.last.Header.Get("Authorization"))

	var payload struct {
		Model string `json:"model"`
	}
	assert.NoError(t, json.NewDecoder(doer.last.Body).Decode(&payload))
	assert.Equal(t, "new-model", payload.Model)
}

func TestTestConnectionReportsFailure(t *testing.T) {
	ctx, mockRepo, doer, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(8)).Return(&llm_provider_entity.LLMProvider{
		ID: 8, Type: "openai-chat", APIKey: "bad", Model: "gpt-4o", Status: 1,
	}, nil)
	doer.err = errors.New("dial tcp: i/o timeout")
	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{ID: 8})
	assert.NoError(t, err)
	assert.False(t, resp.OK)
	assert.Contains(t, resp.Message, "i/o timeout")
}

func TestTestConnectionRequiresDefaultModel(t *testing.T) {
	ctx, mockRepo, _, svc := setupSvcTest(t)
	mockRepo.EXPECT().Find(gomock.Any(), int64(11)).Return(&llm_provider_entity.LLMProvider{
		ID: 11, Type: "openai-chat", APIKey: "test-openai-key", Status: 1,
	}, nil)

	resp, err := svc.TestConnection(ctx, &TestConnectionRequest{ID: 11})
	assert.NoError(t, err)
	assert.False(t, resp.OK)
	assert.Contains(t, resp.Message, "默认模型")
}
