package httpgateway

import (
	"context"
	"net/http"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
)

// GatewayStatus 是 gateway 对外暴露的状态。
//
// json tag 与前端 GatewayStatusResponse 一致；State=stopped 时 URL/Routes 为空、Reason 携
// 带原始 net.Listen 错误摘要。
type GatewayStatus struct {
	State  string   `json:"status"`    // "running" | "stopped"
	URL    string   `json:"listenURL"` // 形如 http://127.0.0.1:60080；stopped 时为空
	Reason string   `json:"reason"`    // stopped 时填错误摘要；running 时为空
	Routes []string `json:"routes"`    // 已挂载的路由列表
}

// 已挂载的路由路径常量。
const (
	RouteAnthropic       = "/v1/messages"
	RouteOpenAIResponses = "/v1/responses"
	RouteOpenAIChat      = "/v1/chat/completions"
	RouteMCPPrefix       = "/mcp/"
	RouteHookInbox       = "/hook/v1/inbox"
)

// DefaultRoutes 在 Status() State=running 时回显给前端。
func DefaultRoutes() []string {
	return []string{RouteAnthropic, RouteOpenAIResponses, RouteOpenAIChat, RouteHookInbox, RouteMCPPrefix + "*"}
}

// TokenIssuer 给 Prober / 未来 chat flow 用：发临时 token、撤销 token、读 URL。
type TokenIssuer interface {
	IssueToken(ctx context.Context, backend *agent_backend_entity.AgentBackend, ttl time.Duration) (token string, err error)
	RevokeToken(token string)
	URL() string
	Status() GatewayStatus
}

// Lifecycle 给 app_settings_svc 用：查状态、重启、注册 MCP handler。
type Lifecycle interface {
	Status() GatewayStatus
	Restart(ctx context.Context) error
	RegisterMCP(prefix string, h http.Handler)
}
