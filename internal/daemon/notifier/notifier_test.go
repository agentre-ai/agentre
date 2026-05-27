package notifier

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"agentre/internal/daemon/rpc"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNotifier_NotifyForwardsToConn(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *rpc.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		require.NoError(t, err)
		c := rpc.NewConn(ws, rpc.NewRegistry())
		serverConnCh <- c
		c.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	clientWS, hsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if hsResp != nil {
		_ = hsResp.Body.Close()
	}
	t.Cleanup(func() { _ = clientWS.Close() })

	var serverConn *rpc.Conn
	select {
	case serverConn = <-serverConnCh:
	case <-time.After(time.Second):
		t.Fatal("server never accepted")
	}

	n := New(serverConn)
	require.NoError(t, n.Notify("chat.event", map[string]string{"hello": "world"}))

	var f rpc.Frame
	require.NoError(t, clientWS.ReadJSON(&f))
	assert.Equal(t, "chat.event", f.Method)
	assert.True(t, f.IsNotification())
	var got map[string]string
	require.NoError(t, json.Unmarshal(f.Params, &got))
	assert.Equal(t, "world", got["hello"])
}

func TestNotifier_RequestUsesConnCall(t *testing.T) {
	// Set up a WS pair where the CLIENT side handles "approval.request"
	// via its own registry; then server calls Request through Notifier.
	upgrader := websocket.Upgrader{}
	serverConnCh := make(chan *rpc.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, _ := upgrader.Upgrade(w, r, nil)
		c := rpc.NewConn(ws, rpc.NewRegistry())
		serverConnCh <- c
		c.Serve(context.Background())
	}))
	t.Cleanup(srv.Close)

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/"
	clientWS, hsResp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if hsResp != nil {
		_ = hsResp.Body.Close()
	}
	t.Cleanup(func() { _ = clientWS.Close() })

	clientReg := rpc.NewRegistry()
	clientReg.Register("approval.request", func(ctx context.Context, p json.RawMessage) (any, error) {
		return map[string]bool{"allow": true}, nil
	})
	clientConn := rpc.NewConn(clientWS, clientReg)
	go clientConn.Serve(context.Background())

	serverConn := <-serverConnCh
	n := New(serverConn)

	var resp map[string]bool
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, n.Request(ctx, "approval.request", map[string]string{"tool": "Bash"}, &resp))
	assert.True(t, resp["allow"])
}
