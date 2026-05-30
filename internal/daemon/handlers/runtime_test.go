package handlers_test

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

	"agentre/internal/daemon/handlers"
	"agentre/internal/daemon/handlers/mock_handlers"
	"agentre/internal/daemon/rpc"
	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/llm_provider_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// ── fake Runtimes ───────────────────────────────────────────────────────────
//
// fullRT implements agentruntime.Runtime + ALL 7 optional sub-interfaces.
// Use for happy-path tests. bareRT implements only Runtime so the handler
// hits its "type assert failed → ErrUnsupported" branch.

type runCall struct {
	req agentruntime.RunRequest
}

type fullRT struct {
	mu sync.Mutex

	cap capability.Capabilities

	runFn   func(ctx context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error)
	runReqs []runCall

	steerErr   error
	steerCalls []steerCall

	cancelSteerFn   func(int64, string) ([]string, error)
	cancelSteerArgs []cancelSteerCall

	drainFn   func(int64) []agentruntime.ConsumedSteer
	drainArgs []int64

	abortErr   error
	abortCalls []int64

	setModeErr   error
	setModeCalls []setModeCall

	submitAnswerErr   error
	submitAnswerCalls []submitAnswerCall

	submitToolPermErr   error
	submitToolPermCalls []submitToolPermCall

	goalErr        error
	getGoalCalls   []goalCall
	setGoalCalls   []goalCall
	clearGoalCalls []goalCall
}

type steerCall struct {
	sid      int64
	queuedID string
	text     string
}

type cancelSteerCall struct {
	sid      int64
	queuedID string
}

type setModeCall struct {
	sid  int64
	mode string
}

type submitAnswerCall struct {
	sid       int64
	requestID string
	questions []agentruntime.AskQuestion
	answers   []agentruntime.AskAnswer
	skipped   bool
}

type submitToolPermCall struct {
	sid                int64
	requestID          string
	allow              bool
	alwaysAllowSession bool
	denyReason         string
}

type goalCall struct {
	req agentruntime.GoalRequest
}

func (r *fullRT) Capabilities() capability.Capabilities { return r.cap }

func (r *fullRT) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	r.mu.Lock()
	r.runReqs = append(r.runReqs, runCall{req: req})
	fn := r.runFn
	r.mu.Unlock()
	if fn != nil {
		return fn(ctx)
	}
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

func (r *fullRT) Steer(_ context.Context, sid int64, queuedID, text string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steerCalls = append(r.steerCalls, steerCall{sid, queuedID, text})
	return r.steerErr
}

func (r *fullRT) CancelSteer(_ context.Context, sid int64, queuedID string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancelSteerArgs = append(r.cancelSteerArgs, cancelSteerCall{sid, queuedID})
	if r.cancelSteerFn != nil {
		return r.cancelSteerFn(sid, queuedID)
	}
	return nil, nil
}

func (r *fullRT) DrainPending(_ context.Context, sid int64) []agentruntime.ConsumedSteer {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.drainArgs = append(r.drainArgs, sid)
	if r.drainFn != nil {
		return r.drainFn(sid)
	}
	return nil
}

func (r *fullRT) Abort(_ context.Context, sid int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.abortCalls = append(r.abortCalls, sid)
	return r.abortErr
}

func (r *fullRT) SetPermissionMode(_ context.Context, sid int64, mode string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setModeCalls = append(r.setModeCalls, setModeCall{sid, mode})
	return r.setModeErr
}

func (r *fullRT) SubmitAnswer(_ context.Context, sid int64, requestID string, questions []agentruntime.AskQuestion, answers []agentruntime.AskAnswer, skipped bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submitAnswerCalls = append(r.submitAnswerCalls, submitAnswerCall{sid, requestID, questions, answers, skipped})
	return r.submitAnswerErr
}

func (r *fullRT) SubmitToolPermission(_ context.Context, sid int64, requestID string, allow, always bool, deny string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.submitToolPermCalls = append(r.submitToolPermCalls, submitToolPermCall{sid, requestID, allow, always, deny})
	return r.submitToolPermErr
}

func (r *fullRT) GetGoal(_ context.Context, req agentruntime.GoalRequest) (*agentruntime.Goal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.getGoalCalls = append(r.getGoalCalls, goalCall{req: req})
	return &agentruntime.Goal{ThreadID: req.ProviderSessionID, Objective: "ship goal rpc", Status: "active"}, r.goalErr
}

func (r *fullRT) SetGoal(_ context.Context, req agentruntime.GoalRequest) (*agentruntime.Goal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.setGoalCalls = append(r.setGoalCalls, goalCall{req: req})
	return &agentruntime.Goal{ThreadID: req.ProviderSessionID, Objective: "ship goal rpc", Status: "active"}, r.goalErr
}

func (r *fullRT) ClearGoal(_ context.Context, req agentruntime.GoalRequest) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clearGoalCalls = append(r.clearGoalCalls, goalCall{req: req})
	return true, r.goalErr
}

// bareRT only implements Runtime — type-asserting any sub-interface fails.
type bareRT struct{}

func (bareRT) Capabilities() capability.Capabilities { return capability.Capabilities{} }
func (bareRT) Run(_ context.Context, _ agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	ch := make(chan agentruntime.Event)
	close(ch)
	return ch, &agentruntime.RunResult{}, nil
}

// recordingNotifier collects every notify call so tests can assert ordering.
type recordingNotifier struct {
	mu      sync.Mutex
	frames  []notifyFrame
	notifyC chan struct{}
}

type notifyFrame struct {
	method string
	params any
}

func newRecordingNotifier() *recordingNotifier {
	return &recordingNotifier{notifyC: make(chan struct{}, 64)}
}

func (n *recordingNotifier) Notify(method string, params any) error {
	n.mu.Lock()
	n.frames = append(n.frames, notifyFrame{method, params})
	n.mu.Unlock()
	select {
	case n.notifyC <- struct{}{}:
	default:
	}
	return nil
}

func (*recordingNotifier) Request(_ context.Context, _ string, _ any, _ any) error { return nil }

func (n *recordingNotifier) snapshot() []notifyFrame {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]notifyFrame, len(n.frames))
	copy(out, n.frames)
	return out
}

// waitFrames blocks until n.snapshot() yields at least want frames, or test fails.
func (n *recordingNotifier) waitFrames(t *testing.T, want int) []notifyFrame {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		got := n.snapshot()
		if len(got) >= want {
			return got
		}
		select {
		case <-n.notifyC:
		case <-deadline:
			t.Fatalf("timed out waiting for %d notify frames; got %d", want, len(got))
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func setupRuntimeTest(t *testing.T, rt agentruntime.Runtime) (
	context.Context,
	*recordingNotifier,
	*mock_handlers.MockGatewayPort,
	*mock_handlers.MockLLMProviderLookupPort,
	*handlers.RuntimeHandlers,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	notif := newRecordingNotifier()
	gw := mock_handlers.NewMockGatewayPort(ctrl)
	lookup := mock_handlers.NewMockLLMProviderLookupPort(ctrl)
	h := handlers.NewRuntimeHandlers(handlers.RuntimeDeps{
		Notify:  notif,
		Gateway: gw,
		Lookup:  lookup,
		RuntimeFor: func(_ agent_backend_entity.BackendType) agentruntime.Runtime {
			return rt
		},
	})
	return context.Background(), notif, gw, lookup, h
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func backendJSON(t *testing.T, be agent_backend_entity.AgentBackend) json.RawMessage {
	return mustJSON(t, be)
}

// ── Capabilities ────────────────────────────────────────────────────────────

func TestRuntime_Capabilities_Found(t *testing.T) {
	rt := &fullRT{cap: capability.Capabilities{}}
	ctx, _, _, _, h := setupRuntimeTest(t, rt)

	out, err := h.Capabilities(ctx, wire.CapabilitiesParams{BackendType: string(agent_backend_entity.TypeClaudeCode)})
	require.NoError(t, err)
	assert.Equal(t, rt.cap, out.Capabilities)
}

func TestRuntime_Capabilities_Unknown(t *testing.T) {
	ctx, _, _, _, h := setupRuntimeTest(t, nil)
	_, err := h.Capabilities(ctx, wire.CapabilitiesParams{BackendType: "nope"})
	require.Error(t, err)
}

func TestRuntime_GoalRoutesWithoutActiveTurn(t *testing.T) {
	rt := &fullRT{}
	ctx, _, _, _, h := setupRuntimeTest(t, rt)
	objective := "ship goal rpc"
	status := "active"
	budget := 123
	params := wire.GoalParams{
		SessionID:         42,
		AgentID:           7,
		ProviderSessionID: "thread-goal",
		Backend:           backendJSON(t, agent_backend_entity.AgentBackend{ID: 3, Type: string(agent_backend_entity.TypeCodex), Name: "codex"}),
		Cwd:               "/tmp/work",
		Objective:         &objective,
		Status:            &status,
		TokenBudget:       &budget,
	}

	setOut, err := h.SetGoal(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, setOut.Goal)
	assert.Equal(t, "thread-goal", setOut.Goal.ThreadID)
	require.Len(t, rt.setGoalCalls, 1)
	setReq := rt.setGoalCalls[0].req
	assert.Equal(t, int64(42), setReq.SessionID)
	assert.Equal(t, int64(7), setReq.AgentID)
	assert.Equal(t, "thread-goal", setReq.ProviderSessionID)
	assert.Equal(t, "/tmp/work", setReq.Cwd)
	require.NotNil(t, setReq.Backend)
	assert.Equal(t, string(agent_backend_entity.TypeCodex), setReq.Backend.Type)
	require.NotNil(t, setReq.Objective)
	assert.Equal(t, "ship goal rpc", *setReq.Objective)
	require.NotNil(t, setReq.Status)
	assert.Equal(t, "active", *setReq.Status)
	require.NotNil(t, setReq.TokenBudget)
	assert.Equal(t, 123, *setReq.TokenBudget)

	getOut, err := h.GetGoal(ctx, params)
	require.NoError(t, err)
	require.NotNil(t, getOut.Goal)
	assert.Equal(t, "thread-goal", getOut.Goal.ThreadID)
	require.Len(t, rt.getGoalCalls, 1)
	require.NotNil(t, rt.getGoalCalls[0].req.Backend)
	assert.Equal(t, string(agent_backend_entity.TypeCodex), rt.getGoalCalls[0].req.Backend.Type)

	clearOut, err := h.ClearGoal(ctx, params)
	require.NoError(t, err)
	assert.True(t, clearOut.Cleared)
	require.Len(t, rt.clearGoalCalls, 1)
	require.NotNil(t, rt.clearGoalCalls[0].req.Backend)
	assert.Equal(t, string(agent_backend_entity.TypeCodex), rt.clearGoalCalls[0].req.Backend.Type)
}

func TestRuntime_GoalWithProviderUsesDaemonProviderAndGateway(t *testing.T) {
	rt := &fullRT{}
	ctx, _, gw, lookup, h := setupRuntimeTest(t, rt)
	be := agent_backend_entity.AgentBackend{
		ID:             3,
		Type:           string(agent_backend_entity.TypeCodex),
		Name:           "codex",
		LLMProviderKey: "provider-key",
	}
	lookup.EXPECT().FindByKey(ctx, "provider-key").Return(&llm_provider_entity.LLMProvider{
		ProviderKey: "provider-key",
		Type:        string(llm_provider_entity.TypeOpenAIResponse),
		Model:       "gpt-5-codex",
	}, nil)
	gw.EXPECT().URL().Return("http://127.0.0.1:12345")
	gw.EXPECT().IssueToken(ctx, gomock.Any(), time.Hour).DoAndReturn(
		func(_ context.Context, got *agent_backend_entity.AgentBackend, _ time.Duration) (string, error) {
			assert.Equal(t, int64(3), got.ID)
			assert.Equal(t, "provider-key", got.LLMProviderKey)
			return "goal-token", nil
		})
	gw.EXPECT().RevokeToken("goal-token")

	_, err := h.GetGoal(ctx, wire.GoalParams{
		SessionID:         42,
		AgentID:           7,
		ProviderSessionID: "thread-goal",
		Backend:           backendJSON(t, be),
	})
	require.NoError(t, err)
	require.Len(t, rt.getGoalCalls, 1)
	req := rt.getGoalCalls[0].req
	require.NotNil(t, req.Provider)
	assert.Equal(t, "provider-key", req.Provider.ProviderKey)
	assert.Equal(t, "gpt-5-codex", req.Provider.Model)
	assert.Equal(t, "http://127.0.0.1:12345", req.GatewayURL)
	assert.Equal(t, "goal-token", req.GatewayToken)
}

func TestRuntime_GoalMissingBackendReturnsNoActiveTurn(t *testing.T) {
	rt := &fullRT{}
	ctx, _, _, _, h := setupRuntimeTest(t, rt)

	_, err := h.GetGoal(ctx, wire.GoalParams{SessionID: 42, ProviderSessionID: "thread-goal"})
	require.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

// ── Run ─────────────────────────────────────────────────────────────────────

func TestRuntime_Run_NoProvider_EmitsEventsAndDone(t *testing.T) {
	rt := &fullRT{}
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		ch := make(chan agentruntime.Event, 3)
		ch <- agentruntime.TextDelta{Text: "hi"}
		ch <- agentruntime.Done{}
		close(ch)
		return ch, &agentruntime.RunResult{
			ProviderSessionID: "psid-1",
			Model:             "claude-sonnet-4-6",
			ContextWindow:     200000,
		}, nil
	}
	ctx, notif, _, _, h := setupRuntimeTest(t, rt)

	be := agent_backend_entity.AgentBackend{ID: 1, Type: string(agent_backend_entity.TypeClaudeCode), Name: "x"}
	ack, err := h.Run(ctx, wire.RunParams{
		Backend:   backendJSON(t, be),
		SessionID: 42,
		AgentID:   7,
		Cwd:       "/tmp",
		UserText:  "hello",
		Compact:   true,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(42), ack.SessionID)
	require.Len(t, rt.runReqs, 1)
	assert.True(t, rt.runReqs[0].req.Compact)

	// 2 events + 1 runResultDone = 3 frames expected.
	frames := notif.waitFrames(t, 3)

	assert.Equal(t, wire.NotifyEvent, frames[0].method)
	assert.Equal(t, wire.NotifyEvent, frames[1].method)
	assert.Equal(t, wire.NotifyRunResultDone, frames[2].method)

	// First event frame must carry sessionId 42 and a text_delta event payload.
	ef0, ok := frames[0].params.(wire.EventFrame)
	require.True(t, ok, "expected wire.EventFrame, got %T", frames[0].params)
	assert.Equal(t, int64(42), ef0.SessionID)
	assert.Contains(t, string(ef0.Event), `"kind":"text_delta"`)
	assert.Contains(t, string(ef0.Event), `"text":"hi"`)

	// Final frame carries the RunResult fields.
	done, ok := frames[2].params.(wire.RunResultDoneFrame)
	require.True(t, ok, "expected wire.RunResultDoneFrame, got %T", frames[2].params)
	assert.Equal(t, int64(42), done.SessionID)
	assert.Equal(t, "psid-1", done.ProviderSessionID)
	assert.Equal(t, "claude-sonnet-4-6", done.Model)
	assert.Equal(t, 200000, done.ContextWindow)
	assert.Empty(t, done.StopErrMsg)
	assert.Zero(t, done.StopErrCode)

	// Session must be cleared after fanout finishes so subsequent Steer
	// returns ErrNoActiveTurn — exercised by a follow-up call.
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		_, err = h.Steer(ctx, wire.SteerParams{SessionID: 42, Text: "late"})
		assert.Error(c, err)
	}, time.Second, 10*time.Millisecond)
	assert.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestRuntime_Run_BadBackendJSON_Errors(t *testing.T) {
	ctx, _, _, _, h := setupRuntimeTest(t, &fullRT{})
	_, err := h.Run(ctx, wire.RunParams{Backend: json.RawMessage(`{bad`)})
	require.Error(t, err)
}

func TestRuntime_Run_BuiltinBackend_Rejected(t *testing.T) {
	ctx, _, _, _, h := setupRuntimeTest(t, &fullRT{})
	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeBuiltin)}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be)})
	require.Error(t, err)
}

func TestRuntime_Run_UnknownBackendType_Errors(t *testing.T) {
	// runtimeFor returns nil for unknown type.
	ctx, _, _, _, h := setupRuntimeTest(t, nil)
	be := agent_backend_entity.AgentBackend{Type: "ghost"}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be)})
	require.Error(t, err)
}

func TestRuntime_Run_ProviderLookupMissing_ReturnsProviderMissingCode(t *testing.T) {
	rt := &fullRT{}
	ctx, _, _, lookup, h := setupRuntimeTest(t, rt)
	be := agent_backend_entity.AgentBackend{
		Type:           string(agent_backend_entity.TypeClaudeCode),
		LLMProviderKey: "missing-key",
	}
	lookup.EXPECT().FindByKey(ctx, "missing-key").Return(nil, errors.New("provider missing-key not configured"))

	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be)})
	require.Error(t, err)

	var rpcErr *rpc.Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, rpc.ErrProviderMissing.Code, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "missing-key")
}

func TestRuntime_Run_RuntimeReturnsErr_RevokesToken(t *testing.T) {
	rt := &fullRT{}
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		return nil, nil, errors.New("boom")
	}
	ctx, _, _, _, h := setupRuntimeTest(t, rt)
	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be)})
	require.Error(t, err)
}

func TestRuntime_Run_StopErrAborted_RehydratesCode(t *testing.T) {
	rt := &fullRT{}
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		ch := make(chan agentruntime.Event)
		close(ch)
		return ch, &agentruntime.RunResult{StopErr: agentruntime.ErrAborted}, nil
	}
	ctx, notif, _, _, h := setupRuntimeTest(t, rt)
	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be), SessionID: 1})
	require.NoError(t, err)

	frames := notif.waitFrames(t, 1)
	done, ok := frames[0].params.(wire.RunResultDoneFrame)
	require.True(t, ok)
	assert.Equal(t, wire.ErrCodeAborted, done.StopErrCode)
	assert.Equal(t, agentruntime.ErrAborted.Error(), done.StopErrMsg)
}

// ── Steer / CancelSteer / DrainPending / Abort / SetPermissionMode ─────────

// runWithRT registers a session by calling Run with a runtime whose Run
// returns a never-closing channel — the goroutine stays alive so the
// session row remains registered for subsequent control RPCs.
func runWithRT(t *testing.T, h *handlers.RuntimeHandlers, ctx context.Context, sid int64) {
	t.Helper()
	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
	ack, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be), SessionID: sid})
	require.NoError(t, err)
	require.Equal(t, sid, ack.SessionID)
}

// runtimeWithLiveSession installs the fake RT and starts a session with the
// given sid; the runtime's events channel stays open so the row stays alive.
func runtimeWithLiveSession(t *testing.T, rt *fullRT, sid int64) (
	context.Context,
	*recordingNotifier,
	*handlers.RuntimeHandlers,
	chan agentruntime.Event,
) {
	t.Helper()
	live := make(chan agentruntime.Event)
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		return live, &agentruntime.RunResult{}, nil
	}
	ctx, notif, _, _, h := setupRuntimeTest(t, rt)
	runWithRT(t, h, ctx, sid)
	return ctx, notif, h, live
}

func TestRuntime_Steer_Success(t *testing.T) {
	rt := &fullRT{}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 9)
	defer close(live)

	_, err := h.Steer(ctx, wire.SteerParams{SessionID: 9, QueuedID: "q-1", Text: "stop"})
	require.NoError(t, err)
	assert.Equal(t, []steerCall{{sid: 9, queuedID: "q-1", text: "stop"}}, rt.steerCalls)
}

func TestRuntime_Steer_NoSession_ErrNoActiveTurn(t *testing.T) {
	ctx, _, _, _, h := setupRuntimeTest(t, &fullRT{})
	_, err := h.Steer(ctx, wire.SteerParams{SessionID: 99, Text: "x"})
	require.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestRuntime_Steer_BackendUnsupported_ErrUnsupported(t *testing.T) {
	// bareRT implements only Runtime — Steerer assertion fails.
	rt := &fullRT{} // start session via fullRT so registration succeeds
	ctx, _, _, _, h := setupRuntimeTest(t, rt)
	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
	live := make(chan agentruntime.Event)
	defer close(live)
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		return live, &agentruntime.RunResult{}, nil
	}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be), SessionID: 5})
	require.NoError(t, err)

	// Swap in a bare runtime *after* the session is registered, so the
	// Steer handler resolves via runtimeFor and finds bareRT (no Steerer).
	h.SwapRuntimeFor(func(_ agent_backend_entity.BackendType) agentruntime.Runtime { return bareRT{} })

	_, err = h.Steer(ctx, wire.SteerParams{SessionID: 5, Text: "x"})
	require.ErrorIs(t, err, agentruntime.ErrUnsupported)
}

func TestRuntime_CancelSteer_ReturnsRemoved(t *testing.T) {
	rt := &fullRT{
		cancelSteerFn: func(_ int64, _ string) ([]string, error) {
			return []string{"a", "b"}, nil
		},
	}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 1)
	defer close(live)

	out, err := h.CancelSteer(ctx, wire.CancelSteerParams{SessionID: 1, QueuedID: "q-1"})
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, out.Removed)
}

func TestRuntime_CancelSteer_NotFound_RehydrateSentinel(t *testing.T) {
	rt := &fullRT{
		cancelSteerFn: func(_ int64, _ string) ([]string, error) {
			return nil, agentruntime.ErrSteerNotFound
		},
	}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 1)
	defer close(live)

	_, err := h.CancelSteer(ctx, wire.CancelSteerParams{SessionID: 1, QueuedID: "q-x"})
	require.ErrorIs(t, err, agentruntime.ErrSteerNotFound)
}

func TestRuntime_DrainPending_ReturnsSteers(t *testing.T) {
	rt := &fullRT{
		drainFn: func(_ int64) []agentruntime.ConsumedSteer {
			return []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "a"}}
		},
	}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 2)
	defer close(live)

	out, err := h.DrainPending(ctx, wire.DrainParams{SessionID: 2})
	require.NoError(t, err)
	assert.Equal(t, []agentruntime.ConsumedSteer{{QueuedID: "q1", Text: "a"}}, out.Steers)
}

func TestRuntime_Abort_Success(t *testing.T) {
	rt := &fullRT{}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 3)
	defer close(live)

	_, err := h.Abort(ctx, wire.AbortParams{SessionID: 3})
	require.NoError(t, err)
	assert.Equal(t, []int64{3}, rt.abortCalls)
}

func TestRuntime_Abort_NoSession_ErrNoActiveTurn(t *testing.T) {
	ctx, _, _, _, h := setupRuntimeTest(t, &fullRT{})
	_, err := h.Abort(ctx, wire.AbortParams{SessionID: 7})
	require.ErrorIs(t, err, agentruntime.ErrNoActiveTurn)
}

func TestRuntime_SetPermissionMode_Success(t *testing.T) {
	rt := &fullRT{}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 4)
	defer close(live)

	_, err := h.SetPermissionMode(ctx, wire.SetPermissionModeParams{SessionID: 4, Mode: "plan"})
	require.NoError(t, err)
	assert.Equal(t, []setModeCall{{sid: 4, mode: "plan"}}, rt.setModeCalls)
}

func TestRuntime_SubmitAnswer_Success(t *testing.T) {
	rt := &fullRT{}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 5)
	defer close(live)

	qs := []agentruntime.AskQuestion{{Question: "ok?"}}
	as := []agentruntime.AskAnswer{{QuestionIndex: 0, Labels: []string{"yes"}}}
	_, err := h.SubmitAnswer(ctx, wire.SubmitAnswerParams{
		SessionID: 5, RequestID: "r-1", Questions: qs, Answers: as, Skipped: false,
	})
	require.NoError(t, err)
	require.Len(t, rt.submitAnswerCalls, 1)
	assert.Equal(t, "r-1", rt.submitAnswerCalls[0].requestID)
	assert.Equal(t, as, rt.submitAnswerCalls[0].answers)
}

func TestRuntime_SubmitToolPermission_Success(t *testing.T) {
	rt := &fullRT{}
	ctx, _, h, live := runtimeWithLiveSession(t, rt, 6)
	defer close(live)

	_, err := h.SubmitToolPermission(ctx, wire.SubmitToolPermissionParams{
		SessionID: 6, RequestID: "p-1", Allow: false, DenyReason: "nope",
	})
	require.NoError(t, err)
	require.Len(t, rt.submitToolPermCalls, 1)
	assert.Equal(t, "p-1", rt.submitToolPermCalls[0].requestID)
	assert.Equal(t, "nope", rt.submitToolPermCalls[0].denyReason)
	assert.False(t, rt.submitToolPermCalls[0].allow)
}

// TestRuntime_AllEventsRoundTripThroughNotify proves every sealed Event type
// can be pumped through the notify fanout (i.e. the JSON marshal step in
// the Run handler tolerates all 19 kinds without panic / silent drop).
func TestRuntime_AllEventsRoundTripThroughNotify(t *testing.T) {
	events := []agentruntime.Event{
		agentruntime.TextDelta{Text: "t"},
		agentruntime.ThinkingDelta{Text: "th"},
		agentruntime.PermissionModeChanged{Mode: "plan"},
		agentruntime.ContextWindowUpdated{Tokens: 200000},
		agentruntime.PlanUpdated{},
		agentruntime.UsageUpdate{},
		agentruntime.Retry{Message: "rate limited"},
		agentruntime.SteerConsumed{},
		agentruntime.Done{},
	}
	rt := &fullRT{}
	rt.runFn = func(_ context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
		ch := make(chan agentruntime.Event, len(events))
		for _, e := range events {
			ch <- e
		}
		close(ch)
		return ch, &agentruntime.RunResult{}, nil
	}
	ctx, notif, _, _, h := setupRuntimeTest(t, rt)

	be := agent_backend_entity.AgentBackend{Type: string(agent_backend_entity.TypeClaudeCode)}
	_, err := h.Run(ctx, wire.RunParams{Backend: backendJSON(t, be), SessionID: 100})
	require.NoError(t, err)

	frames := notif.waitFrames(t, len(events)+1) // +1 for runResultDone

	// Every event frame must carry SessionID + a non-empty Event RawMessage
	// whose top-level "kind" is non-empty.
	for i := range events {
		f := frames[i]
		assert.Equal(t, wire.NotifyEvent, f.method)
		ef, ok := f.params.(wire.EventFrame)
		require.True(t, ok, "frame %d: expected EventFrame, got %T", i, f.params)
		assert.Equal(t, int64(100), ef.SessionID)
		var head struct {
			Kind string `json:"kind"`
		}
		require.NoError(t, json.Unmarshal(ef.Event, &head))
		assert.NotEmpty(t, head.Kind, "frame %d kind must be present", i)
	}
}
