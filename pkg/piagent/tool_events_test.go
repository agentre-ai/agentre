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
		`{"type":"agent_end","messages":[]}`,
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

// Pi can emit agent_end after an assistant message whose stopReason is
// toolUse. That frame only closes the current model/tool sub-step; the RPC
// stream may continue with tool results and another assistant message. Agentre
// must not treat that intermediate agent_end as terminal, otherwise long Pi
// turns stop after every tool batch and the user has to send "continue".
func TestStreamContinuesAfterToolUseAgentEnd(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"toolcall_end","toolCall":{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}}}`,
		`{"type":"tool_execution_start","toolCallId":"call_1","toolName":"bash","args":{"command":"echo hi"}}`,
		`{"type":"tool_execution_end","toolCallId":"call_1","toolName":"bash","result":{"content":[{"type":"text","text":"hi"}]},"isError":false}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"toolCall","id":"call_1","name":"bash","arguments":{"command":"echo hi"}}],"stopReason":"toolUse"}]}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done"}}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"text","text":"done"}],"stopReason":"stop"}]}`,
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
// message. Agentre must surface that as EventError instead of treating the turn
// as a clean Done, otherwise the UI silently shows a half-finished tool-only
// answer.
func TestStreamEmitsErrorFromFinalAgentEnd(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"error","errorMessage":"terminated"}]}`,
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
	assert.False(t, done, "error terminal agent_end must not be reported as a clean done")
	assert.EqualError(t, s.Err(), "piagent: terminated")
}

func TestStreamDiagnosticsIncludeFinalErrorFrameAndStderrTail(t *testing.T) {
	finalFrame := `{"type":"agent_end","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":""}],"stopReason":"error","errorMessage":"terminated","model":"gpt-5.5(xhigh)"}]}`
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		finalFrame,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)
	proc.stderr = strings.NewReader("first stderr line\nlast stderr line\n")

	s, err := client.Stream(context.Background(), "research pi rpc")
	require.NoError(t, err)

	for s.Next() {
	}

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
		`{"type":"agent_end","messages":[{"role":"assistant","stopReason":"error","errorMessage":"terminated"}]}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)
	proc.stderr = strings.NewReader(strings.Repeat("a", diagnosticStderrTailLimit+16))

	s, err := client.Stream(context.Background(), "research pi rpc")
	require.NoError(t, err)

	for s.Next() {
	}

	d := s.Diagnostics()
	assert.Len(t, d.StderrTail, diagnosticStderrTailLimit)
	assert.Equal(t, strings.Repeat("a", diagnosticStderrTailLimit), d.StderrTail)
}
