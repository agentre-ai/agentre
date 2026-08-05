package piagent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Given EOF is observed after the child process has already exited, when the
// stream reports process death and is then closed, Close must reuse the known
// exit result instead of waiting forever for a second process completion.
func TestStreamCloseAfterProcessExitWasObserved(t *testing.T) {
	tests := []struct {
		name    string
		waitErr error
		wantErr error
	}{
		{name: "clean exit", wantErr: ErrProcessDead},
		{name: "failed exit", waitErr: errors.New("exit status 2")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proc := newFakeProcess(t)
			rpcProc := proc.rpcProcess()
			rpcProc.lines = bufio.NewScanner(strings.NewReader(""))
			proc.complete(tt.waitErr)
			<-rpcProc.done
			stream := newStream(rpcProc, 10*time.Millisecond)

			go stream.drain(context.Background())
			for stream.Next() {
			}
			if tt.wantErr != nil {
				require.ErrorIs(t, stream.Err(), tt.wantErr)
			} else {
				require.ErrorIs(t, stream.Err(), tt.waitErr)
			}

			closed := make(chan error, 1)
			go func() {
				closed <- stream.Close(context.Background())
			}()

			select {
			case err := <-closed:
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				} else {
					require.ErrorIs(t, err, tt.waitErr)
				}
			case <-time.After(250 * time.Millisecond):
				t.Fatal("Close blocked after process completion was already observed")
			}
		})
	}
}

func TestStreamClose(t *testing.T) {
	convey.Convey("Given a pi-agent text probe that already reached agent_settled", t, func() {
		runner := &fakeRunner{process: newFakeProcess(t)}
		runner.process.stdout = strings.NewReader(strings.Join([]string{
			`{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"test-native-session"}}`,
			`{"type":"response","command":"prompt","success":true}`,
			`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"pong"}}`,
			`{"type":"agent_end","messages":[],"willRetry":false}`,
			`{"type":"agent_settled"}`,
			"",
		}, "\n"))
		runner.process.finishOnSignal(interruptExitError(t))
		client := New(
			WithRPCProcessRunnerForTesting(runner),
			WithKillGrace(time.Second),
		)

		convey.Convey("When Text closes the completed RPC stream, then SIGINT cleanup is not surfaced as failure", func() {
			text, err := client.Text(context.Background(), "ping")

			convey.So(err, convey.ShouldBeNil)
			convey.So(text, convey.ShouldEqual, "pong")
			assert.True(t, runner.process.signaled, "completed text probe should interrupt the lingering RPC process during cleanup")
		})
	})

	convey.Convey("Given a running pi-agent RPC stream", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), time.Second)

		convey.Convey("When Close interrupts the RPC process and it exits from SIGINT, then Close succeeds", func() {
			proc.finishOnSignal(interruptExitError(t))

			err := stream.Close(context.Background())

			convey.So(err, convey.ShouldBeNil)
			assert.True(t, proc.signaled, "running process should be interrupted during Close")
		})
	})

	convey.Convey("Given a running pi-agent RPC stream", t, func() {
		proc := newFakeProcess(t)
		stream := newStream(proc.rpcProcess(), time.Second)

		convey.Convey("When Close interrupts the RPC process and it exits with another error, then Close returns that error", func() {
			proc.finishOnSignal(errors.New("exit status 2"))

			err := stream.Close(context.Background())

			convey.So(err, convey.ShouldNotBeNil)
			assert.Contains(t, err.Error(), "exit status 2")
			assert.True(t, proc.signaled, "running process should be interrupted during Close")
		})
	})
}

func TestCanceledAcceptedStreamSettlesAnchorBeforeTerminatingProcessTree(t *testing.T) {
	stream, cancel, parentPID, toolPID := startAcceptedRealStream(t, true)

	// Exercise the real cancellation race, not a scripted aborted frame on a live
	// background context: even if cancellation wins before the explicit interrupt,
	// the accepted Pi prompt keeps one scanner/process through settlement metadata.
	cancel()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, stream.Interrupt(context.Background()))

	finished := make(chan struct{})
	go func() {
		for stream.Next() {
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("accepted canceled stream did not finish within the bounded settlement window")
	}

	assert.Equal(t, "turn-user-exact", stream.UserAnchor())
	assert.ErrorContains(t, stream.Err(), "abort")
	assertProcessGoneEventually(t, parentPID)
	assertProcessGoneEventually(t, toolPID)

	started := time.Now()
	assert.Error(t, stream.Close(context.Background()))
	assert.Less(t, time.Since(started), time.Second, "Close must remain responsive after settlement")
	assertProcessGoneEventually(t, parentPID)
	assertProcessGoneEventually(t, toolPID)

	started = time.Now()
	assert.ErrorContains(t, stream.Close(context.Background()), "abort")
	assert.Less(t, time.Since(started), 100*time.Millisecond, "repeated Close must be idempotent")
}

func TestCanceledAcceptedStreamWithoutSettlementTerminatesTreeWithinBound(t *testing.T) {
	stream, cancel, parentPID, toolPID := startAcceptedRealStream(t, false)

	cancel()
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, stream.Interrupt(context.Background()))

	finished := make(chan struct{})
	go func() {
		for stream.Next() {
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("canceled stream waited forever when Pi omitted abort settlement")
	}
	assert.ErrorIs(t, stream.Err(), context.Canceled)
	assertProcessGoneEventually(t, parentPID)
	assertProcessGoneEventually(t, toolPID)

	started := time.Now()
	_ = stream.Close(context.Background())
	assert.Less(t, time.Since(started), time.Second, "forced termination must stay bounded")
	assertProcessGoneEventually(t, parentPID)
	assertProcessGoneEventually(t, toolPID)
}

func startAcceptedRealStream(t *testing.T, settleOnAbort bool) (*Stream, context.CancelFunc, int, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the real shell process-tree regression uses Unix signals")
	}

	dir := t.TempDir()
	parentPIDFile := filepath.Join(dir, "parent.pid")
	toolPIDFile := filepath.Join(dir, "tool.pid")
	toolScript := filepath.Join(dir, "tool.sh")
	serverScript := filepath.Join(dir, "pi-rpc.sh")
	require.NoError(t, os.WriteFile(toolScript, []byte("#!/bin/sh\nexec sleep 600\n"), 0o755))

	settlement := ""
	if settleOnAbort {
		settlement = `
			printf '%s\n' '{"type":"agent_end","messages":[{"role":"assistant","content":[],"stopReason":"aborted","errorMessage":"turn stopped"}],"willRetry":false}'
			printf '%s\n' '{"type":"agent_settled"}'`
	}
	script := `#!/bin/sh
trap '' INT TERM HUP
printf '%s\n' "$$" > "$PI_PARENT_PID_FILE"
prompted=0
while IFS= read -r line; do
	case "$line" in
		*'"type":"get_state"'*)
			printf '%s\n' '{"id":"session-state","type":"response","command":"get_state","success":true,"data":{"sessionId":"session-real"}}'
			;;
		*'"type":"get_entries"'*)
			if [ "$prompted" -eq 0 ]; then
				printf '%s\n' '{"id":"session-entries-before","type":"response","command":"get_entries","success":true,"data":{"entries":[],"leafId":null}}'
			else
				printf '%s\n' '{"id":"session-entries-after","type":"response","command":"get_entries","success":true,"data":{"entries":[{"type":"message","id":"turn-user-exact","parentId":null,"message":{"role":"user","content":"hello"}}],"leafId":"turn-user-exact"}}'
			fi
			;;
		*'"type":"prompt"'*)
			prompted=1
			"$PI_TOOL_SCRIPT" >/dev/null 2>&1 &
			printf '%s\n' "$!" > "$PI_TOOL_PID_FILE"
			printf '%s\n' '{"type":"response","command":"prompt","success":true}'
			;;
		*'"type":"abort"'*)` + settlement + `
			;;
		*'"type":"get_session_stats"'*)
			printf '%s\n' '{"type":"response","command":"get_session_stats","success":true,"data":{}}'
			;;
	esac
done
`
	require.NoError(t, os.WriteFile(serverScript, []byte(script), 0o755))

	client := New(
		WithBinary(serverScript),
		WithEnv(map[string]string{
			"PI_PARENT_PID_FILE": parentPIDFile,
			"PI_TOOL_PID_FILE":   toolPIDFile,
			"PI_TOOL_SCRIPT":     toolScript,
		}),
		WithKillGrace(100*time.Millisecond),
	)
	client.startupTimeout = time.Second
	ctx, cancel := context.WithCancel(context.Background())
	prepared, err := client.PrepareStream(ctx, "hello", RunCaptureUserAnchor())
	require.NoError(t, err)
	stream, err := prepared.Start(ctx)
	require.NoError(t, err)

	parentPID := readPIDEventually(t, parentPIDFile)
	toolPID := readPIDEventually(t, toolPIDFile)
	t.Cleanup(func() {
		cancel()
		terminatePID(parentPID)
		terminatePID(toolPID)
	})
	return stream, cancel, parentPID, toolPID
}

func readPIDEventually(t *testing.T, path string) int {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path) //nolint:gosec // G304: path is a test-owned file under t.TempDir.
		if err == nil {
			pid, convErr := strconv.Atoi(strings.TrimSpace(string(data)))
			if convErr == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process pid file %s was not written", path)
	return 0
}

func assertProcessGoneEventually(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("process %d remained alive after the termination bound", pid)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	return exec.Command("kill", "-0", strconv.Itoa(pid)).Run() == nil //nolint:gosec // G204: fixed executable with an OS-assigned test PID.
}

func terminatePID(pid int) {
	if pid <= 0 {
		return
	}
	_ = exec.Command("kill", "-KILL", strconv.Itoa(pid)).Run() //nolint:gosec // G204: fixed cleanup command with an OS-assigned test PID.
}

type fakeProcess struct {
	t       *testing.T
	stdout  *strings.Reader
	stderr  *strings.Reader
	done    chan struct{}
	waitErr error
	signalC chan os.Signal

	signaled bool
}

func newFakeProcess(t *testing.T) *fakeProcess {
	t.Helper()
	return &fakeProcess{
		t:       t,
		stdout:  strings.NewReader(""),
		stderr:  strings.NewReader(""),
		done:    make(chan struct{}),
		signalC: make(chan os.Signal, 1),
	}
}

func (f *fakeProcess) rpcProcess() *rpcProcess {
	stderrDone := make(chan struct{})
	close(stderrDone)
	p := &rpcProcess{
		handle:     f,
		stdin:      io.Discard,
		lines:      nil,
		stderr:     &lockedBuffer{},
		stderrDone: stderrDone,
		done:       make(chan struct{}),
	}
	go p.awaitExit()
	return p
}

func (f *fakeProcess) complete(err error) {
	f.waitErr = err
	close(f.done)
}

func (f *fakeProcess) finishOnSignal(err error) {
	f.t.Helper()
	go func() {
		<-f.signalC
		f.complete(err)
	}()
}

func (f *fakeProcess) Stdin() io.Writer  { return io.Discard }
func (f *fakeProcess) Stdout() io.Reader { return f.stdout }
func (f *fakeProcess) Stderr() io.Reader { return f.stderr }

func (f *fakeProcess) Wait() error {
	<-f.done
	return f.waitErr
}

func (f *fakeProcess) Kill() error { return nil }

func (f *fakeProcess) Signal(sig os.Signal) error {
	f.signaled = true
	select {
	case f.signalC <- sig:
	default:
	}
	return nil
}

func interruptExitError(t *testing.T) error {
	t.Helper()
	cmd := exec.Command("sh", "-c", "kill -INT $$")
	err := cmd.Run()
	require.Error(t, err)
	return err
}

type fakeRunner struct {
	process *fakeProcess
}

func (r *fakeRunner) Start(context.Context, procOptions) (processHandle, error) {
	return r.process, nil
}

func TestFakeProcessImplementsProcessHandle(t *testing.T) {
	var _ processHandle = (*fakeProcess)(nil)
}
