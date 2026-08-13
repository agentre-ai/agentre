package codex

import (
	"bufio"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCodexBehaviorEval is the deterministic, token-free behavior entrypoint.
// Its subtest path is the stable scenario/invariant identifier consumed by CI
// and external judges through `go test -json`.
func TestCodexBehaviorEval(t *testing.T) {
	t.Run("normal_turn/completes_once_with_streamed_text", evalNormalTurn)
	t.Run("streaming_text_and_tool/preserves_order_and_single_tool_terminal", evalTextAndTool)
	t.Run("approval_allow/responds_accept", func(t *testing.T) { evalApproval(t, true, "accept") })
	t.Run("approval_deny/responds_decline", func(t *testing.T) { evalApproval(t, false, "decline") })
	t.Run("user_input/correlates_question_answers", evalUserInput)
	t.Run("plan_and_update_plan/emits_cumulative_text_and_structured_steps", evalPlan)
	t.Run("usage_and_context/reports_last_usage_and_context_window", evalUsage)
	t.Run("steer/targets_expected_active_turn", evalSteer)
	t.Run("interrupt/converges_to_canceled_terminal", evalInterrupt)
	t.Run("eof_crash/fails_and_marks_process_non_reusable", evalCrash)
	t.Run("duplicate_out_of_order_unknown/isolates_and_deduplicates", evalNoisyWire)
	t.Run("cross_turn_isolation/late_old_turn_cannot_write_new_stream", evalCrossTurnIsolation)
}

func evalNormalTurn(t *testing.T) {
	stream := runEvalStream(t, "normal", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(evalNotification("item/agentMessage/delta", threadID, turnID, map[string]any{
			"itemId": "message-1", "delta": "hello",
		}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, nil)
	require.Len(t, events, 2)
	assert.Equal(t, EventTextDelta, events[0].Kind)
	assert.Equal(t, "hello", events[0].Text)
	assert.Equal(t, EventDone, events[1].Kind)
	assert.Equal(t, TurnStateCompleted, stream.State())
}

func evalTextAndTool(t *testing.T) {
	stream := runEvalStream(t, "tool", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(evalNotification("item/agentMessage/delta", threadID, turnID, map[string]any{
			"itemId": "message-1", "delta": "before ",
		}))
		item := map[string]any{
			"type": "commandExecution", "id": "command-1", "command": "pwd", "cwd": "/tmp",
		}
		h.send(evalNotification("item/started", threadID, turnID, map[string]any{"item": item}))
		item["status"] = "completed"
		item["aggregatedOutput"] = "/tmp\n"
		h.send(evalNotification("item/completed", threadID, turnID, map[string]any{"item": item}))
		// Duplicate delivery must be idempotent.
		h.send(evalNotification("item/completed", threadID, turnID, map[string]any{"item": item}))
		h.send(evalNotification("item/agentMessage/delta", threadID, turnID, map[string]any{
			"itemId": "message-1", "delta": "after",
		}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, nil)
	kinds := make([]EventKind, 0, len(events))
	var text strings.Builder
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == EventTextDelta {
			text.WriteString(ev.Text)
		}
	}
	assert.Equal(t, "before after", text.String())
	assert.Equal(t, []EventKind{EventTextDelta, EventPreToolUse, EventPostToolUse, EventTextDelta, EventDone}, kinds)
}

func evalApproval(t *testing.T, allow bool, wantDecision string) {
	response := make(chan json.RawMessage, 1)
	stream := runEvalStream(t, "approval", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(map[string]any{
			"id":     "approval-1",
			"method": appMethodItemCommandApprovalRequest,
			"params": map[string]any{
				"threadId": threadID, "turnId": turnID, "itemId": "command-1", "command": "pwd",
			},
		})
		if sc.Scan() {
			response <- append(json.RawMessage(nil), sc.Bytes()...)
		}
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, func(ev Event) {
		if ev.Approval != nil {
			require.NoError(t, stream.SubmitApproval(context.Background(), ev.Approval.RequestID, allow, false))
		}
	})
	assert.Equal(t, TurnStateCompleted, stream.State())
	assert.Equal(t, 1, countEvalKind(events, EventApprovalRequest))
	select {
	case raw := <-response:
		var got struct {
			Result struct {
				Decision string `json:"decision"`
			} `json:"result"`
		}
		require.NoError(t, json.Unmarshal(raw, &got))
		assert.Equal(t, wantDecision, got.Result.Decision)
	case <-time.After(time.Second):
		t.Fatal("approval response missing")
	}
}

func evalUserInput(t *testing.T) {
	response := make(chan json.RawMessage, 1)
	stream := runEvalStream(t, "input", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(map[string]any{
			"id":     91,
			"method": appMethodItemToolRequestUserInput,
			"params": map[string]any{
				"threadId": threadID, "turnId": turnID, "itemId": "input-tool",
				"questions": []any{map[string]any{
					"id": "choice", "header": "Mode", "question": "Which mode?",
					"options": []any{map[string]any{"label": "safe", "description": "Use safe mode"}},
				}},
			},
		})
		if sc.Scan() {
			response <- append(json.RawMessage(nil), sc.Bytes()...)
		}
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, func(ev Event) {
		if ev.RequestUserInput != nil {
			require.NoError(t, stream.SubmitUserInput(context.Background(), ev.RequestUserInput.RequestID, map[string][]string{
				"choice": {"safe"},
			}))
		}
	})
	assert.Equal(t, 1, countEvalKind(events, EventRequestUserInput))
	select {
	case raw := <-response:
		assert.JSONEq(t, `{"id":91,"result":{"answers":{"choice":{"answers":["safe"]}}}}`, string(raw))
	case <-time.After(time.Second):
		t.Fatal("user input response missing")
	}
}

func evalPlan(t *testing.T) {
	stream := runEvalStream(t, "plan", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		for _, delta := range []string{"1. inspect\n", "2. verify\n"} {
			h.send(evalNotification(appMethodItemPlanDelta, threadID, turnID, map[string]any{
				"itemId": "plan-1", "delta": delta,
			}))
		}
		h.send(evalNotification(appMethodTurnPlanUpdated, threadID, turnID, map[string]any{
			"plan": []any{
				map[string]any{"step": "inspect", "status": "completed"},
				map[string]any{"step": "verify", "status": "inProgress"},
			},
		}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, nil)
	var texts []string
	var structured []PlanStep
	for _, ev := range events {
		if ev.Kind == EventPlanUpdated && ev.PlanText != "" {
			texts = append(texts, ev.PlanText)
		}
		if len(ev.Plan) > 0 {
			structured = ev.Plan
		}
	}
	assert.Equal(t, []string{"1. inspect\n", "1. inspect\n2. verify\n"}, texts)
	require.Len(t, structured, 2)
	assert.Equal(t, "verify", structured[1].Step)
	assert.Equal(t, 1, countEvalKind(events, EventPreToolUse), "structured update_plan has one tool projection")
	assert.Equal(t, 1, countEvalKind(events, EventPostToolUse))
}

func evalUsage(t *testing.T) {
	stream := runEvalStream(t, "usage", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(evalNotification(appMethodThreadTokenUsageUpdated, threadID, turnID, map[string]any{
			"tokenUsage": map[string]any{
				"last": map[string]any{"inputTokens": 12, "outputTokens": 4, "totalTokens": 16},
			},
			"modelContextWindow": 258400,
		}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, nil)
	require.Len(t, events, 2)
	assert.Equal(t, EventUsage, events[0].Kind)
	assert.Equal(t, 12, events[0].Usage.PromptTokens)
	assert.Equal(t, 258400, events[0].ContextWindow)
	assert.Equal(t, 258400, events[1].ContextWindow)
}

func evalSteer(t *testing.T) {
	captured := make(chan rpcReq, 1)
	ready := make(chan struct{})
	stream := runEvalStream(t, "steer", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		close(ready)
		req := readRPCReq(t, sc)
		captured <- req
		respondRPC(h, req, map[string]any{})
		h.send(evalNotification(appMethodItemAgentMessageDelta, threadID, turnID, map[string]any{
			"itemId": "message-steered", "delta": "adjusted",
		}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	<-ready
	require.NoError(t, stream.Steer(context.Background(), "change direction"))
	events := drainEvalStream(stream, nil)
	assert.Equal(t, "adjusted", events[0].Text)
	req := <-captured
	assert.Equal(t, appMethodTurnSteer, req.Method)
	assert.JSONEq(t, `{"threadId":"thread-steer","expectedTurnId":"turn-steer","input":[{"type":"text","text":"change direction","text_elements":[]}]}`, string(req.Params))
}

func evalInterrupt(t *testing.T) {
	captured := make(chan rpcReq, 1)
	// turnActive 由 fake server 在 turn/start 回 inProgress 之后关闭：只有此刻
	// Stream 才确定持有 active turn，Interrupt 才能安全发出（与 evalSteer 的
	// ready 门闩同构，消除“turn 尚未就绪就发 interrupt”的时序竞争）。
	turnActive := make(chan struct{})
	stream := runEvalStream(t, "interrupt", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		close(turnActive)
		req := readRPCReq(t, sc)
		captured <- req
		respondRPC(h, req, map[string]any{})
		h.send(evalCompleted(threadID, turnID, "interrupted"))
	}, nil)
	<-turnActive
	// Interrupt 经 goroutine + 超时调用：真挂住会 fail-fast，不会卡住测试。
	// ctx 带截止时间，避免 Interrupt 在 fake 无响应时无限阻塞。
	interruptCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case err := <-interruptStream(interruptCtx, stream):
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Interrupt did not return")
	}
	events := drainEvalStream(stream, nil)
	assert.Equal(t, 1, countEvalKind(events, EventDone))
	assert.Equal(t, TurnStateCanceled, stream.State())
	req := <-captured
	assert.JSONEq(t, `{"threadId":"thread-interrupt","turnId":"turn-interrupt"}`, string(req.Params))
}

func evalCrash(t *testing.T) {
	crash := make(chan struct{})
	stream := runEvalStream(t, "crash", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, _, _ string) {
		<-crash
		_ = h.Kill()
	}, nil)
	close(crash)
	events := drainEvalStream(stream, nil)
	assert.Equal(t, 1, countEvalKind(events, EventError))
	assert.Equal(t, TurnStateFailed, stream.State())
	assert.False(t, stream.Reusable())
}

func evalNoisyWire(t *testing.T) {
	stream := runEvalStream(t, "noise", func(t *testing.T, h *fakeAppServerHandle, sc *bufio.Scanner, threadID, turnID string) {
		h.send(evalNotification("turn/future/metadataUpdated", threadID, turnID, map[string]any{"future": true}))
		h.send(evalNotification(appMethodItemAgentMessageDelta, threadID, "wrong-turn", map[string]any{
			"itemId": "wrong", "delta": "WRONG",
		}))
		item := map[string]any{
			"type": "webSearch", "id": "search-1", "arguments": map[string]any{"query": "protocol"},
			"result": map[string]any{"items": []any{}},
		}
		h.send(evalNotification(appMethodItemStarted, threadID, turnID, map[string]any{"item": item}))
		h.send(evalNotification(appMethodItemCompleted, threadID, turnID, map[string]any{"item": item}))
		h.send(evalNotification(appMethodItemCompleted, threadID, turnID, map[string]any{"item": item}))
		h.send(evalCompleted(threadID, turnID, "completed"))
	}, nil)
	events := drainEvalStream(stream, nil)
	assert.Equal(t, 0, countEvalKind(events, EventTextDelta))
	assert.Equal(t, 1, countEvalKind(events, EventPreToolUse))
	assert.Equal(t, 1, countEvalKind(events, EventPostToolUse))
	assert.Equal(t, 1, countEvalKind(events, EventDone))
}

func evalCrossTurnIsolation(t *testing.T) {
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, "thread-cross")
		firstStart := readRPCReq(t, sc)
		respondRPC(h, firstStart, map[string]any{"turn": map[string]any{"id": "turn-first", "status": "inProgress"}})
		h.send(evalCompleted("thread-cross", "turn-first", "completed"))
		secondStart := readRPCReq(t, sc)
		respondRPC(h, secondStart, map[string]any{"turn": map[string]any{"id": "turn-second", "status": "inProgress"}})
		h.send(evalNotification(appMethodItemAgentMessageDelta, "thread-cross", "turn-first", map[string]any{
			"itemId": "old", "delta": "OLD",
		}))
		h.send(evalNotification(appMethodItemAgentMessageDelta, "thread-cross", "turn-second", map[string]any{
			"itemId": "new", "delta": "NEW",
		}))
		h.send(evalCompleted("thread-cross", "turn-second", "completed"))
	}
	client := New(WithAppServerRunnerForTesting(runner), WithKillGrace(10*time.Millisecond))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	sess, err := client.OpenSession(ctx)
	require.NoError(t, err)
	defer func() { _ = sess.Close(context.Background()) }()
	first, err := sess.Stream(ctx, "first")
	require.NoError(t, err)
	drainEvalStream(first, nil)
	second, err := sess.Stream(ctx, "second")
	require.NoError(t, err)
	events := drainEvalStream(second, nil)
	var text strings.Builder
	for _, ev := range events {
		if ev.Kind == EventTextDelta {
			text.WriteString(ev.Text)
		}
	}
	assert.Equal(t, "NEW", text.String())
	assert.Equal(t, TurnStateCompleted, second.State())
}

type evalScript func(*testing.T, *fakeAppServerHandle, *bufio.Scanner, string, string)

// evalFakeServerGrace 是行为评估假 app server 的 kill grace。假服务端同步应答、
// 真实往返是亚毫秒级，因此 grace 给到秒级后，drain 在 Interrupt 时启动的 grace
// 定时器在测试生命周期内按构造不可能触发，也就不会抢占 interrupt RPC 往返而
// 偶发 ErrNoActiveTurn（曾用 20ms，CI 负载下的调度延迟会超过它，见
// evalInterrupt 的 flake）。真实中断语义仍由 TestStream_InterruptForwardsRPC
// 独立验证。
const evalFakeServerGrace = time.Second

func runEvalStream(t *testing.T, suffix string, script evalScript, opts []Option) *Stream {
	t.Helper()
	threadID := "thread-" + suffix
	turnID := "turn-" + suffix
	runner := &fakeAppServerRunner{t: t}
	runner.handler = func(t *testing.T, h *fakeAppServerHandle) {
		sc := bufio.NewScanner(h.stdinR)
		respondAppServerInit(t, h, sc)
		respondThreadStart(t, h, sc, `{"approvalPolicy":"never"}`, threadID)
		turnStart := readRPCReq(t, sc)
		respondRPC(h, turnStart, map[string]any{"turn": map[string]any{"id": turnID, "status": "inProgress"}})
		script(t, h, sc, threadID, turnID)
	}
	clientOpts := make([]Option, 0, 2+len(opts))
	clientOpts = append(clientOpts, WithAppServerRunnerForTesting(runner), WithKillGrace(evalFakeServerGrace))
	clientOpts = append(clientOpts, opts...)
	client := New(clientOpts...)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	stream, err := client.Stream(ctx, suffix)
	require.NoError(t, err)
	return stream
}

func drainEvalStream(stream *Stream, onEvent func(Event)) []Event {
	var events []Event
	for stream.Next() {
		ev := stream.Event()
		events = append(events, ev)
		if onEvent != nil {
			onEvent(ev)
		}
	}
	return events
}

func evalNotification(method, threadID, turnID string, extra map[string]any) map[string]any {
	params := map[string]any{"threadId": threadID, "turnId": turnID}
	for key, value := range extra {
		params[key] = value
	}
	return map[string]any{"method": method, "params": params}
}

func evalCompleted(threadID, turnID, status string) map[string]any {
	return evalNotification(appMethodTurnCompleted, threadID, turnID, map[string]any{
		"turn": map[string]any{"id": turnID, "status": status},
	})
}

func countEvalKind(events []Event, kind EventKind) int {
	count := 0
	for _, ev := range events {
		if ev.Kind == kind {
			count++
		}
	}
	return count
}
