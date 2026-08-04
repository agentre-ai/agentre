package remote_test

import (
	"context"
	"encoding/base64"
	"errors"
	"net"
	"sync"
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
	openErr    error
	closeCalls atomic.Int32
}

func (f *fakeClient) Call(_ context.Context, method string, params any, out any) error {
	switch method {
	case "terminal.open":
		f.openParams <- params.(protocol.TerminalOpenParams)
		if f.openErr != nil {
			return f.openErr
		}
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

type scriptedCloseClient struct {
	fakeClient
	closeCalls   atomic.Int32
	closeStarted chan struct{}
	releaseClose <-chan struct{}
	started      sync.Once
	closeResults []error
}

func (c *scriptedCloseClient) Call(ctx context.Context, method string, params any, out any) error {
	if method != "terminal.close" {
		return c.fakeClient.Call(ctx, method, params, out)
	}
	call := int(c.closeCalls.Add(1))
	c.started.Do(func() {
		if c.closeStarted != nil {
			close(c.closeStarted)
		}
	})
	if c.releaseClose != nil {
		<-c.releaseClose
	}
	if call <= len(c.closeResults) {
		return c.closeResults[call-1]
	}
	return nil
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

func TestRemoteBackend_GivenConcurrentCloseWaitersWhenRPCFailsThenOneRPCReturnsSameFailureToAll(t *testing.T) {
	const callers = 8
	rpcErr := errors.New("terminal.close unavailable")
	releaseClose := make(chan struct{})
	client := &scriptedCloseClient{
		fakeClient: fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent),
			exitPush:   make(chan protocol.TerminalExitEvent),
		},
		closeStarted: make(chan struct{}),
		releaseClose: releaseClose,
		closeResults: []error{rpcErr},
	}
	h, err := remote.NewBackend(client).Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-client.openParams
	t.Cleanup(func() { _ = h.Close() })
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseClose) }) }
	t.Cleanup(release)

	results := make(chan error, callers)
	go func() { results <- h.Close() }()
	<-client.closeStarted
	start := make(chan struct{})
	var ready sync.WaitGroup
	ready.Add(callers - 1)
	for range callers - 1 {
		go func() {
			ready.Done()
			<-start
			results <- h.Close()
		}()
	}
	ready.Wait()
	close(start)

	select {
	case got := <-results:
		t.Errorf("Close returned before the in-flight terminal.close acknowledgement: %v", got)
	case <-time.After(20 * time.Millisecond):
	}
	release()
	for range callers {
		require.ErrorIs(t, <-results, rpcErr)
	}
	require.Equal(t, int32(1), client.closeCalls.Load(), "concurrent callers must share one terminal.close RPC")

	require.NoError(t, h.Close(), "a failed coalesced attempt must remain retryable")
	requireTerminalOutcome(t, h, "killed")
	require.Equal(t, int32(2), client.closeCalls.Load())
}

func TestRemoteBackend_GivenFirstCloseRPCFailsWhenRetriedThenPublishesKilledOnlyAfterSuccess(t *testing.T) {
	rpcErr := errors.New("terminal.close unavailable")
	client := &scriptedCloseClient{
		fakeClient: fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent),
			exitPush:   make(chan protocol.TerminalExitEvent),
		},
		closeResults: []error{rpcErr, nil},
	}
	h, err := remote.NewBackend(client).Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-client.openParams
	t.Cleanup(func() { _ = h.Close() })

	require.ErrorIs(t, h.Close(), rpcErr)
	select {
	case info, ok := <-h.Exit():
		t.Fatalf("failed terminal.close published an authoritative exit: ok=%v info=%+v", ok, info)
	case <-time.After(20 * time.Millisecond):
	}
	select {
	case data, ok := <-h.Data():
		t.Fatalf("failed terminal.close closed or published data: ok=%v data=%q", ok, data)
	case <-time.After(20 * time.Millisecond):
	}

	require.NoError(t, h.Close())
	requireTerminalOutcome(t, h, "killed")
	require.Equal(t, int32(2), client.closeCalls.Load())
}

func TestRemoteBackend_GivenNaturalExitWhileCloseRPCIsPendingThenNaturalOutcomeIsAuthoritative(t *testing.T) {
	rpcErr := errors.New("terminal already exited")
	releaseClose := make(chan struct{})
	client := &scriptedCloseClient{
		fakeClient: fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent),
			exitPush:   make(chan protocol.TerminalExitEvent, 1),
		},
		closeStarted: make(chan struct{}),
		releaseClose: releaseClose,
		closeResults: []error{rpcErr},
	}
	h, err := remote.NewBackend(client).Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-client.openParams
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseClose) }) }
	t.Cleanup(release)

	closeResults := make(chan error, 2)
	go func() { closeResults <- h.Close() }()
	<-client.closeStarted
	go func() { closeResults <- h.Close() }()

	client.exitPush <- protocol.TerminalExitEvent{
		TerminalID: "remote-1", Code: 0, Reason: "natural", Msg: "finished",
	}
	close(client.exitPush)
	close(client.dataPush)
	info := requireTerminalOutcome(t, h, "natural")
	require.Equal(t, "finished", info.Msg)

	release()
	require.NoError(t, <-closeResults)
	require.NoError(t, <-closeResults)
	require.NoError(t, h.Close(), "natural daemon exit makes Close idempotent")
	require.Equal(t, int32(1), client.closeCalls.Load())
}

func TestRemoteBackend_GivenBufferedFinalFramesBeforeDaemonExit_WhenPumped_ThenDrainsValidDataInOrderBeforeExit(t *testing.T) {
	const iterations = 100
	want := []byte("final-one/final-two")

	for i := 0; i < iterations; i++ {
		fc := &fakeClient{
			openParams: make(chan protocol.TerminalOpenParams, 1),
			dataPush:   make(chan protocol.TerminalDataEvent, 3),
			exitPush:   make(chan protocol.TerminalExitEvent, 1),
		}
		fc.dataPush <- protocol.TerminalDataEvent{
			TerminalID: "remote-1", Data: base64.StdEncoding.EncodeToString([]byte("final-one/")),
		}
		fc.dataPush <- protocol.TerminalDataEvent{TerminalID: "remote-1", Data: "malformed-base64"}
		fc.dataPush <- protocol.TerminalDataEvent{
			TerminalID: "remote-1", Data: base64.StdEncoding.EncodeToString([]byte("final-two")),
		}
		fc.exitPush <- protocol.TerminalExitEvent{
			TerminalID: "remote-1", Code: 23, Reason: "natural", Msg: "finished",
		}
		close(fc.exitPush)
		close(fc.dataPush)

		h, err := remote.NewBackend(fc).Open(context.Background(), pty.Spec{Cwd: "/r"})
		require.NoError(t, err)
		<-fc.openParams

		var got []byte
		dataCh := h.Data()
		for dataCh != nil {
			select {
			case chunk, ok := <-dataCh:
				if !ok {
					dataCh = nil
					continue
				}
				got = append(got, chunk...)
			case <-time.After(time.Second):
				t.Fatalf("iteration %d: data channel did not close", i)
			}
		}

		require.Equalf(t, want, got, "iteration %d: final output was truncated or reordered", i)
		info := requireTerminalOutcome(t, h, "natural")
		require.Equal(t, 23, info.Code)
		require.Equal(t, "finished", info.Msg)
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
			close(fc.exitPush)
			close(fc.dataPush)
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
		require.LessOrEqualf(t, fc.closeCalls.Load(), int32(1),
			"iteration %d: a natural exit may make terminal.close unnecessary, but it must never duplicate", i)
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

func TestRemoteBackend_GivenLeasedOpenFailure_WhenOpened_ThenReleasesExactlyOnce(t *testing.T) {
	openErr := errors.New("terminal.open failed")
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent),
		exitPush:   make(chan protocol.TerminalExitEvent),
		openErr:    openErr,
	}
	var releases atomic.Int32

	h, err := remote.NewBackendWithLease(fc, func() { releases.Add(1) }).Open(
		context.Background(),
		pty.Spec{Cwd: "/r"},
	)

	require.Nil(t, h)
	require.ErrorIs(t, err, openErr)
	require.Equal(t, int32(1), releases.Load())
}

func TestRemoteBackend_GivenLeasedConnectionLoss_WhenSubscriptionCloses_ThenReleasesExactlyOnce(t *testing.T) {
	fc := &fakeClient{
		openParams: make(chan protocol.TerminalOpenParams, 1),
		dataPush:   make(chan protocol.TerminalDataEvent),
		exitPush:   make(chan protocol.TerminalExitEvent),
	}
	var releases atomic.Int32
	be := remote.NewBackendWithLease(fc, func() { releases.Add(1) })
	h, err := be.Open(context.Background(), pty.Spec{Cwd: "/r"})
	require.NoError(t, err)
	<-fc.openParams

	close(fc.exitPush)

	requireTerminalOutcome(t, h, "connection_lost")
	require.Eventually(t, func() bool { return releases.Load() == 1 }, time.Second, time.Millisecond)
	require.NoError(t, h.Close())
	require.Equal(t, int32(1), releases.Load())
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
	require.Equal(t, "agentred did not respond within 5s", err.Error())
	var timeoutErr net.Error
	require.ErrorAs(t, err, &timeoutErr)
	require.True(t, timeoutErr.Timeout())
	// Should time out around 5s, not wait for 10s
	require.Less(t, elapsed, 7*time.Second, "should time out near 5s, not wait full delay")
}

func TestRemoteBackend_Open_GivenEarlierParentCancellationOrDeadlineThenPreservesParentError(t *testing.T) {
	tests := []struct {
		name string
		ctx  func() (context.Context, context.CancelFunc)
	}{
		{
			name: "parent canceled",
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, cancel
			},
		},
		{
			name: "parent deadline expires first",
			ctx: func() (context.Context, context.CancelFunc) {
				return context.WithTimeout(context.Background(), 20*time.Millisecond)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fc := &slowClient{
				delay: 10 * time.Second,
				fakeClient: fakeClient{
					openParams: make(chan protocol.TerminalOpenParams, 1),
					dataPush:   make(chan protocol.TerminalDataEvent, 1),
					exitPush:   make(chan protocol.TerminalExitEvent, 1),
				},
			}
			ctx, cancel := tt.ctx()
			defer cancel()

			_, err := remote.NewBackend(fc).Open(ctx, pty.Spec{Cwd: "/r"})
			parentErr := ctx.Err()
			require.Error(t, parentErr)
			require.Equal(t, parentErr, err)
			require.ErrorIs(t, err, parentErr)
			require.NotErrorIs(t, err, remote.ErrDaemonTimeout)
		})
	}
}
