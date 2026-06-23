package chat_svc_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// TestStreamSubagentActivityStarted_KindValue 验证常量值与协议字符串一致。
func TestStreamSubagentActivityStarted_KindValue(t *testing.T) {
	assert.Equal(t, chat_svc.ChatStreamEventKind("subagent_activity_started"),
		chat_svc.StreamSubagentActivityStarted,
		"StreamSubagentActivityStarted 常量应等于 \"subagent_activity_started\"")
}

// TestStreamSubagentActivityStarted_EventMarshal 验证 ChatStreamEvent 可 JSON 序列化
// LaunchMessageID 和 ToolUseID 字段，且字段名与协议约定一致。
func TestStreamSubagentActivityStarted_EventMarshal(t *testing.T) {
	evt := chat_svc.ChatStreamEvent{
		Kind:            chat_svc.StreamSubagentActivityStarted,
		LaunchMessageID: 42,
		ToolUseID:       "toolu_abc123",
		Stream:          "chat:event:1:99",
	}
	data, err := json.Marshal(evt)
	require.NoError(t, err)

	var m map[string]any
	require.NoError(t, json.Unmarshal(data, &m))

	assert.Equal(t, "subagent_activity_started", m["kind"])
	assert.EqualValues(t, 42, m["launchMessageId"], "LaunchMessageID 应序列化为 launchMessageId")
	assert.Equal(t, "toolu_abc123", m["toolUseId"])
	assert.Equal(t, "chat:event:1:99", m["stream"])
}
