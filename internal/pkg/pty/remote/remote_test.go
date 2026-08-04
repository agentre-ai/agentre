package remote_test

import (
	"context"
	"encoding/base64"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/require"
)

type fakeClient struct {
	openParams chan protocol.TerminalOpenParams
	dataPush   chan protocol.TerminalDataEvent
	exitPush   chan protocol.TerminalExitEvent
	closeCalls atomic.Int32
}

func (f *fakeClient) Call(_ context.Context, method string, params any, out any) error {
	switch method {
	case "terminal.open":
		f.openParams <- params.(protocol.TerminalOpenParams)
		*(out.(*protocol.TerminalOpenResult)) = protocol.TerminalOpenResult{TerminalID: "remote-1"}
	case "terminal.close":
		f.closeCalls.Add(1)
	}
	return nil
}
func (f *fakeClient) SubscribeData(_ string) <-chan protocol.TerminalDataEvent {
	return f.dataPush
}
func (f *fakeClient) SubscribeExit(_ string) <-chan protocol.TerminalExitEvent {
	return f.exitPush
}

func requireTerminalOutcome(
	t *testing.T,
	h pty.Handle,
	wantReason string,
) pty.ExitInfo {
	t.Helper()

	var info pty.ExitInfo
	select {
	case got, ok := <-h.Exit():
		require.True(t, ok, "exit channel closed without an outcome")
		info = got
	case <-time.After(time.Second):
		t.Fatal("no terminal outcome within 1s")
	}
	require.Equal(t, wantReason, info.Reason)

	select {
	case _, ok := <-h.Exit():
		require.False(t, ok, "exit channel must close after exactly one outcome")
	case <-time.After(time.Second):
		t.Fatal("exit channel did not close within 1s")
	}
	select {
	case _, ok := <-h.Data():
		require.False(t, ok, "data channel must close on terminal outcome")
	case <-time.After(time.Second):
		t.Fatal("data channel did not close within 1s")
	}
	return info
}

func TestRemoteBackend_Open_RPC_RoundTrip(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent, 1),
	}
	be := remote.NewBackend(fc)

	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r", Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	op := <-fc.openParams
	require.Equal(t, "/r", op.Cwd)
	require.Equal(t, uint16(80), op.Cols)

	// The daemon ships terminal data base64-encoded; the backend decodes it.
	fc.dataPush <- protocol.TerminalDataEvent{
		TerminalID: "remote-1", Data: base64.StdEncoding.EncodeToString([]byte("xyz")),
	}

	select {
	case chunk := <-h.Data():
		require.Equal(t, []byte("xyz"), chunk)
	case <-time.After(time.Second):
		t.Fatal("did not receive data within 1s")
	}
}

// TestRemoteBackend_Data_Base64DecodedAcrossSplit is the desktop-side regression
// for the garbled-terminal bug: the daemon base64-encodes each PTY chunk, so the
// backend must base64-decode it back to raw bytes. A multibyte char '─'
// (E2 94 80) split across two daemon pushes must reassemble exactly — the old
// []byte(ev.Data) reinterpreted the base64 text itself as bytes.
func TestRemoteBackend_Data_Base64DecodedAcrossSplit(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 2),
		exitPush:   make(chan protocol.TerminalExitEvent, 1),
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })
	<-fc.openParams

	full := []byte("─") // E2 94 80
	fc.dataPush <- protocol.TerminalDataEvent{
		TerminalID: "remote-1", Data: base64.StdEncoding.EncodeToString(full[:1]),
	}
	fc.dataPush <- protocol.TerminalDataEvent{
		TerminalID: "remote-1", Data: base64.StdEncoding.EncodeToString(full[1:]),
	}

	var got []byte
	for len(got) < len(full) {
		select {
		case chunk := <-h.Data():
			got = append(got, chunk...)
		case <-time.After(time.Second):
			t.Fatalf("did not receive full data within 1s; got %x", got)
		}
	}
	require.Equal(t, full, got, "split multibyte char must reassemble from base64 daemon pushes")
}

func TestRemoteBackend_GivenOpenSubscriptionsWhenClosedThenEmitsKilledAndClosesChannels(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent),
		exitPush:   make(chan protocol.TerminalExitEvent),
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	require.NoError(t, h.Close())
	require.NoError(t, h.Close(), "Close must remain idempotent")
	requireTerminalOutcome(t, h, "killed")
	require.Equal(t, int32(1), fc.closeCalls.Load(), "terminal.close RPC must be sent exactly once")
}

func TestRemoteBackend_ExitEvent_DeliveredAndChannelsClose(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent, 1),
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	fc.exitPush <- protocol.TerminalExitEvent{TerminalID: "remote-1", Code: 0, Reason: "natural"}
	close(fc.exitPush)
	close(fc.dataPush)

	requireTerminalOutcome(t, h, "natural")
}

func TestRemoteBackend_ExitSubscriptionLost_EmitsConnectionLostAndClosesChannels(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent, 1),
		exitPush:   make(chan protocol.TerminalExitEvent), // unbuffered + closed = connection lost
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	close(fc.exitPush)

	requireTerminalOutcome(t, h, "connection_lost")
}

func TestRemoteBackend_DataSubscriptionLost_EmitsConnectionLostAndClosesChannels(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent),
		exitPush:   make(chan protocol.TerminalExitEvent),
	}
	be := remote.NewBackend(fc)
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	close(fc.dataPush)

	requireTerminalOutcome(t, h, "connection_lost")
}

func TestRemoteBackend_GivenCloseRacingDaemonExitThenPublishesOneAuthoritativeOutcome(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		fc := &fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent),
			exitPush:   make(chan protocol.TerminalExitEvent, 1),
		}
		be := remote.NewBackend(fc)
		h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
		require.NoError(t, err)
		<-fc.openParams

		start := make(chan struct{})
		closeResults := make(chan error, 2)
		daemonSent := make(chan struct{})
		for range 2 {
			go func() {
				<-start
				closeResults <- h.Close()
			}()
		}
		go func() {
			<-start
			fc.exitPush <- protocol.TerminalExitEvent{
				TerminalID: "remote-1",
				Code:       0,
				Reason:     "natural",
			}
			close(daemonSent)
		}()
		close(start)

		require.NoError(t, <-closeResults)
		require.NoError(t, <-closeResults)
		<-daemonSent
		var info pty.ExitInfo
		select {
		case got, ok := <-h.Exit():
			require.Truef(t, ok, "iteration %d: exit channel closed without outcome", i)
			info = got
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: no terminal outcome", i)
		}
		require.Contains(t, []string{"killed", "natural"}, info.Reason)
		select {
		case _, ok := <-h.Exit():
			require.Falsef(t, ok, "iteration %d: more than one terminal outcome", i)
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: exit channel did not close", i)
		}
		select {
		case _, ok := <-h.Data():
			require.Falsef(t, ok, "iteration %d: data channel did not close", i)
		case <-time.After(time.Second):
			t.Fatalf("iteration %d: data channel did not close", i)
		}
		require.NoError(t, h.Close())
		require.Equalf(t, int32(1), fc.closeCalls.Load(),
			"iteration %d: terminal.close RPC count", i)
	}
}

type slowClient struct {
	delay time.Duration
	fakeClient
}

func (s *slowClient) Call(ctx context.Context, method string, params any, out any) error {
	if method == "terminal.open" {
		select {
		case <-time.After(s.delay):
			return s.fakeClient.Call(ctx, method, params, out)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.fakeClient.Call(ctx, method, params, out)
}

func TestRemoteBackend_Open_TimesOutAfter5s(t *testing.T) {
	fc := &slowClient{
		delay: 10 * time.Second, // much longer than the 5s timeout
		fakeClient: fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent, 1),
			exitPush:   make(chan protocol.TerminalExitEvent, 1),
		},
	}
	be := remote.NewBackend(fc)
	start := time.Now()
	_, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	elapsed := time.Since(start)
	require.ErrorIs(t, err, remote.ErrDaemonTimeout)
	// Should time out around 5s, not wait for 10s
	require.Less(t, elapsed, 7*time.Second, "should time out near 5s, not wait full delay")
}
