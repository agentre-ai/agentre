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

func registerTerminalChatService(t *testing.T, service chat_svc.ChatSvc) {
	t.Helper()
	previous := chat_svc.Chat()
	chat_svc.RegisterChat(service)
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

func TestApp_ResolveLocalCommandScope_GivenChatServiceUnset_WhenResolved_ThenReturnsRPCError(t *testing.T) {
	registerTerminalChatService(t, nil)

	var scope *chat_svc.LocalCommandScope
	var err error
	require.NotPanics(t, func() {
		scope, err = (&App{ctx: context.Background()}).ResolveLocalCommandScope(
			&chat_svc.ResolveLocalCommandScopeRequest{SessionID: 71},
		)
	})

	assert.Nil(t, scope)
	require.ErrorIs(t, err, terminal_svc.ErrCommandScopeResolverNotInitialized)
}

func TestApp_ResolveLocalCommandScope_GivenResolverReturnsNil_WhenResolved_ThenReturnsRPCError(t *testing.T) {
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			return nil, nil
		},
	})

	scope, err := (&App{ctx: context.Background()}).ResolveLocalCommandScope(
		&chat_svc.ResolveLocalCommandScopeRequest{SessionID: 71},
	)

	assert.Nil(t, scope)
	require.ErrorIs(t, err, terminal_svc.ErrCommandScopeUnavailable)
}

func TestApp_TerminalRunCommand_GivenProductionAdapterUnavailable_WhenCalled_ThenReturnsRPCErrorWithoutLaunch(t *testing.T) {
	tests := []struct {
		name    string
		service chat_svc.ChatSvc
		wantErr error
	}{
		{
			name:    "chat service is not initialized",
			wantErr: terminal_svc.ErrCommandScopeResolverNotInitialized,
		},
		{
			name: "chat service returns no scope",
			service: &terminalChatServiceStub{
				resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
					return nil, nil
				},
			},
			wantErr: terminal_svc.ErrCommandScopeUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registerTerminalChatService(t, tt.service)
			svc := newTerminalService(context.Background())
			defer svc.Shutdown()
			a := &App{ctx: context.Background(), terminalSvc: svc}

			var response *terminal_svc.RunCommandResponse
			var err error
			require.NotPanics(t, func() {
				response, err = a.TerminalRunCommand(
					"terminal-unavailable", 70, "private-token-command", 80, 24,
				)
			})
			assert.Nil(t, response)
			require.ErrorIs(t, err, tt.wantErr)
			require.ErrorIs(t, svc.Close(context.Background(), "terminal-unavailable"), terminal_svc.ErrTerminalNotOpen)
		})
	}
}

func TestApp_TerminalRunCommand_GivenServiceResolver_WhenCalled_ThenDelegatesWithoutBindingResolution(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	bindingResolveCalls := 0
	registerTerminalChatService(t, &terminalChatServiceStub{
		resolve: func(context.Context, *chat_svc.ResolveLocalCommandScopeRequest) (*chat_svc.LocalCommandScope, error) {
			bindingResolveCalls++
			return nil, errors.New("binding must not resolve command scope")
		},
	})

	data := make(chan []byte)
	close(data)
	exit := make(chan pty.ExitInfo, 1)
	exit <- pty.ExitInfo{}
	close(exit)
	mockHandle := mocks.NewMockHandle(ctrl)
	mockHandle.EXPECT().Data().AnyTimes().Return(data)
	mockHandle.EXPECT().Exit().AnyTimes().Return(exit)
	mockHandle.EXPECT().Close().AnyTimes().Return(nil)
	localBackend := mocks.NewMockPTYBackend(ctrl)
	localBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: "/local/current", Command: "go test ./...", Cols: 100, Rows: 30,
	}).Return(mockHandle, nil).Times(1)
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(localBackend, nil),
		terminal_svc.NoopEmitter{},
	)
	serviceResolveCalls := 0
	svc.SetCommandScopeResolver(func(
		_ context.Context,
		req terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		serviceResolveCalls++
		assert.Equal(t, terminal_svc.ResolveCommandScopeRequest{SessionID: 71}, req)
		return &terminal_svc.CommandScope{Cwd: "/local/current"}, nil
	})
	defer svc.Shutdown()
	a := &App{ctx: context.Background(), terminalSvc: svc}

	response, err := a.TerminalRunCommand("terminal-1", 71, "go test ./...", 100, 30)

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, terminal_svc.CommandScope{Cwd: "/local/current"}, response.Scope)
	assert.Empty(t, response.StartError)
	assert.Equal(t, 0, bindingResolveCalls)
	assert.Equal(t, 1, serviceResolveCalls)
}
