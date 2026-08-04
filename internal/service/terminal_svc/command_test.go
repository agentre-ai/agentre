package terminal_svc_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

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

func newCompletedCommandHandle(output []byte, exitInfo pty.ExitInfo) *completedCommandHandle {
	data := make(chan []byte, 1)
	if len(output) > 0 {
		data <- output
	}
	close(data)
	exit := make(chan pty.ExitInfo, 1)
	exit <- exitInfo
	close(exit)
	return &completedCommandHandle{data: data, exit: exit}
}

func (h *completedCommandHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *completedCommandHandle) Resize(uint16, uint16) error { return nil }
func (h *completedCommandHandle) Close() error                { return nil }
func (h *completedCommandHandle) Data() <-chan []byte         { return h.data }
func (h *completedCommandHandle) Exit() <-chan pty.ExitInfo   { return h.exit }

func TestService_RunCommand_GivenInvalidRequest_WhenStarted_ThenRejectsBeforeResolutionOpenOrLogging(t *testing.T) {
	validRequest := terminal_svc.RunCommandRequest{
		TerminalID: "terminal-valid",
		SessionID:  71,
		Command:    "go test ./...",
		Cols:       100,
		Rows:       30,
	}
	tests := []struct {
		name   string
		mutate func(*terminal_svc.RunCommandRequest)
	}{
		{name: "empty terminal ID", mutate: func(req *terminal_svc.RunCommandRequest) { req.TerminalID = "" }},
		{name: "whitespace terminal ID", mutate: func(req *terminal_svc.RunCommandRequest) { req.TerminalID = " \t\n" }},
		{name: "zero session ID", mutate: func(req *terminal_svc.RunCommandRequest) { req.SessionID = 0 }},
		{name: "negative session ID", mutate: func(req *terminal_svc.RunCommandRequest) { req.SessionID = -1 }},
		{name: "empty command", mutate: func(req *terminal_svc.RunCommandRequest) { req.Command = "" }},
		{name: "whitespace command", mutate: func(req *terminal_svc.RunCommandRequest) { req.Command = " \t\n" }},
		{name: "zero columns", mutate: func(req *terminal_svc.RunCommandRequest) { req.Cols = 0 }},
		{name: "zero rows", mutate: func(req *terminal_svc.RunCommandRequest) { req.Rows = 0 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			request := validRequest
			tt.mutate(&request)
			openCalls := 0
			localBackend := mocks.NewMockPTYBackend(ctrl)
			localBackend.EXPECT().Open(gomock.Any(), gomock.Any()).DoAndReturn(
				func(context.Context, pty.Spec) (pty.Handle, error) {
					openCalls++
					return nil, errors.New("invalid request reached Open")
				},
			).AnyTimes()
			svc := terminal_svc.NewService(
				terminal_svc.NewBackendSelector(localBackend, nil),
				terminal_svc.NoopEmitter{},
			)
			resolveCalls := 0
			svc.SetCommandScopeResolver(func(
				context.Context,
				terminal_svc.ResolveCommandScopeRequest,
			) (*terminal_svc.CommandScope, error) {
				resolveCalls++
				return &terminal_svc.CommandScope{}, nil
			})
			defer svc.Shutdown()
			core, logs := observer.New(zapcore.DebugLevel)
			ctx := logger.WithContextLogger(context.Background(), zap.New(core))

			response, err := svc.RunCommand(ctx, request)

			assert.Nil(t, response)
			assert.ErrorIs(t, err, terminal_svc.ErrInvalidRunCommandRequest)
			assert.Equal(t, 0, resolveCalls)
			assert.Equal(t, 0, openCalls)
			assert.Equal(t, 0, logs.Len())
		})
	}
}

func TestService_RunCommand_GivenResolvedTarget_WhenCommandExits_ThenLogsOneRedactedStartAndExit(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sensitiveCwd := "/Users/alice/private-worktree"
	sensitiveCommand := "deploy --token=fixture-sensitive-token"
	sensitiveOutput := []byte("output with fixture-sensitive-output")
	sensitiveExitMessage := "exit detail with fixture-sensitive-message"
	wantScope := &terminal_svc.CommandScope{DeviceID: "device-9", Cwd: sensitiveCwd}
	resolveCalls := 0
	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: sensitiveCwd, Command: sensitiveCommand, Cols: 100, Rows: 30,
	}).Return(newCompletedCommandHandle(sensitiveOutput, pty.ExitInfo{
		Code: 17, Reason: "natural", Msg: sensitiveExitMessage,
	}), nil).Times(1)
	factoryCalls := 0
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(deviceID string) (terminal_svc.PTYBackend, error) {
			factoryCalls++
			assert.Equal(t, "device-9", deviceID)
			return remoteBackend, nil
		}), emitter)
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
		Command:    sensitiveCommand,
		Cols:       100,
		Rows:       30,
	})

	require.NoError(t, err)
	require.NotNil(t, response)
	assert.Equal(t, *wantScope, response.Scope)
	assert.Empty(t, response.StartError)
	assert.Equal(t, 1, resolveCalls)
	assert.Equal(t, 1, factoryCalls)
	require.Eventually(t, func() bool {
		exitEvents := 0
		for _, event := range emitter.Snapshot() {
			if event.Name == terminal_svc.ExitEventName("terminal-1") {
				exitEvents++
			}
		}
		return logs.Len() == 2 && exitEvents == 1
	}, time.Second, time.Millisecond)

	entries := logs.All()
	require.Len(t, entries, 2)
	assert.Equal(t, zapcore.InfoLevel, entries[0].Level)
	assert.Equal(t, "terminal_svc.RunCommand: command started", entries[0].Message)
	assert.Equal(t, map[string]any{
		"sessionId":  int64(71),
		"terminalId": "terminal-1",
		"deviceId":   "device-9",
	}, entries[0].ContextMap())
	assert.Equal(t, zapcore.InfoLevel, entries[1].Level)
	assert.Equal(t, "terminal_svc.RunCommand: command exited", entries[1].Message)
	assert.Equal(t, map[string]any{
		"sessionId":  int64(71),
		"terminalId": "terminal-1",
		"deviceId":   "device-9",
		"exitCode":   int64(17),
		"exitReason": "natural",
	}, entries[1].ContextMap())
	structuredLogs, marshalErr := json.Marshal(entries)
	require.NoError(t, marshalErr)
	observedLogs := string(structuredLogs)
	for _, sensitive := range []string{
		sensitiveCommand, "fixture-sensitive-token", sensitiveCwd,
		string(sensitiveOutput), "fixture-sensitive-output", sensitiveExitMessage, "fixture-sensitive-message",
	} {
		assert.NotContains(t, observedLogs, sensitive)
	}
}

func TestService_Open_GivenInteractiveTerminal_WhenItExits_ThenLogsNoCommandLifecycle(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	localBackend := mocks.NewMockPTYBackend(ctrl)
	localBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: "/private/interactive-cwd", Cols: 80, Rows: 24,
	}).Return(newCompletedCommandHandle([]byte("private interactive output"), pty.ExitInfo{
		Code: 0, Reason: "natural", Msg: "private interactive exit detail",
	}), nil).Times(1)
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(localBackend, nil),
		emitter,
	)
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	require.NoError(t, svc.Open(ctx, "interactive-1", "", "/private/interactive-cwd", 80, 24))
	require.Eventually(t, func() bool {
		for _, event := range emitter.Snapshot() {
			if event.Name == terminal_svc.ExitEventName("interactive-1") {
				return true
			}
		}
		return false
	}, time.Second, time.Millisecond)
	assert.Zero(t, logs.Len())
}

func TestService_RunCommand_GivenOpenFailure_WhenStarted_ThenReturnsExactScopeStartErrorAndOneRedactedWarning(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	sensitiveCwd := "/Users/alice/private-worktree"
	sensitiveCommand := "deploy --token=fixture-sensitive-token"
	sensitiveShell := "/opt/private/bin/zsh"
	wantScope := &terminal_svc.CommandScope{DeviceID: "device-9", Cwd: sensitiveCwd}
	resolveCalls := 0
	startErr := errors.New("fork/exec " + sensitiveShell + " in " + sensitiveCwd +
		" while starting " + sensitiveCommand + ": permission denied")
	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: sensitiveCwd, Command: sensitiveCommand, Cols: 80, Rows: 24,
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
		Command:    sensitiveCommand,
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
	assert.Zero(t, logs.FilterMessage("terminal_svc.RunCommand: command started").Len())
	assert.Zero(t, logs.FilterMessage("terminal_svc.RunCommand: command exited").Len())
	entry := logs.All()[0]
	assert.Equal(t, zapcore.WarnLevel, entry.Level)
	assert.Equal(t, "terminal_svc.RunCommand: open command failed", entry.Message)
	assert.Equal(t, map[string]any{
		"sessionId":  int64(72),
		"terminalId": "terminal-2",
		"deviceId":   "device-9",
		"errorClass": "terminalCommandStartFailed",
	}, entry.ContextMap())
	structuredFields, marshalErr := json.Marshal(entry.ContextMap())
	require.NoError(t, marshalErr)
	observedLog := entry.Message + string(structuredFields)
	for _, sensitive := range []string{sensitiveCommand, "fixture-sensitive-token", sensitiveCwd, sensitiveShell} {
		assert.NotContains(t, observedLog, sensitive)
	}
}

func TestService_RunCommand_GivenClosePreemptsCancellationIgnoringOpen_WhenBackendReturnsHandle_ThenReturnsScopedStartErrorWithoutLifecycleEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	wantScope := &terminal_svc.CommandScope{
		DeviceID: "device-9",
		Cwd:      "/Users/alice/private-worktree",
	}
	sensitiveCommand := "deploy --token=fixture-sensitive-token"
	localBackend := mocks.NewMockPTYBackend(ctrl)
	remoteBackend := mocks.NewMockPTYBackend(ctrl)
	handle := mocks.NewMockHandle(ctrl)
	openCtxCh := make(chan context.Context, 1)
	proceed := make(chan struct{})
	remoteBackend.EXPECT().Open(gomock.Any(), pty.Spec{
		Cwd: wantScope.Cwd, Command: sensitiveCommand, Cols: 80, Rows: 24,
	}).DoAndReturn(func(openCtx context.Context, _ pty.Spec) (pty.Handle, error) {
		openCtxCh <- openCtx
		<-proceed
		return handle, nil
	}).Times(1)
	handle.EXPECT().Close().Return(nil).Times(1)

	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(localBackend,
		func(deviceID string) (terminal_svc.PTYBackend, error) {
			assert.Equal(t, wantScope.DeviceID, deviceID)
			return remoteBackend, nil
		}), emitter)
	svc.SetCommandScopeResolver(func(
		context.Context,
		terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		return wantScope, nil
	})
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	type runResult struct {
		response *terminal_svc.RunCommandResponse
		err      error
	}
	resultCh := make(chan runResult, 1)
	go func() {
		response, err := svc.RunCommand(ctx, terminal_svc.RunCommandRequest{
			TerminalID: "terminal-preempted",
			SessionID:  72,
			Command:    sensitiveCommand,
			Cols:       80,
			Rows:       24,
		})
		resultCh <- runResult{response: response, err: err}
	}()

	openCtx := <-openCtxCh
	require.NoError(t, svc.Close(context.Background(), "terminal-preempted"))
	require.ErrorIs(t, openCtx.Err(), context.Canceled)
	close(proceed)
	result := <-resultCh

	require.NoError(t, result.err)
	require.NotNil(t, result.response)
	assert.Equal(t, *wantScope, result.response.Scope)
	assert.Equal(t, terminal_svc.ErrCommandStartPreempted.Error(), result.response.StartError)
	var preempted terminal_svc.CommandStartPreemptedError
	assert.ErrorAs(t, terminal_svc.ErrCommandStartPreempted, &preempted)
	assert.Empty(t, emitter.Snapshot())
	assert.Zero(t, logs.Len())
	assert.ErrorIs(t, svc.Write(context.Background(), "terminal-preempted", "x"), terminal_svc.ErrTerminalClosed)
}

// countingCommandBackend records every backend.Open boundary while returning a
// completed handle so a regression cannot strand the test in a pump goroutine.
type countingCommandBackend struct {
	opens atomic.Int32
}

func (b *countingCommandBackend) Open(context.Context, pty.Spec) (pty.Handle, error) {
	b.opens.Add(1)
	return newCompletedCommandHandle(nil, pty.ExitInfo{Code: 0, Reason: "natural"}), nil
}

type runCommandResult struct {
	response *terminal_svc.RunCommandResponse
	err      error
}

func startRunCommand(
	ctx context.Context,
	svc *terminal_svc.Service,
	req terminal_svc.RunCommandRequest,
) <-chan runCommandResult {
	resultCh := make(chan runCommandResult, 1)
	go func() {
		response, err := svc.RunCommand(ctx, req)
		resultCh <- runCommandResult{response: response, err: err}
	}()
	return resultCh
}

func TestService_RunCommand_GivenResolverBlocks_WhenClosedBeforeScopeExists_ThenCancelsAttemptAndNeverOpensBackend(t *testing.T) {
	backend := &countingCommandBackend{}
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(backend, nil),
		emitter,
	)
	resolverStarted := make(chan context.Context, 1)
	releaseResolver := make(chan struct{})
	svc.SetCommandScopeResolver(func(
		resolveCtx context.Context,
		_ terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		resolverStarted <- resolveCtx
		<-releaseResolver // deliberately ignore cancellation
		return &terminal_svc.CommandScope{Cwd: "/private/resolved-too-late"}, nil
	})
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	resultCh := startRunCommand(ctx, svc, terminal_svc.RunCommandRequest{
		TerminalID: "terminal-resolver-stop",
		SessionID:  81,
		Command:    "private command",
		Cols:       80,
		Rows:       24,
	})
	resolveCtx := <-resolverStarted
	closeErr := svc.Close(context.Background(), "terminal-resolver-stop")
	ctxErr := resolveCtx.Err()
	close(releaseResolver)
	result := <-resultCh

	require.NoError(t, closeErr)
	assert.ErrorIs(t, ctxErr, context.Canceled)
	assert.Nil(t, result.response)
	assert.ErrorIs(t, result.err, terminal_svc.ErrCommandStartPreempted)
	var preempted terminal_svc.CommandStartPreemptedError
	assert.ErrorAs(t, result.err, &preempted)
	assert.Zero(t, backend.opens.Load())
	assert.Empty(t, emitter.Snapshot())
	assert.Zero(t, logs.Len())
}

func TestService_RunCommand_GivenSelectorBlocks_WhenClosedAfterScopeExists_ThenReturnsScopedPreemptionWithoutBackendOpen(t *testing.T) {
	backend := &countingCommandBackend{}
	factoryCalls := atomic.Int32{}
	selectorStarted := make(chan struct{})
	releaseSelector := make(chan struct{})
	wantScope := &terminal_svc.CommandScope{
		DeviceID: "device-blocked-selector",
		Cwd:      "/private/exact-scope",
	}
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(terminal_svc.NewBackendSelector(backend,
		func(string) (terminal_svc.PTYBackend, error) {
			factoryCalls.Add(1)
			close(selectorStarted)
			<-releaseSelector // selector has no context boundary
			return backend, nil
		}), emitter)
	svc.SetCommandScopeResolver(func(
		context.Context,
		terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		return wantScope, nil
	})
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	resultCh := startRunCommand(ctx, svc, terminal_svc.RunCommandRequest{
		TerminalID: "terminal-selector-stop",
		SessionID:  82,
		Command:    "private command",
		Cols:       80,
		Rows:       24,
	})
	<-selectorStarted
	closeErr := svc.Close(context.Background(), "terminal-selector-stop")
	close(releaseSelector)
	result := <-resultCh

	require.NoError(t, closeErr)
	require.NoError(t, result.err)
	require.NotNil(t, result.response)
	assert.Equal(t, *wantScope, result.response.Scope)
	assert.Equal(t, terminal_svc.ErrCommandStartPreempted.Error(), result.response.StartError)
	assert.Equal(t, int32(1), factoryCalls.Load())
	assert.Zero(t, backend.opens.Load())
	assert.Empty(t, emitter.Snapshot())
	assert.Zero(t, logs.Len())
}

type blockingCloseHandle struct {
	data         chan []byte
	exit         chan pty.ExitInfo
	closeStarted chan struct{}
	releaseClose chan struct{}
	closeOnce    sync.Once
}

func newBlockingCloseHandle() *blockingCloseHandle {
	return &blockingCloseHandle{
		data:         make(chan []byte),
		exit:         make(chan pty.ExitInfo, 1),
		closeStarted: make(chan struct{}),
		releaseClose: make(chan struct{}),
	}
}

func (h *blockingCloseHandle) Write(p []byte) (int, error) { return len(p), nil }
func (h *blockingCloseHandle) Resize(uint16, uint16) error { return nil }
func (h *blockingCloseHandle) Data() <-chan []byte         { return h.data }
func (h *blockingCloseHandle) Exit() <-chan pty.ExitInfo   { return h.exit }
func (h *blockingCloseHandle) Close() error {
	h.closeOnce.Do(func() {
		close(h.closeStarted)
		<-h.releaseClose
		h.exit <- pty.ExitInfo{Code: 0, Reason: "closed"}
		close(h.exit)
		close(h.data)
	})
	return nil
}

type evictionBlockingBackend struct {
	old          pty.Handle
	commandOpens atomic.Int32
}

func (b *evictionBlockingBackend) Open(_ context.Context, spec pty.Spec) (pty.Handle, error) {
	if spec.Command == "" {
		return b.old, nil
	}
	b.commandOpens.Add(1)
	return newCompletedCommandHandle(nil, pty.ExitInfo{Code: 0, Reason: "natural"}), nil
}

func TestService_RunCommand_GivenExistingHandleEvictionBlocks_WhenClosedInGap_ThenNeverLaunchesCommand(t *testing.T) {
	oldHandle := newBlockingCloseHandle()
	backend := &evictionBlockingBackend{old: oldHandle}
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(backend, nil),
		terminal_svc.NoopEmitter{},
	)
	wantScope := &terminal_svc.CommandScope{Cwd: "/private/exact-scope"}
	svc.SetCommandScopeResolver(func(
		context.Context,
		terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		return wantScope, nil
	})
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))

	require.NoError(t, svc.Open(ctx, "terminal-eviction-stop", "", "/private/old", 80, 24))
	resultCh := startRunCommand(ctx, svc, terminal_svc.RunCommandRequest{
		TerminalID: "terminal-eviction-stop",
		SessionID:  83,
		Command:    "private command",
		Cols:       80,
		Rows:       24,
	})
	<-oldHandle.closeStarted
	closeErr := svc.Close(context.Background(), "terminal-eviction-stop")
	close(oldHandle.releaseClose)
	result := <-resultCh

	require.NoError(t, closeErr)
	require.NoError(t, result.err)
	require.NotNil(t, result.response)
	assert.Equal(t, *wantScope, result.response.Scope)
	assert.Equal(t, terminal_svc.ErrCommandStartPreempted.Error(), result.response.StartError)
	assert.Zero(t, backend.commandOpens.Load())
	assert.Zero(t, logs.Len())
}

func TestService_RunCommand_GivenOlderResolverBlocks_WhenNewerRunClaimsSameID_ThenOnlyNewerLaunches(t *testing.T) {
	backend := &countingCommandBackend{}
	emitter := &recordingEmitter{}
	svc := terminal_svc.NewService(
		terminal_svc.NewBackendSelector(backend, nil),
		emitter,
	)
	resolverCalls := atomic.Int32{}
	olderStarted := make(chan context.Context, 1)
	releaseOlder := make(chan struct{})
	wantScope := &terminal_svc.CommandScope{Cwd: "/private/exact-scope"}
	svc.SetCommandScopeResolver(func(
		resolveCtx context.Context,
		_ terminal_svc.ResolveCommandScopeRequest,
	) (*terminal_svc.CommandScope, error) {
		if resolverCalls.Add(1) == 1 {
			olderStarted <- resolveCtx
			<-releaseOlder
		}
		return wantScope, nil
	})
	defer svc.Shutdown()
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))
	req := terminal_svc.RunCommandRequest{
		TerminalID: "terminal-newer-wins",
		SessionID:  84,
		Command:    "private command",
		Cols:       80,
		Rows:       24,
	}

	olderResultCh := startRunCommand(ctx, svc, req)
	olderCtx := <-olderStarted
	newerResponse, newerErr := svc.RunCommand(ctx, req)
	olderCtxErr := olderCtx.Err()
	close(releaseOlder)
	olderResult := <-olderResultCh

	require.NoError(t, newerErr)
	require.NotNil(t, newerResponse)
	assert.Empty(t, newerResponse.StartError)
	assert.ErrorIs(t, olderCtxErr, context.Canceled)
	assert.Nil(t, olderResult.response)
	assert.ErrorIs(t, olderResult.err, terminal_svc.ErrCommandStartPreempted)
	assert.Equal(t, int32(1), backend.opens.Load())
	require.Eventually(t, func() bool {
		return logs.Len() == 2 && len(emitter.Snapshot()) == 1
	}, time.Second, time.Millisecond)
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
