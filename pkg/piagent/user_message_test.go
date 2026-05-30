package piagent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Pi 把 mid-turn steer 注入回显成一条 user message_start。Stream 必须把它的
// 文本surface 成 EventUserMessage，runtime 才能据此 emit SteerConsumed。
// 助手自己的 message_start（role assistant）不应产生 EventUserMessage。
func TestStreamSurfacesUserMessageEcho(t *testing.T) {
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_start","message":{"role":"user","content":[{"type":"text","text":"now do X instead"}],"timestamp":1}}`,
		`{"type":"message_start","message":{"role":"assistant","content":[],"model":"gpt","timestamp":2}}`,
		`{"type":"agent_end","messages":[]}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	s, err := client.Stream(context.Background(), "do something")
	require.NoError(t, err)

	var userTexts []string
	for s.Next() {
		if s.Event().Kind == EventUserMessage {
			userTexts = append(userTexts, s.Event().Text)
		}
	}

	require.Len(t, userTexts, 1, "only the user echo, not the assistant message")
	assert.Equal(t, "now do X instead", userTexts[0])
}
