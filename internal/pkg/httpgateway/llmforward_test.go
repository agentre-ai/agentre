package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
)

// fakeLookup 测试用 provider lookup，返回构造时 inject 的 map。
type fakeLookup struct {
	providers map[string]*llm_provider_entity.LLMProvider
}

func (f *fakeLookup) FindByKey(_ context.Context, key string) (*llm_provider_entity.LLMProvider, error) {
	return f.providers[key], nil
}

func newFakeLookup(items ...*llm_provider_entity.LLMProvider) *fakeLookup {
	m := make(map[string]*llm_provider_entity.LLMProvider, len(items))
	for _, p := range items {
		m[p.ProviderKey] = p
	}
	return &fakeLookup{providers: m}
}

func assertOpenAIForwarded(
	t *testing.T,
	provider *llm_provider_entity.LLMProvider,
	handler func(*Forwarder) http.HandlerFunc,
	backendID int64,
	path string,
	body string,
	apiKeyName string,
) {
	t.Helper()
	upstream, rec := newRecordingUpstream(t, `{"id":"openai_x"}`)
	provider.BaseURL = upstream.URL
	tokens := NewTokenRegistry()
	lookup := newFakeLookup(provider)
	f := NewForwarder(tokens, lookup)

	w := issueAndRequest(t, handler(f), tokens,
		&agent_backend_entity.AgentBackend{ID: backendID, Type: string(agent_backend_entity.TypeCodex), LLMProviderKey: provider.ProviderKey},
		path,
		body,
	)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, path, rec.Path)
	assert.Equal(t, "Bearer "+testAPIKey(apiKeyName), rec.Header.Get("Authorization"))
}

func testAPIKey(name string) string {
	return strings.Join([]string{"k", name}, "-")
}

func newAnthropicProvider(key string, baseURL string) *llm_provider_entity.LLMProvider {
	return &llm_provider_entity.LLMProvider{
		ProviderKey: key, Type: string(llm_provider_entity.TypeAnthropic), Name: "a",
		Model: "claude-sonnet-4-6", APIKey: testAPIKey("anthropic"), BaseURL: baseURL,
		Status: consts.ACTIVE,
	}
}

func newOpenAIResponseProvider(key string, baseURL string) *llm_provider_entity.LLMProvider {
	return &llm_provider_entity.LLMProvider{
		ProviderKey: key, Type: string(llm_provider_entity.TypeOpenAIResponse), Name: "r",
		Model: "gpt-5-codex", APIKey: testAPIKey("resp"), BaseURL: baseURL,
		Status: consts.ACTIVE,
	}
}

func newOpenAIChatProvider(key string, baseURL string) *llm_provider_entity.LLMProvider {
	return &llm_provider_entity.LLMProvider{
		ProviderKey: key, Type: string(llm_provider_entity.TypeOpenAIChat), Name: "c",
		Model: "gpt-4o", APIKey: testAPIKey("chat"), BaseURL: baseURL,
		Status: consts.ACTIVE,
	}
}

// recordingUpstream 起一个 httptest server 抓所有进来的请求，便于断言 path / headers / body。
type recordedRequest struct {
	Path   string
	Method string
	Header http.Header
	Body   []byte
}

func newRecordingUpstream(t *testing.T, response string) (*httptest.Server, *recordedRequest) {
	t.Helper()
	rec := &recordedRequest{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.Path = r.URL.Path
		rec.Method = r.Method
		rec.Header = r.Header.Clone()
		rec.Body = body
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// issueAndRequest 帮测试发一条带 token 的请求到 forwarder handler。
func issueAndRequest(t *testing.T, h http.HandlerFunc, tokens *TokenRegistry, b *agent_backend_entity.AgentBackend, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	tok, err := tokens.Issue(b, time.Minute)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec
}

func TestForwarder_AnthropicHappyPath(t *testing.T) {
	upstream, rec := newRecordingUpstream(t, `{"id":"msg_x","content":[{"type":"text","text":"pong"}]}`)
	tokens := NewTokenRegistry()
	lookup := newFakeLookup(newAnthropicProvider("key-1", upstream.URL))
	f := NewForwarder(tokens, lookup)

	w := issueAndRequest(t, f.AnthropicHandler(), tokens,
		&agent_backend_entity.AgentBackend{ID: 5, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-1"},
		"/v1/messages",
		`{"model":"opus","messages":[{"role":"user","content":"hi"}]}`,
	)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "/v1/messages", rec.Path)
	assert.Equal(t, testAPIKey("anthropic"), rec.Header.Get("x-api-key"))
	assert.Empty(t, rec.Header.Get("Authorization"))

	var body map[string]any
	assert.NoError(t, json.Unmarshal(rec.Body, &body))
	assert.Equal(t, "claude-sonnet-4-6", body["model"]) // model 已改写
}

func TestForwarder_AliasRoutingPicksTierProvider(t *testing.T) {
	// 主 provider 走 fallback；OPUS alias 路由到另一条 provider，确保用上 tier model。
	mainUpstream, mainRec := newRecordingUpstream(t, `{"ok":"main"}`)
	opusUpstream, opusRec := newRecordingUpstream(t, `{"ok":"opus"}`)
	defer mainUpstream.Close()
	defer opusUpstream.Close()

	tokens := NewTokenRegistry()
	main := newAnthropicProvider("key-1", mainUpstream.URL)
	main.Model = "claude-sonnet-fallback"
	opus := newAnthropicProvider("key-2", opusUpstream.URL)
	opus.Model = "claude-opus-4-1"
	lookup := newFakeLookup(main, opus)
	f := NewForwarder(tokens, lookup)

	w := issueAndRequest(t, f.AnthropicHandler(), tokens,
		&agent_backend_entity.AgentBackend{
			ID: 5, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-1",
			ModelRoutes: `{"OPUS":"key-2"}`,
		},
		"/v1/messages",
		`{"model":"opus","messages":[]}`,
	)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, testAPIKey("anthropic"), opusRec.Header.Get("x-api-key"))
	var body map[string]any
	assert.NoError(t, json.Unmarshal(opusRec.Body, &body))
	assert.Equal(t, "claude-opus-4-1", body["model"])
	// 主 provider 不应该被调到
	assert.Empty(t, mainRec.Path)
}

func TestForwarder_OpenAIResponses(t *testing.T) {
	assertOpenAIForwarded(t,
		newOpenAIResponseProvider("key-1", ""),
		(*Forwarder).OpenAIResponsesHandler,
		6,
		"/v1/responses",
		`{"model":"gpt-5","input":"hi"}`,
		"resp",
	)
}

func TestForwarder_OpenAIChat(t *testing.T) {
	assertOpenAIForwarded(t,
		newOpenAIChatProvider("key-1", ""),
		(*Forwarder).OpenAIChatHandler,
		7,
		"/v1/chat/completions",
		`{"model":"gpt-4o","messages":[]}`,
		"chat",
	)
}

func TestForwarder_RejectsProviderTypeMismatch(t *testing.T) {
	upstream, _ := newRecordingUpstream(t, `{}`)
	tokens := NewTokenRegistry()
	// /v1/messages handler 但 provider 是 openai-chat → 400
	lookup := newFakeLookup(newOpenAIChatProvider("key-1", upstream.URL))
	f := NewForwarder(tokens, lookup)

	w := issueAndRequest(t, f.AnthropicHandler(), tokens,
		&agent_backend_entity.AgentBackend{ID: 1, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-1"},
		"/v1/messages",
		`{"model":"opus"}`,
	)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	var body map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	assert.Contains(t, body["error"], "type mismatch")
}

func TestForwarder_MissingTokenReturns401(t *testing.T) {
	tokens := NewTokenRegistry()
	lookup := newFakeLookup()
	f := NewForwarder(tokens, lookup)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	w := httptest.NewRecorder()
	f.AnthropicHandler()(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestForwarder_UnknownTokenReturns401(t *testing.T) {
	tokens := NewTokenRegistry()
	lookup := newFakeLookup()
	f := NewForwarder(tokens, lookup)

	req := httptest.NewRequest(http.MethodPost, "/v1/messages", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer nope")
	w := httptest.NewRecorder()
	f.AnthropicHandler()(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBuildTargetURL(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		path    string
		want    string
	}{
		{"plain host", "https://api.anthropic.com", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"trailing slash", "https://api.anthropic.com/", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"with /v1", "https://api.anthropic.com/v1", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"with /v1/ trailing", "https://api.anthropic.com/v1/", "/v1/messages", "https://api.anthropic.com/v1/messages"},
		{"openai responses", "https://api.openai.com/v1", "/v1/responses", "https://api.openai.com/v1/responses"},
		{"openai chat", "https://api.openai.com", "/v1/chat/completions", "https://api.openai.com/v1/chat/completions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := buildTargetURL(tc.baseURL, tc.path, llm_provider_entity.TypeAnthropic)
			assert.NoError(t, err)
			if assert.NotNil(t, u) {
				assert.Equal(t, tc.want, u.String())
			}
		})
	}
}

func TestBuildTargetURL_RejectsEmpty(t *testing.T) {
	_, err := buildTargetURL("", "/v1/messages", llm_provider_entity.TypeAnthropic)
	assert.Error(t, err)
}

func TestRewriteModelField(t *testing.T) {
	cases := []struct {
		name     string
		body     string
		model    string
		wantJSON string
	}{
		{"sets model", `{"model":"opus","messages":[]}`, "claude-sonnet-4-6", `{"messages":[],"model":"claude-sonnet-4-6"}`},
		{"empty body passthrough", "", "x", ""},
		{"empty newModel passthrough", `{"model":"foo"}`, "", `{"model":"foo"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := rewriteModelField([]byte(tc.body), tc.model)
			assert.NoError(t, err)
			// 字段顺序 unstable，用解析后比较；空字符串走原样断言
			if tc.wantJSON == "" {
				assert.Empty(t, string(out))
				return
			}
			if tc.model == "" || tc.body == "" {
				assert.Equal(t, tc.wantJSON, string(out))
				return
			}
			var got, want map[string]any
			assert.NoError(t, json.Unmarshal(out, &got))
			assert.NoError(t, json.Unmarshal([]byte(tc.wantJSON), &want))
			assert.Equal(t, want, got)
		})
	}
}

func TestExtractBearerOrAPIKey(t *testing.T) {
	cases := []struct {
		name string
		set  map[string]string
		want string
	}{
		{"bearer", map[string]string{"Authorization": "Bearer tok123"}, "tok123"},
		{"plain auth", map[string]string{"Authorization": "tok"}, "tok"},
		{"x-api-key", map[string]string{"X-Api-Key": "abc"}, "abc"},
		{"none", map[string]string{}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			for k, v := range tc.set {
				req.Header.Set(k, v)
			}
			assert.Equal(t, tc.want, extractBearerOrAPIKey(req))
		})
	}
}

// ensure url package import isn't dropped if test set shrinks
var _ = url.Parse
