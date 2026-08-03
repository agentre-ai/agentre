package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

func TestRewriteMCPServersForDaemon(t *testing.T) {
	specs := []agentruntime.MCPServerSpec{
		{
			Name:    "org",
			URL:     "http://127.0.0.1:52401/mcp/org/",
			Headers: map[string]string{"Authorization": "Bearer tok"},
			Tools:   []string{"org_get"},
		},
		{Name: "group", URL: "http://127.0.0.1:52401/mcp/group/", Tools: []string{"group_send"}},
	}

	out := rewriteMCPServersForDaemon(specs, func() string { return "http://127.0.0.1:7777" })

	// 只换 scheme+host,保留 path,使 CLI 打到 daemon 本地隧道。
	require.Equal(t, "http://127.0.0.1:7777/mcp/org/", out[0].URL)
	require.Equal(t, "http://127.0.0.1:7777/mcp/group/", out[1].URL)
	// desktop 签的 token(Headers)+ tools + name 原样保留(token 在 desktop 侧校验)。
	require.Equal(t, "Bearer tok", out[0].Headers["Authorization"])
	require.Equal(t, []string{"org_get"}, out[0].Tools)
	require.Equal(t, "org", out[0].Name)
	// 不就地改原 slice(desktop 侧若复用同一 spec 不被污染)。
	require.Equal(t, "http://127.0.0.1:52401/mcp/org/", specs[0].URL)

	// 空 base / 空 specs / nil baseFn:原样返回,不炸。
	require.Equal(t, specs, rewriteMCPServersForDaemon(specs, func() string { return "" }))
	require.Equal(t, specs, rewriteMCPServersForDaemon(specs, nil))
	require.Nil(t, rewriteMCPServersForDaemon(nil, func() string { return "http://127.0.0.1:7777" }))
}

// fakeTunnelNotifier 实现 NotifierPort:记录反向 Request 的 method/params,按预置应答回填 result。
type fakeTunnelNotifier struct {
	gotMethod string
	gotParams any
	resp      wire.MCPProxyResponse
	err       error
}

func (f *fakeTunnelNotifier) Notify(string, any) error { return nil }
func (f *fakeTunnelNotifier) Request(_ context.Context, method string, params, result any) error {
	f.gotMethod = method
	f.gotParams = params
	if f.err != nil {
		return f.err
	}
	if rp, ok := result.(*wire.MCPProxyResponse); ok {
		*rp = f.resp
	}
	return nil
}

func TestMCPTunnelHandler_ForwardsRequestAndWritesResponse(t *testing.T) {
	fn := &fakeTunnelNotifier{resp: wire.MCPProxyResponse{
		Status:  200,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    []byte(`{"ok":true}`),
	}}
	h := NewMCPTunnelHandler(func() NotifierPort { return fn })

	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777/mcp/org/", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer tok")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// desktop 应答原样写回 CLI。
	require.Equal(t, 200, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, `{"ok":true}`, rec.Body.String())

	// 经 MethodMCPProxy 反向请求转发,且请求保真(path/method/body/鉴权头)。
	require.Equal(t, wire.MethodMCPProxy, fn.gotMethod)
	fwd, ok := fn.gotParams.(wire.MCPProxyRequest)
	require.True(t, ok)
	require.Equal(t, "/mcp/org/", fwd.Path)
	require.Equal(t, http.MethodPost, fwd.Method)
	require.Equal(t, []byte(body), fwd.Body)
	require.Equal(t, []string{"Bearer tok"}, fwd.Headers["Authorization"])
	// 逐跳头 / Content-Length 不转发(desktop 重放时重算)。
	require.NotContains(t, fwd.Headers, "Content-Length")
}

// TestMCPTunnelHandler_NoActiveConn_ReturnsReadableToolError covers R17: when the
// tunnel has no live desktop connection to route to, the CLI subprocess's MCP client
// must get back something it can hand to the model as an ordinary tool-call error —
// not a bare HTTP 503 whose body a JSON-RPC/MCP client discards as a transport
// failure. The response must therefore be HTTP 200 carrying a JSON-RPC error object
// (the same shape org/subagent/hooktool's own writeRPCError uses for a real tool
// execution failure), correlated to the request's id, whose message names the
// unavailable capability, states the dependency on the originating client being
// online, and tells the model not to retry.
func TestMCPTunnelHandler_NoActiveConn_ReturnsReadableToolError(t *testing.T) {
	h := NewMCPTunnelHandler(func() NotifierPort { return nil })

	body := `{"jsonrpc":"2.0","id":42,"method":"tools/call","params":{"name":"org_get"}}`
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777/mcp/org/", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Not a bare transport failure: HTTP 200 + JSON-RPC error is the shape an
	// MCP-over-HTTP client reads as an ordinary (readable) tool-call error instead
	// of discarding the body because the status line said "infrastructure fault".
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))

	var resp struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "2.0", resp.JSONRPC)
	require.Equal(t, json.RawMessage("42"), resp.ID, "must echo the request id back so the MCP client correlates the response")
	require.Nil(t, resp.Result)
	require.NotNil(t, resp.Error, "must be a JSON-RPC error object, not a bare status-code failure")

	msg := resp.Error.Message
	require.Contains(t, msg, "org", "names which capability is unavailable")
	require.Contains(t, msg, "offline", "states the dependency: the originating client must be online")
	require.Contains(t, msg, "do not retry", "tells the model to proceed instead of retry-looping")
	require.NotContains(t, rec.Body.String(), "503", "must not leak the old bare-503 wording")
}

// TestMCPTunnelHandler_NoActiveConn_UnparsableBodyStillAnswers covers the edge case
// where the request body isn't valid JSON-RPC (or has no id): the handler must still
// answer with a well-formed JSON-RPC error (id degrades to null) instead of panicking
// or falling back to the old bare-503 behavior.
func TestMCPTunnelHandler_NoActiveConn_UnparsableBodyStillAnswers(t *testing.T) {
	h := NewMCPTunnelHandler(func() NotifierPort { return nil })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7777/mcp/org/", strings.NewReader("not json")))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		ID    json.RawMessage           `json:"id"`
		Error *struct{ Message string } `json:"error"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, json.RawMessage("null"), resp.ID)
	require.NotNil(t, resp.Error)
}
