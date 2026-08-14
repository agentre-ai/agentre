package fakepeer

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/jsonrpc"
)

const (
	testDeviceFingerprint    = "sha256:e2e-desktop"
	testDeviceAuthValue      = "e2e-device-auth-value"
	testInstanceUUID         = "e2e-fake-peer-instance"
	testControlAuthorization = "e2e-control-auth-value"
)

func startTestServer(t *testing.T) *Server {
	t.Helper()
	server, err := Start(context.Background(), Options{
		DeviceFingerprint: testDeviceFingerprint,
		DeviceToken:       testDeviceAuthValue,
		InstanceUUID:      testInstanceUUID,
		ControlToken:      testControlAuthorization,
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, server.Close()) })
	return server
}

func authenticatedClient(t *testing.T, server *Server) *client.Client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cli, err := client.Dial(ctx, client.Options{URL: server.URL()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = cli.Close() })
	var auth rpc.ConnectResult
	require.NoError(t, cli.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint:         testDeviceFingerprint,
		DeviceToken:               testDeviceAuthValue,
		ExpectedDaemonFingerprint: rpc.DaemonFingerprint(testInstanceUUID),
	}, &auth))
	require.True(t, auth.OK)
	return cli
}

func TestServerGivenWatcherAndBusinessSocketsInEitherOrderSupportsExactRemoteProtocol(t *testing.T) {
	for _, watcherFirst := range []bool{true, false} {
		name := "business-first"
		if watcherFirst {
			name = "watcher-first"
		}
		t.Run(name, func(t *testing.T) {
			server := startTestServer(t)
			openWatcher := func() {
				watcher := authenticatedClient(t, server)
				var health struct {
					InstanceUUID string `json:"instanceUUID"`
				}
				require.NoError(t, watcher.Call(context.Background(), "health.ping", nil, &health))
				assert.Equal(t, testInstanceUUID, health.InstanceUUID)
			}
			openBusiness := func() {
				business := authenticatedClient(t, server)
				var caps wire.CapabilitiesResult
				require.NoError(t, business.Call(context.Background(), wire.MethodCapabilities,
					wire.CapabilitiesParams{BackendType: "claudecode"}, &caps))
				var sessions wire.SessionListResult
				require.NoError(t, business.Call(context.Background(), wire.MethodSessionList, struct{}{}, &sessions))
				assert.Empty(t, sessions.Sessions)

				events := make(chan agentruntime.Event, 4)
				done := make(chan wire.RunResultDoneFrame, 1)
				business.Handle(wire.NotifyEvent, func(_ context.Context, raw json.RawMessage) (any, error) {
					var frame wire.EventFrame
					require.NoError(t, json.Unmarshal(raw, &frame))
					event, err := agentruntime.UnmarshalEvent(frame.Event)
					require.NoError(t, err)
					events <- event
					return nil, nil
				})
				business.Handle(wire.NotifyRunResultDone, func(_ context.Context, raw json.RawMessage) (any, error) {
					var frame wire.RunResultDoneFrame
					require.NoError(t, json.Unmarshal(raw, &frame))
					done <- frame
					return nil, nil
				})
				var ack wire.RunAck
				require.NoError(t, business.Call(context.Background(), wire.MethodRun, wire.RunParams{
					Backend: json.RawMessage(`{"type":"claudecode"}`), SessionID: 42, UserText: "hello remote",
				}, &ack))
				assert.Equal(t, int64(42), ack.SessionID)
				assert.Equal(t, "e2e-remote-session-42", ack.ProviderSessionID)

				var text strings.Builder
				deadline := time.After(5 * time.Second)
				for {
					select {
					case event := <-events:
						if delta, ok := event.(agentruntime.TextDelta); ok {
							text.WriteString(delta.Text)
						}
					case terminal := <-done:
						assert.Equal(t, "e2e-remote-session-42", terminal.ProviderSessionID)
						assert.Equal(t, "remote-peer-model", terminal.Model)
						assert.Equal(t, "remote-peer-reply: hello remote", text.String())
						return
					case <-deadline:
						t.Fatal("timed out waiting for streamed remote terminal frame")
					}
				}
			}

			if watcherFirst {
				openWatcher()
				openBusiness()
			} else {
				openBusiness()
				openWatcher()
			}

			recorded, err := json.Marshal(server.Snapshot())
			require.NoError(t, err)
			assert.NotContains(t, string(recorded), testDeviceAuthValue)
			assert.Contains(t, string(recorded), `"deviceToken":"[REDACTED]"`)
		})
	}
}

func TestServerGivenMidStreamDisconnectOrProtocolFailureEndsThroughReconnectOrTerminalError(t *testing.T) {
	t.Run("disconnect exposes interrupted session to reconnect attach", func(t *testing.T) {
		server := startTestServer(t)
		server.SetNextRunFault(FaultDisconnect)
		business := authenticatedClient(t, server)
		firstEvent := make(chan struct{}, 1)
		business.Handle(wire.NotifyEvent, func(_ context.Context, _ json.RawMessage) (any, error) {
			firstEvent <- struct{}{}
			return nil, nil
		})
		var ack wire.RunAck
		require.NoError(t, business.Call(context.Background(), wire.MethodRun, wire.RunParams{
			Backend: json.RawMessage(`{"type":"claudecode"}`), SessionID: 77, UserText: "disconnect",
		}, &ack))
		select {
		case <-firstEvent:
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for pre-disconnect stream event")
		}
		select {
		case <-business.Closed():
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for fake peer disconnect")
		}

		reconnected := authenticatedClient(t, server)
		var sessions wire.SessionListResult
		require.NoError(t, reconnected.Call(context.Background(), wire.MethodSessionList, struct{}{}, &sessions))
		require.Len(t, sessions.Sessions, 1)
		assert.Equal(t, wire.SessionLifecycleInterrupted, sessions.Sessions[0].LifecycleState)
		var attached wire.SessionAttachResult
		err := reconnected.Call(context.Background(), wire.MethodSessionAttach,
			wire.SessionAttachParams{SessionID: 77}, &attached)
		var rpcErr *jsonrpc.Error
		require.ErrorAs(t, err, &rpcErr)
		assert.Equal(t, wire.ErrCodeNoActiveTurn, rpcErr.Code)
	})

	t.Run("protocol failure sends a terminal run result instead of leaving the run open", func(t *testing.T) {
		server := startTestServer(t)
		server.SetNextRunFault(FaultProtocol)
		business := authenticatedClient(t, server)
		done := make(chan wire.RunResultDoneFrame, 1)
		business.Handle(wire.NotifyRunResultDone, func(_ context.Context, raw json.RawMessage) (any, error) {
			var frame wire.RunResultDoneFrame
			require.NoError(t, json.Unmarshal(raw, &frame))
			done <- frame
			return nil, nil
		})
		var ack wire.RunAck
		require.NoError(t, business.Call(context.Background(), wire.MethodRun, wire.RunParams{
			Backend: json.RawMessage(`{"type":"claudecode"}`), SessionID: 88, UserText: "bad frame",
		}, &ack))
		select {
		case terminal := <-done:
			assert.Equal(t, "e2e remote protocol failure", terminal.StopErrMsg)
		case <-time.After(5 * time.Second):
			t.Fatal("protocol failure never reached a terminal run result")
		}
	})
}

func TestServerRequiresExactAgentredSubprotocol(t *testing.T) {
	server := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Dial(ctx, client.Options{URL: strings.Replace(server.URL(), "/rpc", "/rpc", 1)})
	require.NoError(t, err, "production client must negotiate the exact subprotocol")

	dialer := websocket.Dialer{Subprotocols: []string{"wrong-subprotocol"}}
	_, response, err := dialer.DialContext(ctx, server.URL(), nil)
	require.Error(t, err)
	if response != nil {
		assert.Equal(t, http.StatusBadRequest, response.StatusCode)
		_ = response.Body.Close()
	}
}

func TestServerRejectsWrongCredentialsAndNeverRecordsTheirSecret(t *testing.T) {
	server := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, err := client.Dial(ctx, client.Options{URL: server.URL()})
	require.NoError(t, err)
	defer func() { _ = cli.Close() }()
	err = cli.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint:         testDeviceFingerprint,
		DeviceToken:               "wrong-secret",
		ExpectedDaemonFingerprint: rpc.DaemonFingerprint(testInstanceUUID),
	}, nil)
	var rpcErr *jsonrpc.Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, jsonrpc.ErrUnauthorized.Code, rpcErr.Code)
	recorded, marshalErr := json.Marshal(server.Snapshot())
	require.NoError(t, marshalErr)
	assert.NotContains(t, string(recorded), "wrong-secret")
}
