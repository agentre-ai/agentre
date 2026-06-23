package terminal_svc_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestService_Open_Local_RegistersHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBE := mocks.NewMockPTYBackend(ctrl)
	mockH := mocks.NewMockHandle(ctrl)
	mockH.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mockH.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockBE.EXPECT().Open(gomock.Any(), pty.Spec{Cwd: "/tmp", Cols: 80, Rows: 24}).Return(mockH, nil)

	sel := terminal_svc.NewBackendSelector(mockBE, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), "t1", "", "/tmp", 80, 24))

	mockH.EXPECT().Write([]byte("x")).Return(1, nil)
	assert.NoError(t, svc.Write(context.Background(), "t1", "x"))
}

func TestService_Write_NoOpenTerminal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	sel := terminal_svc.NewBackendSelector(mocks.NewMockPTYBackend(ctrl), nil)
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})
	err := svc.Write(context.Background(), "t1", "x")
	require.ErrorIs(t, err, terminal_svc.ErrTerminalClosed)
}

func TestService_Write_UnknownTerminalReturnsClosed(t *testing.T) {
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(&fakeBackend{}, nil), terminal_svc.NoopEmitter{})
	if err := svc.Write(context.Background(), "ghost", "x"); !errors.Is(err, terminal_svc.ErrTerminalClosed) {
		t.Fatalf("want ErrTerminalClosed, got %v", err)
	}
}

func TestService_Close_UnknownTerminal(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(&fakeBackend{}, nil)
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})
	err := svc.Close(context.Background(), "ghost")
	require.ErrorIs(t, err, terminal_svc.ErrTerminalNotOpen)
}

func TestService_Open_ReOpenClosesPrevious(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	first := mocks.NewMockHandle(ctrl)
	second := mocks.NewMockHandle(ctrl)
	first.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	first.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	second.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	second.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))

	gomock.InOrder(
		mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(first, nil),
		first.EXPECT().Close().Return(nil),
		mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(second, nil),
	)

	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), "t1", "", "/tmp", 80, 24))
	require.NoError(t, svc.Open(context.Background(), "t1", "", "/tmp", 80, 24))
}

func TestService_Shutdown_ClosesAll(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	mh := mocks.NewMockHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Close().Return(nil)

	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), "t1", "", "/tmp", 80, 24))
	svc.Shutdown()
}

// TestService_Shutdown_PreemptsInFlightOpen_ClosesHandleNotRegistered covers the
// race where Shutdown runs while a backend.Open is still in flight. Shutdown must
// preempt the pending attempt so that a handle returned after Shutdown is torn
// down rather than registered into the just-cleared session map — otherwise the
// PTY (and any remote daemon-side shell) leaks past app shutdown.
func TestService_Shutdown_PreemptsInFlightOpen_ClosesHandleNotRegistered(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockBE := mocks.NewMockPTYBackend(ctrl)
	mockH := mocks.NewMockHandle(ctrl)

	started := make(chan struct{})
	proceed := make(chan struct{})
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, _ pty.Spec) (pty.Handle, error) {
			close(started)
			<-proceed
			return mockH, nil // spawn succeeded despite the concurrent Shutdown
		})
	// The preempted handle must be closed and never registered; no pump should
	// start, so Data()/Exit() must NOT be consumed.
	mockH.EXPECT().Close().Return(nil)

	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	openErr := make(chan error, 1)
	go func() { openErr <- svc.Open(context.Background(), "t1", "", "/tmp", 80, 24) }()

	<-started
	svc.Shutdown() // preempt the in-flight Open
	close(proceed) // backend.Open now returns success
	require.NoError(t, <-openErr)

	// The handle must not be registered.
	require.ErrorIs(t, svc.Write(context.Background(), "t1", "x"), terminal_svc.ErrTerminalClosed)
}

func TestService_Open_CancelledByClose_NoLeakedHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	started := make(chan struct{})
	canceled := make(chan struct{})
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(
		func(openCtx context.Context, _ pty.Spec) (pty.Handle, error) {
			close(started)
			<-openCtx.Done()
			close(canceled)
			return nil, openCtx.Err()
		})

	sel := terminal_svc.NewBackendSelector(mockBE, func(string) (terminal_svc.PTYBackend, error) {
		t.Fatal("should not call remote factory for local")
		return nil, nil
	})
	svc := terminal_svc.NewService(sel, terminal_svc.NoopEmitter{})

	openErrCh := make(chan error, 1)
	go func() {
		openErrCh <- svc.Open(context.Background(), "t1", "", "/tmp", 80, 24)
	}()
	<-started
	// Now preempt via Close
	require.NoError(t, svc.Close(context.Background(), "t1"))
	<-canceled // confirm cancel actually fired
	err := <-openErrCh
	require.ErrorIs(t, err, context.Canceled)
	// Verify no handle leaked
	require.ErrorIs(t, svc.Write(context.Background(), "t1", "x"), terminal_svc.ErrTerminalClosed)
}

type recordingEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}

type recordedEvent struct {
	Name    string
	Payload any
}

func (r *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, recordedEvent{name, payload})
}

func (r *recordingEmitter) Snapshot() []recordedEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]recordedEvent, len(r.events))
	copy(out, r.events)
	return out
}

func TestService_Pump_EmitsDataEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockBE := mocks.NewMockPTYBackend(ctrl)
	mh := mocks.NewMockHandle(ctrl)
	dataCh := make(chan []byte, 1)
	exitCh := make(chan pty.ExitInfo)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	sel := terminal_svc.NewBackendSelector(mockBE, nil)
	svc := terminal_svc.NewService(sel, rec)

	require.NoError(t, svc.Open(context.Background(), "t7", "", "/tmp", 80, 24))
	dataCh <- []byte("abc")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(rec.Snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	evs := rec.Snapshot()
	require.Len(t, evs, 1)
	assert.Equal(t, "terminal:t7:data", evs[0].Name)
}
