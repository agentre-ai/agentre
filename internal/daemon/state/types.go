// Package state owns agentred's single persistent JSON file.
// It is the only place that writes state.json; all callers go through Load/Mutate.
package state

import "sync"

// State is the on-disk shape persisted to <AppDataDir>/state.json.
// Spec reference: docs/superpowers/specs/2026-05-21-agentred-mvp-design.md §5.2.
type State struct {
	SchemaVersion      int                        `json:"schemaVersion"`
	DaemonInstanceUUID string                     `json:"daemonInstanceUUID"`
	Listen             ListenPrefs                `json:"listen"`
	PairedPeers        map[string]PairedPeer      `json:"pairedPeers"`
	LLMProviders       map[string]LLMProviderMeta `json:"llmProviders"`
	Preferences        Preferences                `json:"preferences"`

	mu  *sync.RWMutex `json:"-"`
	dir string        `json:"-"`
}

type ListenPrefs struct {
	LanHost     string `json:"lanHost"`
	LanPort     int    `json:"lanPort"`
	TLSCertFile string `json:"tlsCertFile,omitempty"`
	TLSKeyFile  string `json:"tlsKeyFile,omitempty"`
}

type PairedPeer struct {
	DeviceName  string `json:"deviceName"`
	DeviceToken string `json:"deviceToken"`
	PairedAt    int64  `json:"pairedAt"`
	LastSeenAt  int64  `json:"lastSeenAt"`
}

type LLMProviderMeta struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	BaseURL     string            `json:"baseURL"`
	APIKey      string            `json:"apiKey"`
	Model       string            `json:"model"`
	ModelRoutes map[string]string `json:"modelRoutes"`
	UpdatedAt   int64             `json:"updatedAt"`
}

type Preferences struct {
	LogLevel              string         `json:"logLevel"`
	LogRotateMB           int            `json:"logRotateMB"`
	PairingCodeTTLSeconds int            `json:"pairingCodeTTLSeconds"`
	PairingRateLimit      RateLimitPrefs `json:"pairingRateLimit"`
}

type RateLimitPrefs struct {
	MaxAttemptsPerIP int `json:"maxAttemptsPerIP"`
	WindowSeconds    int `json:"windowSeconds"`
}

// NewDefault builds a State with safe defaults; caller provides a stable UUID.
func NewDefault(daemonUUID string) *State {
	return &State{
		SchemaVersion:      1,
		DaemonInstanceUUID: daemonUUID,
		Listen:             ListenPrefs{LanHost: "0.0.0.0", LanPort: 7456},
		PairedPeers:        map[string]PairedPeer{},
		LLMProviders:       map[string]LLMProviderMeta{},
		Preferences: Preferences{
			LogLevel:              "info",
			LogRotateMB:           50,
			PairingCodeTTLSeconds: 300,
			PairingRateLimit: RateLimitPrefs{
				MaxAttemptsPerIP: 3,
				WindowSeconds:    60,
			},
		},
		mu: &sync.RWMutex{},
	}
}
