package piagent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStreamDiscoversNativeSessionBeforePrompt(t *testing.T) {
	// Given Pi starts a new persistent native session,
	// When Agentre opens a prompt stream,
	// Then it reads get_state before sending the prompt and exposes Pi's native ID.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"pi-native-123","sessionFile":"/home/me/.pi/agent/sessions/project/session.jsonl"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)

	stream, err := client.Stream(context.Background(), "hello")
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "pi-native-123", stream.SessionID())
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 4)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
	assert.Equal(t, "prompt", frames[2]["type"])
	assert.Equal(t, "get_session_stats", frames[3]["type"])
}

func TestCompactDiscoversNativeSessionBeforeCommand(t *testing.T) {
	// Given an existing native Pi session is opened for compaction,
	// When Agentre starts the compact stream,
	// Then it confirms the native ID before issuing compact.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"pi-native-compact"}}`,
		`{"type":"response","command":"compact","success":true,"data":{"summary":"done"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, proc := newCaptureClient(script)
	client.session = "pi-native-compact"

	stream, err := client.Compact(context.Background(), "pi-native-compact")
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "pi-native-compact", stream.SessionID())
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 3)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "compact", frames[1]["type"])
	assert.Equal(t, "get_session_stats", frames[2]["type"])
}

func TestStreamRejectsUnexpectedResumedSessionID(t *testing.T) {
	// Given Agentre asked Pi to resume one native session,
	// When get_state reports a different identity,
	// Then startup fails closed before the prompt is sent.
	client, proc := newCaptureClient(
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"pi-native-other"}}` + "\n",
	)
	client.session = "pi-native-expected"

	stream, err := client.Stream(context.Background(), "must not be sent")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "unexpected session id")
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 1)
	assert.Equal(t, "get_state", frames[0]["type"])
}

func TestStreamRejectsResumedSessionIDWithExpectedPrefix(t *testing.T) {
	// Given Pi resolves a different native session whose ID merely starts with
	// the persisted ID, when startup validates the identity, then it fails
	// closed instead of treating the prefix match as the same session.
	client, proc := newCaptureClient(
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"pi-native-expected-other"}}` + "\n",
	)
	client.session = "pi-native-expected"

	stream, err := client.Stream(context.Background(), "must not be sent")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "unexpected session id")
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 1)
	assert.Equal(t, "get_state", frames[0]["type"])
}

func TestProcessExitClassifiesMissingNativeSession(t *testing.T) {
	// Given Pi rejects --session because the native ID no longer exists,
	// When the subprocess error is classified,
	// Then callers can recover through a stable session-not-found sentinel.
	err := wrapExitError(errors.New("exit status 1"), "No session found matching 'pi-native-gone'")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrSessionNotFound)
	assert.Contains(t, err.Error(), "pi-native-gone")
}

func TestStreamWaitsForMissingNativeSessionExitClassification(t *testing.T) {
	// Given Pi closes stdout before its delayed Wait result and stderr are
	// available, when startup observes EOF, then it waits for the process result
	// and preserves the session-not-found classification.
	proc := newFakeProcess(t)
	proc.stderr = strings.NewReader("No session found matching 'pi-native-gone'")
	client := New(
		WithRPCProcessRunnerForTesting(&fakeRunner{process: proc}),
		WithSession("pi-native-gone"),
		WithKillGrace(time.Second),
	)
	go func() {
		time.Sleep(150 * time.Millisecond)
		proc.complete(errors.New("exit status 1"))
	}()

	stream, err := client.Stream(context.Background(), "must not be sent")

	assert.Nil(t, stream)
	require.ErrorIs(t, err, ErrSessionNotFound)
}

func TestStreamRejectsEmptyNativeSessionBeforePrompt(t *testing.T) {
	// Given Pi cannot report a durable native session identity,
	// When Agentre opens a prompt stream,
	// Then startup fails closed and no prompt is sent.
	client, proc := newCaptureClient(
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":""}}` + "\n",
	)

	stream, err := client.Stream(context.Background(), "must not be sent")

	require.Error(t, err)
	assert.Nil(t, stream)
	assert.Contains(t, err.Error(), "empty session id")
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 1)
	assert.Equal(t, "get_state", frames[0]["type"])
}
