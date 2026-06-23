//go:build !windows

package local_test

import (
	"bytes"
	"context"
	"os"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"

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

func TestLocalBackend_OpenContextCancelAfterSpawn_DoesNotKillShell(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	cancel()

	select {
	case info := <-h.Exit():
		t.Fatalf("shell exited after open context cancel: %+v", info)
	case <-time.After(300 * time.Millisecond):
	}

	_, err = h.Write([]byte("echo context-survived\n"))
	require.NoError(t, err)

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			if !ok {
				t.Fatalf("data channel closed after open context cancel; got: %q", buf.String())
			}
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("context-survived")) {
				return
			}
		case info := <-h.Exit():
			t.Fatalf("shell exited after open context cancel: %+v", info)
		case <-deadline:
			t.Fatalf("timeout waiting for shell after open context cancel; got: %q", buf.String())
		}
	}
}

func TestLocalBackend_OpenBadCwd_Errors(t *testing.T) {
	be := local.NewBackend()
	_, err := be.Open(context.Background(), pty.Spec{
		Cwd:   "/path/that/definitely/does/not/exist/xyzzy",
		Shell: "/bin/sh",
		Cols:  80, Rows: 24,
	})
	require.Error(t, err)
}

func TestLocalBackend_Resize_Reflected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	require.NoError(t, h.Resize(132, 40))
	_, err = h.Write([]byte("stty size\n"))
	require.NoError(t, err)

	deadline := time.After(3 * time.Second)
	var buf bytes.Buffer
	for {
		select {
		case chunk, ok := <-h.Data():
			if !ok {
				t.Fatalf("data closed before stty output; got: %q", buf.String())
			}
			buf.Write(chunk)
			if bytes.Contains(buf.Bytes(), []byte("40 132")) {
				return
			}
		case <-deadline:
			t.Fatalf("timeout waiting for 40 132 in output; got: %q", buf.String())
		}
	}
}

func TestLocalBackend_Close_EmitsKilledExit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)

	require.NoError(t, h.Close())

	select {
	case info := <-h.Exit():
		require.Equal(t, "killed", info.Reason)
	case <-time.After(2 * time.Second):
		t.Fatal("did not receive exit info within 2s")
	}
}

func TestLocalBackend_NaturalExit_EmitsNatural(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	be := local.NewBackend()
	h, err := be.Open(ctx, pty.Spec{Cwd: os.TempDir(), Shell: "/bin/sh", Cols: 80, Rows: 24})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close() })

	_, err = h.Write([]byte("exit 0\n"))
	require.NoError(t, err)

	select {
	case info := <-h.Exit():
		require.Equal(t, "natural", info.Reason)
		require.Equal(t, 0, info.Code)
	case <-time.After(3 * time.Second):
		t.Fatal("did not receive natural exit within 3s")
	}
}
