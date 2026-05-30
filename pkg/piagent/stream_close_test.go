package piagent

import (
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

func TestStreamClose(t *testing.T) {
	convey.Convey("Given a pi-agent text probe that already reached agent_end", t, func() {
		runner := &fakeRunner{process: newFakeProcess(t)}
		runner.process.stdout = strings.NewReader(strings.Join([]string{
			`{"type":"response","command":"prompt","success":true}`,
			`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"pong"}}`,
			`{"type":"agent_end","messages":[]}`,
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
	done    chan error
	signalC chan os.Signal

	signaled bool
}

func newFakeProcess(t *testing.T) *fakeProcess {
	t.Helper()
	return &fakeProcess{
		t:       t,
		stdout:  strings.NewReader(""),
		stderr:  strings.NewReader(""),
		done:    make(chan error, 1),
		signalC: make(chan os.Signal, 1),
	}
}

func (f *fakeProcess) rpcProcess() *rpcProcess {
	return &rpcProcess{
		handle: f,
		stdin:  io.Discard,
		lines:  nil,
		stderr: &lockedBuffer{},
		done:   f.done,
	}
}

func (f *fakeProcess) finishOnSignal(err error) {
	f.t.Helper()
	go func() {
		<-f.signalC
		f.done <- err
	}()
}

func (f *fakeProcess) Stdin() io.Writer  { return io.Discard }
func (f *fakeProcess) Stdout() io.Reader { return f.stdout }
func (f *fakeProcess) Stderr() io.Reader { return f.stderr }

func (f *fakeProcess) Wait() error {
	err, ok := <-f.done
	if !ok {
		return nil
	}
	return err
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
