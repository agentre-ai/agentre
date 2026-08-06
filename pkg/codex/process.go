package codex

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/agentre-ai/agentre/internal/pkg/cliprocess"
)

var (
	ErrBinaryNotFound = errors.New("codex: codex binary not found in PATH or configured CLIPath")
	ErrProcessDead    = errors.New("codex: process exited unexpectedly")
	ErrProtocol       = errors.New("codex: app-server protocol violation")
)

type ExitError struct {
	Err    error
	Stderr string
}

func (e *ExitError) Error() string {
	if e == nil {
		return ""
	}
	stderr := strings.TrimSpace(sanitizeDiagnostic(e.Stderr))
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

const maxStderrTailBytes = 64 * 1024

// lockedBuffer retains only the most recent stderr bytes. An app-server can be
// alive for many turns, so retaining its full lifetime output is an unbounded
// memory leak and makes a later ExitError unnecessarily likely to expose old
// credentials.
type lockedBuffer struct {
	mu sync.Mutex
	b  []byte
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) >= maxStderrTailBytes {
		b.b = append(b.b[:0], p[len(p)-maxStderrTailBytes:]...)
		return len(p), nil
	}
	over := len(b.b) + len(p) - maxStderrTailBytes
	if over > 0 {
		copy(b.b, b.b[over:])
		b.b = b.b[:len(b.b)-over]
	}
	b.b = append(b.b, p...)
	return len(p), nil
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.b)
}

var (
	bearerCredentialRE = regexp.MustCompile(`(?i)(bearer\s+)[^\s,;"']+`)
	openAIKeyRE        = regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}\b`)
	credentialValueRE  = regexp.MustCompile(`(?i)((?:[a-z0-9_]*(?:api[_-]?key|token|secret|authorization))["']?\s*[:=]\s*["']?)[^\s,;"']+`)
)

func sanitizeDiagnostic(value string) string {
	value = bearerCredentialRE.ReplaceAllString(value, `${1}[REDACTED]`)
	value = openAIKeyRE.ReplaceAllString(value, `[REDACTED]`)
	return credentialValueRE.ReplaceAllString(value, `${1}[REDACTED]`)
}
