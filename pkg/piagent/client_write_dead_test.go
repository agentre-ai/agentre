package piagent

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStreamClassifiesDeadProcessOnFailedWrite 钉死:RPC 进程在第一次 stdin 写之前
// 就已早夭(对端关管道 → Write 报 broken pipe)时,Client.Stream 必须按进程退出 +
// stderr 分类成 ErrSessionNotFound,而不是把原始 pipe 错误顶上来。
//
// 这正是 CI 上 TestSessionFactoryClassifiesMissingNativeSessionProcessExit 的 flake
// 根因:Pi 子进程 `echo "No session found …" >&2; exit 1` 后,若 get_state 的 stdin
// 写赶在进程被回收之前下发 → 写成功 → stdout EOF → awaitProcessExitOrScanError 分类
// (绿);但若写赶在管道关闭之后 → broken pipe 直接返回,绕过分类(红)。这里把后一半
// 钉成确定性用例:Stdin 第一次 Write 就失败。
func TestStreamClassifiesDeadProcessOnFailedWrite(t *testing.T) {
	c := New(
		WithRPCProcessRunnerForTesting(&staticRunner{proc: newDeadOnWriteProcess()}),
		WithSession("pi-native-gone"),
	)
	_, err := c.Stream(context.Background(), "must not run")
	assert.True(t, errors.Is(err, ErrSessionNotFound), "error: %v", err)
}

// deadOnWriteProcess:进程在 Stream 第一次写之前就退出 —— Stdin 的 Write 永远失败,
// stderr 带 "No session found …",Wait 立即返回非零退出。复刻 OS 管道对端已关的形状。
type deadOnWriteProcess struct {
	stderr string
	done   chan struct{}
	wait   error
}

func newDeadOnWriteProcess() *deadOnWriteProcess {
	d := &deadOnWriteProcess{
		stderr: "No session found matching 'pi-native-gone'\n",
		done:   make(chan struct{}),
		wait:   errors.New("exit status 1"),
	}
	close(d.done) // 进程已退出,awaitExit 的 Wait 立即返回
	return d
}

func (d *deadOnWriteProcess) Stdin() io.Writer       { return closedPipeWriter{} }
func (d *deadOnWriteProcess) Stdout() io.Reader      { return strings.NewReader("") }
func (d *deadOnWriteProcess) Stderr() io.Reader      { return strings.NewReader(d.stderr) }
func (d *deadOnWriteProcess) Wait() error            { <-d.done; return d.wait }
func (d *deadOnWriteProcess) Kill() error            { return nil }
func (d *deadOnWriteProcess) Signal(os.Signal) error { return nil }

// closedPipeWriter 的 Write 永远失败,复刻对端已关的 stdin 管道。
type closedPipeWriter struct{}

func (closedPipeWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

// staticRunner:每次 Start 返回同一个预设进程句柄(Stream 一次即可)。
type staticRunner struct{ proc processHandle }

func (r *staticRunner) Start(context.Context, procOptions) (processHandle, error) {
	return r.proc, nil
}
