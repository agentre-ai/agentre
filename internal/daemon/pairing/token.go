// Package pairing implements one-shot pairing-code generation, deviceToken
// minting, and per-IP rate-limiting for agentred's auth handshake.
// Spec reference: docs/superpowers/specs/2026-05-21-agentred-mvp-design.md §4.3.
package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
)

// NewDeviceToken returns a 256-bit base64url-encoded random token.
func NewDeviceToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

// VerifyDeviceToken is constant-time equality to dodge timing oracles.
func VerifyDeviceToken(stored, presented string) bool {
	if len(stored) == 0 || len(stored) != len(presented) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(stored), []byte(presented)) == 1
}
