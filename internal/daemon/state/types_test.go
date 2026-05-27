package state

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestState_JSONRoundTrip(t *testing.T) {
	in := &State{
		SchemaVersion:      1,
		DaemonInstanceUUID: "8f3a9c2b",
		Listen: ListenPrefs{
			LanHost:     "0.0.0.0",
			LanPort:     7456,
			TLSCertFile: "/etc/ssl/cert.pem",
			TLSKeyFile:  "/etc/ssl/key.pem",
		},
		PairedPeers: map[string]PairedPeer{
			"sha256:abc": {
				DeviceName:  "mac-pro-m4",
				DeviceToken: "tok123",
				PairedAt:    1716000000,
				LastSeenAt:  1716001000,
			},
		},
		LLMProviders: map[string]LLMProviderMeta{
			"42": { //nolint:gosec // credential-shaped API key is a test fixture.
				Name: "anthropic-main", Type: "anthropic",
				BaseURL:     "https://api.anthropic.com",
				APIKey:      "sk-ant-real-key",
				Model:       "claude-sonnet-4-6",
				ModelRoutes: map[string]string{"OPUS": "claude-opus-4"},
				UpdatedAt:   1716000500,
			},
		},
		Preferences: Preferences{
			LogLevel:              "info",
			LogRotateMB:           50,
			PairingCodeTTLSeconds: 300,
			PairingRateLimit: RateLimitPrefs{
				MaxAttemptsPerIP: 3,
				WindowSeconds:    60,
			},
		},
	}

	b, err := json.Marshal(in)
	require.NoError(t, err)

	var out State
	require.NoError(t, json.Unmarshal(b, &out))
	assert.Equal(t, in, &out)
}

func TestState_DefaultsAreSane(t *testing.T) {
	s := NewDefault("uuid-x")
	assert.Equal(t, 1, s.SchemaVersion)
	assert.Equal(t, "uuid-x", s.DaemonInstanceUUID)
	assert.Equal(t, "0.0.0.0", s.Listen.LanHost)
	assert.Equal(t, 7456, s.Listen.LanPort)
	assert.Equal(t, "info", s.Preferences.LogLevel)
	assert.Equal(t, 300, s.Preferences.PairingCodeTTLSeconds)
	assert.Equal(t, 3, s.Preferences.PairingRateLimit.MaxAttemptsPerIP)
	assert.NotNil(t, s.PairedPeers)
	assert.NotNil(t, s.LLMProviders)
}
