package piagent

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamRecordsFirstUserEntryAfterPromptBoundary(t *testing.T) {
	// Given history contains the same prompt text and the turn later adds a steer,
	// When terminal entries are read from the new leaf back to the pre-prompt leaf,
	// Then the first user entry on that tree path is recorded as the stable anchor.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"history-user","parentId":null,"message":{"role":"user","content":"repeat"}},{"type":"message","id":"before-leaf","parentId":"history-user","message":{"role":"assistant"}},{"type":"custom","id":"custom-before-prompt","parentId":"before-leaf","customType":"test"},{"type":"compaction","id":"compaction-before-prompt","parentId":"custom-before-prompt","summary":"summary","firstKeptEntryId":"history-user","tokensBefore":1}],"leafId":"compaction-before-prompt"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"answer"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"history-user","parentId":null,"message":{"role":"user","content":"repeat"}},{"type":"message","id":"before-leaf","parentId":"history-user","message":{"role":"assistant"}},{"type":"custom","id":"custom-before-prompt","parentId":"before-leaf","customType":"test"},{"type":"compaction","id":"compaction-before-prompt","parentId":"custom-before-prompt","summary":"summary","firstKeptEntryId":"history-user","tokensBefore":1},{"type":"message","id":"abandoned-user","parentId":"before-leaf","message":{"role":"user","content":"repeat"}},{"type":"message","id":"turn-user","parentId":"compaction-before-prompt","message":{"role":"user","content":"repeat"}},{"type":"message","id":"turn-assistant","parentId":"turn-user","message":{"role":"assistant"}},{"type":"message","id":"steer-user","parentId":"turn-assistant","message":{"role":"user","content":"repeat"}},{"type":"message","id":"steer-assistant","parentId":"steer-user","message":{"role":"assistant"}}],"leafId":"steer-assistant"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _, _ := newSingleProcessCaptureClient(script)
	client.session = "session-1"

	stream, err := client.Stream(context.Background(), "repeat", RunCaptureUserAnchor())
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "turn-user", stream.UserAnchor())
}

func TestStreamLeavesAnchorEmptyWhenNoNewUserEntryExists(t *testing.T) {
	// Given an extension handles the prompt without appending a normal user message,
	// When the turn settles,
	// Then completion succeeds and no anchor is fabricated.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}}],"leafId":"before-leaf"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}},{"type":"custom","id":"extension-entry","parentId":"before-leaf","customType":"handled"}],"leafId":"extension-entry"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _, _ := newSingleProcessCaptureClient(script)

	stream, err := client.Stream(context.Background(), "/handled", RunCaptureUserAnchor())
	require.NoError(t, err)
	var kinds []EventKind
	for stream.Next() {
		kinds = append(kinds, stream.Event().Kind)
	}

	assert.Empty(t, stream.UserAnchor())
	require.NotEmpty(t, kinds)
	assert.Equal(t, EventDone, kinds[len(kinds)-1])
	assert.NotContains(t, kinds, EventError)
}

func TestStreamKeepsAnchorWhenOnlySessionStatsTimesOut(t *testing.T) {
	// Given terminal entries identify the new user but optional stats never respond,
	// When terminal metadata times out,
	// Then the parsed user anchor is retained instead of being coupled to stats.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"turn-user","parentId":null,"message":{"role":"user","content":"hello"}}],"leafId":"turn-user"}}`,
		"",
	}, "\n")
	stdout, stdoutWriter := io.Pipe()
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: stdout,
		done:   make(chan error, 1),
	}
	client := New(WithRPCProcessRunnerForTesting(&captureRunner{proc: proc}))
	writeDone := make(chan struct{})
	go func() {
		defer close(writeDone)
		_, _ = io.WriteString(stdoutWriter, script)
	}()

	stream, err := client.Stream(context.Background(), "hello", RunCaptureUserAnchor())
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "turn-user", stream.UserAnchor())
	<-writeDone
	require.NoError(t, stdoutWriter.Close())
	require.NoError(t, stream.Close(context.Background()))
}

func TestStreamPreservesCompletedAnswerWhenAnchorMetadataFails(t *testing.T) {
	// Given Pi has completed the assistant answer but terminal entry metadata fails,
	// When the stream settles,
	// Then the answer and Done event remain while the anchor stays empty.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"completed answer"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":false,"error":"metadata unavailable"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, _, _ := newSingleProcessCaptureClient(script)

	stream, err := client.Stream(context.Background(), "hello", RunCaptureUserAnchor())
	require.NoError(t, err)
	var text strings.Builder
	var kinds []EventKind
	for stream.Next() {
		event := stream.Event()
		kinds = append(kinds, event.Kind)
		if event.Kind == EventTextDelta {
			text.WriteString(event.Text)
		}
	}

	assert.Equal(t, "completed answer", text.String())
	assert.Empty(t, stream.UserAnchor())
	require.NotEmpty(t, kinds)
	assert.Equal(t, EventDone, kinds[len(kinds)-1])
	assert.NotContains(t, kinds, EventError)
}
