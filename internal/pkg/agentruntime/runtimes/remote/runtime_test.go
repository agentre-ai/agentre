package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/mock_agentruntime"
	piagentrt "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// Compile-time guard: *Runtime must satisfy the full Runtime contract +
// every optional sub-interface. Adding a new sub-interface to agentruntime
// without implementing it here fails the build instead of silently being
// downgraded by chat_svc's type assertions.
var (
	_ agentruntime.Runtime              = (*Runtime)(nil)
	_ agentruntime.Steerer              = (*Runtime)(nil)
	_ agentruntime.SteerCanceler        = (*Runtime)(nil)
	_ agentruntime.SteerDrainer         = (*Runtime)(nil)
	_ agentruntime.Aborter              = (*Runtime)(nil)
	_ agentruntime.PermissionModeSetter = (*Runtime)(nil)
	_ agentruntime.AskAnswerSink        = (*Runtime)(nil)
	_ agentruntime.ToolPermissionSink   = (*Runtime)(nil)
	_ agentruntime.GoalController       = (*Runtime)(nil)
	_ piagentrt.RunPreparer             = (*Runtime)(nil)
)

// handlerCapture grabs the Handle("runtime.event"|"runtime.runResultDone")
// callbacks that *Runtime registers on the conn during New(), so tests can
// drive server-push notifications synchronously.
type handlerCapture struct {
	mu    sync.Mutex
	funcs map[string]func(context.Context, json.RawMessage) (any, error)
}

func newHandlerCapture() *handlerCapture {
	return &handlerCapture{funcs: map[string]func(context.Context, json.RawMessage) (any, error){}}
}

func (h *handlerCapture) record(method string, fn func(context.Context, json.RawMessage) (any, error)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.funcs[method] = fn
}

func (h *handlerCapture) deliver(t *testing.T, method string, payload any) {
	t.Helper()
	h.mu.Lock()
	fn, ok := h.funcs[method]
	h.mu.Unlock()
	require.True(t, ok, "no handler captured for %s", method)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = fn(context.Background(), raw)
	require.NoError(t, err)
}

func setupRemote(t *testing.T) (
	*gomock.Controller,
	*mock_agentruntime.MockDaemonClientPort,
	*handlerCapture,
	*Runtime,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	cli := mock_agentruntime.NewMockDaemonClientPort(ctrl)
	capture := newHandlerCapture()
	cli.EXPECT().Handle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(method string, fn func(context.Context, json.RawMessage) (any, error)) {
			capture.record(method, fn)
		}).AnyTimes()
	// Closed() 由 New() 调用一次起 watchClose goroutine;返回 nil 等价于"不监
	// 听断连"——单测不需要触发断连分支,默认走纯 RPC 路径。
	cli.EXPECT().Closed().Return(nil).AnyTimes()
	rt := New(cli)
	return ctrl, cli, capture, rt
}

// ── Run ─────────────────────────────────────────────────────────────────────

func TestPrepareRun_PiExposesIdentityBeforePromptAndStartsThroughExistingRunRPC(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)

	var (
		mu       sync.Mutex
		runCalls []wire.RunParams
	)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp := params.(wire.RunParams)
			mu.Lock()
			runCalls = append(runCalls, rp)
			call := len(runCalls)
			mu.Unlock()
			switch call {
			case 1:
				assert.Equal(t, "pi-session-old", rp.ProviderSessionID)
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2:
				assert.Equal(t, "pi-session-old", rp.ProviderSessionID)
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "pi-session-new"}
			case 3:
				assert.Equal(t, "pi-session-new", rp.ProviderSessionID,
					"Start must identify the exact prepared generation through the existing provider-session form")
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "pi-session-new"}
			default:
				t.Fatalf("unexpected runtime.run call %d", call)
			}
			return nil
		}).Times(3)

	prepared, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID:         42,
		ProviderSessionID: "pi-session-old",
		ForkAnchor:        "pi-entry-1",
		UserText:          "replacement",
	})
	require.NoError(t, err)
	identity, ok := prepared.(piagentrt.PreparedRunIdentity)
	require.True(t, ok)
	assert.Equal(t, "pi-session-new", identity.ProviderSessionID())
	mu.Lock()
	assert.Len(t, runCalls, 2, "registration + preparation must return before the prompt-start RPC")
	mu.Unlock()

	events, result, err := prepared.Start(context.Background())
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "pi-session-new", result.ProviderSessionID)
	mu.Lock()
	assert.Len(t, runCalls, 3)
	require.NotEmpty(t, runCalls[0].PermissionMode)
	assert.Equal(t, runCalls[0].PermissionMode, runCalls[1].PermissionMode)
	assert.Equal(t, runCalls[1].PermissionMode, runCalls[2].PermissionMode)
	mu.Unlock()

	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 42, ProviderSessionID: "pi-session-new",
	})
	_, ok = <-events
	assert.False(t, ok)
}

func TestPrepareRun_PreAckBurstReturnsAckBeforeConsumerDrainsAndPreservesOrder(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)
	const burst = 96

	call := 0
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			switch call {
			case 1:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "pi-session-burst"}
			case 3:
				for i := 0; i < burst; i++ {
					capture.deliver(t, wire.NotifyEvent, wire.EventFrame{
						SessionID: rp.SessionID,
						Event:     mustRawFrame(t, agentruntime.TextDelta{Text: fmt.Sprintf("event-%03d", i)}),
					})
				}
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "pi-session-burst"}
			default:
				t.Fatalf("unexpected runtime.run call %d", call)
			}
			return nil
		}).Times(3)

	prepared, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID: 164,
	})
	require.NoError(t, err)

	type startResult struct {
		events <-chan agentruntime.Event
		result *agentruntime.RunResult
		err    error
	}
	started := make(chan startResult, 1)
	go func() {
		events, result, startErr := prepared.Start(context.Background())
		started <- startResult{events: events, result: result, err: startErr}
	}()

	var got startResult
	select {
	case got = <-started:
	case <-time.After(time.Second):
		// Release the historical direct-send deadlock so the failed RED leaves no
		// parked goroutine or mock expectation behind.
		rt.mu.RLock()
		blocked := rt.sessions[int64(164)]
		rt.mu.RUnlock()
		require.NotNil(t, blocked)
		go func() {
			for i := 0; i < burst; i++ {
				<-blocked.events
			}
		}()
		got = <-started
		capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
			SessionID: 164, ProviderSessionID: "pi-session-burst",
		})
		t.Fatal("Start acknowledgement was blocked by the pre-ack event burst")
	}
	require.NoError(t, got.err)
	require.NotNil(t, got.result)

	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 164, ProviderSessionID: "pi-session-burst",
	})
	for i := 0; i < burst; i++ {
		event, open := <-got.events
		require.True(t, open, "event stream closed at %d/%d", i, burst)
		assert.Equal(t, agentruntime.TextDelta{Text: fmt.Sprintf("event-%03d", i)}, event)
	}
	_, open := <-got.events
	assert.False(t, open)
}

func TestPrepareRun_SlowConsumerOverflowCancelsExactGenerationAndAllowsRetry(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)
	const (
		sessionID = int64(165)
		burst     = 512
		secret    = "private-overflow-event-payload"
	)

	var call int
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			switch call {
			case 1, 4:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2, 3, 5, 6:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "shared-native-session"}
			default:
				t.Fatalf("unexpected runtime.run call %d", call)
			}
			return nil
		}).Times(6)
	abortCalled := make(chan struct{})
	var abortOnce sync.Once
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: sessionID}, gomock.Any()).
		DoAndReturn(func(context.Context, string, any, any) error {
			abortOnce.Do(func() { close(abortCalled) })
			return nil
		})

	first, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID: sessionID,
	})
	require.NoError(t, err)
	firstEvents, firstResult, err := first.Start(context.Background())
	require.NoError(t, err)

	deliveryDone := make(chan struct{})
	go func() {
		defer close(deliveryDone)
		for i := 0; i < burst; i++ {
			capture.deliver(t, wire.NotifyEvent, wire.EventFrame{
				SessionID: sessionID,
				Event:     mustRawFrame(t, agentruntime.TextDelta{Text: fmt.Sprintf("%s-%03d", secret, i)}),
			})
		}
	}()

	overflowed := true
	select {
	case <-abortCalled:
	case <-time.After(time.Second):
		overflowed = false
		go func() {
			for range firstEvents {
			}
		}()
		<-deliveryDone
		require.NoError(t, first.Close(context.Background()))
	}
	if !overflowed {
		t.Fatal("a non-consuming caller blocked event delivery instead of canceling the exact generation")
	}
	<-deliveryDone
	for range firstEvents {
	}
	require.Error(t, firstResult.StopErr)
	assert.Equal(t, "remote runtime: event delivery exceeded bounded buffer", firstResult.StopErr.Error())
	assert.NotContains(t, firstResult.StopErr.Error(), secret)
	assert.ErrorIs(t, rt.Abort(context.Background(), sessionID), agentruntime.ErrNoActiveTurn)

	// A stale frame observed after exact-owner finalization is dropped before a
	// same-SessionID retry is installed.
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{
		SessionID: sessionID,
		Event:     mustRawFrame(t, agentruntime.TextDelta{Text: secret + "-late"}),
	})
	second, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID: sessionID,
	})
	require.NoError(t, err)
	secondEvents, secondResult, err := second.Start(context.Background())
	require.NoError(t, err)
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{
		SessionID: sessionID,
		Event:     mustRawFrame(t, agentruntime.TextDelta{Text: "retry-current"}),
	})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: sessionID, ProviderSessionID: "shared-native-session", Model: "retry-model",
	})
	event, open := <-secondEvents
	require.True(t, open)
	assert.Equal(t, agentruntime.TextDelta{Text: "retry-current"}, event)
	_, open = <-secondEvents
	assert.False(t, open)
	assert.Equal(t, "retry-model", secondResult.Model)
}

func TestPrepareRun_StopDuringRegistrationWaitsForOwnerAckThenAborts(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	registrationEntered := make(chan struct{})
	allowRegistration := make(chan struct{})
	abortCalled := make(chan struct{})

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			close(registrationEntered)
			<-allowRegistration
			rp := params.(wire.RunParams)
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			return nil
		})
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 72}, gomock.Any()).
		DoAndReturn(func(context.Context, string, any, any) error {
			close(abortCalled)
			return nil
		})

	prepareErrC := make(chan error, 1)
	go func() {
		_, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
			SessionID: 72,
		})
		prepareErrC <- err
	}()
	<-registrationEntered

	abortStarted := make(chan struct{})
	abortErrC := make(chan error, 1)
	go func() {
		close(abortStarted)
		abortErrC <- rt.Abort(context.Background(), 72)
	}()
	<-abortStarted
	select {
	case <-abortCalled:
		t.Fatal("Abort reached agentred before the registration owner was acknowledged")
	default:
	}
	close(allowRegistration)

	require.NoError(t, <-abortErrC)
	require.ErrorIs(t, <-prepareErrC, context.Canceled)
	<-abortCalled
}

func TestPrepareRun_PendingPiAbortCancelsTheRegisteredGeneration(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	entered := make(chan struct{})
	call := 0

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			if call == 1 {
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
				return nil
			}
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}).Times(2)
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 73}, gomock.Any()).
		Return(nil)

	errC := make(chan error, 1)
	go func() {
		_, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
			SessionID: 73,
		})
		errC <- err
	}()
	<-entered

	require.NoError(t, rt.Abort(context.Background(), 73))
	require.ErrorIs(t, <-errC, context.Canceled)
	assert.ErrorIs(t, rt.Abort(context.Background(), 73), agentruntime.ErrNoActiveTurn)
}

func TestPrepareRun_RequestCancellationAbortsPendingDaemonGeneration(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	entered := make(chan struct{})
	abortCalled := make(chan struct{})
	call := 0

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			if call == 1 {
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
				return nil
			}
			close(entered)
			<-ctx.Done()
			return ctx.Err()
		}).Times(2)
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 74}, gomock.Any()).
		DoAndReturn(func(context.Context, string, any, any) error {
			close(abortCalled)
			return nil
		})

	ctx, cancel := context.WithCancel(context.Background())
	errC := make(chan error, 1)
	go func() {
		_, err := rt.PrepareRun(ctx, agentruntime.RunRequest{
			Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
			SessionID: 74,
		})
		errC <- err
	}()
	<-entered
	cancel()

	require.ErrorIs(t, <-errC, context.Canceled)
	<-abortCalled
	assert.ErrorIs(t, rt.Abort(context.Background(), 74), agentruntime.ErrNoActiveTurn)
}

func TestPrepareRun_StartCancellationAbortsPromptAcknowledgement(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	startEntered := make(chan struct{})
	abortCalled := make(chan struct{})
	call := 0

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			switch call {
			case 1:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
				return nil
			case 2:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "pi-session-new"}
				return nil
			default:
				close(startEntered)
				<-ctx.Done()
				return ctx.Err()
			}
		}).Times(3)
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 75}, gomock.Any()).
		DoAndReturn(func(context.Context, string, any, any) error {
			close(abortCalled)
			return nil
		})

	prepared, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID: 75,
	})
	require.NoError(t, err)
	startCtx, cancelStart := context.WithCancel(context.Background())
	startErrC := make(chan error, 1)
	go func() {
		_, _, err := prepared.Start(startCtx)
		startErrC <- err
	}()
	<-startEntered
	cancelStart()

	require.ErrorIs(t, <-startErrC, context.Canceled)
	<-abortCalled
	assert.ErrorIs(t, rt.Abort(context.Background(), 75), agentruntime.ErrNoActiveTurn)
}

func TestPrepareRun_CurrentPiCompletionIsNotConsumedWhenAbortedOwnerEmitsNoTerminalFrame(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)

	call := 0
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			call++
			rp := params.(wire.RunParams)
			switch call {
			case 1, 4:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2, 3, 5, 6:
				*(result.(*wire.RunAck)) = wire.RunAck{
					SessionID: rp.SessionID, ProviderSessionID: "shared-native-session",
				}
			default:
				t.Fatalf("unexpected runtime.run call %d", call)
			}
			return nil
		}).Times(6)
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 83}, gomock.Any()).Return(nil)

	first, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID:         83,
		ProviderSessionID: "shared-native-session",
	})
	require.NoError(t, err)
	firstEvents, _, err := first.Start(context.Background())
	require.NoError(t, err)
	require.NoError(t, first.Close(context.Background()))
	_, open := <-firstEvents
	assert.False(t, open)

	second, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID:         83,
		ProviderSessionID: "shared-native-session",
	})
	require.NoError(t, err)
	secondEvents, result, err := second.Start(context.Background())
	require.NoError(t, err)

	currentEvent, err := json.Marshal(agentruntime.TextDelta{Text: "current generation"})
	require.NoError(t, err)
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: 83, Event: currentEvent})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 83, ProviderSessionID: "shared-native-session", Model: "current-model",
	})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 83, ProviderSessionID: "shared-native-session", Model: "duplicate-model",
	})

	select {
	case event, ok := <-secondEvents:
		require.True(t, ok)
		assert.Equal(t, agentruntime.TextDelta{Text: "current generation"}, event)
	case <-time.After(time.Second):
		t.Fatal("current generation event was consumed by the aborted owner")
	}
	_, open = <-secondEvents
	assert.False(t, open)
	assert.Equal(t, "current-model", result.Model)
}

func TestPrepareRun_StaleCloseCannotOverlapNewRegistrationWhenTerminalIdentityIsEmpty(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)
	abortEntered := make(chan struct{})
	allowAbort := make(chan struct{})
	secondRegistrationEntered := make(chan struct{})
	var (
		mu   sync.Mutex
		call int
	)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			mu.Lock()
			call++
			currentCall := call
			mu.Unlock()
			rp := params.(wire.RunParams)
			switch currentCall {
			case 1, 4:
				if currentCall == 4 {
					close(secondRegistrationEntered)
				}
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2, 3, 5, 6:
				*(result.(*wire.RunAck)) = wire.RunAck{
					SessionID: rp.SessionID, ProviderSessionID: "shared-native-session",
				}
			default:
				t.Fatalf("unexpected runtime.run call %d", currentCall)
			}
			return nil
		}).Times(6)
	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, wire.AbortParams{SessionID: 82}, gomock.Any()).
		DoAndReturn(func(context.Context, string, any, any) error {
			close(abortEntered)
			<-allowAbort
			return nil
		})

	first, err := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
		Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
		SessionID:         82,
		ProviderSessionID: "shared-native-session",
	})
	require.NoError(t, err)
	firstEvents, _, err := first.Start(context.Background())
	require.NoError(t, err)

	closeErrC := make(chan error, 1)
	go func() { closeErrC <- first.Close(context.Background()) }()
	<-abortEntered
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 82, Model: "completed-before-abort-response",
	})
	_, open := <-firstEvents
	assert.False(t, open)

	preparedC := make(chan piagentrt.PreparedRun, 1)
	errC := make(chan error, 1)
	go func() {
		prepared, prepareErr := rt.PrepareRun(context.Background(), agentruntime.RunRequest{
			Backend:           &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent)},
			SessionID:         82,
			ProviderSessionID: "shared-native-session",
		})
		preparedC <- prepared
		errC <- prepareErr
	}()

	registeredBeforeAbortSettled := false
	select {
	case <-secondRegistrationEntered:
		registeredBeforeAbortSettled = true
	case <-time.After(100 * time.Millisecond):
	}
	close(allowAbort)
	require.NoError(t, <-closeErrC)
	assert.False(t, registeredBeforeAbortSettled,
		"a stale Close must finish its exact abort before a retry can register on the daemon")
	if !registeredBeforeAbortSettled {
		<-secondRegistrationEntered
	}

	require.NoError(t, <-errC)
	second := <-preparedC
	require.NotNil(t, second)
	secondEvents, result, err := second.Start(context.Background())
	require.NoError(t, err)
	currentEvent, err := json.Marshal(agentruntime.TextDelta{Text: "current generation"})
	require.NoError(t, err)
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: 82, Event: currentEvent})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 82, Model: "current-model",
	})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID: 82, Model: "duplicate-model",
	})

	event, open := <-secondEvents
	require.True(t, open)
	assert.Equal(t, agentruntime.TextDelta{Text: "current generation"}, event)
	_, open = <-secondEvents
	assert.False(t, open)
	assert.Equal(t, "current-model", result.Model)
}

func TestHandleEvent_UnknownAndMalformedFramesNeverLogPayload(t *testing.T) {
	_, _, _, rt := setupRemote(t)
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))
	const secret = "private-prompt-and-image-payload"

	unknown, err := json.Marshal(wire.EventFrame{
		SessionID: 901,
		Event:     json.RawMessage(fmt.Sprintf(`{"kind":%q,"text":%q}`, secret, secret)),
	})
	require.NoError(t, err)
	_, err = rt.handleEvent(ctx, unknown)
	require.NoError(t, err)

	rt.mu.Lock()
	rt.sessions[902] = &remoteSession{id: 902, events: make(chan agentruntime.Event, 1), result: &agentruntime.RunResult{}}
	rt.mu.Unlock()
	malformed, err := json.Marshal(wire.EventFrame{
		SessionID: 902,
		Event:     json.RawMessage(fmt.Sprintf(`{"kind":"not_real","detail":%q}`, secret)),
	})
	require.NoError(t, err)
	_, err = rt.handleEvent(ctx, malformed)
	require.NoError(t, err)

	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, secret)
		for _, value := range entry.ContextMap() {
			assert.NotContains(t, fmt.Sprint(value), secret)
		}
	}
}

func TestHandleRunResultDone_LateFrameNeverLogsStopPayload(t *testing.T) {
	_, _, _, rt := setupRemote(t)
	core, logs := observer.New(zapcore.DebugLevel)
	ctx := logger.WithContextLogger(context.Background(), zap.New(core))
	const secret = "private-late-error-payload"

	raw, err := json.Marshal(wire.RunResultDoneFrame{
		SessionID: 903, StopErrMsg: secret, StopErrCode: wire.ErrCodeAborted,
	})
	require.NoError(t, err)
	_, err = rt.handleRunResultDone(ctx, raw)
	require.NoError(t, err)

	for _, entry := range logs.All() {
		assert.NotContains(t, entry.Message, secret)
		for _, value := range entry.ContextMap() {
			assert.NotContains(t, fmt.Sprint(value), secret)
		}
	}
}

func TestRun_Success_DispatchesEventsThenCloses(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)

	runCalls := 0
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			runCalls++
			rp, ok := params.(wire.RunParams)
			require.True(t, ok, "expected wire.RunParams, got %T", params)
			assert.Equal(t, int64(42), rp.SessionID)
			assert.Equal(t, "hello", rp.UserText)
			assert.True(t, rp.Compact)
			switch runCalls {
			case 1:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			case 2:
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "psid-early"}
			case 3:
				assert.Equal(t, "psid-early", rp.ProviderSessionID)
				*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "psid-early"}
			}
			return nil
		}).Times(3)

	events, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypePiAgent), ID: 1, Name: "x"},
		SessionID: 42,
		UserText:  "hello",
		Compact:   true,
	})
	require.NoError(t, err)
	require.NotNil(t, runResult)
	assert.Equal(t, "psid-early", runResult.ProviderSessionID)

	// Deliver a TextDelta then a runResultDone with Usage + Model.
	textJSON, _ := json.Marshal(agentruntime.TextDelta{Text: "hi"})
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: 42, Event: textJSON})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID:         42,
		ProviderSessionID: "psid-early",
		Model:             "claude-sonnet-4-6",
		ContextWindow:     200000,
		Usage:             &wire.UsageWire{PromptTokens: 10, TotalTokens: 10},
	})

	// First event arrives.
	select {
	case ev := <-events:
		td, ok := ev.(agentruntime.TextDelta)
		require.True(t, ok, "got %T", ev)
		assert.Equal(t, "hi", td.Text)
	case <-time.After(time.Second):
		t.Fatal("never got text delta")
	}

	// Channel must close after runResultDone.
	select {
	case _, ok := <-events:
		assert.False(t, ok, "events channel must close after runResultDone")
	case <-time.After(time.Second):
		t.Fatal("events channel never closed")
	}

	// RunResult fields hydrated.
	assert.Equal(t, "psid-early", runResult.ProviderSessionID)
	assert.Equal(t, "claude-sonnet-4-6", runResult.Model)
	assert.Equal(t, 200000, runResult.ContextWindow)
	require.NotNil(t, runResult.Usage)
	assert.Equal(t, 10, runResult.Usage.PromptTokens)
	assert.NoError(t, runResult.StopErr)
}

// TestAutonomousTurns_ReconstructsForwardedTurn 验证 client 把 daemon 转发的
// Started → Event → Done 三帧还原成一个 agentruntime.AutonomousTurn:Events 收到
// 文本后 close,Result 在 close 后填好。
func TestAutonomousTurns_ReconstructsForwardedTurn(t *testing.T) {
	_, _, capture, rt := setupRemote(t)
	turns := rt.AutonomousTurns(42)

	capture.deliver(t, wire.NotifyAutonomousTurnStarted, wire.AutonomousTurnStartedFrame{
		SessionID: 42, Trigger: "background_task",
	})

	var at agentruntime.AutonomousTurn
	select {
	case at = <-turns:
	case <-time.After(time.Second):
		t.Fatal("never got autonomous turn")
	}
	assert.Equal(t, "background_task", at.Trigger)
	require.NotNil(t, at.Result)

	textJSON, err := json.Marshal(agentruntime.TextDelta{Text: "autonomous:listing"})
	require.NoError(t, err)
	capture.deliver(t, wire.NotifyAutonomousTurnEvent, wire.EventFrame{SessionID: 42, Event: textJSON})
	capture.deliver(t, wire.NotifyAutonomousTurnDone, wire.RunResultDoneFrame{
		SessionID: 42, ProviderSessionID: "psid-1", Model: "claude-sonnet-4-6",
	})

	select {
	case ev := <-at.Events:
		td, ok := ev.(agentruntime.TextDelta)
		require.True(t, ok, "got %T", ev)
		assert.Equal(t, "autonomous:listing", td.Text)
	case <-time.After(time.Second):
		t.Fatal("never got autonomous event")
	}
	select {
	case _, ok := <-at.Events:
		assert.False(t, ok, "events must close after done")
	case <-time.After(time.Second):
		t.Fatal("events never closed")
	}
	assert.Equal(t, "psid-1", at.Result.ProviderSessionID)
	assert.Equal(t, "claude-sonnet-4-6", at.Result.Model)
}

// TestAutonomousTurnEvent_ClosingRaceMustNotPanic 锁定一个真实并发缺陷:
// daemon 在自主续轮投递事件期间断连时,watchClose goroutine 调
// closeAllAutoSessions() 关 cur.events;若 handleAutonomousTurnEvent 在 a.mu 之外
// 往 cur.events 送,则关与送不互斥 → send-on-closed-channel panic(读循环 goroutine
// 无 recover → 整进程崩)。per-Run 的 handleEvent 早就靠"持 sess.mu 期间送"规避这一点,
// 自主轮必须对齐。
//
// 复现手法:把 cur.events(cap 64)填满让下一次 event 送 park 住,再让
// closeAllAutoSessions() 与之竞争。修复前:关 channel 把 park 的 send 打 panic;
// 修复后:send 持 a.mu,closeAll 阻塞到 drain 放行,无 panic。
func TestAutonomousTurnEvent_ClosingRaceMustNotPanic(t *testing.T) {
	_, _, capture, rt := setupRemote(t)
	_ = rt.AutonomousTurns(42) // 建好 autoSession(out 缓冲,不必 drain)

	capture.deliver(t, wire.NotifyAutonomousTurnStarted, wire.AutonomousTurnStartedFrame{
		SessionID: 42, Trigger: "background_task",
	})

	a := rt.lookupAutoSession(42)
	require.NotNil(t, a)
	require.NotNil(t, a.cur)
	evCh := a.cur.events // close 前抓住引用,供 drainer 用

	marshalEvent := func() json.RawMessage {
		b, err := json.Marshal(agentruntime.TextDelta{Text: "x"})
		require.NoError(t, err)
		return b
	}

	// 填满 cur.events 缓冲(cap 64),下一次 event 送就会 park。
	for i := 0; i < cap(evCh); i++ {
		_, err := rt.handleAutonomousTurnEvent(context.Background(),
			mustRawFrame(t, wire.EventFrame{SessionID: 42, Event: marshalEvent()}))
		require.NoError(t, err)
	}
	require.Equal(t, cap(evCh), len(evCh), "buffer 应被填满,下一次送才会 park")

	// park 第 65 次 event 送;若 panic 由该 goroutine 自己 recover 上报。
	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_, _ = rt.handleAutonomousTurnEvent(context.Background(),
			mustRawFrame(t, wire.EventFrame{SessionID: 42, Event: marshalEvent()}))
	}()
	time.Sleep(50 * time.Millisecond) // 确保上面那次送已 park 在满缓冲上

	// 模拟断连:watchClose → closeAllAutoSessions() 与 park 的 send 竞争。
	closeDone := make(chan struct{})
	go func() {
		rt.closeAllAutoSessions()
		close(closeDone)
	}()
	// drainer:修复后 send 持 a.mu,需 drain 一个放行让 closeAll 拿到锁;修复前 send
	// 已被 close 打 panic,drain 只是把缓冲抽干、无副作用。
	go func() {
		for range evCh { //nolint:revive // 抽干
		}
	}()

	<-closeDone
	got := <-panicked
	require.Nil(t, got,
		"closeAllAutoSessions 与 handleAutonomousTurnEvent 竞争时不得 send-on-closed panic;got=%v", got)
}

// TestAutonomousTurnStarted_ClosingRaceMustNotPanic 锁定与
// TestAutonomousTurnEvent_ClosingRaceMustNotPanic 同类、但发生在「起一轮」路径上的
// 并发缺陷:handleAutonomousTurnStarted 在 a.mu 之外往 a.out 送新 turn,而
// closeAllAutoSessions()(watchClose goroutine,daemon 断连触发)在 a.mu 内
// close(a.out)。两者不互斥 → daemon 断连恰在投递新 turn 期间会 send-on-closed panic
// (读循环 goroutine 无 recover → 整进程崩)。524f33c 只把 event 送(cur.events)纳入
// a.mu,Started 送(a.out)漏了同一层纪律。
//
// 复现手法对齐 event 版:把 a.out(cap 4)填满让下一次 Started 送 park 住,再让
// closeAllAutoSessions() 与之竞争。修复前(-race):close 与 park 的 send 竞争 / panic;
// 修复后:send 持 a.mu,closeAll 阻塞到 drain 放行,无 panic、无 race。
func TestAutonomousTurnStarted_ClosingRaceMustNotPanic(t *testing.T) {
	_, _, _, rt := setupRemote(t)
	outCh := rt.AutonomousTurns(42) // 建好 autoSession;a.out cap 4,不 drain

	// 预 marshal 一次,goroutine 内复用,避免在非测试 goroutine 里调 testify。
	startedRaw := mustRawFrame(t, wire.AutonomousTurnStartedFrame{
		SessionID: 42, Trigger: "background_task",
	})

	// 填满 a.out 缓冲(cap 4),下一次 Started 送就会 park。
	for i := 0; i < cap(outCh); i++ {
		_, err := rt.handleAutonomousTurnStarted(context.Background(), startedRaw)
		require.NoError(t, err)
	}
	require.Equal(t, cap(outCh), len(outCh), "buffer 应被填满,下一次送才会 park")

	// park 第 cap+1 次 Started 送;若 panic 由该 goroutine 自己 recover 上报。
	panicked := make(chan any, 1)
	go func() {
		defer func() { panicked <- recover() }()
		_, _ = rt.handleAutonomousTurnStarted(context.Background(), startedRaw)
	}()
	time.Sleep(50 * time.Millisecond) // 确保上面那次送已 park 在满缓冲上

	// 模拟断连:watchClose → closeAllAutoSessions() 与 park 的 send 竞争。
	closeDone := make(chan struct{})
	go func() {
		rt.closeAllAutoSessions()
		close(closeDone)
	}()
	// drainer:修复后 send 持 a.mu,需 drain 放行让 closeAll 拿到锁;修复前 send 已被
	// close 打 panic / 触发 race,drain 只是把缓冲抽干。
	go func() {
		for range outCh { //nolint:revive // 抽干
		}
	}()

	<-closeDone
	got := <-panicked
	require.Nil(t, got,
		"closeAllAutoSessions 与 handleAutonomousTurnStarted 竞争时不得 send-on-closed panic;got=%v", got)
}

func mustRawFrame(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestRun_DeliversEventArrivingBeforeRunAckReturns(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)
	textJSON, err := json.Marshal(agentruntime.TextDelta{Text: "early"})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp, ok := params.(wire.RunParams)
			require.True(t, ok, "expected wire.RunParams, got %T", params)
			capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: rp.SessionID, Event: textJSON})
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			return nil
		})

	events, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode", ID: 1, Name: "x"},
		SessionID: 42,
		UserText:  "hello",
	})
	require.NoError(t, err)

	select {
	case ev := <-events:
		td, ok := ev.(agentruntime.TextDelta)
		require.True(t, ok, "got %T", ev)
		assert.Equal(t, "early", td.Text)
	case <-time.After(time.Second):
		t.Fatal("early event was dropped before Run ack returned")
	}

	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{SessionID: 42})
}

func TestRun_StopErrAborted_Rehydrates(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 7}
			return nil
		})
	_, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 7,
	})
	require.NoError(t, err)

	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID:   7,
		StopErrMsg:  "aborted by user",
		StopErrCode: wire.ErrCodeAborted,
	})
	// drain
	for range capture.funcs {
	}
	// Allow handler to run.
	time.Sleep(20 * time.Millisecond)

	require.Error(t, runResult.StopErr)
	assert.ErrorIs(t, runResult.StopErr, agentruntime.ErrAborted)
}

func TestRun_RPCError_PropagatesAndDoesNotRegister(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		Return(errors.New("transport down"))
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 5,
	})
	require.Error(t, err)

	// Steer after failed Run must see ErrNoActiveTurn — session was never
	// registered.
	err = rt.Steer(context.Background(), 5, "", "x")
	assert.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestRun_EventForUnknownSession_DroppedSilently(t *testing.T) {
	_, _, capture, _ := setupRemote(t)
	// No Run call → no session known. Delivering an event must not panic
	// nor produce an error from the handler.
	textJSON, _ := json.Marshal(agentruntime.TextDelta{Text: "noise"})
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: 999, Event: textJSON})
}

// ── Steer ───────────────────────────────────────────────────────────────────

func TestSteer_Success(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 9}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 9,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodSteer, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, _ any) error {
			sp, ok := params.(wire.SteerParams)
			require.True(t, ok)
			assert.Equal(t, int64(9), sp.SessionID)
			assert.Equal(t, "q-1", sp.QueuedID)
			assert.Equal(t, "stop", sp.Text)
			return nil
		})
	require.NoError(t, rt.Steer(context.Background(), 9, "q-1", "stop"))
}

func TestSteer_NoSession_ErrNoActiveTurn(t *testing.T) {
	_, _, _, rt := setupRemote(t)
	err := rt.Steer(context.Background(), 1, "", "x")
	assert.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestSteer_ServerSentinel_Rehydrates(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 3}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 3,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodSteer, gomock.Any(), gomock.Any()).
		Return(&rpc.Error{Code: wire.ErrCodeUnsupported, Message: "no"})
	err = rt.Steer(context.Background(), 3, "", "x")
	assert.ErrorIs(t, err, agentruntime.ErrUnsupported)
}

// ── CancelSteer / DrainPending / Abort / SetPermissionMode ─────────────────

func TestCancelSteer_HappyAndSentinel(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 1}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 1,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodCancelSteer, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.CancelSteerResult)) = wire.CancelSteerResult{Removed: []string{"a"}}
			return nil
		})
	removed, err := rt.CancelSteer(context.Background(), 1, "a")
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, removed)

	cli.EXPECT().Call(gomock.Any(), wire.MethodCancelSteer, gomock.Any(), gomock.Any()).
		Return(&rpc.Error{Code: wire.ErrCodeSteerNotFound})
	_, err = rt.CancelSteer(context.Background(), 1, "ghost")
	assert.ErrorIs(t, err, agentruntime.ErrSteerNotFound)
}

func TestDrainPending_ReturnsSteers(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 2}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 2,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodDrainPending, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.DrainResult)) = wire.DrainResult{
				Steers: []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "t"}},
			}
			return nil
		})
	out := rt.DrainPending(context.Background(), 2)
	assert.Equal(t, []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "t"}}, out)
}

func TestAbort_SuccessAndNoSession(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 4}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 4,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodAbort, gomock.Any(), gomock.Any()).
		Return(nil)
	require.NoError(t, rt.Abort(context.Background(), 4))

	// Unknown session
	err = rt.Abort(context.Background(), 999)
	assert.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestStopBackgroundTask_SuccessAndNoSession(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 4}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 4,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodStopBackgroundTask, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, _ any) error {
			sp := params.(wire.StopBackgroundTaskParams)
			assert.Equal(t, int64(4), sp.SessionID)
			assert.Equal(t, "b0n82mqaj", sp.TaskID)
			return nil
		})
	require.NoError(t, rt.StopBackgroundTask(context.Background(), 4, "b0n82mqaj"))

	// Unknown session → ErrNoActiveTurn(不发 RPC)
	err = rt.StopBackgroundTask(context.Background(), 999, "b0n82mqaj")
	assert.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestSetPermissionMode_Success(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 6}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 6,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodSetPermissionMode, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, _ any) error {
			sp := params.(wire.SetPermissionModeParams)
			assert.Equal(t, "plan", sp.Mode)
			return nil
		})
	require.NoError(t, rt.SetPermissionMode(context.Background(), 6, "plan"))
}

func TestSubmitAnswer_Success(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 8}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 8,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodSubmitAnswer, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, _ any) error {
			sp := params.(wire.SubmitAnswerParams)
			assert.Equal(t, "r-1", sp.RequestID)
			assert.True(t, sp.Skipped)
			return nil
		})
	require.NoError(t, rt.SubmitAnswer(context.Background(), 8, "r-1", nil, nil, true))
}

func TestSubmitToolPermission_Success(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, _ any, result any) error {
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: 10}
			return nil
		})
	_, _, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode"},
		SessionID: 10,
	})
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodSubmitToolPermission, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, _ any) error {
			sp := params.(wire.SubmitToolPermissionParams)
			assert.True(t, sp.Allow)
			assert.True(t, sp.AlwaysAllowSession)
			return nil
		})
	require.NoError(t, rt.SubmitToolPermission(context.Background(), 10, "p-1", true, true, ""))
}

// ── Capabilities ────────────────────────────────────────────────────────────

func TestCapabilities_DefaultBeforePrefetch(t *testing.T) {
	_, _, _, rt := setupRemote(t)
	caps := rt.Capabilities()
	// Default placeholder: must at minimum expose CapSteer + CapAbort + CapAnswerUserAsk
	// so chat_svc UI gating doesn't false-flag a fresh, unprefetched runtime.
	assert.True(t, caps.Has(capability.CapSteer))
	assert.True(t, caps.Has(capability.CapAbort))
	assert.True(t, caps.Has(capability.CapAnswerUserAsk))
	// claudecode/codex(daemon 最常见 backend)都声明 CapSkills;占位矩阵对齐它们,
	// 这样 Prefetch 失败兜底时也不会误判远端不支持技能(enabledPluginsForTurn 不被吞)。
	assert.True(t, caps.Has(capability.CapSkills))
}

func TestPrefetch_CachesAndCapabilitiesReturnsIt(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	wantCaps := capability.Capabilities{
		Set: map[capability.Capability]bool{
			capability.CapSteer:               true,
			capability.CapCancelSteer:         true,
			capability.CapDrainSteer:          true,
			capability.CapAbort:               true,
			capability.CapAnswerUserAsk:       true,
			capability.CapToolPermission:      true,
			capability.CapSetPermission:       true,
			capability.CapForkSession:         true,
			capability.CapReportContextWindow: true,
		},
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes: []string{"default", "plan"},
			DefaultMode:  "default",
		},
	}
	cli.EXPECT().Call(gomock.Any(), wire.MethodCapabilities, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			cp := params.(wire.CapabilitiesParams)
			assert.Equal(t, "claudecode", cp.BackendType)
			*(result.(*wire.CapabilitiesResult)) = wire.CapabilitiesResult{Capabilities: wantCaps}
			return nil
		})
	require.NoError(t, rt.Prefetch(context.Background(), agent_backend_entity.TypeClaudeCode))

	caps := rt.Capabilities()
	assert.Equal(t, wantCaps, caps)

	// Second Prefetch with same backend type must hit cache → no extra RPC.
	require.NoError(t, rt.Prefetch(context.Background(), agent_backend_entity.TypeClaudeCode))
}

// TestRun_LaunchPermissionMode_PassThrough 钉死 RunAck.LaunchPermissionMode
// 在 remote 客户端被同步写入 RunResult.LaunchPermissionMode —— chat_svc 依赖
// 这条线在主进程侧持久化 session.PermissionModeAtLaunch(daemon 进程不
// bootstrap chat_repo,不能直接写库)。
func TestRun_LaunchPermissionMode_PassThrough(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp := params.(wire.RunParams)
			*(result.(*wire.RunAck)) = wire.RunAck{
				SessionID:            rp.SessionID,
				LaunchPermissionMode: "bypassPermissions",
			}
			return nil
		})

	_, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode", ID: 1, Name: "x"},
		SessionID: 77,
		UserText:  "go",
	})
	require.NoError(t, err)
	assert.Equal(t, "bypassPermissions", runResult.LaunchPermissionMode)
}

// TestWatchClose_InjectsStopErrAndClosesEvents 模拟 daemon 进程崩 / 网络断:
// client.Closed() 触发后,所有在飞 session 的 events channel 必须被关闭,
// RunResult.StopErr 必须 = ErrDaemonDisconnected,这样 chat_svc.runTurn 才能
// 跳出 `for ev := range events` 走 StreamError 通路解锁前端「生成中」状态。
func TestWatchClose_InjectsStopErrAndClosesEvents(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	cli := mock_agentruntime.NewMockDaemonClientPort(ctrl)
	capture := newHandlerCapture()
	cli.EXPECT().Handle(gomock.Any(), gomock.Any()).DoAndReturn(
		func(method string, fn func(context.Context, json.RawMessage) (any, error)) {
			capture.record(method, fn)
		}).AnyTimes()
	closeCh := make(chan struct{})
	cli.EXPECT().Closed().Return((<-chan struct{})(closeCh)).AnyTimes()
	rt := New(cli)

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp := params.(wire.RunParams)
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			return nil
		})

	events, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode", ID: 1, Name: "x"},
		SessionID: 99,
		UserText:  "hi",
	})
	require.NoError(t, err)

	// 模拟 daemon 断连。
	close(closeCh)

	// events 必须在合理时限内关闭。
	select {
	case _, ok := <-events:
		assert.False(t, ok, "events should be closed after daemon disconnect")
	case <-time.After(time.Second):
		t.Fatal("events channel not closed after daemon disconnect")
	}
	assert.ErrorIs(t, runResult.StopErr, ErrDaemonDisconnected)
}

// TestBuildRunParams_ForwardsMCPServers 钉死 buildRunParams 把 RunRequest.MCPServers
// 透传到 wire.RunParams，且 JSON round-trip 保留该字段（含 Headers map）。
// 修复 Edit 1–3 之前此测试会 FAIL（params.MCPServers 为 nil / 字段不存在）。
func TestBuildRunParams_ForwardsMCPServers(t *testing.T) {
	specs := []agentruntime.MCPServerSpec{{
		Name:    "group",
		URL:     "http://127.0.0.1:1/mcp/group/",
		Headers: map[string]string{"Authorization": "Bearer t"},
		Tools:   []string{"group_send"},
	}}
	params, err := buildRunParams(agentruntime.RunRequest{
		Backend:    &agent_backend_entity.AgentBackend{},
		SessionID:  9,
		MCPServers: specs,
	})
	if err != nil {
		t.Fatalf("buildRunParams: %v", err)
	}
	if len(params.MCPServers) != 1 || params.MCPServers[0].Name != "group" || params.MCPServers[0].Tools[0] != "group_send" {
		t.Fatalf("buildRunParams dropped MCPServers: %+v", params.MCPServers)
	}

	// wire JSON round-trip preserves the field.
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out wire.RunParams
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.MCPServers) != 1 || out.MCPServers[0].Headers["Authorization"] != "Bearer t" {
		t.Fatalf("MCPServers not preserved across wire JSON: %+v", out.MCPServers)
	}
}

// TestBuildRunParams_ForwardsEnabledPlugins 钉死 buildRunParams 把
// RunRequest.EnabledPlugins 透传到 wire.RunParams，且 JSON round-trip 保留该字段。
func TestBuildRunParams_ForwardsEnabledPlugins(t *testing.T) {
	plugins := map[string]bool{
		"browser@openai-bundled":     true,
		"superpowers@openai-curated": false,
	}
	params, err := buildRunParams(agentruntime.RunRequest{
		Backend:        &agent_backend_entity.AgentBackend{},
		SessionID:      9,
		EnabledPlugins: plugins,
	})
	if err != nil {
		t.Fatalf("buildRunParams: %v", err)
	}
	if len(params.EnabledPlugins) != 2 || !params.EnabledPlugins["browser@openai-bundled"] || params.EnabledPlugins["superpowers@openai-curated"] {
		t.Fatalf("buildRunParams dropped EnabledPlugins: %+v", params.EnabledPlugins)
	}

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out wire.RunParams
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out.EnabledPlugins) != 2 || !out.EnabledPlugins["browser@openai-bundled"] || out.EnabledPlugins["superpowers@openai-curated"] {
		t.Fatalf("EnabledPlugins not preserved across wire JSON: %+v", out.EnabledPlugins)
	}
}

func TestGoal_DispatchesWireRPCsWithBackendMetadata(t *testing.T) {
	_, cli, _, rt := setupRemote(t)
	objective := "ship goal rpc"
	status := "active"
	budget := 123
	req := agentruntime.GoalRequest{
		SessionID:         42,
		AgentID:           7,
		ProviderSessionID: "thread-goal",
		Backend:           &agent_backend_entity.AgentBackend{ID: 3, Type: string(agent_backend_entity.TypeCodex), Name: "codex"},
		Cwd:               "/tmp/work",
		Objective:         &objective,
		Status:            &status,
		TokenBudget:       &budget,
	}

	cli.EXPECT().Call(gomock.Any(), wire.MethodSetGoal, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			gp, ok := params.(wire.GoalParams)
			require.True(t, ok, "expected wire.GoalParams, got %T", params)
			assert.Equal(t, int64(42), gp.SessionID)
			assert.Equal(t, int64(7), gp.AgentID)
			assert.Equal(t, "thread-goal", gp.ProviderSessionID)
			assert.Equal(t, "/tmp/work", gp.Cwd)
			assert.Contains(t, string(gp.Backend), `"ID":3`)
			assert.Contains(t, string(gp.Backend), `"Name":"codex"`)
			assert.Contains(t, string(gp.Backend), `"Type":"codex"`)
			require.NotNil(t, gp.Objective)
			assert.Equal(t, "ship goal rpc", *gp.Objective)
			require.NotNil(t, gp.Status)
			assert.Equal(t, "active", *gp.Status)
			require.NotNil(t, gp.TokenBudget)
			assert.Equal(t, 123, *gp.TokenBudget)
			*(result.(*wire.GoalResult)) = wire.GoalResult{Goal: &agentruntime.Goal{ThreadID: "thread-goal", Objective: "ship goal rpc", Status: "active"}}
			return nil
		})
	goal, err := rt.SetGoal(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, goal)
	assert.Equal(t, "ship goal rpc", goal.Objective)

	cli.EXPECT().Call(gomock.Any(), wire.MethodGetGoal, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			gp, ok := params.(wire.GoalParams)
			require.True(t, ok, "expected wire.GoalParams, got %T", params)
			assert.Equal(t, "thread-goal", gp.ProviderSessionID)
			assert.Contains(t, string(gp.Backend), `"ID":3`)
			assert.Contains(t, string(gp.Backend), `"Name":"codex"`)
			assert.Contains(t, string(gp.Backend), `"Type":"codex"`)
			*(result.(*wire.GoalResult)) = wire.GoalResult{Goal: &agentruntime.Goal{ThreadID: "thread-goal", Objective: "ship goal rpc", Status: "active"}}
			return nil
		})
	_, err = rt.GetGoal(context.Background(), req)
	require.NoError(t, err)

	cli.EXPECT().Call(gomock.Any(), wire.MethodClearGoal, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			gp, ok := params.(wire.GoalParams)
			require.True(t, ok, "expected wire.GoalParams, got %T", params)
			assert.Equal(t, "thread-goal", gp.ProviderSessionID)
			assert.Contains(t, string(gp.Backend), `"ID":3`)
			assert.Contains(t, string(gp.Backend), `"Name":"codex"`)
			assert.Contains(t, string(gp.Backend), `"Type":"codex"`)
			*(result.(*wire.GoalClearResult)) = wire.GoalClearResult{Cleared: true}
			return nil
		})
	cleared, err := rt.ClearGoal(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, cleared)
}

func TestRun_DirectForkTurnAcceptsChangedProviderSessionID(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp, ok := params.(wire.RunParams)
			require.True(t, ok, "expected wire.RunParams, got %T", params)
			assert.Equal(t, int64(42), rp.SessionID)
			// A direct (non-Pi) run carries the resumed identity in the ack...
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID, ProviderSessionID: "orig-session"}
			return nil
		}).Times(1)

	events, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode), ID: 1, Name: "cc"},
		SessionID: 42,
		UserText:  "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, runResult)
	assert.Equal(t, "orig-session", runResult.ProviderSessionID)

	// A claudecode fork (Regenerate) legitimately changes the provider session
	// identity during the turn: the ack carries the resumed session and the final
	// runResultDone carries the forked session. That final frame must still
	// finalize the run instead of being dropped as a stale generation.
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID:         42,
		ProviderSessionID: "forked-session",
	})

	select {
	case _, ok := <-events:
		assert.False(t, ok, "events channel must close after runResultDone")
	case <-time.After(time.Second):
		t.Fatal("events channel never closed: fork runResultDone was dropped")
	}
	assert.Equal(t, "forked-session", runResult.ProviderSessionID)
}
