package remote

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

func TestHandleMCPProxy_DispatchesToRegisteredDispatcher(t *testing.T) {
	t.Cleanup(func() { RegisterMCPProxyDispatcher(nil) })

	var gotReq wire.MCPProxyRequest
	RegisterMCPProxyDispatcher(func(_ context.Context, req wire.MCPProxyRequest) (wire.MCPProxyResponse, error) {
		gotReq = req
		return wire.MCPProxyResponse{
			Status:  200,
			Headers: map[string][]string{"Content-Type": {"application/json"}},
			Body:    []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`),
		}, nil
	})

	r := &Runtime{}
	raw, err := json.Marshal(wire.MCPProxyRequest{
		Path:    "/mcp/org/",
		Method:  "POST",
		Headers: map[string][]string{"Authorization": {"Bearer tok"}},
		Body:    []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`),
	})
	require.NoError(t, err)

	out, err := r.handleMCPProxy(context.Background(), raw)
	require.NoError(t, err)
	resp, ok := out.(wire.MCPProxyResponse)
	require.True(t, ok)
	require.Equal(t, 200, resp.Status)
	require.Equal(t, []byte(`{"jsonrpc":"2.0","id":1,"result":{}}`), resp.Body)
	// 转发的请求保真:path / method / 鉴权头原样到 dispatcher。
	require.Equal(t, "/mcp/org/", gotReq.Path)
	require.Equal(t, "POST", gotReq.Method)
	require.Equal(t, []string{"Bearer tok"}, gotReq.Headers["Authorization"])
}

func TestHandleMCPProxy_NoDispatcherReturns502(t *testing.T) {
	t.Cleanup(func() { RegisterMCPProxyDispatcher(nil) })
	RegisterMCPProxyDispatcher(nil)

	r := &Runtime{}
	raw, err := json.Marshal(wire.MCPProxyRequest{Path: "/mcp/org/", Method: "POST"})
	require.NoError(t, err)

	out, err := r.handleMCPProxy(context.Background(), raw)
	require.NoError(t, err)
	resp, ok := out.(wire.MCPProxyResponse)
	require.True(t, ok)
	require.Equal(t, 502, resp.Status, "未注册 dispatcher 时回 502,让 CLI 看到错误而非 RPC 失败")
}

func TestNewLocalGatewayDispatcher_ReplaysAgainstBaseURL(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod, gotAuth = r.URL.Path, r.Method, r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"replayed":true}`))
	}))
	defer srv.Close()

	d := NewLocalGatewayDispatcher(func() string { return srv.URL }, srv.Client())
	resp, err := d(context.Background(), wire.MCPProxyRequest{
		Path:    "/mcp/org/",
		Method:  "POST",
		Headers: map[string][]string{"Authorization": {"Bearer tok"}},
		Body:    []byte(`{"q":1}`),
	})
	require.NoError(t, err)

	// desktop 本机 gateway 应答原样回传。
	require.Equal(t, 201, resp.Status)
	require.Equal(t, []byte(`{"replayed":true}`), resp.Body)
	require.Equal(t, "application/json", resp.Headers["Content-Type"][0])
	// 请求按 base+path 重放,method/鉴权头/body 保真。
	require.Equal(t, "/mcp/org/", gotPath)
	require.Equal(t, "POST", gotMethod)
	require.Equal(t, "Bearer tok", gotAuth)
	require.Equal(t, []byte(`{"q":1}`), gotBody)
}

func TestNewLocalGatewayDispatcher_EmptyBaseURLErrors(t *testing.T) {
	d := NewLocalGatewayDispatcher(func() string { return "" }, http.DefaultClient)
	_, err := d(context.Background(), wire.MCPProxyRequest{Path: "/mcp/org/", Method: "POST"})
	require.Error(t, err)
}
