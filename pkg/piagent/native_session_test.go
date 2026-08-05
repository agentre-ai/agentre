package piagent

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
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
	require.Len(t, frames, 3)
	assert.Equal(t, "get_state", frames[0]["type"])
	assert.Equal(t, "prompt", frames[1]["type"])
	assert.Equal(t, "get_session_stats", frames[2]["type"])
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

type silentStartupRunner struct {
	process processHandle
}

func (r *silentStartupRunner) Start(context.Context, procOptions) (processHandle, error) {
	return r.process, nil
}

type silentStartupProcess struct {
	stdin   lockedBuffer
	stdoutR *io.PipeReader
	stdoutW *io.PipeWriter
	exited  chan struct{}

	once    sync.Once
	waitErr error
}

func newSilentStartupProcess(prefix string) *silentStartupProcess {
	stdoutR, stdoutW := io.Pipe()
	proc := &silentStartupProcess{
		stdoutR: stdoutR,
		stdoutW: stdoutW,
		exited:  make(chan struct{}),
	}
	if prefix != "" {
		go func() {
			_, _ = io.WriteString(stdoutW, prefix)
		}()
	}
	return proc
}

func (p *silentStartupProcess) Stdin() io.Writer  { return &p.stdin }
func (p *silentStartupProcess) Stdout() io.Reader { return p.stdoutR }
func (p *silentStartupProcess) Stderr() io.Reader { return strings.NewReader("") }
func (p *silentStartupProcess) Wait() error {
	<-p.exited
	return p.waitErr
}
func (p *silentStartupProcess) Kill() error {
	p.finish(errors.New("signal: killed"))
	return nil
}
func (p *silentStartupProcess) Signal(os.Signal) error {
	p.finish(errors.New("signal: interrupt"))
	return nil
}
func (p *silentStartupProcess) finish(err error) {
	p.once.Do(func() {
		p.waitErr = err
		_ = p.stdoutW.Close()
		close(p.exited)
	})
}

func TestPrepareStreamStartupHonorsCallerDeadlineWhilePiIsSilent(t *testing.T) {
	tests := []struct {
		name      string
		prefix    string
		runOption RunOption
		wantTypes []string
	}{
		{
			name:      "Given get_state stays silent, when the startup deadline expires, then startup stops before prompt",
			wantTypes: []string{"get_state"},
		},
		{
			name:      "Given fork stays silent, when the startup deadline expires, then startup stops before prompt",
			prefix:    `{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}` + "\n",
			runOption: RunForkAnchor("fork-user"),
			wantTypes: []string{"get_state", "fork"},
		},
		{
			name:      "Given get_entries stays silent, when the startup deadline expires, then startup stops before prompt",
			prefix:    `{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-old"}}` + "\n",
			runOption: RunCaptureUserAnchor(),
			wantTypes: []string{"get_state", "get_entries"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := newSilentStartupProcess(tt.prefix)
			client := New(
				WithRPCProcessRunnerForTesting(&silentStartupRunner{process: proc}),
				WithSession("session-old"),
				WithKillGrace(50*time.Millisecond),
			)
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()

			type startupResult struct {
				prepared *PreparedStream
				err      error
			}
			resultC := make(chan startupResult, 1)
			go func() {
				var opts []RunOption
				if tt.runOption != nil {
					opts = append(opts, tt.runOption)
				}
				prepared, err := client.PrepareStream(ctx, "must not be sent", opts...)
				resultC <- startupResult{prepared: prepared, err: err}
			}()

			var result startupResult
			select {
			case result = <-resultC:
			case <-time.After(250 * time.Millisecond):
				t.Error("Pi startup remained blocked after its caller deadline")
				_ = proc.Signal(interruptSignal())
				result = <-resultC
			}

			assert.Nil(t, result.prepared)
			require.ErrorIs(t, result.err, context.DeadlineExceeded)
			frames := stdinFrames(t, proc.stdin.String())
			require.Len(t, frames, len(tt.wantTypes))
			for i, wantType := range tt.wantTypes {
				assert.Equal(t, wantType, frames[i]["type"])
			}
			for _, frame := range frames {
				assert.NotEqual(t, "prompt", frame["type"])
			}
			select {
			case <-proc.exited:
			case <-time.After(time.Second):
				t.Fatal("Pi startup process was not released after cancellation")
			}
		})
	}
}

func TestPrepareStreamUsesBoundedStartupTimeoutWithoutCallerDeadline(t *testing.T) {
	proc := newSilentStartupProcess("")
	client := New(
		WithRPCProcessRunnerForTesting(&silentStartupRunner{process: proc}),
		WithKillGrace(50*time.Millisecond),
	)
	client.startupTimeout = 40 * time.Millisecond

	type startupResult struct {
		prepared *PreparedStream
		err      error
	}
	resultC := make(chan startupResult, 1)
	go func() {
		prepared, err := client.PrepareStream(context.Background(), "must not be sent")
		resultC <- startupResult{prepared: prepared, err: err}
	}()

	var result startupResult
	select {
	case result = <-resultC:
	case <-time.After(250 * time.Millisecond):
		t.Error("Pi startup ignored its bounded default timeout")
		_ = proc.Signal(interruptSignal())
		result = <-resultC
	}
	assert.Nil(t, result.prepared)
	require.ErrorIs(t, result.err, context.DeadlineExceeded)
	frames := stdinFrames(t, proc.stdin.String())
	require.Len(t, frames, 1)
	assert.Equal(t, "get_state", frames[0]["type"])
	select {
	case <-proc.exited:
	case <-time.After(time.Second):
		t.Fatal("timed-out Pi startup process was not released")
	}
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
