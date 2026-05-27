// Package httpgateway 暴露一个本地 HTTP 转发服务：把 claude / codex CLI 子进程的
// LLM 请求按 Bearer token 路由到目标 LLMProvider。token 仅在内存中维护，App 退出即失效；
// MCP 服务通过 RegisterMCP 在同一端口暴露。
//
// 设计要点见 docs/superpowers/specs/2026-05-15-claudecode-codex-backend-design.md。
package httpgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"agentre/internal/model/entity/agent_backend_entity"
)

// stateRunning / stateStopped Gateway 当前生命周期阶段。
const (
	stateRunning = "running"
	stateStopped = "stopped"
)

// shutdownTimeout 优雅关闭旧 listener 的等待上限。
const shutdownTimeout = 10 * time.Second

// Gateway 单例。NEW 后调用 Start；运行时通过 TokenIssuer / Lifecycle 接口被外部消费。
type Gateway struct {
	host string
	port int // 0 表示请求随机分配

	mu        sync.RWMutex
	listener  net.Listener
	server    *http.Server
	state     string
	reason    string
	actualURL string

	mux       *http.ServeMux
	tokens    *TokenRegistry
	forwarder *Forwarder
	steer     *SteerInbox

	mcpMu sync.RWMutex
	mcps  map[string]http.Handler
}

// New 构造 Gateway 但**不**绑端口。bootstrap 在 RunMigrations 后显式调用 Start。
//
// lookup 通常传 llm_provider_repo.LLMProvider() 的单例；测试可以 inject fake。
func New(host string, port int, lookup ProviderLookup) *Gateway {
	g := &Gateway{
		host:   host,
		port:   port,
		state:  stateStopped,
		tokens: NewTokenRegistry(),
		steer:  NewSteerInbox(),
		mcps:   make(map[string]http.Handler),
	}
	g.forwarder = NewForwarder(g.tokens, lookup)
	g.mux = g.buildMux()
	return g
}

// buildMux 把三条 LLM 路由、Steer 入箱路由与 /mcp/* 挂上。
func (g *Gateway) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc(RouteAnthropic, g.forwarder.AnthropicHandler())
	mux.HandleFunc(RouteOpenAIResponses, g.forwarder.OpenAIResponsesHandler())
	mux.HandleFunc(RouteOpenAIChat, g.forwarder.OpenAIChatHandler())
	mux.HandleFunc(RouteHookInbox, g.serveHookInbox)
	mux.HandleFunc(RouteMCPPrefix, g.serveMCP)
	return mux
}

// serveMCP 在 /mcp/<server>/* 上按最长前缀匹配 mcps map；没注册返 404。
func (g *Gateway) serveMCP(w http.ResponseWriter, r *http.Request) {
	g.mcpMu.RLock()
	defer g.mcpMu.RUnlock()
	if len(g.mcps) == 0 {
		http.NotFound(w, r)
		return
	}
	// 最长前缀匹配
	var (
		bestPrefix string
		best       http.Handler
	)
	for p, h := range g.mcps {
		if strings.HasPrefix(r.URL.Path, p) && len(p) > len(bestPrefix) {
			bestPrefix = p
			best = h
		}
	}
	if best == nil {
		http.NotFound(w, r)
		return
	}
	best.ServeHTTP(w, r)
}

// Start 实际 net.Listen 并起 http.Server。失败时不返 error：把 state 置 stopped、reason 填错误，
// App 整体继续运行（builtin 后端不需要 gateway）。
//
// ctx 仅用于决定 shutdown 时机；server 实际运行用自己的 goroutine。
func (g *Gateway) Start(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.startLocked(ctx)
}

func (g *Gateway) startLocked(_ context.Context) error {
	addr := net.JoinHostPort(g.host, strconv.Itoa(g.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		g.state = stateStopped
		g.reason = err.Error()
		g.actualURL = ""
		g.listener = nil
		g.server = nil
		return nil //nolint:nilerr // Start records bind failures in Status so the app can continue without the gateway.
	}
	srv := &http.Server{
		Handler:           g.mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
	g.listener = ln
	g.server = srv
	g.state = stateRunning
	g.reason = ""
	g.actualURL = "http://" + ln.Addr().String()

	go func() {
		_ = srv.Serve(ln)
	}()
	return nil
}

// Stop 优雅关闭当前 listener 与 server。Status 切到 stopped。
func (g *Gateway) Stop(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.stopLocked(ctx)
}

func (g *Gateway) stopLocked(ctx context.Context) error {
	if g.server == nil {
		g.state = stateStopped
		g.actualURL = ""
		return nil
	}
	shutCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()
	err := g.server.Shutdown(shutCtx)
	g.server = nil
	g.listener = nil
	g.state = stateStopped
	g.actualURL = ""
	return err
}

// Restart 先尝试用当前 host:port 起新 listener；成功后切换并优雅关闭旧 listener。
// 失败时保留旧 listener 继续运行，返 error。
func (g *Gateway) Restart(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	addr := net.JoinHostPort(g.host, strconv.Itoa(g.port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// 旧 listener 不动；返回原始错误供 svc 包装成 AppGatewayRestartFailed。
		return fmt.Errorf("bind %s: %w", addr, err)
	}

	srv := &http.Server{
		Handler:           g.mux,
		ReadHeaderTimeout: 30 * time.Second,
	}
	// 关旧
	if g.server != nil {
		shutCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
		_ = g.server.Shutdown(shutCtx)
		cancel()
	}
	g.listener = ln
	g.server = srv
	g.state = stateRunning
	g.reason = ""
	g.actualURL = "http://" + ln.Addr().String()

	go func() {
		_ = srv.Serve(ln)
	}()
	return nil
}

// ApplyAddr 在 Restart 之前更新目标 host / port。Restart 时会用这两个值绑端口。
// 端口已经在 app_settings_svc 校验过 [0,65535]；host 已经校验是合法 IP。
func (g *Gateway) ApplyAddr(host string, port int) {
	g.mu.Lock()
	g.host = host
	g.port = port
	g.mu.Unlock()
}

// Status 返回当前快照。
func (g *Gateway) Status() GatewayStatus {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var routes []string
	if g.state == stateRunning {
		routes = DefaultRoutes()
	}
	return GatewayStatus{
		State:  g.state,
		URL:    g.actualURL,
		Reason: g.reason,
		Routes: routes,
	}
}

// URL 当前 listener URL；State=stopped 时返回空。
func (g *Gateway) URL() string {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.actualURL
}

// Steer returns the in-process Steer message inbox. agentruntime pushes into
// it; the /hook/v1/inbox handler drains on hook GET.
func (g *Gateway) Steer() *SteerInbox { return g.steer }

// serveHookInbox is the GET /hook/v1/inbox?session_id=<uuid> handler that the
// agentre claudecode hook subcommand calls. Returns the pending Steer message
// queue for the given uuid (and clears it). Token-guarded by the same
// TokenRegistry that LLM forwarding uses, so any token issued for the
// active backend is sufficient.
func (g *Gateway) serveHookInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	tok := extractBearerOrAPIKey(r)
	if tok == "" {
		http.Error(w, "missing token", http.StatusUnauthorized)
		return
	}
	if _, ok := g.tokens.Resolve(tok); !ok {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		http.Error(w, "missing session_id", http.StatusBadRequest)
		return
	}
	items := g.steer.Drain(sid)
	// Hook protocol stays a plain `{"messages": [...]}` string array — the
	// queuedID is a chat_svc/runner internal concern; the claude CLI hook
	// only needs the text to inject as additionalContext.
	msgs := make([]string, 0, len(items))
	for _, it := range items {
		msgs = append(msgs, it.Text)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Messages []string `json:"messages"`
	}{Messages: msgs})
}

// RegisterMCP 注册 MCP 服务 prefix → handler。prefix 必须以 "/mcp/" 开头。
// 重复 prefix 会覆盖旧 handler；未注册任何 handler 时 /mcp/* 返回 404。
func (g *Gateway) RegisterMCP(prefix string, h http.Handler) {
	if !strings.HasPrefix(prefix, RouteMCPPrefix) {
		return
	}
	g.mcpMu.Lock()
	g.mcps[prefix] = h
	g.mcpMu.Unlock()
}

// IssueToken 给一个 backend 申请 token；ttl <= 0 视作永久（chat flow 用）。
// State=stopped 时返回 ErrGatewayNotRunning，调用方据此返回软失败给前端。
func (g *Gateway) IssueToken(_ context.Context, backend *agent_backend_entity.AgentBackend, ttl time.Duration) (string, error) {
	g.mu.RLock()
	running := g.state == stateRunning
	g.mu.RUnlock()
	if !running {
		return "", ErrGatewayNotRunning
	}
	return g.tokens.Issue(backend, ttl)
}

// RevokeToken 立刻删除 token。
func (g *Gateway) RevokeToken(token string) {
	g.tokens.Revoke(token)
}

// ErrGatewayNotRunning 在 gateway 未启动时 IssueToken / Prober.Run 看到的哨兵错误。
var ErrGatewayNotRunning = errors.New("httpgateway: not running")
