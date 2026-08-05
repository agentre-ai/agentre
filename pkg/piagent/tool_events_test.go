package piagent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pi 对一次工具调用同时发 message_update/toolcall_end 和 tool_execution_start
// （同一个 toolCallId）。下游 Agentre 只应看到一个 PreToolUse，否则工具卡重复。
func TestStreamEmitsSingleToolCallPerExecution(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "run echo")
	require.NoError(t, err)

	var pre, post []Event
	for s.Next() {
		switch s.Event().Kind {
		case EventPreToolUse:
			pre = append(pre, s.Event())
		case EventPostToolUse:
			post = append(post, s.Event())
		}
	}

	require.Len(t, pre, 1, "exactly one PreToolUse per executed tool")
	assert.Equal(t, "call_1", pre[0].Tool.ID)
	assert.Equal(t, "bash", pre[0].Tool.Name)
	assert.JSONEq(t, `{"command":"echo hi"}`, string(pre[0].Tool.Input))
	require.Len(t, post, 1)
	assert.Equal(t, "hi", post[0].Tool.Content)
}

// Given Pi reports a partial tool result update, when the RPC frame is decoded,
// then the wrapper surfaces the tool identity and preserves the raw partialResult.
func TestStreamPreservesToolExecutionUpdatePartialResult(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"tool_execution_update","toolCallId":"call_update","toolName":"subagent","partialResult":{"content":[{"type":"text","text":"working"}],"details":{"results":[{"status":"running"}]}}}`,
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "delegate")
	require.NoError(t, err)

	var updates []Event
	for s.Next() {
		if s.Event().Kind == EventToolUseUpdate {
			updates = append(updates, s.Event())
		}
	}

	require.Len(t, updates, 1, "tool_execution_update must survive the RPC boundary")
	assert.Equal(t, "call_update", updates[0].Tool.ID)
	assert.Equal(t, "subagent", updates[0].Tool.Name)
	assert.JSONEq(t, `{"content":[{"type":"text","text":"working"}],"details":{"results":[{"status":"running"}]}}`, string(updates[0].Tool.PartialResult))
}

// Given Pi reports a final result with structured details, when the RPC frame is
// decoded, then details remain recoverable without changing outer text/error.
func TestStreamPreservesToolExecutionEndDetails(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"tool_execution_end","toolCallId":"call_final","toolName":"subagent","result":{"content":[{"type":"text","text":"outer result"}],"details":{"results":[{"status":"completed","messages":[]}]}} ,"isError":true}`,
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "delegate")
	require.NoError(t, err)

	var results []Event
	for s.Next() {
		if s.Event().Kind == EventPostToolUse {
			results = append(results, s.Event())
		}
	}

	require.Len(t, results, 1)
	assert.Equal(t, "outer result", results[0].Tool.Content)
	assert.True(t, results[0].Tool.IsError)
	assert.JSONEq(t, `{"results":[{"status":"completed","messages":[]}]}`, string(results[0].Tool.Details))
}

// Given details has an unsupported shape, when the final result is decoded,
// then the raw value remains available and ordinary outer text stays intact.
func TestStreamKeepsMalformedToolDetailsNonFatal(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"tool_execution_end","toolCallId":"call_malformed","toolName":"subagent","result":{"content":[{"type":"text","text":"still visible"}],"details":"not-an-object"},"isError":false}`,
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "delegate")
	require.NoError(t, err)

	var result Event
	for s.Next() {
		if s.Event().Kind == EventPostToolUse {
			result = s.Event()
		}
	}

	require.Equal(t, EventPostToolUse, result.Kind)
	assert.Equal(t, "still visible", result.Tool.Content)
	assert.JSONEq(t, `"not-an-object"`, string(result.Tool.Details))
	assert.NoError(t, s.Err())
}

// Pi 0.81.1 can emit agent_end after an assistant message whose stopReason is
// toolUse. Like every agent_end, that frame only closes one low-level run; the
// RPC stream may continue with tool results and another assistant message until
// agent_settled. Agentre must not stop after a tool batch and make the user send
// "continue".
func TestStreamContinuesAfterToolUseAgentEnd(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}],"stopReason":"toolUse"}],"willRetry":false}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "run echo")
	require.NoError(t, err)

	var text string
	var done bool
	for s.Next() {
		switch s.Event().Kind {
		case EventTextDelta:
			text += s.Event().Text
		case EventDone:
			done = true
		}
	}

	assert.Equal(t, "done", text)
	assert.True(t, done)
}

// Pi reports provider/transport failures on the final agent_end assistant
// message. Pi 0.81.1 settles the whole prompt afterward; Agentre must surface
// that candidate as EventError only then instead of treating the turn as a
// clean Done, otherwise the UI silently shows a half-finished tool-only answer.
func TestStreamEmitsErrorFromFinalAgentEnd(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"error","errorMessage":"terminated"}],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "research pi rpc")
	require.NoError(t, err)

	var gotErr Event
	var done bool
	for s.Next() {
		switch s.Event().Kind {
		case EventError:
			gotErr = s.Event()
		case EventDone:
			done = true
		}
	}

	require.Equal(t, EventError, gotErr.Kind)
	require.Error(t, gotErr.Err)
	assert.EqualError(t, gotErr.Err, "piagent: terminated")
	assert.False(t, done, "a settled final agent_end error must not be reported as a clean done")
	assert.EqualError(t, s.Err(), "piagent: terminated")
}

func TestStreamDiagnosticsIncludeFinalErrorFrameAndStderrTail(t *testing.T) {
	finalFrame := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"error","errorMessage":"terminated","model":"gpt-5.5(xhigh)"}],"willRetry":false}`
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		finalFrame,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)
	proc.stderr = strings.NewReader("first stderr line\nlast stderr line\n")

	s, err := client.Stream(context.Background(), "research pi rpc")
	require.NoError(t, err)

	for s.Next() {
	}
	<-s.proc.stderrDone

	d := s.Diagnostics()
	assert.Equal(t, "agent_end", d.FinalErrorEventType)
	assert.Equal(t, "error", d.FinalErrorStopReason)
	assert.Equal(t, "terminated", d.FinalErrorMessage)
	assert.JSONEq(t, finalFrame, d.FinalErrorFrame)
	assert.Equal(t, "first stderr line\nlast stderr line", d.StderrTail)
}

func TestStreamDiagnosticsTruncateLongStderrTail(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","stopReason":"error","errorMessage":"terminated"}],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)
	proc.stderr = strings.NewReader(strings.Repeat("a", diagnosticStderrTailLimit+16))

	s, err := client.Stream(context.Background(), "research pi rpc")
	require.NoError(t, err)

	for s.Next() {
	}
	<-s.proc.stderrDone

	d := s.Diagnostics()
	assert.Len(t, d.StderrTail, diagnosticStderrTailLimit)
	assert.Equal(t, strings.Repeat("a", diagnosticStderrTailLimit), d.StderrTail)
}
