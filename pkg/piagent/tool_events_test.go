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
