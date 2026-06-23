package remote_test

import (
	"context"
	"encoding/base64"
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
}

func (f *fakeClient) Call(_ context.Context, method string, params any, out any) error {
	switch method {
	case "terminal.open":
		f.openParams <- params.(protocol.TerminalOpenParams)
		*(out.(*protocol.TerminalOpenResult)) = protocol.TerminalOpenResult{TerminalID: "remote-1"}
	}
	return nil
}
func (f *fakeClient) SubscribeData(_ string) <-chan protocol.TerminalDataEvent {
	return f.dataPush
}
func (f *fakeClient) SubscribeExit(_ string) <-chan protocol.TerminalExitEvent {
	return f.exitPush
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

	select {
	case info := <-h.Exit():
		require.Equal(t, "natural", info.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit info")
	}
}

func TestRemoteBackend_ConnectionLost_EmitsConnectionLost(t *testing.T) {
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

	select {
	case info := <-h.Exit():
		require.Equal(t, "connection_lost", info.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit info")
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
