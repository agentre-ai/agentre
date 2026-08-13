//go:build !windows

package agentredipc

import (
	"context"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGivenUnixDataDirectoryWhenServingLocalHTTPThenSocketModeAndRoundTripStayCompatible(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "agentred-ipc-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	listener, err := Listen(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = listener.Close()
		_ = Cleanup(dataDir)
	})

	info, err := os.Stat(UnixSocketPath(dataDir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"ok":true}`))
		}),
		ReadHeaderTimeout: time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Shutdown(context.Background()) })

	client := &http.Client{Transport: &http.Transport{DialContext: DialContext(dataDir)}}
	response, err := client.Get("http://daemon/local/status")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	assert.JSONEq(t, `{"ok":true}`, string(body))
}
