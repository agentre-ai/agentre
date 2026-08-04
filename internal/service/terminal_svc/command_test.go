package terminal_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

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
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	response, err := svc.RunCommand(ctx, terminal_svc.RunCommandRequest{
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
	assert.Equal(t, 0, logs.Len())
}

func TestService_RunCommand_GivenOpenFailure_WhenStarted_ThenReturnsExactScopeStartErrorAndOneRedactedWarning(t *testing.T) {
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
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	response, err := svc.RunCommand(ctx, terminal_svc.RunCommandRequest{
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
	require.Equal(t, 1, logs.Len())
	entry := logs.All()[0]
	assert.Equal(t, zapcore.WarnLevel, entry.Level)
	assert.Equal(t, "terminal_svc.RunCommand: open command failed", entry.Message)
	assert.Equal(t, map[string]any{
		"sessionId":  int64(72),
		"terminalId": "terminal-2",
		"deviceId":   "device-9",
		"error":      startErr.Error(),
	}, entry.ContextMap())
	assert.NotContains(t, entry.Message, "make check")
	assert.NotContains(t, entry.Message, "/remote/current")
}

func TestService_RunCommand_GivenResolverUnavailable_WhenStarted_ThenReturnsErrorWithoutPanicOrLaunch(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*terminal_svc.Service)
		wantErr   error
	}{
		{
			name:      "resolver is not initialized",
			configure: func(*terminal_svc.Service) {},
			wantErr:   terminal_svc.ErrCommandScopeResolverNotInitialized,
		},
		{
			name: "resolver returns no scope",
			configure: func(svc *terminal_svc.Service) {
				svc.SetCommandScopeResolver(func(
					context.Context,
					terminal_svc.ResolveCommandScopeRequest,
				) (*terminal_svc.CommandScope, error) {
					return nil, nil
				})
			},
			wantErr: terminal_svc.ErrCommandScopeUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			localBackend := mocks.NewMockPTYBackend(ctrl)
			svc := terminal_svc.NewService(
				terminal_svc.NewBackendSelector(localBackend, nil),
				terminal_svc.NoopEmitter{},
			)
			tt.configure(svc)

			var response *terminal_svc.RunCommandResponse
			var err error
			require.NotPanics(t, func() {
				response, err = svc.RunCommand(context.Background(), terminal_svc.RunCommandRequest{
					TerminalID: "terminal-unavailable",
					SessionID:  70,
					Command:    "private-token-command",
					Cols:       80,
					Rows:       24,
				})
			})
			assert.Nil(t, response)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
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
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	response, err := svc.RunCommand(ctx, terminal_svc.RunCommandRequest{
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
	assert.Equal(t, 0, logs.Len())
}
