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

type singleProcessCaptureRunner struct {
	proc   *captureProc
	starts int
}

func (r *singleProcessCaptureRunner) Start(context.Context, procOptions) (processHandle, error) {
	r.starts++
	return r.proc, nil
}

func newSingleProcessCaptureClient(stdout string) (*Client, *captureProc, *singleProcessCaptureRunner) {
	proc := &captureProc{
		stdin:  &lockedBuffer{},
		stdout: strings.NewReader(stdout),
		done:   make(chan error, 1),
	}
	runner := &singleProcessCaptureRunner{proc: proc}
	return New(WithRPCProcessRunnerForTesting(runner)), proc, runner
}

func TestPrepareStreamForksWithoutSendingPromptUntilStart(t *testing.T) {
	// Given an existing native session and fork anchor,
	// When the caller prepares the stream before its transcript transaction,
	// Then fork completes in the retained process but the prompt is withheld until Start.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
		`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-new"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"new-user","parentId":null,"message":{"role":"user"}}],"leafId":"new-user"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, proc, runner := newSingleProcessCaptureClient(script)
	client.session = "session-old"

	prepared, err := client.PrepareStream(context.Background(), "commit first", RunForkAnchor("fork-user"))
	require.NoError(t, err)
	assert.Equal(t, "session-new", prepared.SessionID())
	assert.Equal(t, 1, runner.starts)
	beforeStart := stdinFrames(t, proc.stdin.String())
	require.Len(t, beforeStart, 5)
	for _, frame := range beforeStart {
		assert.NotEqual(t, "prompt", frame["type"])
	}

	stream, err := prepared.Start(context.Background())
	require.NoError(t, err)
	for stream.Next() {
	}
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 8)
	assert.Equal(t, "prompt", frames[5]["type"])
	assert.Equal(t, "new-user", stream.UserAnchor())
}

func TestStreamForksBeforePromptInTheSameRPCProcess(t *testing.T) {
	// Given an existing Pi session and a native user-entry anchor,
	// When a prompt stream starts from that anchor,
	// Then the same process restores, forks, exposes the new session, and only then prompts.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
		`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"text":"repeat","cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-new"}}`,
		`{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}}],"leafId":"before-leaf"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"before-leaf","parentId":null,"message":{"role":"assistant"}},{"type":"message","id":"new-user","parentId":"before-leaf","message":{"role":"user","content":"repeat"}},{"type":"message","id":"new-assistant","parentId":"new-user","message":{"role":"assistant"}}],"leafId":"new-assistant"}}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, proc, runner := newSingleProcessCaptureClient(script)
	client.session = "session-old"

	stream, err := client.Stream(context.Background(), "repeat", RunForkAnchor("fork-user"))
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, 1, runner.starts)
	assert.Equal(t, "session-new", stream.SessionID())
	assert.Equal(t, "new-user", stream.UserAnchor())
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 8)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "fork", frames[1]["type"])
	assert.Equal(t, "fork-user", frames[1]["entryId"])
	assert.Equal(t, "get_state", frames[2]["type"])
	assert.Equal(t, "get_entries", frames[3]["type"])
	assert.Equal(t, "get_session_stats", frames[4]["type"])
	assert.Equal(t, "prompt", frames[5]["type"])
	assert.Equal(t, "get_entries", frames[6]["type"])
	assert.Equal(t, "get_session_stats", frames[7]["type"])
}

func TestStreamWithEmptyForkAnchorKeepsNormalPromptFlow(t *testing.T) {
	// Given the per-turn fork target is empty,
	// When the prompt stream starts,
	// Then it uses the existing session and never sends a fork command.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-existing"}}`,
		`{"type":"response","command":"prompt","success":true}`,
		`{"type":"agent_end","messages":[],"willRetry":false}`,
		`{"type":"agent_settled"}`,
		`{"type":"response","command":"get_session_stats","success":true,"data":{}}`,
		"",
	}, "\n")
	client, proc, _ := newSingleProcessCaptureClient(script)
	client.session = "session-existing"

	stream, err := client.Stream(context.Background(), "hello", RunForkAnchor(""))
	require.NoError(t, err)
	for stream.Next() {
	}

	assert.Equal(t, "session-existing", stream.SessionID())
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 4)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "get_session_stats", frames[1]["type"])
	assert.Equal(t, "prompt", frames[2]["type"])
	assert.Equal(t, "get_session_stats", frames[3]["type"])
}

func TestStreamForkFailsExplicitlyWhenExtensionRequestsBlockingUI(t *testing.T) {
	// Given a session_before_fork extension asks for confirmation over the generic UI bridge,
	// When Agentre starts a fork without implementing that bridge,
	// Then startup fails explicitly before prompt instead of waiting for the request forever.
	script := strings.Join([]string{
		`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
		`{"type":"extension_ui_request","id":"ui-before-fork","method":"confirm","title":"Fork session?","message":"session_before_fork requires confirmation"}`,
		"",
	}, "\n")
	client, proc, _ := newSingleProcessCaptureClient(script)
	client.session = "session-old"
	var rawFrames []string
	client.rawSink = func(line []byte) { rawFrames = append(rawFrames, string(line)) }
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	stream, err := client.Stream(ctx, "must not be sent", RunForkAnchor("fork-user"))

	assert.Nil(t, stream)
	require.Error(t, err)
	assert.NotErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "extension UI")
	assert.Contains(t, err.Error(), "confirm")
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 2)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "fork", frames[1]["type"])
	assert.NotContains(t, strings.Join(rawFrames, "\n"), "session_before_fork requires confirmation")
}

func TestStreamForkFailuresStopBeforePrompt(t *testing.T) {
	tests := []struct {
		name       string
		forkResult string
		wantError  string
		wantTypes  []string
		waitErr    error
	}{
		{
			name:       "fork response fails",
			forkResult: `{"id":"session-fork","type":"response","command":"fork","success":false,"error":"Invalid entry ID for forking"}`,
			wantError:  "Invalid entry ID for forking",
			wantTypes:  []string{"get_state", "fork"},
		},
		{
			name:       "fork is canceled",
			forkResult: `{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"text":"hello","cancelled":true}}`, //nolint:misspell // Pi RPC field uses British spelling.
			wantError:  "canceled",
			wantTypes:  []string{"get_state", "fork"},
		},
		{
			name: "forked session id is empty",
			forkResult: strings.Join([]string{
				`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"text":"hello","cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
				`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":""}}`,
			}, "\n"),
			wantError: "empty session id",
			wantTypes: []string{"get_state", "fork", "get_state"},
		},
		{
			name: "forked session id is unchanged",
			forkResult: strings.Join([]string{
				`{"id":"session-fork","type":"response","command":"fork","success":true,"data":{"text":"hello","cancelled":false}}`, //nolint:misspell // Pi RPC field uses British spelling.
				`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
			}, "\n"),
			wantError: "did not change session id",
			wantTypes: []string{"get_state", "fork", "get_state"},
		},
		{
			name:      "process exits before fork response",
			wantError: "process exited",
			wantTypes: []string{"get_state", "fork"},
			waitErr:   errors.New("exit status 1"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given Pi cannot establish a distinct forked session,
			// When the stream starts from an anchor,
			// Then startup fails closed without sending the prompt.
			script := strings.Join([]string{
				`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}`,
				tt.forkResult,
				"",
			}, "\n")
			client, proc, _ := newSingleProcessCaptureClient(script)
			if tt.waitErr != nil {
				proc.done <- tt.waitErr
			}
			client.session = "session-old"

			stream, err := client.Stream(context.Background(), "must not be sent", RunForkAnchor("fork-user"))

			assert.Nil(t, stream)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantError)
			frames := stdinFrames(t, proc.stdin.String())
			require.Len(t, frames, len(tt.wantTypes))
			for i, wantType := range tt.wantTypes {
				assert.Equal(t, wantType, frames[i]["type"])
			}
			for _, frame := range frames {
				assert.NotEqual(t, "prompt", frame["type"])
			}
		})
	}
}
