// Package composition is the dedicated E2E composition root. Production
// entrypoints must never import it.
package composition

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/agentre-ai/agentre/e2e/fakes"
	"github.com/agentre-ai/agentre/e2e/preflight"
)

const (
	syncServerURLEnv    = "AGENTRE_E2E_SERVER_URL"
	syncServerUserIDEnv = "AGENTRE_E2E_SERVER_USER_ID"
	syncDeviceIDEnv     = "AGENTRE_E2E_DEVICE_ID"
	syncDeviceFPEnv     = "AGENTRE_E2E_DEVICE_FINGERPRINT"
	syncRefreshTokenEnv = "AGENTRE_E2E_REFRESH_TOKEN" //nolint:gosec // G101: environment-variable name, not a credential
)

// LoggedInIdentity is runner-generated and accepted only by the dedicated E2E
// composition after storage preflight succeeds.
type LoggedInIdentity struct {
	ServerURL         string
	ServerUserID      int64
	DeviceID          int64
	DeviceFingerprint string
	RefreshToken      string
}

// Config extends validated storage with an isolated fake login identity.
type Config struct {
	preflight.Config
	Identity LoggedInIdentity
}

// FromPreflight reads the runner-owned fake identity after path/token preflight.
func FromPreflight(config preflight.Config) (Config, error) {
	userID, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(syncServerUserIDEnv)), 10, 64)
	if err != nil || userID <= 0 {
		return Config{}, fmt.Errorf("e2e composition requires a valid fake server user id")
	}
	deviceID, err := strconv.ParseInt(strings.TrimSpace(os.Getenv(syncDeviceIDEnv)), 10, 64)
	if err != nil || deviceID <= 0 {
		return Config{}, fmt.Errorf("e2e composition requires a valid fake device id")
	}
	identity := LoggedInIdentity{
		ServerURL:         strings.TrimSpace(os.Getenv(syncServerURLEnv)),
		ServerUserID:      userID,
		DeviceID:          deviceID,
		DeviceFingerprint: strings.TrimSpace(os.Getenv(syncDeviceFPEnv)),
		RefreshToken:      strings.TrimSpace(os.Getenv(syncRefreshTokenEnv)),
	}
	if !strings.HasPrefix(identity.ServerURL, "http://127.0.0.1:") ||
		identity.DeviceFingerprint == "" || identity.RefreshToken == "" {
		return Config{}, fmt.Errorf("e2e composition requires a loopback fake sync identity")
	}
	return Config{Config: config, Identity: identity}, nil
}

// Install replaces only external/runtime boundaries and seeds deterministic
// E2E state after production bootstrap has completed.
func Install(ctx context.Context, validated preflight.Config) error {
	config, err := FromPreflight(validated)
	if err != nil {
		return err
	}
	if config.RunRoot == "" || config.DataDir == "" || config.KeychainDir == "" {
		return fmt.Errorf("e2e composition requires validated preflight config")
	}
	identityEnv := map[string]string{
		syncServerURLEnv:    config.Identity.ServerURL,
		syncServerUserIDEnv: strconv.FormatInt(config.Identity.ServerUserID, 10),
		syncDeviceIDEnv:     strconv.FormatInt(config.Identity.DeviceID, 10),
		syncDeviceFPEnv:     config.Identity.DeviceFingerprint,
		syncRefreshTokenEnv: config.Identity.RefreshToken,
	}
	for name, value := range identityEnv {
		if err := os.Setenv(name, value); err != nil {
			return fmt.Errorf("set fake login identity: %w", err)
		}
	}
	return fakes.Install(ctx)
}
