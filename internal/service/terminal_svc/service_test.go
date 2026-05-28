package terminal_svc_test

import (
	"context"
	"testing"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/pty"
	"agentre/internal/service/terminal_svc"
	"agentre/internal/service/terminal_svc/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type stubSessionLookup struct {
	sess *chat_entity.Session
	be   *agent_backend_entity.AgentBackend
	cwd  string
	err  error
}

func (s stubSessionLookup) Lookup(_ context.Context, _ int64) (*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error) {
	return s.sess, s.be, s.cwd, s.err
}

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
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))

	mockH.EXPECT().Write([]byte("x")).Return(1, nil)
	assert.NoError(t, svc.Write(context.Background(), 1, "x"))
}

func TestService_Open_SessionNotFound(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(nil, nil)
	svc := terminal_svc.NewService(stubSessionLookup{}, sel, terminal_svc.NoopEmitter{})
	err := svc.Open(context.Background(), 999, 80, 24)
	require.ErrorIs(t, err, terminal_svc.ErrSessionNotFound)
}

func TestService_Write_NoOpenTerminal(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	sel := terminal_svc.NewBackendSelector(mocks.NewMockPTYBackend(ctrl), nil)
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})
	err := svc.Write(context.Background(), 1, "x")
	require.ErrorIs(t, err, terminal_svc.ErrTerminalClosed)
}

func TestService_Close_UnknownSession(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(nil, nil)
	svc := terminal_svc.NewService(stubSessionLookup{}, sel, terminal_svc.NoopEmitter{})
	err := svc.Close(context.Background(), 999)
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
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
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
	svc := terminal_svc.NewService(stubSessionLookup{
		sess: &chat_entity.Session{ID: 1},
		be:   &agent_backend_entity.AgentBackend{DeviceID: ""},
		cwd:  "/tmp",
	}, sel, terminal_svc.NoopEmitter{})

	require.NoError(t, svc.Open(context.Background(), 1, 80, 24))
	svc.Shutdown()
}
