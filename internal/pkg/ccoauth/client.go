package ccoauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 默认调真实 Anthropic endpoint;测试通过 NewClient(baseURL) 注入 httptest server。
const defaultBaseURL = "https://api.anthropic.com"

// betaHeader 是 Claude Code 内部 OAuth endpoint 要求的 anthropic-beta header 值。
// OMC 也使用同样值;若 Anthropic 升级到新 beta 标识,这里需同步。
const betaHeader = "oauth-2025-04-20"

// 错误分类供上层 cache 策略与前端 UI 区分:
//   - ErrAuthExpired: 401, 通常意味着 OAuth 凭证过期, 需要用户去对应机器 `claude /login`
//   - ErrRateLimited: 429, 配合指数退避; 上层可继续展示 stale 数据
//   - ErrNetwork: 其它 4xx/5xx / dial 失败 / 响应解析失败
var (
	ErrAuthExpired = errors.New("ccoauth: OAuth access token expired or invalid")
	ErrRateLimited = errors.New("ccoauth: rate limited by /api/oauth/usage")
	ErrNetwork     = errors.New("ccoauth: network or parse error")
)

// Client 调用 Anthropic 内部 OAuth usage endpoint。零依赖、可被 httptest server 替换。
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient 用给定 baseURL 构造;空串走 defaultBaseURL。Timeout 10s 与 OMC 对齐。
func NewClient(baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 10 * time.Second},
	}
}

// FetchUsage 调一次 GET /api/oauth/usage,带 Bearer accessToken。
// 错误统一为 ErrAuthExpired / ErrRateLimited / ErrNetwork(errors.Is 可用)。
func (c *Client) FetchUsage(ctx context.Context, accessToken string) (*RateLimits, error) {
	u, err := url.Parse(c.baseURL + "/api/oauth/usage")
	if err != nil {
		return nil, errors.Join(ErrNetwork, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, errors.Join(ErrNetwork, err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", betaHeader)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, errors.Join(ErrNetwork, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch resp.StatusCode {
	case http.StatusOK:
		body, rerr := io.ReadAll(resp.Body)
		if rerr != nil {
			return nil, errors.Join(ErrNetwork, rerr)
		}
		rl, perr := ParseUsageResponse(body)
		if perr != nil {
			return nil, errors.Join(ErrNetwork, perr)
		}
		return rl, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrAuthExpired
	case http.StatusTooManyRequests:
		return nil, ErrRateLimited
	default:
		return nil, ErrNetwork
	}
}
