package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/state"
)

func TestLoginCompletesDeviceFlowAndPersistsOpaqueAccountClaim(t *testing.T) {
	dir := t.TempDir()
	st, err := state.Load(dir)
	require.NoError(t, err)

	accessToken := unsignedJWT(t, map[string]any{"uid": 42})
	var polls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/oauth/device/authorize":
			assert.Equal(t, http.MethodPost, r.Method)
			var body map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			assert.Equal(t, "agentred", body["device_kind"])
			assert.Equal(t, st.InstanceUUID(), body["fingerprint"])
			assert.Equal(t, "linux", body["platform"])
			assert.Equal(t, "dev", body["version"])
			assert.Equal(t, true, body["capabilities"].(map[string]any)["compute"])
			_, _ = io.WriteString(w, `{"device_code":"code-1","user_code":"ABCD-EFGH","verification_uri":"https://verify.example/device","verification_uri_complete":"https://verify.example/device?user_code=ABCD-EFGH","interval":1,"expires_in":60}`)
		case "/v1/oauth/device/token":
			polls++
			if polls == 1 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","token_type":"Bearer","expires_in":3600,"refresh_token":"refresh-token","refresh_expires_in":7200,"device_id":9}`)
		case "/v1/keys":
			assert.Equal(t, http.MethodGet, r.Method)
			assert.Empty(t, r.Header.Get("Authorization"), "public key distribution is unauthenticated")
			_, _ = io.WriteString(w, `{"public_key":"-----BEGIN PUBLIC KEY-----\ncached-key"}`)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	var openedURL string
	cmd := newLoginCmdWithDeps(loginDeps{
		dataDir: func() (string, error) { return dir, nil },
		http:    server.Client(),
		openBrowser: func(url string) error {
			openedURL = url
			return nil
		},
		wait:     func(_ time.Duration) error { return nil },
		platform: "linux",
		version:  "dev",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--server", server.URL})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "https://verify.example/device?user_code=ABCD-EFGH", openedURL)
	assert.Contains(t, out.String(), "ABCD-EFGH")
	assert.Contains(t, out.String(), "https://verify.example/device?user_code=ABCD-EFGH")
	assert.Contains(t, out.String(), "Logged in")
	assert.Equal(t, 2, polls)

	got, err := state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "42", got.AccountID)
	assert.Equal(t, "-----BEGIN PUBLIC KEY-----\ncached-key", got.VerificationPublicKeyPEM)
	assert.Equal(t, int64(9), got.Credential.DeviceID)
	assert.Equal(t, accessToken, got.Credential.AccessToken)
	assert.Equal(t, "refresh-token", got.Credential.RefreshToken)
	assert.NotZero(t, got.Credential.AccessTokenExpiresAt)
	assert.NotZero(t, got.Credential.RefreshTokenExpiresAt)
}

func TestLoginRejectsAlreadyClaimedDaemonWithoutNetwork(t *testing.T) {
	dir := t.TempDir()
	st, err := state.Load(dir)
	require.NoError(t, err)
	st.Mutate(func(s *state.State) { s.AccountID = "account-1" })
	require.NoError(t, st.Save())

	var networkCalls int
	cmd := newLoginCmdWithDeps(loginDeps{
		dataDir: func() (string, error) { return dir, nil },
		http: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			networkCalls++
			return nil, assert.AnError
		})},
		openBrowser: func(string) error { return nil },
		wait:        func(time.Duration) error { return nil },
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs(nil)
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "already claimed")
	assert.Equal(t, 0, networkCalls)
}

func unsignedJWT(t *testing.T, claims map[string]any) string {
	t.Helper()
	payload, err := json.Marshal(claims)
	require.NoError(t, err)
	return strings.Join([]string{
		base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)),
		base64.RawURLEncoding.EncodeToString(payload),
		"signature",
	}, ".")
}
