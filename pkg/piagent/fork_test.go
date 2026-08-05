package piagent

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	require.Len(t, frames, 7)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "fork", frames[1]["type"])
	assert.Equal(t, "fork-user", frames[1]["entryId"])
	assert.Equal(t, "get_state", frames[2]["type"])
	assert.Equal(t, "get_entries", frames[3]["type"])
	assert.Equal(t, "prompt", frames[4]["type"])
	assert.Equal(t, "get_entries", frames[5]["type"])
	assert.Equal(t, "get_session_stats", frames[6]["type"])
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
	require.Len(t, frames, 3)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "prompt", frames[1]["type"])
	assert.Equal(t, "get_session_stats", frames[2]["type"])
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
