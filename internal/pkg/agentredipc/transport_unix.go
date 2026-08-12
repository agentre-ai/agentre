//go:build !windows

package agentredipc

import (
	"context"
	"errors"
	"net"
	"os"
)

// Endpoint returns the unchanged Unix-domain socket path.
func Endpoint(dataDir string) string {
	return UnixSocketPath(dataDir)
}

// Listen creates the current-user-only Unix-domain socket used by agentred.
func Listen(dataDir string) (net.Listener, error) {
	path := Endpoint(dataDir)
	_ = os.Remove(path)
	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = listener.Close()
		return nil, err
	}
	return listener, nil
}

// DialContext returns an HTTP transport dialer for the local Unix socket.
func DialContext(dataDir string) func(context.Context, string, string) (net.Conn, error) {
	path := Endpoint(dataDir)
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, "unix", path)
	}
}

// Cleanup removes the socket after shutdown.
func Cleanup(dataDir string) error {
	err := os.Remove(Endpoint(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
