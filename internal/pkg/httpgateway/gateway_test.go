package httpgateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
)

func TestGateway_StartStopRoundTrip(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	assert.Equal(t, stateStopped, g.Status().State)

	assert.NoError(t, g.Start(context.Background()))
	st := g.Status()
	assert.Equal(t, stateRunning, st.State)
	assert.NotEmpty(t, st.URL)
	assert.Contains(t, st.Routes, RouteAnthropic)
	assert.Contains(t, st.Routes, RouteOpenAIResponses)

	assert.NoError(t, g.Stop(context.Background()))
	assert.Equal(t, stateStopped, g.Status().State)
}

func TestGateway_StartSoftFailWhenPortTaken(t *testing.T) {
	// 先占一个端口
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	g := New("127.0.0.1", port, newFakeLookup())
	assert.NoError(t, g.Start(context.Background()), "Start 不应返 error，只把 state 设 stopped")
	st := g.Status()
	assert.Equal(t, stateStopped, st.State)
	assert.NotEmpty(t, st.Reason)
	assert.Empty(t, st.URL)
}

func TestGateway_RestartSucceedsWithNewPort(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	assert.NoError(t, g.Start(context.Background()))
	oldURL := g.URL()

	g.ApplyAddr("127.0.0.1", 0) // 随机一个新端口
	assert.NoError(t, g.Restart(context.Background()))

	newURL := g.URL()
	assert.NotEmpty(t, newURL)
	assert.NotEqual(t, oldURL, newURL, "Restart 应该拿到不同端口")
	assert.Equal(t, stateRunning, g.Status().State)
	assert.NoError(t, g.Stop(context.Background()))
}

func TestGateway_RestartFailureKeepsOldListener(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	assert.NoError(t, g.Start(context.Background()))
	oldURL := g.URL()

	// 占用一个端口当作 Restart 目标
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	defer func() { _ = blocker.Close() }()
	blocked := blocker.Addr().(*net.TCPAddr).Port

	g.ApplyAddr("127.0.0.1", blocked)
	err = g.Restart(context.Background())
	assert.Error(t, err)
	// 旧 listener 仍然 running
	assert.Equal(t, stateRunning, g.Status().State)
	assert.Equal(t, oldURL, g.URL(), "Restart 失败时旧 URL 不变")

	assert.NoError(t, g.Stop(context.Background()))
}

func TestGateway_TokenLifecycle(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	// State=stopped 时 IssueToken 直接拒绝
	_, err := g.IssueToken(context.Background(),
		&agent_backend_entity.AgentBackend{ID: 1, LLMProviderKey: "key-1"}, time.Minute)
	assert.ErrorIs(t, err, ErrGatewayNotRunning)

	assert.NoError(t, g.Start(context.Background()))
	tok, err := g.IssueToken(context.Background(),
		&agent_backend_entity.AgentBackend{ID: 1, LLMProviderKey: "key-1"}, time.Minute)
	assert.NoError(t, err)
	assert.NotEmpty(t, tok)
	assert.Equal(t, 1, g.tokens.Size())

	g.RevokeToken(tok)
	assert.Equal(t, 0, g.tokens.Size())
	assert.NoError(t, g.Stop(context.Background()))
}

func TestGateway_BaseURL(t *testing.T) {
	// 未启动时 BaseURL 为空。
	g := New("127.0.0.1", 0, newFakeLookup())
	assert.Empty(t, g.BaseURL())

	// 启动后 BaseURL 非空且带 http:// 前缀。
	assert.NoError(t, g.Start(context.Background()))
	defer func() { _ = g.Stop(context.Background()) }()
	base := g.BaseURL()
	assert.NotEmpty(t, base)
	assert.True(t, strings.HasPrefix(base, "http://"), "BaseURL 应以 http:// 开头: %s", base)
}

func TestGateway_RegisterMCP(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	assert.NoError(t, g.Start(context.Background()))
	defer func() { _ = g.Stop(context.Background()) }()

	hit := make(chan string, 1)
	g.RegisterMCP("/mcp/echo/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit <- r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))

	// 命中
	resp, err := http.Get(g.URL() + "/mcp/echo/foo")
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	select {
	case got := <-hit:
		assert.Equal(t, "/mcp/echo/foo", got)
	case <-time.After(time.Second):
		t.Fatal("mcp handler 未被调用")
	}

	// 未注册 prefix 返 404
	resp, err = http.Get(g.URL() + "/mcp/unknown/x")
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestGateway_RegisterMCPIgnoresBadPrefix(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	g.RegisterMCP("/wrong/", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	assert.Equal(t, 0, len(g.mcps))
}

func TestGateway_ServesAnthropicRouteEndToEnd(t *testing.T) {
	upstream, rec := newRecordingUpstream(t, `{"id":"msg_x"}`)
	lookup := newFakeLookup(newAnthropicProvider("key-1", upstream.URL))

	g := New("127.0.0.1", 0, lookup)
	assert.NoError(t, g.Start(context.Background()))
	defer func() { _ = g.Stop(context.Background()) }()

	tok, err := g.IssueToken(context.Background(),
		&agent_backend_entity.AgentBackend{ID: 1, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-1"},
		time.Minute,
	)
	assert.NoError(t, err)

	req, _ := http.NewRequest(http.MethodPost, g.URL()+RouteAnthropic, strings.NewReader(`{"model":"opus","messages":[]}`))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	assert.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, RouteAnthropic, rec.Path)
	assert.Equal(t, testAPIKey("anthropic"), rec.Header.Get("x-api-key"))
}

// 确保 httptest import 没被无用警告掉
var _ = httptest.NewRecorder

func TestServeHookInbox(t *testing.T) {
	g := New("127.0.0.1", 0, newFakeLookup())
	backend := &agent_backend_entity.AgentBackend{ID: 1, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "key-99"}
	tok, err := g.tokens.Issue(backend, time.Minute)
	assert.NoError(t, err)

	g.Steer().Push("abc", "qid-1", "first")
	g.Steer().Push("abc", "qid-2", "second")

	t.Run("missing token → 401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox?session_id=abc", nil)
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("invalid token → 401", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox?session_id=abc", nil)
		req.Header.Set("Authorization", "Bearer notvalid")
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)
	})

	t.Run("non-GET → 405", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/hook/v1/inbox?session_id=abc", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
	})

	t.Run("missing session_id → 400", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusBadRequest, rr.Code)
	})

	t.Run("unknown sid → 200 empty", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox?session_id=unknown", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
		var got struct {
			Messages []string `json:"messages"`
		}
		assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		assert.Empty(t, got.Messages)
	})

	t.Run("happy path drains queue", func(t *testing.T) {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox?session_id=abc", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		g.serveHookInbox(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)
		var got struct {
			Messages []string `json:"messages"`
		}
		assert.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
		assert.Equal(t, []string{"first", "second"}, got.Messages)

		// Second call should be empty (queue drained).
		rr2 := httptest.NewRecorder()
		req2 := httptest.NewRequest(http.MethodGet, "/hook/v1/inbox?session_id=abc", nil)
		req2.Header.Set("Authorization", "Bearer "+tok)
		g.serveHookInbox(rr2, req2)
		var got2 struct {
			Messages []string `json:"messages"`
		}
		assert.NoError(t, json.Unmarshal(rr2.Body.Bytes(), &got2))
		assert.Empty(t, got2.Messages)
	})
}
