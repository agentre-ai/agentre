package rpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	daemonclient "github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	ptyremote "github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

type rpcTerminalBackendFunc func(context.Context, pty.Spec) (handlers.PTYHandle, error)

func (f rpcTerminalBackendFunc) Open(ctx context.Context, spec pty.Spec) (handlers.PTYHandle, error) {
	return f(ctx, spec)
}

type rpcTrackedTerminalHandle struct {
	data chan []byte
	exit chan pty.ExitInfo

	completeOnce sync.Once
	closeCalls   atomic.Int32
	dataCalls    atomic.Int32
	exitCalls    atomic.Int32
}

func newRPCFastTerminalHandle(data []byte, exit pty.ExitInfo) *rpcTrackedTerminalHandle {
	h := &rpcTrackedTerminalHandle{
		data: make(chan []byte, 1),
		exit: make(chan pty.ExitInfo, 1),
	}
	h.data <- data
	h.complete(exit)
	return h
}

func newRPCLateTerminalHandle() *rpcTrackedTerminalHandle {
	return &rpcTrackedTerminalHandle{
		data: make(chan []byte),
		exit: make(chan pty.ExitInfo, 1),
	}
}

func (h *rpcTrackedTerminalHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *rpcTrackedTerminalHandle) Resize(uint16, uint16) error { return nil }
func (h *rpcTrackedTerminalHandle) Data() <-chan []byte {
	h.dataCalls.Add(1)
	return h.data
}
func (h *rpcTrackedTerminalHandle) Exit() <-chan pty.ExitInfo {
	h.exitCalls.Add(1)
	return h.exit
}
func (h *rpcTrackedTerminalHandle) Close() error {
	h.closeCalls.Add(1)
	h.complete(pty.ExitInfo{Code: 137, Reason: "killed"})
	return nil
}

func (h *rpcTrackedTerminalHandle) complete(exit pty.ExitInfo) {
	h.completeOnce.Do(func() {
		h.exit <- exit
		close(h.exit)
		close(h.data)
	})
}

type terminalWebSocketHarness struct {
	server *httptest.Server
	errors chan error
}

func startTerminalWebSocketHarness(
	t *testing.T,
	backend handlers.PTYBackend,
	onNotify func(string),
	beforeOpenResponse func(),
) (*terminalWebSocketHarness, string) {
	t.Helper()
	registry := rpc.NewRegistry()
	errorsCh := make(chan error, 8)
	terminalHandlers := handlers.NewTerminalHandlers(backend, handlers.EmitterFunc(
		func(ctx context.Context, name string, payload any) {
			conn := rpc.ConnFromContext(ctx)
			if conn == nil {
				errorsCh <- errors.New("terminal emitter missing rpc.Conn")
				return
			}
			if err := conn.Notify(name, payload); err != nil {
				errorsCh <- err
				return
			}
			if onNotify != nil {
				onNotify(name)
			}
		},
	))
	registry.Register("terminal.open", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params protocol.TerminalOpenParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		result, err := terminalHandlers.Open(ctx, params)
		if err == nil && beforeOpenResponse != nil {
			beforeOpenResponse()
		}
		return result, err
	})
	registry.Register("terminal.close", func(ctx context.Context, raw json.RawMessage) (any, error) {
		var params protocol.TerminalCloseParams
		if err := json.Unmarshal(raw, &params); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		return terminalHandlers.Close(ctx, params)
	})

	upgrader := websocket.Upgrader{Subprotocols: []string{rpc.Subprotocol}}
	harness := &terminalWebSocketHarness{errors: errorsCh}
	harness.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		ws, err := upgrader.Upgrade(w, req, nil)
		if err != nil {
			errorsCh <- err
			return
		}
		conn := rpc.NewConn(ws, registry)
		conn.Serve(context.Background())
		terminalHandlers.CloseAll()
	}))
	t.Cleanup(func() {
		terminalHandlers.CloseAll()
		harness.server.Close()
	})
	return harness, "ws" + strings.TrimPrefix(harness.server.URL, "http") + "/"
}

func (h *terminalWebSocketHarness) requireNoError(t *testing.T) {
	t.Helper()
	select {
	case err := <-h.errors:
		require.NoError(t, err)
	default:
	}
}

func dialTerminalRPCClient(t *testing.T, url string) *daemonclient.Client {
	t.Helper()
	client, err := daemonclient.Dial(t.Context(), daemonclient.Options{URL: url})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestRPCTrackedTerminalHandle_GivenNaturalCompletionWhenCleanupClosesThenPreservesOutcomeWithoutPanic(t *testing.T) {
	fastHandle := newRPCFastTerminalHandle([]byte("done"), pty.ExitInfo{Code: 0, Reason: "natural"})

	require.NotPanics(t, func() {
		require.NoError(t, fastHandle.Close())
	})
	require.Equal(t, int32(1), fastHandle.closeCalls.Load())
	require.Equal(t, []byte("done"), <-fastHandle.Data())
	outcome, ok := <-fastHandle.Exit()
	require.True(t, ok)
	require.Equal(t, pty.ExitInfo{Code: 0, Reason: "natural"}, outcome)
}

func TestTerminalProtocolRealWebSocket_GivenDataAndExitSentBeforeOpenResponseWhenDesktopPreSubscribesThenCapturesBoth(t *testing.T) {
	fastHandle := newRPCFastTerminalHandle([]byte("response-race-output"), pty.ExitInfo{
		Code: 0, Reason: "natural",
	})
	exitNotified := make(chan struct{})
	var exitOnce sync.Once
	var notificationTimeout atomic.Bool
	harness, url := startTerminalWebSocketHarness(t,
		rpcTerminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
			return fastHandle, nil
		}),
		func(name string) {
			if name == handlers.EventNameTerminalExit {
				exitOnce.Do(func() { close(exitNotified) })
			}
		},
		func() {
			select {
			case <-exitNotified:
			case <-time.After(time.Second):
				notificationTimeout.Store(true)
			}
		},
	)

	client := dialTerminalRPCClient(t, url)
	adapter := ptyremote.NewClientAdapter(client)

	handle, err := ptyremote.NewBackend(adapter).Open(context.Background(), pty.Spec{
		TerminalID: "rpc-fast-terminal-1",
		Cwd:        "/repo",
		Cols:       80,
		Rows:       24,
	})
	require.NoError(t, err)
	select {
	case data := <-handle.Data():
		require.Equal(t, []byte("response-race-output"), data)
	case <-time.After(time.Second):
		t.Fatal("response-before-subscription output was lost over the real websocket")
	}
	select {
	case outcome, ok := <-handle.Exit():
		require.True(t, ok)
		require.Equal(t, "natural", outcome.Reason)
	case <-time.After(time.Second):
		t.Fatal("response-before-subscription exit was lost over the real websocket")
	}
	require.False(t, notificationTimeout.Load(), "server did not send terminal exit before terminal.open response")
	harness.requireNoError(t)
}

func TestTerminalProtocolRealWebSocket_GivenDaemonOpenIgnoresCancellationWhenDesktopStopsThenLateHandleIsClosedWithoutPump(t *testing.T) {
	started := make(chan context.Context, 1)
	releaseBackend := make(chan struct{})
	var releaseOnce sync.Once
	lateHandle := newRPCLateTerminalHandle()
	harness, url := startTerminalWebSocketHarness(t,
		rpcTerminalBackendFunc(func(ctx context.Context, _ pty.Spec) (handlers.PTYHandle, error) {
			started <- ctx
			<-releaseBackend
			return lateHandle, nil
		}),
		nil,
		nil,
	)
	t.Cleanup(func() { releaseOnce.Do(func() { close(releaseBackend) }) })
	client := dialTerminalRPCClient(t, url)
	adapter := ptyremote.NewClientAdapter(client)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		_, err := ptyremote.NewBackend(adapter).Open(ctx, pty.Spec{
			TerminalID: "rpc-stop-terminal-1",
			Cwd:        "/repo",
			Cols:       80,
			Rows:       24,
		})
		result <- err
	}()
	openCtx := <-started
	cancel()

	require.Equal(t, context.Canceled, <-result)
	select {
	case <-openCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("desktop Stop returned before pending-open cancellation reached daemon")
	}
	releaseOnce.Do(func() { close(releaseBackend) })
	require.Eventually(t, func() bool {
		return lateHandle.closeCalls.Load() == 1
	}, time.Second, time.Millisecond, "daemon did not close the cancellation-ignoring late handle")
	require.Zero(t, lateHandle.dataCalls.Load(), "late handle must not start a daemon pump")
	require.Zero(t, lateHandle.exitCalls.Load(), "late handle must not start a daemon pump")
	harness.requireNoError(t)
}
