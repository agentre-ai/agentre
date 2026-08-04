package app

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo/mock_project_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc"
	"github.com/agentre-ai/agentre/internal/service/terminal_svc/mocks"
)

type terminalChatServiceStub struct {
	chat_svc.ChatSvc
	resolve func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error)
}

func (s *terminalChatServiceStub) ResolveLocalCommandScope(
	ctx context.Context,
	req *chat_svc.ResolveLocalCommandScopeRequest,
) (*chat_svc.LocalCommandScope, error) {
	return s.resolve(ctx, req)
}

type completedTerminalHandle struct {
	data <-chan []byte
	exit <-chan pty.ExitInfo
}

func newCompletedTerminalHandle() *completedTerminalHandle {
	data := make(chan []byte)
	close(data)
	exit := make(chan pty.ExitInfo, 1)
	exit <- pty.ExitInfo{}
	close(exit)
	return &completedTerminalHandle{data: data, exit: exit}
}

func (h *completedTerminalHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *completedTerminalHandle) Resize(uint16, uint16) error { return nil }
func (h *completedTerminalHandle) Close() error                { return nil }
func (h *completedTerminalHandle) Data() <-chan []byte         { return h.data }
func (h *completedTerminalHandle) Exit() <-chan pty.ExitInfo   { return h.exit }

func registerTerminalChatService(t *testing.T, stub *terminalChatServiceStub) {
	t.Helper()
	previous := chat_svc.Chat()
	chat_svc.RegisterChat(stub)
	t.Cleanup(func() { chat_svc.RegisterChat(previous) })
}

// TestApp_TerminalOpen_NilService locks the nil-guard: TerminalOpen must not
// panic before the service is wired (Startup not run yet).
func TestApp_TerminalOpen_NilService(t *testing.T) {
	a := &App{}
	require.ErrorIs(t, a.TerminalOpen("t1", 7, "", 80, 24), errTerminalSvcNotInitialized)
}

// TestApp_TerminalOpen_ResolvesProjectCwdThenOpens locks the app-layer glue that
// the service itself can't see: TerminalOpen resolves the project cwd and threads
// it into terminal_svc.Open's pty.Spec. This is the only place that wiring is
// exercised end-to-end (the service is handed a pre-resolved cwd).
func TestApp_TerminalOpen_ResolvesProjectCwdThenOpens(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProj := mock_project_repo.NewMockProjectRepo(ctrl)
	project_repo.RegisterProject(mockProj)
	mockProj.EXPECT().Find(gomock.Any(), int64(7)).Return(
		&project_entity.Project{ID: 7, Path: "/repo", Status: consts.ACTIVE}, nil)

	mockBE := mocks.NewMockPTYBackend(ctrl)
	mockH := mocks.NewMockHandle(ctrl)
	mockH.EXPECT().Data().AnyTimes().Return(make(chan []byte))
	mockH.EXPECT().Exit().AnyTimes().Return(make(chan pty.ExitInfo))
	mockH.EXPECT().Close().AnyTimes().Return(nil)
	var gotSpec pty.Spec
	mockBE.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, spec pty.Spec) (pty.Handle, error) {
			gotSpec = spec
			return mockH, nil
		})

	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(mockBE, nil), terminal_svc.NoopEmitter{})
	defer svc.Shutdown()
	a := &App{ctx: context.Background(), terminalSvc: svc}

	require.NoError(t, a.TerminalOpen("t1", 7, "", 80, 24))
	// cwd came from ResolveProjectCwd (local project.Path), dims passed through.
	assert.Equal(t, pty.Spec{Cwd: "/repo", Cols: 80, Rows: 24}, gotSpec)
}

// TestApp_TerminalOpen_PropagatesResolveErrorWithoutOpening locks that a cwd
// resolution failure (e.g. project deleted) surfaces as an error and never
// reaches the backend — no PTY is spawned for a project we can't locate.
func TestApp_TerminalOpen_PropagatesResolveErrorWithoutOpening(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockProj := mock_project_repo.NewMockProjectRepo(ctrl)
	project_repo.RegisterProject(mockProj)
	mockProj.EXPECT().Find(gomock.Any(), int64(7)).Return(nil, nil) // not found → ProjectNotFound

	// No Open expectation: backend.Open must NOT be called when cwd resolution fails.
	mockBE := mocks.NewMockPTYBackend(ctrl)
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(mockBE, nil), terminal_svc.NoopEmitter{})
	a := &App{ctx: context.Background(), terminalSvc: svc}

	require.Error(t, a.TerminalOpen("t1", 7, "", 80, 24))
}

func TestApp_ResolveLocalCommandScope_GivenPreSessionTarget_WhenResolved_ThenDelegatesReadOnly(t *testing.T) {
	req := &chat_svc.ResolveLocalCommandScopeRequest{AgentID: 31, ProjectID: 41}
	want := &chat_svc.LocalCommandScope{DeviceID: "device-9", Cwd: "/remote/current"}
	calls := 0
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(_ context.Context, got *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			calls++
			require.Same(t, req, got)
			return want, nil
		},
	})

	got, err := (&App{ctx: context.Background()}).ResolveLocalCommandScope(req)

	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.Equal(t, 1, calls)
}

func TestApp_ResolveLocalCommandScope_GivenResolverFailure_WhenResolved_ThenReturnsRPCError(t *testing.T) {
	resolveErr := errors.New("scope unavailable")
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			return nil, resolveErr
		},
	})

	got, err := (&App{ctx: context.Background()}).ResolveLocalCommandScope(
		&chat_svc.ResolveLocalCommandScopeRequest{SessionID: 71},
	)

	assert.Nil(t, got)
	require.ErrorIs(t, err, resolveErr)
}

func TestApp_TerminalRunCommand_GivenResolvedTarget_WhenStarted_ThenOpensOnceAndReturnsExactScope(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantScope := &chat_svc.LocalCommandScope{DeviceID: "device-9", Cwd: "/remote/current"}
	resolveCalls := 0
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(_ context.Context, req *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			resolveCalls++
			assert.Equal(t, &chat_svc.ResolveLocalCommandScopeRequest{SessionID: 71}, req)
			return wantScope, nil
		},
	})

	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: "/remote/current", Command: "go test ./...", Cols: 100, Rows: 30,
	}).Return(newCompletedTerminalHandle(), nil).Times(1)
	factoryCalls := 0
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(deviceID string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			assert.Equal(t, "device-9", deviceID)
			return remoteBackend, nil
		}), terminal_svc.NoopEmitter{})
	defer svc.Shutdown()
	a := &App{ctx: context.Background(), terminalSvc: svc}

	response, err := a.TerminalRunCommand("terminal-1", 71, "go test ./...", 100, 30)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, *wantScope, response.Scope)
	assert.Empty(t, response.StartError)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, factoryCalls)
}

func TestApp_TerminalRunCommand_GivenOpenCommandFailure_WhenStarted_ThenReturnsScopeAndStartErrorWithoutRetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantScope := &chat_svc.LocalCommandScope{DeviceID: "device-9", Cwd: "/remote/current"}
	resolveCalls := 0
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			resolveCalls++
			return wantScope, nil
		},
	})

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
	defer svc.Shutdown()
	a := &App{ctx: context.Background(), terminalSvc: svc}

	response, err := a.TerminalRunCommand("terminal-2", 72, "make check", 80, 24)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, *wantScope, response.Scope)
	assert.Equal(t, startErr.Error(), response.StartError)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, factoryCalls)
}

func TestApp_TerminalRunCommand_GivenTargetResolutionFailure_WhenStarted_ThenReturnsRPCErrorWithoutOpening(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	resolveErr := errors.New("target resolution failed")
	resolveCalls := 0
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			resolveCalls++
			return nil, resolveErr
		},
	})

	localBackend := mocks.NewMockPTYBackend(ctrl)
	factoryCalls := 0
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			return mocks.NewMockPTYBackend(ctrl), nil
		}), terminal_svc.NoopEmitter{})
	defer svc.Shutdown()
	a := &App{ctx: context.Background(), terminalSvc: svc}

	response, err := a.TerminalRunCommand("terminal-3", 73, "pwd", 80, 24)

	assert.Nil(t, response)
	require.ErrorIs(t, err, resolveErr)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 0, factoryCalls)
}
