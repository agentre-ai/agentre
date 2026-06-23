package codex

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/cliprocess"
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

type procOptions = cliprocess.Options
type processHandle = cliprocess.Handle

type appServerRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

type execAppServerRunner struct{}

func (r execAppServerRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	return cliprocess.Start(ctx, opts, ErrBinaryNotFound)
}

type lockedBuffer = cliprocess.LockedBuffer
