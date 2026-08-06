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

func TestTrackedMetadataCancellationReleasesTheSingleScannerOwner(t *testing.T) {
	// Given terminal metadata owns the process scanner while Pi sends no response,
	// When the bounded settlement context is canceled,
	// Then the scanner call itself returns instead of leaving a detached reader goroutine.
	scanner := newContextTrackingScanner()
	proc := &rpcProcess{stdin: io.Discard, lines: scanner}
	stream := newStream(proc, time.Second)
	stream.setUserAnchorBoundary("")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		stream.emitTrackedSessionMetadata(ctx)
		close(done)
	}()

	<-scanner.entered
	cancel()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		scanner.Release()
		<-done
		t.Fatal("metadata settlement ignored its canceled context")
	}
	select {
	case <-scanner.exited:
	case <-time.After(100 * time.Millisecond):
		scanner.Release()
		<-scanner.exited
		t.Fatal("metadata settlement returned while a detached scanner owner remained blocked")
	}
	_ = proc.terminate(context.Background(), 0)
}

type contextTrackingScanner struct {
	entered   chan struct{}
	exited    chan struct{}
	release   chan struct{}
	enterOnce sync.Once
	exitOnce  sync.Once
	closeOnce sync.Once
}

func newContextTrackingScanner() *contextTrackingScanner {
	return &contextTrackingScanner{
		entered: make(chan struct{}),
		exited:  make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (s *contextTrackingScanner) Scan() bool {
	s.enterOnce.Do(func() { close(s.entered) })
	<-s.release
	s.exitOnce.Do(func() { close(s.exited) })
	return false
}

func (s *contextTrackingScanner) ScanContext(ctx context.Context) bool {
	s.enterOnce.Do(func() { close(s.entered) })
	select {
	case <-ctx.Done():
	case <-s.release:
	}
	s.exitOnce.Do(func() { close(s.exited) })
	return false
}

func (*contextTrackingScanner) Bytes() []byte { return nil }
func (*contextTrackingScanner) Text() string  { return "" }
func (*contextTrackingScanner) Err() error    { return nil }
func (s *contextTrackingScanner) Release() {
	s.closeOnce.Do(func() { close(s.release) })
}

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

func TestStreamRecordsUserAnchorForAcceptedTurnsThatEndWithErrorOrAbort(t *testing.T) {
	for _, stopReason := range []string{"error", "aborted"} {
		t.Run(stopReason, func(t *testing.T) {
			// Given Pi accepted and appended the user prompt before the assistant failed or was aborted,
			// When the turn settles,
			// Then post-turn entries still identify the appended user while the stream remains failed.
			script := strings.Join([]string{
				`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
				`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}}],"leafId":"before-leaf"}}`,
				`{"type":"response","command":"prompt","success":true}`,
				`{"type":"agent_end","messages":[{"role":"assistant","content":[],"stopReason":"` + stopReason + `","errorMessage":"turn stopped"}],"willRetry":false}`,
				`{"type":"agent_settled"}`,
				`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}},{"type":"message","id":"turn-user","parentId":"before-leaf","message":{"role":"user","content":"hello"}},{"type":"message","id":"turn-assistant","parentId":"turn-user","message":{"role":"assistant"}}],"leafId":"turn-assistant"}}`,
				`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
				"",
			}, "\n")
			client, proc, _ := newSingleProcessCaptureClient(script)

			stream, err := client.Stream(context.Background(), "hello", RunCaptureUserAnchor())
			require.NoError(t, err)
			var kinds []EventKind
			for stream.Next() {
				kinds = append(kinds, stream.Event().Kind)
			}

			assert.Equal(t, "turn-user", stream.UserAnchor())
			assert.Error(t, stream.Err())
			assert.Contains(t, kinds, EventError)
			assert.NotContains(t, kinds, EventDone)
			frames := stdinFrames(t, proc.stdin.String())
			require.GreaterOrEqual(t, len(frames), 2)
			assert.Equal(t, "get_entries", frames[len(frames)-2]["type"])
		})
	}
}

func TestStreamLeavesAnchorEmptyWhenPrePromptLeafIsMissingOrInvalid(t *testing.T) {
	tests := []struct {
		name          string
		beforeEntries string
		beforeLeaf    string
	}{
		{
			name:          "missing leaf with existing entries",
			beforeEntries: `[{"type":"message","id":"history-user","parentId":null,"message":{"role":"user"}}]`,
			beforeLeaf:    `null`,
		},
		{
			name:          "leaf absent from the pre-prompt snapshot",
			beforeEntries: `[{"type":"message","id":"history-user","parentId":null,"message":{"role":"user"}}]`,
			beforeLeaf:    `"missing-from-pre"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given pre-prompt entries cannot prove a valid active leaf,
			// When the post-turn tree contains historical and current user entries,
			// Then anchor capture fails closed instead of treating history as a root boundary.
			script := strings.Join([]string{
				`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
				`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":` + tt.beforeEntries + `,"leafId":` + tt.beforeLeaf + `}}`,
				`{"type":"response","command":"prompt","success":true}`,
				`{"type":"agent_end","messages":[],"willRetry":false}`,
				`{"type":"agent_settled"}`,
				`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"history-user","parentId":null,"message":{"role":"user"}},{"type":"message","id":"missing-from-pre","parentId":"history-user","message":{"role":"assistant"}},{"type":"message","id":"turn-user","parentId":"missing-from-pre","message":{"role":"user"}},{"type":"message","id":"turn-assistant","parentId":"turn-user","message":{"role":"assistant"}}],"leafId":"turn-assistant"}}`,
				`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
				"",
			}, "\n")
			client, _, _ := newSingleProcessCaptureClient(script)

			stream, err := client.Stream(context.Background(), "hello", RunCaptureUserAnchor())
			require.NoError(t, err)
			for stream.Next() {
			}

			assert.Empty(t, stream.UserAnchor())
		})
	}
}

func TestStreamPreservesCompletedAnswerWhenPostAnswerMetadataExceedsFrameBound(t *testing.T) {
	// Given Pi completes the assistant answer but the post-turn get_entries frame
	// exceeds the bounded frame safety limit,
	// When the stream settles,
	// Then the answer and Done event survive while the anchor degrades to empty.
	prefix := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-1"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"completed answer"}}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"turn-user","parentId":null,"message":{"role":"user","images":[{"data":"`,
	}, "\n")
	oversized := io.MultiReader(
		strings.NewReader(prefix),
		&repeatingByteReader{remaining: rpcFrameSafetyLimit + 1, value: 'A'},
		strings.NewReader(`"}]}}],"leafId":"turn-user"}}`+"\n"),
	)
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: oversized,
		done:   make(chan error, 1),
	}
	client := New(WithRPCProcessRunnerForTesting(&captureRunner{proc: proc}))

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
