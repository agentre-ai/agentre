package fakes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/bootstrap"
	"github.com/agentre-ai/agentre/internal/model/entity/server_state_entity"
	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo/mock_remote_device_repo"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo/mock_server_state_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	"github.com/agentre-ai/agentre/internal/service/server_svc"
	"github.com/agentre-ai/agentre/internal/service/sync_svc"
)

// TestGivenDualLoginWhenHarnessInstallsAccountThenRemoteDeviceUsesLoginFingerprint
// guards the real e2e installation boundary. The boot service initially captures a
// different keychain. The login fixture must rebind remote_device_svc after writing
// the run-scoped canonical identity, before Install creates the local backend.
func TestGivenDualLoginWhenHarnessInstallsAccountThenRemoteDeviceUsesLoginFingerprint(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	originalKeychain := keychain.Default()
	originalRemoteDevice := remote_device_svc.Default()
	originalServer := server_svc.Server()
	originalSync := sync_svc.Default()
	originalServerStateRepo := server_state_repo.ServerState()
	originalPairedRepo := remote_device_repo.PairedAgentred()
	t.Cleanup(func() {
		keychain.SetDefault(originalKeychain)
		remote_device_svc.SetDefault(originalRemoteDevice)
		server_svc.SetDefault(originalServer)
		sync_svc.SetDefault(originalSync)
		server_state_repo.RegisterServerState(originalServerStateRepo)
		remote_device_repo.RegisterPairedAgentred(originalPairedRepo)
	})

	bootKeychain := keychain.NewMemory()
	require.NoError(t, bootKeychain.Set(keychainAccountFingerprint, "sha256:boot"))
	keychain.SetDefault(bootKeychain)
	require.NoError(t, bootstrap.InitRemoteDevice(ctx))

	loginKeychain := keychain.NewMemory()
	keychain.SetDefault(loginKeychain)

	var refreshHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/oauth/token/refresh" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		refreshHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"data":{"access_token":"access","refresh_token":"rotated"}}`))
	}))
	defer server.Close()

	stateRepo := mock_server_state_repo.NewMockServerStateRepo(ctrl)
	stateRepo.EXPECT().Save(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, state *server_state_entity.ServerState) error {
			assert.Equal(t, "sha256:isolated", state.DeviceFingerprint)
			return nil
		},
	)
	server_state_repo.RegisterServerState(stateRepo)

	t.Setenv(e2eServerURLEnv, server.URL)
	t.Setenv(e2eServerUserIDEnv, "101")
	t.Setenv(e2eDeviceIDEnv, "202")
	t.Setenv(e2eDeviceFPEnv, "sha256:isolated")
	t.Setenv(e2eRefreshTokenEnv, "refresh")

	installE2ELoggedInAccount(ctx)

	assert.Equal(t, int32(1), refreshHits.Load())
	fingerprint, err := remote_device_svc.Default().DeviceFingerprint()
	require.NoError(t, err)
	assert.Equal(t, "sha256:isolated", fingerprint)
	assert.True(t, remote_device_svc.IsSelfDevice(fingerprint))
}

func TestGivenLoggedInIdentityWhenRemoteAgentredSeedFailsThenSelfIdentityIsNotRebound(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)

	originalKeychain := keychain.Default()
	originalRemoteDevice := remote_device_svc.Default()
	originalPairedRepo := remote_device_repo.PairedAgentred()
	t.Cleanup(func() {
		keychain.SetDefault(originalKeychain)
		remote_device_svc.SetDefault(originalRemoteDevice)
		remote_device_repo.RegisterPairedAgentred(originalPairedRepo)
	})

	loginKeychain := keychain.NewMemory()
	require.NoError(t, loginKeychain.Set(keychainAccountFingerprint, "sha256:login"))
	keychain.SetDefault(loginKeychain)
	require.NoError(t, bootstrap.InitRemoteDevice(ctx))

	// A later global keychain change must not matter: remote-agentred seeding owns
	// rows only and must not silently reconstruct the already-login-bound service.
	otherKeychain := keychain.NewMemory()
	require.NoError(t, otherKeychain.Set(keychainAccountFingerprint, "sha256:other"))
	keychain.SetDefault(otherKeychain)

	seedErr := errors.New("stop after identity boundary")
	pairedRepo := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	pairedRepo.EXPECT().FindByURL(gomock.Any(), "wss://agentred.example").Return(nil, seedErr)
	remote_device_repo.RegisterPairedAgentred(pairedRepo)
	t.Setenv(e2eAgentredFingerprintEnv, "sha256:agentred")
	t.Setenv(e2eAgentredURLEnv, "wss://agentred.example")

	installE2ERemoteAgentred(ctx)

	fingerprint, err := remote_device_svc.Default().DeviceFingerprint()
	require.NoError(t, err)
	assert.Equal(t, "sha256:login", fingerprint)
	assert.True(t, remote_device_svc.IsSelfDevice(fingerprint))
}
