package remote_test

import (
	"context"
	"testing"
	"time"

	"agentre/internal/pkg/pty"
	"agentre/internal/pkg/pty/remote"
	"agentre/pkg/agentred/protocol"

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

	fc.dataPush <- protocol.TerminalDataEvent{TerminalID: "remote-1", Data: "xyz"}

	select {
	case chunk := <-h.Data():
		require.Equal(t, []byte("xyz"), chunk)
	case <-time.After(time.Second):
		t.Fatal("did not receive data within 1s")
	}
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
