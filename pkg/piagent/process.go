package piagent

import (
	"context"
	"fmt"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/cliprocess"
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
		return fmt.Sprintf("piagent: process exited: %v", e.Err)
	}
	return fmt.Sprintf("piagent: process exited: %v: %s", e.Err, stderr)
}

func (e *ExitError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

type procOptions = cliprocess.Options
type processHandle = cliprocess.Handle

type processRunner interface {
	Start(ctx context.Context, opts procOptions) (processHandle, error)
}

type execProcessRunner struct{}

func (r execProcessRunner) Start(ctx context.Context, opts procOptions) (processHandle, error) {
	return cliprocess.Start(ctx, opts, ErrBinaryNotFound)
}

type lockedBuffer = cliprocess.LockedBuffer
