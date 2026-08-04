package terminal_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc/mocks"
)

type completedCommandHandle struct {
	data <-chan []byte
	exit <-chan pty.ExitInfo
}

func newCompletedCommandHandle() *completedCommandHandle {
	data := make(chan []byte)
	close(data)
	exit := make(chan pty.ExitInfo, 1)
	exit <- pty.ExitInfo{}
	close(exit)
	return &completedCommandHandle{data: data, exit: exit}
}

func (h *completedCommandHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *completedCommandHandle) Resize(uint16, uint16) error { return nil }
func (h *completedCommandHandle) Close() error                { return nil }
func (h *completedCommandHandle) Data() <-chan []byte         { return h.data }
func (h *completedCommandHandle) Exit() <-chan pty.ExitInfo   { return h.exit }

func TestService_RunCommand_GivenResolvedTarget_WhenStarted_ThenResolvesOnceOpensOnceAndReturnsExactScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantScope := &terminal_svc.CommandScope{DeviceID: "device-9", Cwd: "/remote/current"}
	resolveCalls := 0
	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: "/remote/current", Command: "go test ./...", Cols: 100, Rows: 30,
	}).Return(newCompletedCommandHandle(), nil).Times(1)
	factoryCalls := 0
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(deviceID string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			assert.Equal(t, "device-9", deviceID)
			return remoteBackend, nil
		}), terminal_svc.NoopEmitter{})
	svc.SetCommandScopeResolver(func(
		_ context.Context,
		req terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		resolveCalls++
		assert.Equal(t, terminal_svc.ResolveCommandScopeRequest{SessionID: 71}, req)
		return wantScope, nil
	})
	defer svc.Shutdown()

	response, err := svc.RunCommand(context.Background(), terminal_svc.RunCommandRequest{
		TerminalID: "terminal-1",
		SessionID:  71,
		Command:    "go test ./...",
		Cols:       100,
		Rows:       30,
	})

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, *wantScope, response.Scope)
	assert.Empty(t, response.StartError)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, factoryCalls)
}

func TestService_RunCommand_GivenOpenFailure_WhenStarted_ThenReturnsExactScopeAndStartErrorWithoutRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantScope := &terminal_svc.CommandScope{DeviceID: "device-9", Cwd: "/remote/current"}
	resolveCalls := 0
	startErr := errors.New("shell start denied")
	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: "/remote/current", Command: "make check", Cols: 80, Rows: 24,
	}).Return(nil, startErr).Times(1)
	factoryCalls := 0
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			return remoteBackend, nil
		}), terminal_svc.NoopEmitter{})
	svc.SetCommandScopeResolver(func(
		context.Context,
		terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		resolveCalls++
		return wantScope, nil
	})
	defer svc.Shutdown()

	response, err := svc.RunCommand(context.Background(), terminal_svc.RunCommandRequest{
		TerminalID: "terminal-2",
		SessionID:  72,
		Command:    "make check",
		Cols:       80,
		Rows:       24,
	})

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, *wantScope, response.Scope)
	assert.Equal(t, startErr.Error(), response.StartError)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, factoryCalls)
}

func TestService_RunCommand_GivenResolutionFailure_WhenStarted_ThenReturnsRPCErrorWithoutScopeOrLaunch(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resolveErr := errors.New("target resolution failed")
	resolveCalls := 0
	localBackend := mocks.NewMockPTYBackend(ctrl)
	factoryCalls := 0
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			return mocks.NewMockPTYBackend(ctrl), nil
		}), terminal_svc.NoopEmitter{})
	svc.SetCommandScopeResolver(func(
		context.Context,
		terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		resolveCalls++
		return nil, resolveErr
	})
	defer svc.Shutdown()

	response, err := svc.RunCommand(context.Background(), terminal_svc.RunCommandRequest{
		TerminalID: "terminal-3",
		SessionID:  73,
		Command:    "pwd",
		Cols:       80,
		Rows:       24,
	})

	assert.Nil(t, response)
	require.ErrorIs(t, err, resolveErr)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 0, factoryCalls)
}
