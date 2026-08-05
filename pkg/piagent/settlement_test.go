package piagent

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	preSettlementBarrier      = "__pre-settlement-barrier__"
	settlementBarrierTimeout  = 2 * time.Second
	settlementTerminalTimeout = 2 * time.Second
)

// A1: Given a successful low-level agent_end followed by compaction, when the
// prompt stream is consumed, then compaction and stats are retained and Done
// is emitted only after agent_settled.
func TestStreamWaitsForSettledAfterCompaction(t *testing.T) {
	reader := newStreamingRPCReader()
	client, proc := newStreamingCaptureClient(reader)
	t.Cleanup(reader.Close)

	s, err := client.Stream(context.Background(), "long task")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	reader.Push(
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":1111,"contextWindow":111111,"percent":0.11}}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"before compact"}],"stopReason":"stop"}],"willRetry":false}`,
		`{"type":"compaction_start","reason":"threshold"}`,
		`{"type":"compaction_end","reason":"threshold","result":{"summary":"condensed"}}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"after compact"}}`,
		preSettlementBarrierFrame(),
	)
	preSettled := assertPreSettlementBarrier(t, s, proc, []EventKind{
		EventRuntimeStatus,
		EventCompactBoundary,
		EventTextDelta,
	})

	reader.Push(
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":1234,"contextWindow":123456,"percent":0.12}}}`,
	)
	postSettled := collectUntilTerminal(t, s)
	all := append(preSettled, postSettled...)

	kinds := eventKinds(all)
	require.Contains(t, kinds, EventCompactBoundary)
	require.Contains(t, kinds, EventContextWindow)
	require.Contains(t, kinds, EventDone)
	assert.Equal(t, "after compact", eventText(all))
	assert.Less(t, eventIndex(kinds, EventCompactBoundary), eventIndex(kinds, EventDone))
	assert.Less(t, eventIndex(kinds, EventContextWindow), eventIndex(kinds, EventDone))
	assert.Equal(t, []int{123456}, contextWindows(all))
	assertStatsRequestedAfterSettlement(t, proc)
}

// A2: Given a successful low-level agent_end followed by another tool batch and
// assistant output, when the intermediate frame arrives, then the continuation
// is consumed instead of terminating the prompt stream.
func TestStreamConsumesContinuationAfterSettlingBoundary(t *testing.T) {
	reader := newStreamingRPCReader()
	client, proc := newStreamingCaptureClient(reader)
	t.Cleanup(reader.Close)

	s, err := client.Stream(context.Background(), "continue the task")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	reader.Push(
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":2222,"contextWindow":222222,"percent":0.21}}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"first step"}],"stopReason":"stop"}],"willRetry":false}`,
		`{"type":"tool_execution_start","toolCallId":"call_2","toolName":"bash","args":{"command":"echo continued"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_2","toolName":"bash","result":{"content":[{"type":"text","text":"continued"}]},"isError":false}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"final continuation"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"final continuation"}],"stopReason":"stop"}],"willRetry":false}`,
		preSettlementBarrierFrame(),
	)
	preSettled := assertPreSettlementBarrier(t, s, proc, []EventKind{
		EventPreToolUse,
		EventPostToolUse,
		EventTextDelta,
	})

	reader.Push(
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":4321,"contextWindow":432222,"percent":0.41}}}`,
	)
	postSettled := collectUntilTerminal(t, s)
	all := append(preSettled, postSettled...)

	var preTool, postTool bool
	for _, ev := range all {
		if ev.Kind == EventPreToolUse && ev.Tool.ID == "call_2" {
			preTool = true
		}
		if ev.Kind == EventPostToolUse && ev.Tool.Content == "continued" {
			postTool = true
		}
	}
	assert.True(t, preTool)
	assert.True(t, postTool)
	assert.Equal(t, "final continuation", eventText(all))
	assert.Contains(t, eventKinds(all), EventDone)
	assert.Equal(t, []int{432222}, contextWindows(all))
	assertStatsRequestedAfterSettlement(t, proc)
}

// A3: Given an error agent_end followed by a successful automatic retry, when
// the full RPC sequence settles, then the transient error is not surfaced
// before settlement and the final result is clean.
func TestStreamSuppressesRetriedAgentEndErrorUntilSettled(t *testing.T) {
	reader := newStreamingRPCReader()
	client, proc := newStreamingCaptureClient(reader)
	t.Cleanup(reader.Close)

	s, err := client.Stream(context.Background(), "retry this")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	reader.Push(
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":2222,"contextWindow":333333,"percent":0.21}}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"error","reason":"provider overloaded"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"temporary failure"}],"stopReason":"error","errorMessage":"provider overloaded"}],"willRetry":true}`,
		`{"type":"auto_retry_start","errorMessage":"provider overloaded"}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"retry succeeded"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"retry succeeded"}],"stopReason":"stop"}],"willRetry":false}`,
		preSettlementBarrierFrame(),
	)
	preSettled := assertPreSettlementBarrier(t, s, proc, []EventKind{
		EventRuntimeStatus,
		EventTextDelta,
	})

	reader.Push(
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":2345,"contextWindow":334444,"percent":0.22}}}`,
	)
	postSettled := collectUntilTerminal(t, s)
	all := append(preSettled, postSettled...)

	assert.Equal(t, "retry succeeded", eventText(all))
	assert.NotContains(t, eventKinds(all), EventError)
	assert.Contains(t, eventKinds(all), EventDone)
	assert.NoError(t, s.Err())
	assert.Equal(t, []int{334444}, contextWindows(all))
	assertStatsRequestedAfterSettlement(t, proc)
}

// A4: Given an intermediate successful agent_end followed by a final error and
// agent_settled, when the prompt stream is consumed, then the final error is
// withheld until settlement, retains diagnostics, and never emits Done.
func TestStreamReportsSettledFinalAgentEndError(t *testing.T) {
	finalFrame := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"error","errorMessage":"final provider failure","model":"gpt-5.5(xhigh)"}],"willRetry":false}`
	reader := newStreamingRPCReader()
	client, proc := newStreamingCaptureClient(reader)
	proc.stderr = strings.NewReader("pi stderr tail\n")
	t.Cleanup(reader.Close)

	s, err := client.Stream(context.Background(), "fail at settlement")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	reader.Push(
		`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":3333,"contextWindow":444444,"percent":0.32}}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"intermediate"}],"stopReason":"stop"}],"willRetry":false}`,
		finalFrame,
		preSettlementBarrierFrame(),
	)
	preSettled := assertPreSettlementBarrier(t, s, proc, []EventKind{})

	reader.Push(`{"type":"agent_settled"}`)
	postSettled := collectUntilTerminal(t, s)
	all := append(preSettled, postSettled...)
	<-s.proc.stderrDone

	assert.Contains(t, eventKinds(all), EventError)
	assert.NotContains(t, eventKinds(all), EventDone)
	assert.EqualError(t, s.Err(), "piagent: final provider failure")
	diagnostics := s.Diagnostics()
	assert.Equal(t, "agent_end", diagnostics.FinalErrorEventType)
	assert.Equal(t, "error", diagnostics.FinalErrorStopReason)
	assert.Equal(t, "final provider failure", diagnostics.FinalErrorMessage)
	assert.JSONEq(t, finalFrame, diagnostics.FinalErrorFrame)
	assert.Equal(t, "pi stderr tail", diagnostics.StderrTail)
}

// Given Pi's documented aborted stream sequence, when the assistant's
// authoritative agent_end stops as aborted, then the staged error remains a
// settled failure rather than being cleared as a successful agent_end.
func TestStreamReportsSettledAbortedAgentEnd(t *testing.T) {
	tests := []struct {
		name               string
		errorMessage       string
		wantError          string
		wantDiagnosticText string
	}{
		{
			name:               "authoritative agent end error message supersedes streaming delta",
			errorMessage:       "user requested abort",
			wantError:          "piagent: user requested abort",
			wantDiagnosticText: "user requested abort",
		},
		{
			name:               "stop reason supplies fallback when agent end has no useful error message",
			wantError:          "piagent: aborted",
			wantDiagnosticText: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			finalFrame := abortedAgentEndFrame(tt.errorMessage)
			reader := newStreamingRPCReader()
			client, proc := newStreamingCaptureClient(reader)
			t.Cleanup(reader.Close)

			s, err := client.Stream(context.Background(), "abort this")
			require.NoError(t, err)
			t.Cleanup(func() { _ = s.Close(context.Background()) })
			require.NoError(t, s.Interrupt(context.Background()))

			reader.Push(
				`{"type":"response","command":"prompt","success":true}`,
				`{"type":"message_update","assistantMessageEvent":{"type":"error","reason":"aborted"}}`,
				finalFrame,
				preSettlementBarrierFrame(),
			)
			preSettled := assertPreSettlementBarrier(t, s, proc, []EventKind{})
			assert.NotContains(t, eventKinds(preSettled), EventError)
			assert.NotContains(t, eventKinds(preSettled), EventDone)
			assert.NoError(t, s.Err())

			reader.Push(
				`{"type":"agent_settled"}`,
				`{"type":"response","command":"get_session_stats","success":true,"data":{"contextUsage":{"tokens":3333,"contextWindow":444444,"percent":0.32}}}`,
			)
			postSettled := collectUntilTerminal(t, s)
			assert.Contains(t, eventKinds(postSettled), EventError)
			assert.NotContains(t, eventKinds(postSettled), EventDone)
			require.Error(t, s.Err())
			assert.EqualError(t, s.Err(), tt.wantError)

			diagnostics := s.Diagnostics()
			assert.Equal(t, "agent_end", diagnostics.FinalErrorEventType)
			assert.Equal(t, "aborted", diagnostics.FinalErrorStopReason)
			assert.Equal(t, tt.wantDiagnosticText, diagnostics.FinalErrorMessage)
			assert.JSONEq(t, finalFrame, diagnostics.FinalErrorFrame)

			frames := stdinFrames(t, proc.stdin.String())
			require.Len(t, frames, 4)
			assert.Equal(t, "get_state", frames[0]["type"])
			assert.Equal(t, "get_session_stats", frames[1]["type"])
			assert.Equal(t, "prompt", frames[2]["type"])
			assert.Equal(t, "abort", frames[3]["type"])
		})
	}
}

func abortedAgentEndFrame(errorMessage string) string {
	message := ""
	if errorMessage != "" {
		message = `,"errorMessage":"` + errorMessage + `"`
	}
	return `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"aborted"` + message + `}],"willRetry":false}`
}

// Given a prompt stream whose process ends after a nonterminal agent_end,
// when Pi never emits agent_settled, then the stream reports process death
// instead of completing cleanly.
func TestStreamReportsProcessDeathBeforeSettled(t *testing.T) {
	client, _ := newCaptureClient(strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"unfinished"}],"stopReason":"stop"}],"willRetry":false}`,
		"",
	}, "\n"))

	s, err := client.Stream(context.Background(), "wait for settlement")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close(context.Background()) })

	var kinds []EventKind
	for s.Next() {
		kinds = append(kinds, s.Event().Kind)
	}

	assert.Contains(t, kinds, EventError)
	assert.NotContains(t, kinds, EventDone)
	assert.ErrorIs(t, s.Err(), ErrProcessDead)
}

type streamingRPCReader struct {
	mu     sync.Mutex
	cond   *sync.Cond
	chunks [][]byte
	closed bool
}

func newStreamingRPCReader() *streamingRPCReader {
	r := &streamingRPCReader{}
	r.cond = sync.NewCond(&r.mu)
	return r
}

func (r *streamingRPCReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for len(r.chunks) == 0 && !r.closed {
		r.cond.Wait()
	}
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	chunk := r.chunks[0]
	n := copy(p, chunk)
	if n == len(chunk) {
		r.chunks = r.chunks[1:]
	} else {
		r.chunks[0] = chunk[n:]
	}
	return n, nil
}

func (r *streamingRPCReader) Push(frames ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, frame := range frames {
		r.chunks = append(r.chunks, []byte(frame+"\n"))
	}
	r.cond.Broadcast()
}

func (r *streamingRPCReader) Close() {
	r.mu.Lock()
	r.closed = true
	r.cond.Broadcast()
	r.mu.Unlock()
}

func newStreamingCaptureClient(reader io.Reader) (*Client, *captureProc) {
	if streaming, ok := reader.(*streamingRPCReader); ok {
		streaming.Push(`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"test-native-session"}}`)
	}
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: reader,
		done:   make(chan error, 1),
	}
	return New(WithRPCProcessRunnerForTesting(&captureRunner{proc: proc})), proc
}

func preSettlementBarrierFrame() string {
	return `{"type":"message_update","assistantMessageEvent":{"type":"thinking_delta","delta":"` + preSettlementBarrier + `"}}`
}

func assertPreSettlementBarrier(t *testing.T, s *Stream, proc *captureProc, wantKinds []EventKind) []Event {
	t.Helper()
	events := collectUntilPreSettlementBarrier(t, s)
	require.NotEmpty(t, events)
	assert.Equal(t, preSettlementBarrier, events[len(events)-1].Text)
	assert.Equal(t, append(append([]EventKind{}, wantKinds...), EventThinkingDelta), eventKinds(events),
		"pre-settlement frames must be processed in order")
	frames := stdinFrames(t, proc.stdin.String())
	require.GreaterOrEqual(t, len(frames), 3)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
	assert.Equal(t, "prompt", frames[2]["type"])
	statsRequests := 0
	for _, frame := range frames {
		if frame["type"] == "get_session_stats" {
			statsRequests++
		}
	}
	assert.Equal(t, 1, statsRequests, "final session stats must wait for agent_settled")
	return events
}

func collectUntilPreSettlementBarrier(t *testing.T, s *Stream) []Event {
	t.Helper()
	timer := time.NewTimer(settlementBarrierTimeout)
	defer timer.Stop()
	var events []Event
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("agent_settled gate: stream closed before pre-settlement barrier")
			}
			s.cur = ev
			if ev.Kind == EventDone || ev.Kind == EventError {
				t.Fatalf("agent_settled gate: terminal %s emitted before pre-settlement barrier", ev.Kind)
			}
			events = append(events, ev)
			if ev.Kind == EventThinkingDelta && ev.Text == preSettlementBarrier {
				return events
			}
		case <-timer.C:
			t.Fatalf("agent_settled gate: pre-settlement barrier not observed")
		}
	}
}

func collectUntilTerminal(t *testing.T, s *Stream) []Event {
	t.Helper()
	timer := time.NewTimer(settlementTerminalTimeout)
	defer timer.Stop()
	var events []Event
	for {
		select {
		case ev, ok := <-s.events:
			if !ok {
				t.Fatalf("agent_settled gate: stream closed before terminal event")
			}
			s.cur = ev
			events = append(events, ev)
			if ev.Kind == EventDone || ev.Kind == EventError {
				return events
			}
		case <-timer.C:
			t.Fatalf("agent_settled gate: no terminal event after agent_settled")
		}
	}
}

func assertStatsRequestedAfterSettlement(t *testing.T, proc *captureProc) {
	t.Helper()
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 4)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
	assert.Equal(t, "prompt", frames[2]["type"])
	assert.Equal(t, "get_session_stats", frames[3]["type"])
}

func contextWindows(events []Event) []int {
	windows := make([]int, 0)
	for _, ev := range events {
		if ev.Kind == EventContextWindow {
			windows = append(windows, ev.ContextWindow)
		}
	}
	return windows
}

func eventKinds(events []Event) []EventKind {
	kinds := make([]EventKind, 0, len(events))
	for _, ev := range events {
		kinds = append(kinds, ev.Kind)
	}
	return kinds
}

func eventText(events []Event) string {
	var b strings.Builder
	for _, ev := range events {
		if ev.Kind == EventTextDelta {
			b.WriteString(ev.Text)
		}
	}
	return b.String()
}

func eventIndex(kinds []EventKind, target EventKind) int {
	for i, kind := range kinds {
		if kind == target {
			return i
		}
	}
	return -1
}
