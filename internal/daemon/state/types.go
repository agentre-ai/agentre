// Package state owns agentred's single persistent JSON file.
// It is the only place that writes state.json; all callers go through Load/Mutate.
package state

import "sync"

// State is the on-disk shape persisted to <AppDataDir>/state.json.
type State struct {
	SchemaVersion            int                        `json:"schemaVersion"`
	DaemonInstanceUUID       string                     `json:"daemonInstanceUUID"`
	HubServerURL             string                     `json:"hubServerURL,omitempty"`
	Listen                   ListenPrefs                `json:"listen"`
	PairedPeers              map[string]PairedPeer      `json:"pairedPeers"`
	LLMProviders             map[string]LLMProviderMeta `json:"llmProviders"`
	Preferences              Preferences                `json:"preferences"`
	AccountID                string                     `json:"accountId,omitempty"`
	VerificationPublicKeyPEM string                     `json:"verificationPublicKeyPEM,omitempty"`
	VerificationCurrentKID   string                     `json:"verificationCurrentKID,omitempty"`
	VerificationPublicKeys   map[string]string          `json:"verificationPublicKeys,omitempty"`
	MaxTokenLifetimeSeconds  int64                      `json:"maxTokenLifetimeSeconds,omitempty"`
	Credential               AccountCredential          `json:"credential,omitempty"`

	// RevokedJTIs is the account's revoked access-token jti list as last pulled
	// from the account server, and RevocationsAsOf is when the server generated
	// it (unix ms). They are persisted because the check must keep working while
	// the daemon is offline and across restarts: revocation takes effect locally
	// from this cached list alone, never from a lookup at handshake time (R3/R4).
	RevokedJTIs     []string `json:"revokedJTIs,omitempty"`
	RevocationsAsOf int64    `json:"revocationsAsOf,omitempty"`

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

// AccountCredential is the daemon's refreshable account credential. It
// deliberately contains no user profile data: AccountID is the only account
// identity retained by agentred.
type AccountCredential struct {
	DeviceID              int64  `json:"deviceId,omitempty"`
	AccessToken           string `json:"accessToken,omitempty"`
	AccessTokenExpiresAt  int64  `json:"accessTokenExpiresAt,omitempty"`
	RefreshToken          string `json:"refreshToken,omitempty"`
	RefreshTokenExpiresAt int64  `json:"refreshTokenExpiresAt,omitempty"`
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
