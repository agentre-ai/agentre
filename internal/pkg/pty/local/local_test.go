//go:build !windows

package local_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"agentre/internal/pkg/pty"
	"agentre/internal/pkg/pty/local"

	"github.com/stretchr/testify/require"
)

func TestLocalBackend_OpenEchoRoundTrip(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	_, err = h.Write([]byte("echo hello-pty\n"))
	require.NoError(t, err)

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			if !ok {
				t.Fatalf("data channel closed before seeing echo output; got: %q", buf.String())
			}
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("hello-pty")) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for echo output; got: %q", buf.String())
		}
	}
}
