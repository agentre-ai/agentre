package piagent

import (
	"context"
	"io"
	"strconv"
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

func TestStreamScalesAnchorMetadataBeyondThreeImageHeavyTurns(t *testing.T) {
	// Given a valid session within Agentre's supported image limits (4 × 5 MiB
	// images per turn) has accumulated four image-heavy turns,
	// When the pre/post-turn get_entries frames carry every accumulated image,
	// Then framing/parsing scales past the old fixed cap and the exact post-turn
	// user entry is still extracted instead of silently losing the anchor.
	const (
		turns         = 4
		imagesPerTurn = 4
		imageBytes    = 5 * 1024 * 1024
	)
	// Image blobs are streamed through a repeating reader so the ~80 MiB frames
	// are generated on the fly instead of being materialized as giant strings.
	userMessageSegments := func(turn int) []io.Reader {
		parent := "null"
		if turn > 1 {
			parent = `"h` + strconv.Itoa(turn-1) + `-assistant"`
		}
		segments := []io.Reader{strings.NewReader(
			`{"type":"message","id":"h` + strconv.Itoa(turn) +
				`-user","parentId":` + parent + `,"message":{"role":"user","images":[`)}
		for i := 0; i < imagesPerTurn; i++ {
			if i > 0 {
				segments = append(segments, strings.NewReader(","))
			}
			segments = append(segments, strings.NewReader(`{"type":"image","data":"`))
			segments = append(segments, &repeatingByteReader{remaining: imageBytes, value: 'A'})
			segments = append(segments, strings.NewReader(`","mimeType":"image/png"}`))
		}
		segments = append(segments, strings.NewReader(`]}}`))
		return segments
	}
	historySegments := func() []io.Reader {
		var segments []io.Reader
		for turn := 1; turn <= turns; turn++ {
			segments = append(segments, userMessageSegments(turn)...)
			segments = append(segments, strings.NewReader(","))
			segments = append(segments, strings.NewReader(
				`{"type":"message","id":"h`+strconv.Itoa(turn)+
					`-assistant","parentId":"h`+strconv.Itoa(turn)+`-user","message":{"role":"assistant"}}`))
			if turn < turns {
				segments = append(segments, strings.NewReader(","))
			}
		}
		return segments
	}

	parts := make([]io.Reader, 0, 128)
	parts = append(parts, strings.NewReader(
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-heavy"}}`+"\n"))
	parts = append(parts, strings.NewReader(
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[`))
	parts = append(parts, historySegments()...)
	parts = append(parts, strings.NewReader(`],"leafId":"h`+strconv.Itoa(turns)+`-assistant"}}`+"\n"))
	parts = append(parts, strings.NewReader(
		`{"type":"response","command":"prompt","success":true}`+"\n"+
			`{"type":"agent_end","messages":[],"willRetry":false}`+"\n"+
			`{"type":"agent_settled"}`+"\n"))
	parts = append(parts, strings.NewReader(
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[`))
	parts = append(parts, historySegments()...)
	parts = append(parts, strings.NewReader(
		`,{"type":"message","id":"turn-user","parentId":"h`+strconv.Itoa(turns)+
			`-assistant","message":{"role":"user","content":"hello"}}`+
			`,{"type":"message","id":"turn-assistant","parentId":"turn-user","message":{"role":"assistant"}}`+
			`,{"type":"message","id":"steer-user","parentId":"turn-assistant","message":{"role":"user","content":"steer"}}`+
			`,{"type":"message","id":"steer-assistant","parentId":"steer-user","message":{"role":"assistant"}}`+
			`],"leafId":"steer-assistant"}}`+"\n"))
	parts = append(parts, strings.NewReader(
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`+"\n"))
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: io.MultiReader(parts...),
		done:   make(chan error, 1),
	}
	client := New(WithRPCProcessRunnerForTesting(&captureRunner{proc: proc}))
	client.session = "session-heavy"

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
