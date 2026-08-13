package composition

import (
	"testing"

	"github.com/agentre-ai/agentre/e2e/preflight"
)

func TestFromPreflightGivenRunnerIdentityReturnsLoggedInLoopbackConfig(t *testing.T) {
	t.Setenv(syncServerURLEnv, "http://127.0.0.1:43210")
	t.Setenv(syncServerUserIDEnv, "7001")
	t.Setenv(syncDeviceIDEnv, "7101")
	t.Setenv(syncDeviceFPEnv, "sha256:test-device")
	t.Setenv(syncRefreshTokenEnv, "test-refresh-token")
	validated := preflight.Config{RunRoot: "/run", DataDir: "/run/data", KeychainDir: "/run/keychain"}

	got, err := FromPreflight(validated)
	if err != nil {
		t.Fatalf("FromPreflight: %v", err)
	}
	if got.Config != validated {
		t.Fatalf("validated config = %+v, want %+v", got.Config, validated)
	}
	if got.Identity.ServerURL != "http://127.0.0.1:43210" || got.Identity.ServerUserID != 7001 ||
		got.Identity.DeviceID != 7101 || got.Identity.DeviceFingerprint != "sha256:test-device" ||
		got.Identity.RefreshToken != "test-refresh-token" {
		t.Fatalf("identity = %+v", got.Identity)
	}
}

func TestFromPreflightGivenNonLoopbackOrMissingIdentityRejectsComposition(t *testing.T) {
	validated := preflight.Config{RunRoot: "/run", DataDir: "/run/data", KeychainDir: "/run/keychain"}
	for name, serverURL := range map[string]string{
		"missing":      "",
		"non-loopback": "https://example.com",
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(syncServerURLEnv, serverURL)
			t.Setenv(syncServerUserIDEnv, "7001")
			t.Setenv(syncDeviceIDEnv, "7101")
			t.Setenv(syncDeviceFPEnv, "sha256:test-device")
			t.Setenv(syncRefreshTokenEnv, "test-refresh-token")

			if _, err := FromPreflight(validated); err == nil {
				t.Fatal("FromPreflight error = nil, want unsafe identity rejection")
			}
		})
	}
}
