package rpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// dialPair 起一个 httptest server,把 server / client 两端 *rpc.Conn 都返回。
func dialPair(t *testing.T) (server *rpc.Conn, client *rpc.Conn, cleanup func()) {
	t.Helper()
	upgrader := websocket.Upgrader{Subprotocols: []string{rpc.Subprotocol}}
	var srvConn *rpc.Conn
	ready := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		srvConn = rpc.NewConn(ws, rpc.NewRegistry())
		close(ready)
		<-r.Context().Done()
	}))
	u := "ws" + s.URL[len("http"):] + "/"
	d := *websocket.DefaultDialer
	d.Subprotocols = []string{rpc.Subprotocol}
	ws, resp, err := d.Dial(u, nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	<-ready
	client = rpc.NewConn(ws, rpc.NewRegistry())
	return srvConn, client, func() {
		_ = ws.Close()
		s.Close()
	}
}

func TestConn_Done_ClosedOnClose(t *testing.T) {
	_, c, cleanup := dialPair(t)
	defer cleanup()
	select {
	case <-c.Done():
		t.Fatal("Done() closed before Close()")
	default:
	}
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close within 1s after Close")
	}
}

func TestConn_Done_ClosedOnServeExit(t *testing.T) {
	_, c, cleanup := dialPair(t)
	defer cleanup()
	go c.Serve(t.Context())
	// abruptly close the underlying WS to make Serve exit
	_ = c.Close()
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done() did not close after Serve exited")
	}
}

// TestConn_Call_UnblocksOnConnClose 回归:对端永不回应 + 永不超时的 ctx 发 Call,
// 连接关闭时 Call 必须及时返回 ErrConnClosed,而不是挂到 ctx deadline。旧实现 select
// 只看 ch / ctx.Done,反向隧道(MCP tunnel)的 Call 携 CLI 的 ~285s HTTP ctx,WS 中途
// 断会挂那么久。
func TestConn_Call_UnblocksOnConnClose(t *testing.T) {
	// dialPair 的 server 端不跑 Serve → 永远不会回应。
	_, client, cleanup := dialPair(t)
	defer cleanup()

	errCh := make(chan error, 1)
	go func() { errCh <- client.Call(context.Background(), "never.responds", nil, nil) }()

	// 让 Call 写完帧、进入 select 等待,再断连接。
	time.Sleep(50 * time.Millisecond)
	_ = client.Close()

	select {
	case err := <-errCh:
		require.Error(t, err)
		require.ErrorIs(t, err, rpc.ErrConnClosed)
	case <-time.After(2 * time.Second):
		t.Fatal("Call 未在连接关闭后及时返回(反向隧道会挂到 CLI 超时)")
	}
}
