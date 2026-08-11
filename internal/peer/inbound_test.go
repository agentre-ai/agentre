package peer_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/peer"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// Given a signed-in desktop App owns an inbound relay link, when the first
// physical connection drops, then it reconnects, accepts an account-authorized
// peer on the existing wire vocabulary, and disappears from the relay when the
// App lifetime ends.
func TestInbound_GivenRelayReconnectAndShutdown_WhenAuthorizedPeerCallsCapabilities_ThenItDispatchesAndUnregisters(t *testing.T) {
	var attempts atomic.Int32
	secondConnection := make(chan *websocket.Conn, 1)
	holdRelay := make(chan struct{})
	var releaseRelay sync.Once
	t.Cleanup(func() { releaseRelay.Do(func() { close(holdRelay) }) })
	upgrader := websocket.Upgrader{}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/relay/daemon" {
			t.Errorf("path = %q, want /v1/relay/daemon", r.URL.Path)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer desktop-token" {
			t.Errorf("Authorization = %q, want desktop bearer", got)
			return
		}
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		if attempts.Add(1) == 1 {
			_ = ws.Close()
			return
		}
		secondConnection <- ws
		<-holdRelay
		_ = ws.Close()
	}))
	t.Cleanup(relay.Close)

	link := rpc.NewHubLink(rpc.HubLinkOptions{
		ServerURL:         relay.URL,
		AccessToken:       "desktop-token",
		RetryInitial:      time.Millisecond,
		RetryMax:          time.Millisecond,
		RetryWait:         func(context.Context, time.Duration) error { return nil },
		Random:            func() float64 { return 1 },
		HeartbeatInterval: time.Hour,
	})
	inbound := peer.NewInbound(link)
	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- inbound.Run(ctx) }()
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			require.NoError(t, <-runDone)
		})
	}
	t.Cleanup(stop)

	var ws *websocket.Conn
	select {
	case ws = <-secondConnection:
	case <-time.After(time.Second):
		t.Fatal("desktop did not re-register after relay disconnect")
	}

	unauthenticated := relayRequest(t, ws, "desktop-peer", rpc.Frame{
		JSONRPC: "2.0", ID: json.RawMessage(`1`), Method: wire.MethodCapabilities,
		Params: mustJSON(t, wire.CapabilitiesParams{BackendType: "claudecode"}),
	})
	require.NotNil(t, unauthenticated.Error)
	assert.Equal(t, rpc.ErrUnauthorized.Code, unauthenticated.Error.Code)

	authenticated := relayRequest(t, ws, "desktop-peer", rpc.Frame{
		JSONRPC: "2.0", ID: json.RawMessage(`2`), Method: "auth.account",
		Params: mustJSON(t, rpc.AccountParams{Credential: "same-account-device-jwt", DeviceFingerprint: "sha256:peer"}),
	})
	require.Nil(t, authenticated.Error)

	capabilities := relayRequest(t, ws, "desktop-peer", rpc.Frame{
		JSONRPC: "2.0", ID: json.RawMessage(`3`), Method: wire.MethodCapabilities,
		Params: mustJSON(t, wire.CapabilitiesParams{BackendType: "claudecode"}),
	})
	require.Nil(t, capabilities.Error)
	assert.NotEmpty(t, capabilities.Result, "the existing runtime method must reach the desktop peer registry")

	stop()
	require.NoError(t, ws.SetReadDeadline(time.Now().Add(time.Second)))
	_, _, err := ws.ReadMessage()
	require.Error(t, err, "desktop relay registration remained connected after App shutdown")
	releaseRelay.Do(func() { close(holdRelay) })
}

func relayRequest(t *testing.T, conn *websocket.Conn, channelID string, request rpc.Frame) rpc.Frame {
	t.Helper()
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, relayEnvelope(channelID, mustJSON(t, request))))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(time.Second)))
	messageType, payload, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.BinaryMessage, messageType)
	responseID, responseJSON := unpackRelayEnvelope(t, payload)
	require.Equal(t, channelID, responseID)
	var response rpc.Frame
	require.NoError(t, json.Unmarshal(responseJSON, &response))
	return response
}

func relayEnvelope(channelID string, frame []byte) []byte {
	payload := make([]byte, 2+len(channelID)+len(frame))
	binary.BigEndian.PutUint16(payload, uint16(len(channelID)))
	copy(payload[2:], channelID)
	copy(payload[2+len(channelID):], frame)
	return payload
}

func unpackRelayEnvelope(t *testing.T, payload []byte) (string, []byte) {
	t.Helper()
	require.GreaterOrEqual(t, len(payload), 2)
	length := int(binary.BigEndian.Uint16(payload[:2]))
	require.GreaterOrEqual(t, len(payload), 2+length)
	return string(payload[2 : 2+length]), payload[2+length:]
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	require.NoError(t, err)
	return payload
}
