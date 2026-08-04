package handlers_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/require"
)

type terminalBackendFunc func(context.Context, pty.Spec) (handlers.PTYHandle, error)

func (f terminalBackendFunc) Open(ctx context.Context, spec pty.Spec) (handlers.PTYHandle, error) {
	return f(ctx, spec)
}

type trackedTerminalHandle struct {
	data chan []byte
	exit chan pty.ExitInfo

	closeOnce  sync.Once
	closeCalls atomic.Int32
	dataCalls  atomic.Int32
	exitCalls  atomic.Int32
}

func newTrackedTerminalHandle() *trackedTerminalHandle {
	return &trackedTerminalHandle{
		data: make(chan []byte),
		exit: make(chan pty.ExitInfo, 1),
	}
}

func (h *trackedTerminalHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *trackedTerminalHandle) Resize(_, _ uint16) error    { return nil }

func (h *trackedTerminalHandle) Close() error {
	h.closeCalls.Add(1)
	h.closeOnce.Do(func() {
		h.exit <- pty.ExitInfo{Code: 137, Reason: "killed"}
		close(h.exit)
		close(h.data)
	})
	return nil
}

func (h *trackedTerminalHandle) Data() <-chan []byte {
	h.dataCalls.Add(1)
	return h.data
}

func (h *trackedTerminalHandle) Exit() <-chan pty.ExitInfo {
	h.exitCalls.Add(1)
	return h.exit
}

type terminalOpenOutcome struct {
	result protocol.TerminalOpenResult
	err    error
}

func openTerminalAsync(h *handlers.TerminalHandlers, params protocol.TerminalOpenParams) <-chan terminalOpenOutcome {
	out := make(chan terminalOpenOutcome, 1)
	go func() {
		result, err := h.Open(context.Background(), params)
		out <- terminalOpenOutcome{result: result, err: err}
	}()
	return out
}

func receiveTerminalTestValue[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for terminal test event")
		var zero T
		return zero
	}
}

func requireTerminalContextCanceled(t *testing.T, ctx context.Context) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("pending terminal context was not canceled")
	}
}

func TestTerminalOpen_GivenSuppliedIDAndFastBackendWhenOpenedWithoutSubscribersThenResultUsesSuppliedID(t *testing.T) {
	handle := newTrackedTerminalHandle()
	var calls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		calls.Add(1)
		return handle, nil
	}), &recordingEmitter{})
	t.Cleanup(h.CloseAll)

	result, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: "desktop-terminal-1",
		Cols:       80,
		Rows:       24,
	})

	require.NoError(t, err)
	require.Equal(t, "desktop-terminal-1", result.TerminalID)
	require.Equal(t, int32(1), calls.Load())
}

func TestTerminalOpen_GivenEmptyLegacyIDWhenOpenedThenGeneratesDaemonID(t *testing.T) {
	handle := newTrackedTerminalHandle()
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		return handle, nil
	}), &recordingEmitter{})
	t.Cleanup(h.CloseAll)

	result, err := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})

	require.NoError(t, err)
	require.NotEmpty(t, result.TerminalID)
}

func TestTerminalOpen_GivenUnsafeOrOversizedSuppliedIDWhenOpenedThenRejectsBeforeBackend(t *testing.T) {
	var calls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		calls.Add(1)
		return newTrackedTerminalHandle(), nil
	}), &recordingEmitter{})

	for _, id := range []string{"has space", "bad/slash", "-missing-prefix", strings.Repeat("a", 129)} {
		_, err := h.Open(context.Background(), protocol.TerminalOpenParams{TerminalID: id, Cols: 80, Rows: 24})
		require.ErrorIsf(t, err, handlers.ErrTerminalIDInvalid, "id=%q", id)
	}
	require.Equal(t, int32(0), calls.Load())

	result, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: strings.Repeat("a", 128),
		Cols:       80,
		Rows:       24,
	})
	require.NoError(t, err)
	require.Len(t, result.TerminalID, 128)
	h.CloseAll()
}

func TestTerminalClose_GivenRegisteredPendingOpenWhenCancellationRequestedThenCancelsIdempotently(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(ctx context.Context, _ pty.Spec) (handlers.PTYHandle, error) {
		started <- ctx
		<-release
		return nil, ctx.Err()
	}), &recordingEmitter{})
	outcome := openTerminalAsync(h, protocol.TerminalOpenParams{TerminalID: "pending-cancel-1", Cols: 80, Rows: 24})
	openCtx := receiveTerminalTestValue(t, started)

	params := protocol.TerminalCloseParams{TerminalID: "pending-cancel-1", CancelPendingOpen: true}
	_, err := h.Close(context.Background(), params)
	require.NoError(t, err)
	_, err = h.Close(context.Background(), params)
	require.NoError(t, err)
	requireTerminalContextCanceled(t, openCtx)
	close(release)

	result := receiveTerminalTestValue(t, outcome)
	require.ErrorIs(t, result.err, handlers.ErrTerminalOpenCanceled)
}

func TestTerminalClose_GivenCancellationDispatchedBeforeOpenWhenMatchingOpenArrivesThenBackendIsNotCalled(t *testing.T) {
	var calls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		calls.Add(1)
		return newTrackedTerminalHandle(), nil
	}), &recordingEmitter{})
	params := protocol.TerminalCloseParams{TerminalID: "close-before-open-1", CancelPendingOpen: true}

	_, err := h.Close(context.Background(), params)
	require.NoError(t, err)
	_, err = h.Close(context.Background(), params)
	require.NoError(t, err)
	_, err = h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: "close-before-open-1",
		Cols:       80,
		Rows:       24,
	})

	require.ErrorIs(t, err, handlers.ErrTerminalOpenCanceled)
	require.Equal(t, int32(0), calls.Load())
}

func TestTerminalOpen_GivenCanceledBackendIgnoresContextWhenHandleReturnsLateThenClosesWithoutPumpOrEvents(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	handle := newTrackedTerminalHandle()
	recorder := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(ctx context.Context, _ pty.Spec) (handlers.PTYHandle, error) {
		started <- ctx
		<-release
		return handle, nil
	}), recorder)
	outcome := openTerminalAsync(h, protocol.TerminalOpenParams{TerminalID: "late-handle-1", Cols: 80, Rows: 24})
	openCtx := receiveTerminalTestValue(t, started)

	_, err := h.Close(context.Background(), protocol.TerminalCloseParams{
		TerminalID:        "late-handle-1",
		CancelPendingOpen: true,
	})
	require.NoError(t, err)
	requireTerminalContextCanceled(t, openCtx)
	close(release)

	result := receiveTerminalTestValue(t, outcome)
	require.ErrorIs(t, result.err, handlers.ErrTerminalOpenCanceled)
	require.Equal(t, int32(1), handle.closeCalls.Load())
	require.Zero(t, handle.dataCalls.Load())
	require.Zero(t, handle.exitCalls.Load())
	require.Empty(t, recorder.snapshot())
}

func TestTerminalOpen_GivenDuplicatePendingIDWhenOpenedThenDoesNotOverwriteFirstAttempt(t *testing.T) {
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	firstHandle := newTrackedTerminalHandle()
	var calls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		if calls.Add(1) == 1 {
			started <- struct{}{}
			<-release
			return firstHandle, nil
		}
		return newTrackedTerminalHandle(), nil
	}), &recordingEmitter{})
	first := openTerminalAsync(h, protocol.TerminalOpenParams{TerminalID: "duplicate-pending-1", Cols: 80, Rows: 24})
	receiveTerminalTestValue(t, started)

	_, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: "duplicate-pending-1",
		Cols:       80,
		Rows:       24,
	})

	require.ErrorIs(t, err, handlers.ErrTerminalIDInUse)
	require.Equal(t, int32(1), calls.Load())
	_, err = h.Close(context.Background(), protocol.TerminalCloseParams{
		TerminalID:        "duplicate-pending-1",
		CancelPendingOpen: true,
	})
	require.NoError(t, err)
	close(release)
	require.ErrorIs(t, receiveTerminalTestValue(t, first).err, handlers.ErrTerminalOpenCanceled)
}

func TestTerminalOpen_GivenDuplicateActiveIDWhenOpenedThenDoesNotOverwriteLiveHandle(t *testing.T) {
	var calls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		calls.Add(1)
		return newTrackedTerminalHandle(), nil
	}), &recordingEmitter{})
	t.Cleanup(h.CloseAll)
	params := protocol.TerminalOpenParams{TerminalID: "duplicate-active-1", Cols: 80, Rows: 24}
	_, err := h.Open(context.Background(), params)
	require.NoError(t, err)

	_, err = h.Open(context.Background(), params)

	require.ErrorIs(t, err, handlers.ErrTerminalIDInUse)
	require.Equal(t, int32(1), calls.Load())
}

func TestTerminalClose_GivenCancellationTombstonesReachCapacityThenRejectsWithoutEvictionAndRecoversAfterConsumption(t *testing.T) {
	var backendCalls atomic.Int32
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
		backendCalls.Add(1)
		return newTrackedTerminalHandle(), nil
	}), &recordingEmitter{})

	accepted := make([]string, 0, 512)
	var rejectedID string
	for i := 0; i < 2048; i++ {
		id := fmt.Sprintf("cancel-%04d", i)
		_, err := h.Close(context.Background(), protocol.TerminalCloseParams{
			TerminalID:        id,
			CancelPendingOpen: true,
		})
		if err != nil {
			require.ErrorIs(t, err, handlers.ErrTerminalCancelCapacity)
			rejectedID = id
			break
		}
		accepted = append(accepted, id)
	}
	require.NotEmpty(t, accepted)
	require.NotEmpty(t, rejectedID, "pending cancellation ownership must be bounded")

	_, err := h.Close(context.Background(), protocol.TerminalCloseParams{
		TerminalID:        accepted[0],
		CancelPendingOpen: true,
	})
	require.NoError(t, err, "repeating an owned cancellation must stay idempotent at capacity")
	_, err = h.Close(context.Background(), protocol.TerminalCloseParams{
		TerminalID:        rejectedID,
		CancelPendingOpen: true,
	})
	require.ErrorIs(t, err, handlers.ErrTerminalCancelCapacity)

	_, err = h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: accepted[0],
		Cols:       80,
		Rows:       24,
	})
	require.ErrorIs(t, err, handlers.ErrTerminalOpenCanceled)
	require.Equal(t, int32(0), backendCalls.Load(), "a live tombstone must not have been evicted")

	_, err = h.Close(context.Background(), protocol.TerminalCloseParams{
		TerminalID:        rejectedID,
		CancelPendingOpen: true,
	})
	require.NoError(t, err, "caller retry must succeed after a tombstone is consumed")
}

func TestTerminalCloseAll_GivenPendingOpenReturnsAfterDisconnectThenClosesLateHandleAndRejectsNewClaims(t *testing.T) {
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	handle := newTrackedTerminalHandle()
	var calls atomic.Int32
	recorder := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(terminalBackendFunc(func(ctx context.Context, _ pty.Spec) (handlers.PTYHandle, error) {
		calls.Add(1)
		started <- ctx
		<-release
		return handle, nil
	}), recorder)
	outcome := openTerminalAsync(h, protocol.TerminalOpenParams{TerminalID: "disconnect-pending-1", Cols: 80, Rows: 24})
	openCtx := receiveTerminalTestValue(t, started)

	h.CloseAll()
	requireTerminalContextCanceled(t, openCtx)
	close(release)

	result := receiveTerminalTestValue(t, outcome)
	require.ErrorIs(t, result.err, handlers.ErrTerminalHandlerClosed)
	require.Equal(t, int32(1), handle.closeCalls.Load())
	require.Zero(t, handle.dataCalls.Load())
	require.Zero(t, handle.exitCalls.Load())
	require.Empty(t, recorder.snapshot())

	_, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		TerminalID: "after-disconnect-1",
		Cols:       80,
		Rows:       24,
	})
	require.ErrorIs(t, err, handlers.ErrTerminalHandlerClosed)
	require.Equal(t, int32(1), calls.Load())
}

func TestTerminalCloseAll_GivenOpenCompletionRacesDisconnectThenNoHandleSurvives(t *testing.T) {
	const iterations = 100
	for i := 0; i < iterations; i++ {
		handle := newTrackedTerminalHandle()
		started := make(chan struct{}, 1)
		release := make(chan struct{})
		h := handlers.NewTerminalHandlers(terminalBackendFunc(func(context.Context, pty.Spec) (handlers.PTYHandle, error) {
			started <- struct{}{}
			<-release
			return handle, nil
		}), &recordingEmitter{})
		id := fmt.Sprintf("disconnect-race-%03d", i)
		outcome := openTerminalAsync(h, protocol.TerminalOpenParams{TerminalID: id, Cols: 80, Rows: 24})
		receiveTerminalTestValue(t, started)

		gate := make(chan struct{})
		closeAllDone := make(chan struct{})
		go func() {
			<-gate
			h.CloseAll()
			close(closeAllDone)
		}()
		go func() {
			<-gate
			close(release)
		}()
		close(gate)

		result := receiveTerminalTestValue(t, outcome)
		receiveTerminalTestValue(t, closeAllDone)
		if result.err != nil {
			require.ErrorIsf(t, result.err, handlers.ErrTerminalHandlerClosed, "iteration=%d", i)
		}
		require.Equalf(t, int32(1), handle.closeCalls.Load(), "iteration=%d", i)
		_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: id})
		require.ErrorIsf(t, err, handlers.ErrTerminalNotFound, "iteration=%d", i)
	}
}
