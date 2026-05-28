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
