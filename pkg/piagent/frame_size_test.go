package piagent

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamAcceptsValidSessionMetadataFrameLargerThanFourMiB(t *testing.T) {
	// Given a supported image makes get_entries metadata exceed the old 4 MiB scanner cap,
	// When Pi returns that otherwise valid frame,
	// Then the turn starts and anchor tracking continues normally.
	imageData := strings.Repeat("A", 5*1024*1024)
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-large"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant","images":[{"data":"` + imageData + `"}]}}],"leafId":"before-leaf"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}},{"type":"message","id":"turn-user","parentId":"before-leaf","message":{"role":"user"}}],"leafId":"turn-user"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _, _ := newSingleProcessCaptureClient(script)

	stream, err := client.Stream(context.Background(), "hello", RunCaptureUserAnchor())
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "turn-user", stream.UserAnchor())
	assert.NoError(t, stream.Err())
}

func TestStreamRejectsFrameBeyondBoundedSafetyLimit(t *testing.T) {
	// Given a single RPC frame exceeds the bounded diagnostic/session safety limit,
	// When startup scans stdout,
	// Then it rejects the frame instead of growing memory without bound.
	oversized := io.MultiReader(
		strings.NewReader(`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"`),
		&repeatingByteReader{remaining: rpcFrameSafetyLimit + 1, value: 'A'},
		strings.NewReader(`"}}`+"\n"),
	)
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: oversized,
		done:   make(chan error, 1),
	}
	client := New(
		WithRPCProcessRunnerForTesting(&captureRunner{proc: proc}),
		WithKillGrace(10*time.Millisecond),
	)

	stream, err := client.Stream(context.Background(), "must not start")

	assert.Nil(t, stream)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "token too long")
}

type repeatingByteReader struct {
	remaining int
	value     byte
}

func (r *repeatingByteReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = r.value
	}
	r.remaining -= len(p)
	return len(p), nil
}
