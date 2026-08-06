package rpc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHubLink_GivenRelayDropsAndRecovers_WhenRunning_ThenReconnectsRenewsAndExchangesFrames
// covers the daemon-owned part of R14/R20: a failed dial and a dropped outbound
// connection retry with backoff, the replacement sends pings that renew the server
// TTL, and the raw frame seam remains available to the future multiplexer.
func TestHubLink_GivenRelayDropsAndRecovers_WhenRunning_ThenReconnectsRenewsAndExchangesFrames(t *testing.T) {
	var attempts atomic.Int32
	connected := make(chan int, 2)
	pings := make(chan struct{}, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/relay/daemon", r.URL.Path)
		if got := r.Header.Get("Authorization"); got != "Bearer device-access-token" {
			t.Errorf("Authorization = %q, want device bearer token", got)
		}

		attempt := int(attempts.Add(1))
		if attempt == 1 {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = ws.Close() }()
		connected <- attempt
		if attempt == 2 {
			return // force a post-dial disconnect; the next connection must re-register.
		}

		ws.SetPingHandler(func(appData string) error {
			select {
			case pings <- struct{}{}:
			default:
			}
			return ws.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(time.Second))
		})
		for {
			messageType, payload, readErr := ws.ReadMessage()
			if readErr != nil {
				return
			}
			if messageType == websocket.BinaryMessage && string(payload) == "outbound" {
				if err := ws.WriteMessage(websocket.BinaryMessage, []byte("inbound")); err != nil {
					return
				}
			}
		}
	}))
	t.Cleanup(server.Close)

	retryDelays := make(chan time.Duration, 2)
	dialed := make(chan struct{}, 2)
	disconnected := make(chan error, 1)
	link := NewHubLink(HubLinkOptions{
		ServerURL:         server.URL,
		AccessToken:       "device-access-token",
		HeartbeatInterval: 10 * time.Millisecond,
		RetryInitial:      time.Second,
		RetryMax:          4 * time.Second,
		RetryWait: func(_ context.Context, delay time.Duration) error {
			retryDelays <- delay
			return nil // deterministic test clock: advance every retry immediately.
		},
		Random: func() float64 { return 1 },
		OnDial: func() {
			dialed <- struct{}{}
		},
		OnDisconnect: func(err error) {
			disconnected <- err
		},
	})
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- link.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-runDone)
	})

	require.Equal(t, 2, <-connected, "the first successful dial must be registered")
	require.Equal(t, 3, <-connected, "a dropped connection must be re-established")
	assert.Equal(t, time.Second, <-retryDelays, "failed dials begin at the configured backoff")
	assert.Equal(t, 2*time.Second, <-retryDelays, "an immediately dropped connection compounds the reconnect backoff")
	<-dialed
	<-dialed
	require.Error(t, <-disconnected, "the drop must be observable to the multiplexer seam")

	require.NoError(t, link.Send(websocket.BinaryMessage, []byte("outbound")))
	select {
	case frame := <-link.Receive():
		assert.Equal(t, websocket.BinaryMessage, frame.MessageType)
		assert.Equal(t, []byte("inbound"), frame.Payload)
	case <-time.After(time.Second):
		t.Fatal("relay response did not reach the hub frame seam")
	}
	select {
	case <-pings:
	case <-time.After(time.Second):
		t.Fatal("hub link did not send a heartbeat to renew the relay registration")
	}
}

// TestHubLink_GivenAccessTokenProvider_WhenRunning_ThenEachDialReResolvesTheToken
// pins the R4/R14 seam that lets the daemon own state while HubLink stays
// transport-only: the provider is consulted at every dial, so an access token
// that expires mid-connection is replaced by the fresh one on the next reconnect.
func TestHubLink_GivenAccessTokenProvider_WhenRunning_ThenEachDialReResolvesTheToken(t *testing.T) {
	var (
		mu          sync.Mutex
		authHeaders []string
	)
	upgrader := websocket.Upgrader{}
	conns := make(chan *websocket.Conn, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		mu.Unlock()

		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conns <- ws
		// Keep the connection open until the test (or the peer) closes it.
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)

	var token atomic.Value
	token.Store("token-1")
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	link := NewHubLink(HubLinkOptions{
		ServerURL:           server.URL,
		AccessToken:         "token-0", // stale static fallback; the provider must win
		AccessTokenProvider: func() string { return token.Load().(string) },
		HeartbeatInterval:   100 * time.Millisecond,
		RetryInitial:        time.Second,
		RetryMax:            4 * time.Second,
		RetryWait:           func(context.Context, time.Duration) error { return nil },
		Random:              func() float64 { return 1 },
	})
	runDone := make(chan error, 1)
	go func() { runDone <- link.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		require.NoError(t, <-runDone)
	})

	var first *websocket.Conn
	select {
	case first = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("first relay dial never happened")
	}
	mu.Lock()
	assert.Equal(t, "Bearer token-1", authHeaders[0], "the provider value is used for the first dial")
	mu.Unlock()

	// Rotate the token while the first connection is still up, then drop it:
	// the reconnect must re-resolve the provider and dial with the fresh token.
	token.Store("token-2")
	_ = first.Close()
	select {
	case <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("relay did not reconnect after the connection was dropped")
	}
	mu.Lock()
	assert.Equal(t, "Bearer token-2", authHeaders[1], "each dial must re-resolve the token provider")
	mu.Unlock()
}
