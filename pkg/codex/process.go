package codex

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"strings"
	"sync"

	"agentre/internal/pkg/clienv"
	"agentre/internal/pkg/procattr"
)

var (
	ErrBinaryNotFound = errors.New("codex: codex binary not found in PATH or configured CLIPath")
	ErrProcessDead    = errors.New("codex: process exited unexpectedly")
)

type ExitError struct {
	Err    error
	Stderr string
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	stderr := strings.TrimSpace(e.Stderr)
	if stderr == "" {
		return fmt.Sprintf("codex: process exited: %v", e.Err)
	}
	return fmt.Sprintf("codex: process exited: %v: %s", e.Err, stderr)
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type procOptions struct {
	Binary string
	Args   []string
	Cwd    string
	Env    []string
}

type processHandle interface {
	Stdin() io.Writer
	Stdout() io.Reader
	Stderr() io.Reader
	Wait() error
	Kill() error
	Signal(os.Signal) error
}

type appServerRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

type execAppServerRunner struct{}

type execHandle struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
}

func (h *execHandle) Stdin() io.Writer  { return h.stdin }
func (h *execHandle) Stdout() io.Reader { return h.stdout }
func (h *execHandle) Stderr() io.Reader { return h.stderr }
func (h *execHandle) Wait() error       { return h.cmd.Wait() }

func (h *execHandle) Kill() error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Kill()
}

func (h *execHandle) Signal(sig os.Signal) error {
	if h.cmd.Process == nil {
		return nil
	}
	return h.cmd.Process.Signal(sig)
}

func (r execAppServerRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	extraEnv := envListToMap(opts.Env)
	searchEnv := clienv.BuildEnv(extraEnv, opts.Binary)
	binary, ok := clienv.ResolveBinaryForEnv(opts.Binary, searchEnv)
	if !ok {
		return nil, ErrBinaryNotFound
	}
	// #nosec G204 -- binary and args are assembled from AgentBackend config and
	// fixed protocol flags. Launching the configured CLI is the intended behavior.
	cmd := exec.CommandContext(ctx, binary, opts.Args...)
	procattr.ApplyNoConsoleWindow(cmd)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}
	cmd.Env = clienv.BuildEnv(extraEnv, binary)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		if errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist) {
			return nil, ErrBinaryNotFound
		}
		return nil, err
	}
	return &execHandle{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr}, nil
}

func envListToMap(items []string) map[string]string {
	if len(items) == 0 {
		return nil
	}
	out := make(map[string]string, len(items))
	for _, item := range items {
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	_, _ = b.b.Write(p)
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}
