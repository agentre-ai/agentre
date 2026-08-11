//go:build windows

package agentredipc

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestGivenCurrentWindowsIdentityWhenCreatingPipeACLThenDescriptorNamesThatSIDOnly(t *testing.T) {
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	require.NoError(t, err)

	descriptor, err := currentUserSecurityDescriptor()
	require.NoError(t, err)
	assert.Equal(t, securityDescriptorForSID(user.User.Sid.String()), descriptor)
}

func TestGivenWindowsDataDirectoryWhenServingLocalHTTPThenNamedPipeRoundTrips(t *testing.T) {
	dataDir := t.TempDir()
	listener, err := Listen(dataDir)
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

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
