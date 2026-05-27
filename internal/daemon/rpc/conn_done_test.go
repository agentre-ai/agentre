package rpc_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"agentre/internal/daemon/rpc"
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
