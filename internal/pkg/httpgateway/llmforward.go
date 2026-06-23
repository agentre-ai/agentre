package httpgateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
)

// ProviderLookup 抽象 llm_provider 仓储依赖，方便单测注入 mock。
type ProviderLookup interface {
	FindByKey(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error)
}

// Forwarder 单实例承担三条路由的 HTTP 转发；类型在 mux 装配阶段绑死，避免每次请求重判。
type Forwarder struct {
	tokens *TokenRegistry
	lookup ProviderLookup
}

// NewForwarder 构造转发器。
func NewForwarder(tokens *TokenRegistry, lookup ProviderLookup) *Forwarder {
	return &Forwarder{tokens: tokens, lookup: lookup}
}

// Tokens 返回转发器持有的 token registry。
func (f *Forwarder) Tokens() *TokenRegistry { return f.tokens }

// AnthropicHandler /v1/messages → 严格匹配 type=anthropic。
func (f *Forwarder) AnthropicHandler() http.HandlerFunc {
	return f.handle(llm_provider_entity.TypeAnthropic)
}

// OpenAIResponsesHandler /v1/responses → 严格匹配 type=openai-response（codex 默认）。
func (f *Forwarder) OpenAIResponsesHandler() http.HandlerFunc {
	return f.handle(llm_provider_entity.TypeOpenAIResponse)
}

// OpenAIChatHandler /v1/chat/completions → 严格匹配 type=openai-chat（codex wire_api=chat）。
func (f *Forwarder) OpenAIChatHandler() http.HandlerFunc {
	return f.handle(llm_provider_entity.TypeOpenAIChat)
}

// handle 是统一转发逻辑：鉴权 → 模型路由 → 严格匹配 provider type → body 改写 →
// httputil.ReverseProxy 透传上游。
func (f *Forwarder) handle(expected llm_provider_entity.ProviderType) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := extractBearerOrAPIKey(r)
		if token == "" {
			writeJSONError(w, http.StatusUnauthorized, "missing token")
			return
		}
		entry, ok := f.tokens.Resolve(token)
		if !ok {
			writeJSONError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		// 读 body 一次。SSE 请求体很小，全量读入内存可接受。
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "read body: "+err.Error())
			return
		}

		modelField := extractModelField(body)
		providerKey, _ := entry.ResolveModel(modelField)

		provider, err := f.lookup.FindByKey(r.Context(), providerKey)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "lookup provider: "+err.Error())
			return
		}
		if provider == nil || !provider.IsActive() {
			writeJSONError(w, http.StatusBadGateway, "provider missing or inactive")
			return
		}
		if llm_provider_entity.ProviderType(provider.Type) != expected {
			writeJSONError(w, http.StatusBadRequest, "provider type mismatch")
			return
		}

		rewritten, err := rewriteModelField(body, provider.Model)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "rewrite model: "+err.Error())
			return
		}

		target, err := buildTargetURL(provider.BaseURL, r.URL.Path, expected)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "build upstream URL: "+err.Error())
			return
		}

		proxy := &httputil.ReverseProxy{
			FlushInterval: -1, // SSE 立刻 flush
			Rewrite: func(pr *httputil.ProxyRequest) {
				pr.SetURL(target)
				pr.Out.URL.Path = target.Path
				pr.Out.URL.RawQuery = r.URL.RawQuery
				pr.Out.Host = target.Host
				// 去掉子进程带来的认证头，按上游协议补正确的。
				pr.Out.Header.Del("Authorization")
				pr.Out.Header.Del("X-Api-Key")
				pr.Out.Header.Del("x-api-key")
				applyUpstreamAuth(pr.Out.Header, expected, provider.APIKey)
				pr.Out.Header.Set("Content-Type", "application/json")
				pr.Out.Body = io.NopCloser(bytes.NewReader(rewritten))
				pr.Out.ContentLength = int64(len(rewritten))
			},
		}
		proxy.ServeHTTP(w, r)
	}
}

// extractBearerOrAPIKey 兼容 OpenAI（Authorization: Bearer xxx）与 Anthropic（x-api-key: xxx）两种鉴权头。
func extractBearerOrAPIKey(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("Authorization")); v != "" {
		const prefix = "Bearer "
		if strings.HasPrefix(v, prefix) {
			return strings.TrimSpace(v[len(prefix):])
		}
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("x-api-key")); v != "" {
		return v
	}
	if v := strings.TrimSpace(r.Header.Get("X-Api-Key")); v != "" {
		return v
	}
	return ""
}

// applyUpstreamAuth 按目标 provider 类型补正确的鉴权头。
func applyUpstreamAuth(h http.Header, t llm_provider_entity.ProviderType, key string) {
	switch t {
	case llm_provider_entity.TypeAnthropic:
		h.Set("x-api-key", key)
		// 保留 client 端发的 anthropic-version 头（如有）。
		if h.Get("anthropic-version") == "" {
			h.Set("anthropic-version", "2023-06-01")
		}
	default:
		h.Set("Authorization", "Bearer "+key)
	}
}

// extractModelField 从 body 里抓 "model" 字段；解析失败或缺字段返空串（→ 走主 provider）。
func extractModelField(body []byte) string {
	if len(body) == 0 {
		return ""
	}
	var probe struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return ""
	}
	return probe.Model
}

// rewriteModelField 把 body 里的 "model" 字段改写成目标 provider 的真实 model id。
// 空 newModel 时不改 body。其它字段全部保留。
func rewriteModelField(body []byte, newModel string) ([]byte, error) {
	if len(body) == 0 || strings.TrimSpace(newModel) == "" {
		return body, nil
	}
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		// body 不是 JSON 对象，原样转发。
		return body, nil //nolint:nilerr
	}
	obj["model"] = newModel
	return json.Marshal(obj)
}

// buildTargetURL 根据 provider.BaseURL + 请求路径拼上游 URL。
//
// BaseURL 形态都接受：
//   - "https://api.anthropic.com"          → + "/v1/messages" → "https://api.anthropic.com/v1/messages"
//   - "https://api.anthropic.com/v1"       → 同上
//   - "https://api.anthropic.com/v1/"      → 同上
//
// 类型兜底：openai-chat 走 chat/completions，openai-response 走 responses；
// anthropic 仅识别 /v1/messages；若 BaseURL 已带 /v1 后缀会被剥掉再拼，避免重复。
func buildTargetURL(baseURL, path string, _ llm_provider_entity.ProviderType) (*url.URL, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		return nil, errors.New("provider base url is empty")
	}
	base = strings.TrimRight(base, "/")
	base = strings.TrimSuffix(base, "/v1")
	full := base + path
	u, err := url.Parse(full)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, errors.New("invalid upstream URL: " + full)
	}
	return u, nil
}

// writeJSONError 输出 `{"error":"..."}` 的 JSON 错误响应。
func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
