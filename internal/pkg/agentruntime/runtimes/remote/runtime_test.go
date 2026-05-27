package remote

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"agentre/internal/daemon/rpc"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/agentruntime/mock_agentruntime"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
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

func TestRun_Success_DispatchesEventsThenCloses(t *testing.T) {
	_, cli, capture, rt := setupRemote(t)

	cli.EXPECT().Call(gomock.Any(), wire.MethodRun, gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, _ string, params any, result any) error {
			rp, ok := params.(wire.RunParams)
			require.True(t, ok, "expected wire.RunParams, got %T", params)
			assert.Equal(t, int64(42), rp.SessionID)
			assert.Equal(t, "hello", rp.UserText)
			assert.True(t, rp.Compact)
			// echo session id back
			*(result.(*wire.RunAck)) = wire.RunAck{SessionID: rp.SessionID}
			return nil
		})

	events, runResult, err := rt.Run(context.Background(), agentruntime.RunRequest{
		Backend:   &agent_backend_entity.AgentBackend{Type: "claudecode", ID: 1, Name: "x"},
		SessionID: 42,
		UserText:  "hello",
		Compact:   true,
	})
	require.NoError(t, err)
	require.NotNil(t, runResult)

	// Deliver a TextDelta then a runResultDone with Usage + Model.
	textJSON, _ := json.Marshal(agentruntime.TextDelta{Text: "hi"})
	capture.deliver(t, wire.NotifyEvent, wire.EventFrame{SessionID: 42, Event: textJSON})
	capture.deliver(t, wire.NotifyRunResultDone, wire.RunResultDoneFrame{
		SessionID:         42,
		ProviderSessionID: "psid-1",
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
	assert.Equal(t, "psid-1", runResult.ProviderSessionID)
	assert.Equal(t, "claude-sonnet-4-6", runResult.Model)
	assert.Equal(t, 200000, runResult.ContextWindow)
	require.NotNil(t, runResult.Usage)
	assert.Equal(t, 10, runResult.Usage.PromptTokens)
	assert.NoError(t, runResult.StopErr)
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
