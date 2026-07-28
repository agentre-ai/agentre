package piagent

import (
	"bufio"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
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
	p := &rpcProcess{
		handle: f,
		stdin:  io.Discard,
		lines:  nil,
		stderr: &lockedBuffer{},
		done:   make(chan struct{}),
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
