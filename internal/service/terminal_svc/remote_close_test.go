package terminal_svc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type remoteCloseClient struct {
	data       chan protocol.TerminalDataEvent
	exit       chan protocol.TerminalExitEvent
	closeCalls atomic.Int32
}

func (c *remoteCloseClient) Call(_ context.Context, method string, _ any, out any) error {
	switch method {
	case "terminal.open":
		*(out.(*protocol.TerminalOpenResult)) = protocol.TerminalOpenResult{TerminalID: "daemon-terminal-1"}
	case "terminal.close":
		c.closeCalls.Add(1)
	}
	return nil
}

func (c *remoteCloseClient) SubscribeData(string) <-chan protocol.TerminalDataEvent {
	return c.data
}

func (c *remoteCloseClient) SubscribeExit(string) <-chan protocol.TerminalExitEvent {
	return c.exit
}

func TestService_GivenRunningRemoteCommandWhenClosedThenEmitsKilledExitAndCleansLifecycle(t *testing.T) {
	client := &remoteCloseClient{
		data: make(chan protocol.TerminalDataEvent),
		exit: make(chan protocol.TerminalExitEvent),
	}
	remoteBackend := remote.NewBackend(client)
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(nil, func(deviceID string) (terminal_svc.PTYBackend, error) {
			assert.Equal(t, "device-1", deviceID)
			return remoteBackend, nil
		}),
		emitter,
	)

	require.NoError(t, svc.OpenCommand(
		context.Background(), "terminal-1", "device-1", "/remote", "go test ./...", 80, 24,
	))
	require.NoError(t, svc.Close(context.Background(), "terminal-1"))

	require.Eventually(t, func() bool {
		exitEvents := 0
		for _, event := range emitter.Snapshot() {
			if event.Name != terminal_svc.ExitEventName("terminal-1") {
				continue
			}
			exitEvents++
			exitEvent, ok := event.Payload.(protocol.TerminalExitEvent)
			if !ok || exitEvent.Reason != "killed" {
				return false
			}
		}
		return exitEvents == 1
	}, time.Second, time.Millisecond)
	assert.ErrorIs(t, svc.Write(context.Background(), "terminal-1", "x"), terminal_svc.ErrTerminalClosed)
	assert.Equal(t, int32(1), client.closeCalls.Load())
}
