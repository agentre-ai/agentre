package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBehavior_PlanDeltaIsCumulative(t *testing.T) {
	// Given an app-server turn that streams a plan in more than one delta.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-plan-delta")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-plan-delta", "status": "inProgress"},
		})
		for _, delta := range []string{"1. Inspect\n", "2. Verify\n"} {
			h.send(map[string]any{
				"method": "item/plan/delta",
				"params": map[string]any{
					"threadId": "thread-plan-delta",
					"turnId":   "turn-plan-delta",
					"itemId":   "plan-1",
					"delta":    delta,
				},
			})
		}
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-plan-delta",
				"turnId":   "turn-plan-delta",
				"turn":     map[string]any{"id": "turn-plan-delta", "status": "completed"},
			},
		})
	}

	// When the wrapper drains the turn.
	stream := startTestStream(t, runner, "plan")
	var plans []string
	for stream.Next() {
		if ev := stream.Event(); ev.Kind == EventPlanUpdated {
			plans = append(plans, ev.PlanText)
		}
	}
	require.NoError(t, stream.Close(context.Background()))

	// Then each plan event is a complete snapshot, not an implementation delta.
	assert.Equal(t, []string{
		"1. Inspect\n",
		"1. Inspect\n2. Verify\n",
	}, plans)
}

func TestBehavior_FailedTurnWithoutDetailsIsDiagnostic(t *testing.T) {
	// Given Codex reports the terminal status as failed without an optional error object.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-failed")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-failed", "status": "inProgress"},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-failed",
				"turnId":   "turn-failed",
				"turn":     map[string]any{"id": "turn-failed", "status": "failed"},
			},
		})
	}

	// When the wrapper drains the terminal notification.
	stream := startTestStream(t, runner, "fail")
	var gotErr error
	doneCount := 0
	for stream.Next() {
		ev := stream.Event()
		if ev.Kind == EventError {
			gotErr = ev.Err
		}
		if ev.Kind == EventDone {
			doneCount++
		}
	}

	// Then failure is surfaced exactly once and cannot masquerade as success.
	require.Error(t, gotErr)
	assert.Contains(t, gotErr.Error(), "failed")
	assert.Equal(t, 1, doneCount)
}

func TestBehavior_MalformedJSONIsNotSilentlyDiscarded(t *testing.T) {
	// Given stdout contains a JSON-looking line that is not valid JSON-RPC.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-invalid-json")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-invalid-json", "status": "inProgress"},
		})
		_, _ = h.stdoutW.Write([]byte("{not-json}\n"))
		_ = h.Kill()
	}

	// When the stream consumes stdout, then the protocol violation is diagnosable.
	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, startErr := client.Stream(ctx, "invalid")
	if startErr != nil {
		assert.Contains(t, strings.ToLower(startErr.Error()), "json")
		return
	}
	var gotErr error
	for stream.Next() {
		if ev := stream.Event(); ev.Kind == EventError {
			gotErr = ev.Err
		}
	}
	require.Error(t, gotErr)
	assert.Contains(t, strings.ToLower(gotErr.Error()), "json")
}

func TestBehavior_UnknownServerRequestReturnsMethodNotFound(t *testing.T) {
	// Given a future app-server sends a server request the wrapper does not implement.
	runner := &fakeAppServerRunner{t: t}
	responseCaptured := make(chan json.RawMessage, 1)
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-unknown-request")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-unknown-request", "status": "inProgress"},
		})
		h.send(map[string]any{
			"id":     "future-request-1",
			"method": "item/future/requestAction",
			"params": map[string]any{"threadId": "thread-unknown-request", "turnId": "turn-unknown-request"},
		})
		if sc.Scan() {
			responseCaptured <- append(json.RawMessage(nil), sc.Bytes()...)
		}
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-unknown-request",
				"turnId":   "turn-unknown-request",
				"turn":     map[string]any{"id": "turn-unknown-request", "status": "completed"},
			},
		})
	}

	// When the wrapper receives it, it must not claim a successful empty result.
	stream := startTestStream(t, runner, "future")
	for stream.Next() {
	}
	require.NoError(t, stream.Close(context.Background()))
	select {
	case raw := <-responseCaptured:
		var response struct {
			Error *rpcError `json:"error"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		require.NotNil(t, response.Error)
		assert.EqualValues(t, -32601, response.Error.Code)
	case <-time.After(2 * time.Second):
		t.Fatal("unknown server request was not answered")
	}
}

func TestBehavior_WaiterCannotBeAnsweredAfterTerminal(t *testing.T) {
	// Given an approval request races with a terminal notification.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-terminal-waiter")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-terminal-waiter", "status": "inProgress"},
		})
		h.send(map[string]any{
			"id":     "approval-terminal",
			"method": "item/commandExecution/requestApproval",
			"params": map[string]any{
				"threadId": "thread-terminal-waiter",
				"turnId":   "turn-terminal-waiter",
				"itemId":   "command-terminal",
				"command":  "true",
			},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-terminal-waiter",
				"turnId":   "turn-terminal-waiter",
				"turn":     map[string]any{"id": "turn-terminal-waiter", "status": "interrupted"},
			},
		})
	}

	// When the turn closes before the user answers.
	stream := startTestStream(t, runner, "wait")
	requestID := ""
	for stream.Next() {
		if ev := stream.Event(); ev.Approval != nil {
			requestID = ev.Approval.RequestID
		}
	}
	require.NotEmpty(t, requestID)

	// Then the waiter is released and a late answer cannot write into a terminal turn.
	err := stream.SubmitApproval(context.Background(), requestID, true, false)
	require.ErrorIs(t, err, ErrNoActiveTurn)
}

func TestBehavior_FailedWaiterResponseDoesNotLosePendingRequest(t *testing.T) {
	t.Run("Given user input is waiting when its response cannot be written then the waiter remains pending", func(t *testing.T) {
		h := newFakeAppServerHandle()
		t.Cleanup(func() { _ = h.Kill() })
		stream := newStream(&appClient{proc: h}, 10*time.Millisecond, "thread-write-failure", "turn-write-failure", "")
		requestID := stream.registerUserInputRequest(json.RawMessage(`"input-write-failure"`))
		require.NoError(t, h.stdinR.Close())

		err := stream.SubmitUserInput(context.Background(), requestID, map[string][]string{"q1": {"yes"}})
		require.Error(t, err)
		stream.userInputMu.Lock()
		_, stillPending := stream.userInputRequests[requestID]
		stream.userInputMu.Unlock()
		assert.True(t, stillPending)
		assert.Equal(t, TurnStateWaiting, stream.State())
	})

	t.Run("Given approval is waiting when its response cannot be written then the waiter remains pending", func(t *testing.T) {
		h := newFakeAppServerHandle()
		t.Cleanup(func() { _ = h.Kill() })
		stream := newStream(&appClient{proc: h}, 10*time.Millisecond, "thread-write-failure", "turn-write-failure", "")
		requestID := stream.registerApprovalRequest(
			json.RawMessage(`"approval-write-failure"`),
			appMethodItemCommandApprovalRequest,
			json.RawMessage(`{"threadId":"thread-write-failure","turnId":"turn-write-failure"}`),
		)
		require.NoError(t, h.stdinR.Close())

		err := stream.SubmitApproval(context.Background(), requestID, true, false)
		require.Error(t, err)
		stream.userInputMu.Lock()
		_, stillPending := stream.approvalRequests[requestID]
		stream.userInputMu.Unlock()
		assert.True(t, stillPending)
		assert.Equal(t, TurnStateWaiting, stream.State())
	})
}

func TestBehavior_DuplicateToolCompletionEmitsOneTerminalToolEvent(t *testing.T) {
	// Given item/completed is duplicated by a reconnect or retry boundary.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-duplicate")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-duplicate", "status": "inProgress"},
		})
		item := map[string]any{
			"type":             "commandExecution",
			"id":               "command-duplicate",
			"command":          "pwd",
			"cwd":              "/tmp",
			"status":           "completed",
			"aggregatedOutput": "/tmp\n",
		}
		for i := 0; i < 2; i++ {
			h.send(map[string]any{
				"method": "item/completed",
				"params": map[string]any{
					"threadId": "thread-duplicate",
					"turnId":   "turn-duplicate",
					"item":     item,
				},
			})
		}
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-duplicate",
				"turnId":   "turn-duplicate",
				"turn":     map[string]any{"id": "turn-duplicate", "status": "completed"},
			},
		})
	}

	// When the stream drains, then the tool has one start and one completion.
	stream := startTestStream(t, runner, "duplicate")
	pre, post := 0, 0
	for stream.Next() {
		switch stream.Event().Kind {
		case EventPreToolUse:
			pre++
		case EventPostToolUse:
			post++
		}
	}
	assert.Equal(t, 1, pre)
	assert.Equal(t, 1, post)
}

func TestBehavior_UnknownNotificationIsForwardCompatible(t *testing.T) {
	// Given a newer app-server adds a notification with fields this wrapper does not know.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-future-notification")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-future-notification", "status": "inProgress"},
		})
		h.send(map[string]any{
			"method": "turn/future/metadataUpdated",
			"params": map[string]any{
				"threadId": "thread-future-notification",
				"turnId":   "turn-future-notification",
				"newField": map[string]any{"nested": true},
			},
		})
		h.send(map[string]any{
			"method": "item/agentMessage/delta",
			"params": map[string]any{
				"threadId": "thread-future-notification",
				"turnId":   "turn-future-notification",
				"itemId":   "message-future",
				"delta":    "still works",
			},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-future-notification",
				"turnId":   "turn-future-notification",
				"turn":     map[string]any{"id": "turn-future-notification", "status": "completed"},
			},
		})
	}

	// When it is followed by known progress, then the turn remains healthy.
	stream := startTestStream(t, runner, "future notification")
	var text strings.Builder
	var stopErr error
	for stream.Next() {
		ev := stream.Event()
		if ev.Kind == EventTextDelta {
			text.WriteString(ev.Text)
		}
		if ev.Kind == EventError {
			stopErr = errors.Join(stopErr, ev.Err)
		}
	}
	assert.Equal(t, "still works", text.String())
	assert.NoError(t, stopErr)
}

func TestBehavior_ContextCancelInterruptsAndConvergesBeforeReuse(t *testing.T) {
	// Given a persistent app-server turn is running when its caller context is canceled.
	runner := &fakeAppServerRunner{t: t}
	interruptCaptured := make(chan rpcReq, 1)
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-cancel")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-cancel", "status": "inProgress"},
		})
		interrupt := readRPCReq(t, sc)
		interruptCaptured <- interrupt
		respondRPC(h, interrupt, map[string]any{})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-cancel",
				"turnId":   "turn-cancel",
				"turn":     map[string]any{"id": "turn-cancel", "status": "interrupted"},
			},
		})
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(100*time.Millisecond))
	openCtx, openCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer openCancel()
	sess, err := client.OpenSession(openCtx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	turnCtx, cancelTurn := context.WithCancel(context.Background())
	stream, err := sess.Stream(turnCtx, "cancel me")
	require.NoError(t, err)
	cancelTurn()

	var kinds []EventKind
	for stream.Next() {
		kinds = append(kinds, stream.Event().Kind)
	}

	select {
	case req := <-interruptCaptured:
		assert.Equal(t, appMethodTurnInterrupt, req.Method)
		assert.JSONEq(t, `{"threadId":"thread-cancel","turnId":"turn-cancel"}`, string(req.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not send turn/interrupt")
	}
	assert.Contains(t, kinds, EventError, "caller cancellation remains observable")
	assert.Contains(t, kinds, EventDone, "matching interrupted completion closes the turn")
	assert.Equal(t, TurnStateCanceled, stream.State())
	assert.True(t, stream.Reusable(), "a converged interrupt keeps the persistent process reusable")
}

func TestBehavior_ExplicitInterruptAckWithoutTerminalCannotStayStuck(t *testing.T) {
	// Given an explicit interrupt is acknowledged but app-server never sends a
	// terminal notification, the Stream itself must own the convergence timer.
	runner := &fakeAppServerRunner{t: t}
	processKilled := make(chan struct{})
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-explicit-interrupt")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-explicit-interrupt", "status": "inProgress"},
		})
		interrupt := readRPCReq(t, sc)
		respondRPC(h, interrupt, map[string]any{})
		<-h.done
		close(processKilled)
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(40*time.Millisecond))
	openCtx, openCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer openCancel()
	sess, err := client.OpenSession(openCtx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()
	stream, err := sess.Stream(context.Background(), "interrupt without terminal")
	require.NoError(t, err)
	require.NoError(t, stream.Interrupt(context.Background()))

	drained := make(chan struct{})
	go func() {
		for stream.Next() {
		}
		close(drained)
	}()
	select {
	case <-drained:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("explicit interrupt remained stuck without a terminal notification")
	}
	select {
	case <-processKilled:
	case <-time.After(2 * time.Second):
		t.Fatal("stuck explicit interrupt did not terminate app-server")
	}
	assert.Equal(t, TurnStateFailed, stream.State())
	assert.False(t, stream.Reusable())
}

func TestBehavior_ExplicitInterruptWithoutRPCAckCannotBlockCallerForever(t *testing.T) {
	// Given app-server reads an explicit interrupt but never acknowledges the
	// RPC, the Stream watchdog must terminate the process and unblock even a
	// caller that supplied context.Background.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-unacked-interrupt")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-unacked-interrupt", "status": "inProgress"},
		})
		_ = readRPCReq(t, sc)
		<-h.done
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(40*time.Millisecond))
	openCtx, openCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer openCancel()
	sess, err := client.OpenSession(openCtx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()
	stream, err := sess.Stream(context.Background(), "interrupt without rpc ack")
	require.NoError(t, err)

	interruptDone := make(chan error, 1)
	go func() { interruptDone <- stream.Interrupt(context.Background()) }()
	select {
	case interruptErr := <-interruptDone:
		require.Error(t, interruptErr)
	case <-time.After(250 * time.Millisecond):
		t.Fatal("unacknowledged explicit interrupt blocked its caller")
	}
	for stream.Next() {
	}
	assert.Equal(t, TurnStateFailed, stream.State())
	assert.False(t, stream.Reusable())
}

func TestBehavior_ConcurrentInterruptIsIdempotentOnTheWire(t *testing.T) {
	// Given two callers interrupt the same running turn concurrently, only one
	// turn/interrupt request may be written and both callers observe convergence.
	runner := &fakeAppServerRunner{t: t}
	requestCount := make(chan int, 1)
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-concurrent-interrupt")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-concurrent-interrupt", "status": "inProgress"},
		})
		first := readRPCReq(t, sc)
		time.Sleep(20 * time.Millisecond)
		respondRPC(h, first, map[string]any{})

		secondRequest := make(chan rpcReq, 1)
		go func() {
			if sc.Scan() {
				var req rpcReq
				if json.Unmarshal(sc.Bytes(), &req) == nil {
					secondRequest <- req
				}
			}
		}()
		count := 1
		select {
		case second := <-secondRequest:
			count++
			respondRPC(h, second, map[string]any{})
		case <-time.After(5 * time.Millisecond):
		}
		requestCount <- count
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-concurrent-interrupt",
				"turnId":   "turn-concurrent-interrupt",
				"turn":     map[string]any{"id": "turn-concurrent-interrupt", "status": "interrupted"},
			},
		})
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(100*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stream, err := client.Stream(ctx, "concurrent interrupt")
	require.NoError(t, err)
	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- stream.Interrupt(context.Background())
		}()
	}
	close(start)
	for range 2 {
		interruptErr := <-errs
		assert.True(t, interruptErr == nil || errors.Is(interruptErr, ErrNoActiveTurn), interruptErr)
	}
	for stream.Next() {
	}
	assert.Equal(t, 1, <-requestCount)
	assert.Equal(t, TurnStateCanceled, stream.State())
}

func TestBehavior_ContextCancelInterruptTimeoutFailsAndKillsProcess(t *testing.T) {
	// Given app-server accepts the interrupt request bytes but never acknowledges
	// the RPC or emits turn/completed, cancellation must still converge locally.
	runner := &fakeAppServerRunner{t: t}
	interruptCaptured := make(chan rpcReq, 1)
	processKilled := make(chan struct{})
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-cancel-timeout")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-cancel-timeout", "status": "inProgress"},
		})
		interruptCaptured <- readRPCReq(t, sc)
		<-h.done
		close(processKilled)
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(40*time.Millisecond))
	openCtx, openCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer openCancel()
	sess, err := client.OpenSession(openCtx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	turnCtx, cancelTurn := context.WithCancel(context.Background())
	stream, err := sess.Stream(turnCtx, "cancel without terminal")
	require.NoError(t, err)
	cancelTurn()

	var stopErrors []error
	for stream.Next() {
		if ev := stream.Event(); ev.Kind == EventError && ev.Err != nil {
			stopErrors = append(stopErrors, ev.Err)
		}
	}

	select {
	case req := <-interruptCaptured:
		assert.Equal(t, appMethodTurnInterrupt, req.Method)
		assert.JSONEq(t, `{"threadId":"thread-cancel-timeout","turnId":"turn-cancel-timeout"}`, string(req.Params))
	case <-time.After(2 * time.Second):
		t.Fatal("context cancellation did not send turn/interrupt")
	}
	select {
	case <-processKilled:
	case <-time.After(2 * time.Second):
		t.Fatal("non-responsive app-server process was not terminated")
	}
	require.NotEmpty(t, stopErrors)
	assert.True(t, errors.Is(stopErrors[0], context.Canceled), "caller cancellation must remain observable")
	assert.Equal(t, TurnStateFailed, stream.State())
	assert.False(t, stream.Reusable(), "an unconfirmed interrupt cannot safely reuse the process")
}

func TestBehavior_LatePreviousTurnTrafficCannotPolluteNextTurn(t *testing.T) {
	// Given one persistent app-server process is reused for two serialized turns.
	runner := &fakeAppServerRunner{t: t}
	lateRequestResponse := make(chan json.RawMessage, 1)
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-isolation")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-old", "status": "inProgress"},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turnId":   "turn-old",
				"turn":     map[string]any{"id": "turn-old", "status": "completed"},
			},
		})

		secondStart := readRPCReq(t, sc)
		respondRPC(h, secondStart, map[string]any{
			"turn": map[string]any{"id": "turn-new", "status": "inProgress"},
		})
		h.send(map[string]any{
			"method": "turn/started",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turn":     map[string]any{"id": "turn-old", "status": "inProgress"},
			},
		})
		h.send(map[string]any{
			"id":     "late-approval",
			"method": "item/commandExecution/requestApproval",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turnId":   "turn-old",
				"itemId":   "old-command",
			},
		})
		if sc.Scan() {
			lateRequestResponse <- append(json.RawMessage(nil), sc.Bytes()...)
		}
		h.send(map[string]any{
			"method": "item/agentMessage/delta",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turnId":   "turn-old",
				"itemId":   "old-text",
				"delta":    "OLD",
			},
		})
		h.send(map[string]any{
			"method": "item/agentMessage/delta",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turnId":   "turn-new",
				"itemId":   "new-text",
				"delta":    "NEW",
			},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-isolation",
				"turnId":   "turn-new",
				"turn":     map[string]any{"id": "turn-new", "status": "completed"},
			},
		})
	}

	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := client.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()

	first, err := sess.Stream(ctx, "first")
	require.NoError(t, err)
	for first.Next() {
	}
	require.NoError(t, first.Close(ctx))

	second, err := sess.Stream(ctx, "second")
	require.NoError(t, err)
	var text strings.Builder
	approvals := 0
	for second.Next() {
		ev := second.Event()
		if ev.Kind == EventTextDelta {
			text.WriteString(ev.Text)
		}
		if ev.Kind == EventApprovalRequest {
			approvals++
		}
	}

	assert.Equal(t, "NEW", text.String())
	assert.Zero(t, approvals)
	assert.Equal(t, TurnStateCompleted, second.State())
	select {
	case raw := <-lateRequestResponse:
		var response struct {
			Error *rpcError `json:"error"`
		}
		require.NoError(t, json.Unmarshal(raw, &response))
		require.NotNil(t, response.Error)
	case <-time.After(2 * time.Second):
		t.Fatal("late request was not safely rejected")
	}
}

func TestBehavior_ServerRequestResolvedReleasesWaiter(t *testing.T) {
	// Given Codex resolves a request server-side before the Agentre user answers it.
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-resolved")
		respondRPC(h, readRPCReq(t, sc), map[string]any{
			"turn": map[string]any{"id": "turn-resolved", "status": "inProgress"},
		})
		h.send(map[string]any{
			"id":     "input-auto-resolved",
			"method": "item/tool/requestUserInput",
			"params": map[string]any{
				"threadId": "thread-resolved",
				"turnId":   "turn-resolved",
				"itemId":   "tool-input",
				"questions": []any{map[string]any{
					"id": "q1", "header": "Choice", "question": "Continue?", "options": []any{},
				}},
			},
		})
		h.send(map[string]any{
			"method": "serverRequest/resolved",
			"params": map[string]any{
				"threadId":  "thread-resolved",
				"requestId": "input-auto-resolved",
			},
		})
		h.send(map[string]any{
			"method": "turn/completed",
			"params": map[string]any{
				"threadId": "thread-resolved",
				"turnId":   "turn-resolved",
				"turn":     map[string]any{"id": "turn-resolved", "status": "completed"},
			},
		})
	}

	stream := startTestStream(t, runner, "resolved")
	requestID := ""
	resolvedKind := RequestKind("")
	for stream.Next() {
		ev := stream.Event()
		if ev.RequestUserInput != nil {
			requestID = ev.RequestUserInput.RequestID
		}
		if ev.RequestResolved != nil {
			resolvedKind = ev.RequestResolved.Kind
		}
	}

	assert.Equal(t, "input-auto-resolved", requestID)
	assert.Equal(t, RequestKindUserInput, resolvedKind)
	assert.ErrorIs(t, stream.SubmitUserInput(context.Background(), requestID, nil), ErrNoActiveTurn)
}

func TestBehavior_DiagnosticsAreBoundedAndRedacted(t *testing.T) {
	// Given subprocess and RPC diagnostics contain credential-shaped values.
	fakeSecret := "sk-test-1234567890abcdefghijklmnopqrstuvwxyz"
	rpcErr := (&rpcError{
		Code:    -32000,
		Message: "authorization failed for Bearer " + fakeSecret,
		Data:    json.RawMessage(`{"OPENAI_API_KEY":"` + fakeSecret + `"}`),
	}).Error()
	exitErr := (&ExitError{
		Err:    errors.New("exit status 1"),
		Stderr: "OPENAI_API_KEY=" + fakeSecret,
	}).Error()
	turnErr := appTurnErr(&appTurn{
		Status: appStatusFailed,
		Error: &appTurnError{
			Message:           "request failed with api_key=" + fakeSecret,
			AdditionalDetails: "authorization: Bearer " + fakeSecret,
		},
	}).Error()
	retry := appRetryEvent(appNotification{Error: &appNotifyError{
		Message:           "retry 1/3 token=" + fakeSecret,
		AdditionalDetails: "authorization: Bearer " + fakeSecret,
	}})

	// Then user-visible errors retain diagnosis but never the credential value or raw RPC data.
	assert.NotContains(t, rpcErr, fakeSecret)
	assert.NotContains(t, rpcErr, "OPENAI_API_KEY")
	assert.Contains(t, rpcErr, "rpc error -32000")
	assert.NotContains(t, exitErr, fakeSecret)
	assert.Contains(t, exitErr, "process exited")
	assert.NotContains(t, turnErr, fakeSecret)
	assert.NotContains(t, retry.Message, fakeSecret)
	assert.NotContains(t, retry.AdditionalDetails, fakeSecret)

	// And stderr retention is a fixed-size tail rather than an unbounded process-lifetime buffer.
	buf := &lockedBuffer{}
	prefix := strings.Repeat("x", 80*1024)
	_, err := buf.Write([]byte(prefix + "diagnostic-tail"))
	require.NoError(t, err)
	assert.LessOrEqual(t, len(buf.String()), 64*1024)
	assert.True(t, strings.HasSuffix(buf.String(), "diagnostic-tail"))
}

func startTestStream(t *testing.T, runner *fakeAppServerRunner, prompt string) *Stream {
	t.Helper()
	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	stream, err := client.Stream(ctx, prompt)
	require.NoError(t, err)
	return stream
}
