package piagent

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextAcceptsLargeRPCFrame(t *testing.T) {
	// Pi repeats the accumulated partial assistant message on message_update
	// frames. A large tool-call patch can therefore make one valid JSONL frame
	// exceed the historical 4 MiB scanner limit.
	largePartial := strings.Repeat("x", 5<<20)
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done","partial":"` + largePartial + `"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	text, err := client.Text(context.Background(), "handle a large patch")

	require.NoError(t, err)
	assert.Equal(t, "done", text)
}

func TestTextRejectsRPCFrameBeyondSafetyLimit(t *testing.T) {
	oversizedPartial := strings.Repeat("x", maxRPCFrameBytes)
	script := strings.Join([]string{
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"done","partial":"` + oversizedPartial + `"}}`,
		"",
	}, "\n")
	client, _ := newCaptureClient(script)

	_, err := client.Text(context.Background(), "reject an unreasonable frame")

	require.Error(t, err)
	assert.ErrorContains(t, err, "bufio.Scanner: token too long")
}
