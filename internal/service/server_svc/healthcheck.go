package server_svc

import (
	"context"
	"net/http"
)

// HealthzResponse 与 hub /v1/healthz 的 data 字段对齐。
// 字段比 Hub 端实际响应略宽容（version 在 Hub 端 Foundation 中可能尚未填充，留空可接受）。
type HealthzResponse struct {
	Status  string `json:"status"`
	DBPing  bool   `json:"db_ping"`
	Redis   bool   `json:"redis"`
	Version string `json:"version"`
}

// Healthcheck 对 GET /v1/healthz 做一次握手，主要用于 StartLogin 前的 URL 校验。
// 返回 Hub 端报告的版本字符串（可能为空）。
func (c *serverClient) Healthcheck(ctx context.Context) (string, error) {
	var env envelope[HealthzResponse]
	status, err := c.do(ctx, http.MethodGet, "/v1/healthz", nil, &env)
	if err != nil {
		// healthcheck cares about reachability, not the specific HTTP shape:
		// any error (network failure, non-2xx, malformed body) means "the URL
		// the user pasted is not a healthy Hub."
		return "", ErrServerUnreachable
	}
	if status != 200 || env.Code != 0 {
		return "", ErrServerUnreachable
	}
	return env.Data.Version, nil
}
