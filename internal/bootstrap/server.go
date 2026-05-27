package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/cago-frame/cago/pkg/gogo"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/server_state_entity"
	"agentre/internal/pkg/keychain"
	"agentre/internal/repository/server_state_repo"
	"agentre/internal/service/server_svc"
)

// InitServer wires the desktop's keychain + server_state_repo + server_svc defaults.
// The server_svc starts WITHOUT a wails event emitter; app.go.startup binds it
// later via server_svc.Server().SetEmitter(...).
//
// Reads the persisted server_url to point the http client at the right host;
// when no row exists yet, the client base URL is empty and StartLogin will
// rebuild it from the user-supplied URL anyway.
func InitServer(ctx context.Context) error {
	keychain.SetDefault(keychain.NewSystem())
	server_state_repo.RegisterServerState(server_state_repo.NewServerState())

	row, _ := server_state_repo.ServerState().Get(ctx)
	baseURL := ""
	if row != nil {
		baseURL = row.ServerURL
	}
	svc := server_svc.New(server_svc.NewHTTPClient(baseURL, ""), nil)
	server_svc.SetDefault(svc)
	return nil
}

// ServerBoot runs the per-startup server-side warm-up: ensures a device fingerprint
// exists, refreshes the access token if logged in, emits the current state.
//
// Should be called from app.go.startup, after SetEmitter is bound. Runs in a
// goroutine (via gogo.Go) so it doesn't block UI; logs all errors.
func ServerBoot(ctx context.Context) {
	gogo.Go(func() error {
		row, err := server_state_repo.ServerState().Get(ctx)
		if err != nil {
			logger.Default().Warn("server boot: load state", zap.Error(err))
			return nil
		}
		if row == nil {
			// Fresh install — persist a fingerprint so future logins reuse it.
			fp := newBootFingerprint()
			if err := server_state_repo.ServerState().Save(ctx, &server_state_entity.ServerState{ID: 1, DeviceFingerprint: fp}); err != nil {
				logger.Default().Warn("server boot: persist fingerprint", zap.Error(err))
			}
			return nil
		}
		if row.DeviceFingerprint == "" {
			row.DeviceFingerprint = newBootFingerprint()
			if err := server_state_repo.ServerState().Save(ctx, row); err != nil {
				logger.Default().Warn("server boot: persist fingerprint", zap.Error(err))
			}
		}

		// Inconsistent-state guard: any of (user_id, device_id, keychain_account)
		// being zero when others are non-zero means a previous logout was interrupted.
		// Clear and re-emit logged_out so the UI doesn't show stale identity.
		userBound := row.ServerUserID != 0
		deviceBound := row.DeviceID != 0
		keychainBound := row.KeychainAccount != ""
		if userBound != deviceBound || userBound != keychainBound {
			_ = server_svc.Server().ClearLogin(ctx) // also emits logged_out{reason:"refresh_expired"}
			return nil
		}

		if !row.IsLoggedIn() {
			return nil
		}

		// Best-effort refresh on boot — keeps access token fresh after long sleep.
		// If the refresh token itself has expired, scrub local state so the UI does
		// not stay on the Connected panel until the user triggers another call.
		if err := server_svc.Server().Refresh(ctx); err != nil {
			logger.Default().Warn("server boot: refresh failed; clearing local login", zap.Error(err))
			_ = server_svc.Server().ClearLogin(ctx)
			return nil
		}
		return nil
	})
}

func newBootFingerprint() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
