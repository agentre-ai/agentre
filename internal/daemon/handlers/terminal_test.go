package handlers_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"agentre/internal/daemon/handlers"
	"agentre/internal/daemon/handlers/mock_handlers"
	"agentre/internal/pkg/pty"
	"agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type recordingEmitter struct {
	mu     sync.Mutex
	events []recordedEvent
}
type recordedEvent struct {
	Name    string
	Payload any
}

func (e *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, recordedEvent{name, payload})
}

func (e *recordingEmitter) len() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.events)
}

func (e *recordingEmitter) snapshot() []recordedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]recordedEvent, len(e.events))
	copy(out, e.events)
	return out
}

func TestTerminal_Open_RegistersHandleAndReturnsID(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, err := h.Open(context.Background(), protocol.TerminalOpenParams{
		SessionID: 1, Cwd: "/tmp", Cols: 80, Rows: 24,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, res.TerminalID)
	_ = time.Millisecond // keep import used in later tests
}

func TestTerminal_Write_DispatchesToHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Write([]byte("ls\n")).Return(3, nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: res.TerminalID, Data: "ls\n"})
	require.NoError(t, err)
}

func TestTerminal_Write_UnknownID_ReturnsNotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: "nope", Data: "x"})
	require.ErrorIs(t, err, handlers.ErrTerminalNotFound)
}

func TestTerminal_Resize_DispatchesToHandle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Resize(uint16(120), uint16(30)).Return(nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Resize(context.Background(), protocol.TerminalResizeParams{TerminalID: res.TerminalID, Cols: 120, Rows: 30})
	require.NoError(t, err)
}

func TestTerminal_Close_CallsHandleClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	mh.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mh.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)
	mh.EXPECT().Close().Return(nil)

	h := handlers.NewTerminalHandlers(mbe, &recordingEmitter{})
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	_, err := h.Close(context.Background(), protocol.TerminalCloseParams(res))
	require.NoError(t, err)
}

func TestTerminal_Pump_EmitsDataEvent(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	dataCh := make(chan []byte, 1)
	exitCh := make(chan pty.ExitInfo)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})

	dataCh <- []byte("hello")

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rec.len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	events := rec.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, handlers.EventNameTerminalData, events[0].Name)
	pay := events[0].Payload.(protocol.TerminalDataEvent)
	assert.Equal(t, res.TerminalID, pay.TerminalID)
	decoded, err := base64.StdEncoding.DecodeString(pay.Data)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(decoded))
}

func TestTerminal_Pump_EmitsExitAndClearsMap(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	dataCh := make(chan []byte)
	exitCh := make(chan pty.ExitInfo, 1)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	rec := &recordingEmitter{}
	h := handlers.NewTerminalHandlers(mbe, rec)
	res, _ := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})

	exitCh <- pty.ExitInfo{Code: 0, Reason: "natural"}
	close(exitCh)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if rec.len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	events := rec.snapshot()
	require.Len(t, events, 1)
	assert.Equal(t, handlers.EventNameTerminalExit, events[0].Name)

	_, err := h.Write(context.Background(), protocol.TerminalWriteParams{TerminalID: res.TerminalID})
	assert.ErrorIs(t, err, handlers.ErrTerminalNotFound)
}

// blockingEmitter starts in blocked state. Emit calls queue the event but
// block until unblock() is called.
type blockingEmitter struct {
	mu      sync.Mutex
	blocked bool
	cond    *sync.Cond
	events  []recordedEvent
}

func newBlockingEmitter() *blockingEmitter {
	e := &blockingEmitter{blocked: true}
	e.cond = sync.NewCond(&e.mu)
	return e
}

func (e *blockingEmitter) Emit(_ context.Context, name string, payload any) {
	e.mu.Lock()
	for e.blocked {
		e.cond.Wait()
	}
	e.events = append(e.events, recordedEvent{name, payload})
	e.mu.Unlock()
}

func (e *blockingEmitter) unblock() {
	e.mu.Lock()
	e.blocked = false
	e.cond.Broadcast()
	e.mu.Unlock()
}

func (e *blockingEmitter) snapshot() []recordedEvent {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]recordedEvent, len(e.events))
	copy(out, e.events)
	return out
}

func TestTerminal_Pump_DropsOldestAndInsertsThrottleMarkerWhenBufferFull(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mbe := mock_handlers.NewMockPTYBackend(ctrl)
	mh := mock_handlers.NewMockPTYHandle(ctrl)
	dataCh := make(chan []byte, 1000) // big enough that we control the rate
	exitCh := make(chan pty.ExitInfo)
	mh.EXPECT().Data().AnyTimes().Return(dataCh)
	mh.EXPECT().Exit().AnyTimes().Return(exitCh)
	mbe.EXPECT().Open(gomock.Any(), gomock.Any()).Return(mh, nil)

	blockEmit := newBlockingEmitter()
	h := handlers.NewTerminalHandlers(mbe, blockEmit)
	_, err := h.Open(context.Background(), protocol.TerminalOpenParams{Cols: 80, Rows: 24})
	require.NoError(t, err)

	// Flood with 400 chunks while emitter is blocked. Buffer cap is 256.
	for i := 0; i < 400; i++ {
		dataCh <- []byte(fmt.Sprintf("chunk-%d", i))
	}

	// Let the pump goroutine queue up to cap before unblocking.
	time.Sleep(50 * time.Millisecond)
	blockEmit.unblock()
	time.Sleep(200 * time.Millisecond) // let drain complete

	events := blockEmit.snapshot()
	var sawThrottle bool
	for _, ev := range events {
		if pay, ok := ev.Payload.(protocol.TerminalDataEvent); ok {
			decoded, err := base64.StdEncoding.DecodeString(pay.Data)
			require.NoError(t, err, "terminal data event must be valid base64")
			if strings.Contains(string(decoded), "--- output throttled ---") {
				sawThrottle = true
				break
			}
		}
	}
	require.True(t, sawThrottle, "expected throttle marker in emitted events")
	// And total events < 400 (i.e., we dropped chunks)
	require.Less(t, len(events), 400, "expected drops below input rate")
}
