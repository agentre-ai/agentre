package handlers_test

import (
	"context"
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
	events []recordedEvent
}
type recordedEvent struct {
	Name    string
	Payload any
}

func (e *recordingEmitter) Emit(_ context.Context, name string, payload any) {
	e.events = append(e.events, recordedEvent{name, payload})
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
	_, err := h.Close(context.Background(), protocol.TerminalCloseParams{TerminalID: res.TerminalID})
	require.NoError(t, err)
}
