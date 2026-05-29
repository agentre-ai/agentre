//go:build !windows

// Package local implements pty.Backend with github.com/creack/pty.
package local

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	creackpty "github.com/creack/pty"

	pkgpty "agentre/internal/pkg/pty"
)

// Backend is the local creack/pty implementation of pty.Backend.
type Backend struct{}

func NewBackend() *Backend { return &Backend{} }

const sigkillGrace = 200 * time.Millisecond

// Open spawns a shell under a PTY and starts the reaper + reader goroutines.
func (b *Backend) Open(ctx context.Context, spec pkgpty.Spec) (pkgpty.Handle, error) {
	shell := spec.Shell
	if shell == "" {
		if env := os.Getenv("SHELL"); env != "" {
			shell = env
		} else {
			shell = "/bin/sh"
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	cmd := exec.Command(shell, "-l") //nolint:gosec // G204: shell is from user spec or $SHELL env; not from request input
	cmd.Dir = spec.Cwd
	cmd.Env = append(os.Environ(), append(spec.Env, "TERM=xterm-256color")...)

	ws := &creackpty.Winsize{Cols: spec.Cols, Rows: spec.Rows}
	f, err := creackpty.StartWithSize(cmd, ws)
	if err != nil {
		return nil, err
	}
	h := &handleImpl{
		cmd:  cmd,
		file: f,
		data: make(chan []byte, 32),
		exit: make(chan pkgpty.ExitInfo, 1),
		done: make(chan struct{}),
	}
	go h.reader()
	go h.reaper()
	return h, nil
}

type handleImpl struct {
	cmd  *exec.Cmd
	file *os.File

	data chan []byte
	exit chan pkgpty.ExitInfo

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func (h *handleImpl) Write(p []byte) (int, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return 0, errors.New("pty closed")
	}
	h.mu.Unlock()
	return h.file.Write(p)
}

func (h *handleImpl) Resize(cols, rows uint16) error {
	return creackpty.Setsize(h.file, &creackpty.Winsize{Cols: cols, Rows: rows})
}

func (h *handleImpl) Close() error {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	h.mu.Unlock()

	if h.cmd.Process != nil {
		_ = h.cmd.Process.Signal(syscall.SIGHUP)
	}
	select {
	case <-h.done:
	case <-time.After(sigkillGrace):
		if h.cmd.Process != nil {
			_ = h.cmd.Process.Kill()
		}
		<-h.done
	}
	return nil
}

func (h *handleImpl) Data() <-chan []byte          { return h.data }
func (h *handleImpl) Exit() <-chan pkgpty.ExitInfo { return h.exit }

func (h *handleImpl) reader() {
	// The reader is the SOLE closer of h.data: it both sends to and closes the
	// channel, so there is never a concurrent send-and-close (which would panic
	// "send on closed channel"). The reaper must NOT close h.data.
	defer close(h.data)
	buf := make([]byte, 8192)
	for {
		n, err := h.file.Read(buf)
		if n > 0 {
			out := make([]byte, n)
			copy(out, buf[:n])
			select {
			case h.data <- out:
			case <-h.done:
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func (h *handleImpl) reaper() {
	err := h.cmd.Wait()
	close(h.done)
	_ = h.file.Close()

	info := pkgpty.ExitInfo{}
	if err == nil {
		info.Reason = "natural"
	} else {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			info.Code = ee.ExitCode()
			if h.wasKilled() {
				info.Reason = "killed"
			} else {
				info.Reason = "natural"
			}
		} else {
			info.Reason = "error"
			info.Msg = err.Error()
		}
	}
	h.exit <- info
	close(h.exit)
	// h.data is closed by reader() (its sole owner), not here — closing it from
	// two goroutines races the reader's send and panics.
}

func (h *handleImpl) wasKilled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

var _ pkgpty.Handle = (*handleImpl)(nil)
var _ pkgpty.Backend = (*Backend)(nil)
