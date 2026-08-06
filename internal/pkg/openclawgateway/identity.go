package openclawgateway

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
)

type DeviceIdentity struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	deviceID   string
}

func GenerateDeviceIdentity() (*DeviceIdentity, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate openclaw device identity: %w", err)
	}
	return newDeviceIdentity(publicKey, privateKey), nil
}

func NewDeviceIdentityFromSeed(seed []byte) (*DeviceIdentity, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("openclaw device identity seed must contain %d bytes", ed25519.SeedSize)
	}
	privateKey := ed25519.NewKeyFromSeed(seed)
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("derive openclaw device public key")
	}
	return newDeviceIdentity(publicKey, privateKey), nil
}

func newDeviceIdentity(publicKey ed25519.PublicKey, privateKey ed25519.PrivateKey) *DeviceIdentity {
	digest := sha256.Sum256(publicKey)
	return &DeviceIdentity{
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		deviceID:   hex.EncodeToString(digest[:]),
	}
}

func (d *DeviceIdentity) ID() string {
	if d == nil {
		return ""
	}
	return d.deviceID
}

// Seed returns a defensive copy suitable for persistence in the platform
// keychain. Callers must never log or serialize the result.
func (d *DeviceIdentity) Seed() []byte {
	if d == nil || len(d.privateKey) != ed25519.PrivateKeySize {
		return nil
	}
	return append([]byte(nil), d.privateKey.Seed()...)
}

type deviceProof struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce"`
}

func (d *DeviceIdentity) proof(
	clientID, clientMode, role string,
	scopes []string,
	signedAt int64,
	token, nonce, platform, deviceFamily string,
) (deviceProof, error) {
	if d == nil || len(d.privateKey) != ed25519.PrivateKeySize {
		return deviceProof{}, fmt.Errorf("openclaw device identity is required")
	}
	payload := BuildDeviceAuthPayload(
		d.deviceID, clientID, clientMode, role, scopes, signedAt,
		token, nonce, platform, deviceFamily,
	)
	signature := ed25519.Sign(d.privateKey, []byte(payload))
	return deviceProof{
		ID:        d.deviceID,
		PublicKey: base64.RawURLEncoding.EncodeToString(d.publicKey),
		Signature: base64.RawURLEncoding.EncodeToString(signature),
		SignedAt:  signedAt,
		Nonce:     nonce,
	}, nil
}

// BuildDeviceAuthPayload implements the OpenClaw v3 challenge signature
// contract. Field order and comma-joined scope order are protocol-significant.
func BuildDeviceAuthPayload(
	deviceID, clientID, clientMode, role string,
	scopes []string,
	signedAt int64,
	token, nonce, platform, deviceFamily string,
) string {
	return strings.Join([]string{
		"v3",
		deviceID,
		clientID,
		clientMode,
		role,
		strings.Join(scopes, ","),
		strconv.FormatInt(signedAt, 10),
		token,
		nonce,
		strings.ToLower(strings.TrimSpace(platform)),
		strings.ToLower(strings.TrimSpace(deviceFamily)),
	}, "|")
}
