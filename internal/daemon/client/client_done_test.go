package client_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

func TestClient_Closed_OnClose(t *testing.T) {
	upgrader := websocket.Upgrader{Subprotocols: []string{rpc.Subprotocol}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, _ := upgrader.Upgrade(w, r, nil)
		<-r.Context().Done()
		_ = ws.Close()
	}))
	defer s.Close()
	u := "ws" + s.URL[len("http"):] + "/"
	c, err := client.Dial(t.Context(), client.Options{URL: u})
	require.NoError(t, err)
	select {
	case <-c.Closed():
		t.Fatal("Closed() fired before Close")
	default:
	}
	_ = c.Close()
	select {
	case <-c.Closed():
	case <-time.After(time.Second):
		t.Fatal("Closed() did not fire after Close")
	}
}
