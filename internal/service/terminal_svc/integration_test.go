//go:build !windows

package terminal_svc_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"agentre/internal/pkg/pty"
	"agentre/internal/pkg/pty/local"
	"agentre/internal/service/terminal_svc"

	"github.com/stretchr/testify/require"
)

// localBackendBridge mirrors the production app/terminal_wiring.go adapter.
type localBackendBridge struct{ be *local.Backend }

func (b localBackendBridge) Open(ctx context.Context, spec pty.Spec) (pty.Handle, error) {
	return b.be.Open(ctx, spec)
}

type collectingEmitter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (c *collectingEmitter) Emit(_ context.Context, _ string, payload any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m, ok := payload.(map[string]string); ok {
		c.buf.WriteString(m["data"])
	}
}

func (c *collectingEmitter) Bytes() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.buf.Bytes()...)
}

func TestIntegration_LocalPTY_HappyPath(t *testing.T) {
	sel := terminal_svc.NewBackendSelector(
		localBackendBridge{be: local.NewBackend()}, nil,
	)
	emit := &collectingEmitter{}
	svc := terminal_svc.NewService(sel, emit)
	t.Cleanup(svc.Shutdown)

	require.NoError(t, svc.Open(context.Background(), "integ-1", "", "/tmp", 80, 24))
	require.NoError(t, svc.Write(context.Background(), "integ-1", "echo integ-test\n"))

	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("did not see echo output; got: %q", string(emit.Bytes()))
		case <-time.After(50 * time.Millisecond):
			if bytes.Contains(emit.Bytes(), []byte("integ-test")) {
				return
			}
		}
	}
}
