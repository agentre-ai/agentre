package claudecode

import (
	"context"
	"fmt"
	"io"
	"testing"
)

// writeFrame 是 fake CLI 写 stdout 帧的薄封装：fmt.Fprintf + 自动换行 + 吞错。
// stdout 是 io.Pipe 写端,只有在 fakeCLI 退出 / pipe close 时才会写失败,fake CLI
// 测试场景下这不需要单独报；用 helper 把 errcheck 噪音消掉。
func writeFrame(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format+"\n", args...)
}

// fakeCLIFunc 模拟 claude CLI 的 stdio loop：从 stdin 读 JSONL 帧（或单纯阻塞），
// 往 stdout 写响应帧。运行结束（返回）后 process 进入 "已退出"。
type fakeCLIFunc func(stdin io.Reader, stdout io.Writer)

// pipeProcessOpt 控制 fake process 的退出语义，覆盖 startProcess 真实路径的
// 各种结局（stderr → ErrSessionNotFound、非 0 exit → *ProcessExitError、
// exit 0 + 无 stderr → nil 错）。
type pipeProcessOpt func(*pipeProcessConfig)

type pipeProcessConfig struct {
	exitCode int
	stderr   string
}

func withExitCode(code int) pipeProcessOpt {
	return func(cfg *pipeProcessConfig) { cfg.exitCode = code }
}

func withStderr(s string) pipeProcessOpt {
	return func(cfg *pipeProcessConfig) { cfg.stderr = s }
}

// newPipeProcess 构造一个用 io.Pipe 模拟 stdio 的 *process，避开真子进程。
// fakeCLI 在独立 goroutine 跑；返回后 stdout pipe 关闭、stderr 写入 boundedBuffer
// 并按 cfg.exitCode 计算 exitErr（与真 reaper goroutine 的语义一致）。
//
// 调用方：
//
//	p := newPipeProcess(t, ctx, func(stdin io.Reader, stdout io.Writer) {
//	    fmt.Fprintln(stdout, `{"type":"system",...}`)
//	    ...
//	}, withExitCode(0))
func newPipeProcess(t *testing.T, ctx context.Context, fakeCLI fakeCLIFunc, opts ...pipeProcessOpt) *process {
	t.Helper()
	cfg := pipeProcessConfig{}
	for _, o := range opts {
		o(&cfg)
	}

	stdinR, stdinW := io.Pipe()
	stdoutR, stdoutW := io.Pipe()
	stderrBuf := newBoundedBuffer(maxStderrBytes)
	if cfg.stderr != "" {
		_, _ = stderrBuf.Write([]byte(cfg.stderr))
	}

	p := &process{
		stdin:     stdinW,
		stdout:    stdoutR,
		stderrBuf: stderrBuf,
		exit:      make(chan struct{}),
	}

	// nil fakeCLI：模拟"启动就已死"的进程（比如 resume 失效那种立刻 stderr+exit 1
	// 的场景）。同步关 stdout / stdin pipe 并填好 exit cache,让 OpenSession 的
	// 200ms 健康检查窗口里 `select <-p.exit` 确定命中（无 goroutine 调度竞争）。
	if fakeCLI == nil {
		_ = stdoutW.Close()
		_ = stdinR.Close()
		p.exitCode = cfg.exitCode
		p.exitErr = classifyStderr(stderrBuf.String(), cfg.exitCode)
		close(p.exit)
		return p
	}

	go func() {
		defer close(p.exit)
		defer func() { _ = stdoutW.Close() }()
		defer func() { _ = stdinR.Close() }()
		fakeCLI(stdinR, stdoutW)
		p.exitCode = cfg.exitCode
		p.exitErr = classifyStderr(stderrBuf.String(), cfg.exitCode)
	}()

	// 跟 startProcess 一致：ctx 取消后让 stdout 提前 EOF，避免测试卡住。
	go func() {
		select {
		case <-ctx.Done():
			_ = stdoutW.CloseWithError(ctx.Err())
			_ = stdinR.CloseWithError(ctx.Err())
		case <-p.exit:
		}
	}()

	return p
}

// pipeSpawner 把 fakeCLI/选项打包成一个 Option，注入到 Client.spawner 上。
// 测试里这样用：
//
//	c := New(WithBinary("fake"), pipeSpawner(t, fakeCLI))
//	sess, err := c.OpenSession(ctx)
func pipeSpawner(t *testing.T, fakeCLI fakeCLIFunc, opts ...pipeProcessOpt) Option {
	t.Helper()
	return func(c *Client) {
		c.spawner = func(ctx context.Context, _ processSpec) (*process, error) {
			return newPipeProcess(t, ctx, fakeCLI, opts...), nil
		}
	}
}
