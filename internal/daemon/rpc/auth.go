package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"time"

	"agentre/internal/daemon/pairing"
	"agentre/internal/daemon/state"
)

// PairParams is the payload of an auth.pair request (Mode A).
type PairParams struct {
	Code              string `json:"code"`
	DeviceName        string `json:"deviceName"`
	DeviceFingerprint string `json:"deviceFingerprint"`
}

// PairResult is returned to the client after a successful pair, providing
// the deviceToken (used in subsequent connects) and the daemonFingerprint
// for TOFU pinning.
type PairResult struct {
	DeviceToken       string `json:"deviceToken"`
	DaemonFingerprint string `json:"daemonFingerprint"`
	InstanceUUID      string `json:"instanceUUID"`
}

// ConnectParams is the payload of an auth.connect request (Mode B).
type ConnectParams struct {
	DeviceFingerprint         string `json:"deviceFingerprint"`
	DeviceToken               string `json:"deviceToken"`
	ExpectedDaemonFingerprint string `json:"expectedDaemonFingerprint"`
}

// ConnectResult is returned after a successful Mode B handshake.
type ConnectResult struct {
	OK           bool   `json:"ok"`
	InstanceUUID string `json:"instanceUUID"`
}

// AuthHandlers owns the pre-authentication gate. The daemon wires these
// into the registry under method names "auth.pair" / "auth.connect" /
// "auth.revoke".
type AuthHandlers struct {
	st      *state.State
	pairing *pairing.Manager
	rl      *pairing.RateLimiter
}

// NewAuthHandlers constructs an AuthHandlers wired to the given state,
// pairing manager, and rate limiter.
func NewAuthHandlers(st *state.State, pm *pairing.Manager, rl *pairing.RateLimiter) *AuthHandlers {
	return &AuthHandlers{st: st, pairing: pm, rl: rl}
}

// DaemonFingerprint returns the canonical TOFU identifier "sha256:<hex>"
// derived from the daemon's instance UUID.
func DaemonFingerprint(uuid string) string {
	h := sha256.Sum256([]byte(uuid))
	return "sha256:" + hex.EncodeToString(h[:])
}

// HandlePair implements Mode A. The ip arg is the source remote address
// used by the per-IP rate limiter.
func (a *AuthHandlers) HandlePair(ctx context.Context, ip string, p PairParams) (*PairResult, error) {
	if !a.rl.Allow(ip) {
		return nil, &Error{Code: ErrPairing.Code, Message: "Pairing rate-limited"}
	}
	if !a.pairing.Consume(p.Code) {
		return nil, ErrPairing
	}
	tok, err := pairing.NewDeviceToken()
	if err != nil {
		return nil, &Error{Code: ErrInternal.Code, Message: err.Error()}
	}
	now := time.Now().UnixMilli()
	a.st.Mutate(func(s *state.State) {
		s.PairedPeers[p.DeviceFingerprint] = state.PairedPeer{
			DeviceName:  p.DeviceName,
			DeviceToken: tok,
			PairedAt:    now,
			LastSeenAt:  now,
		}
	})
	if err := a.st.Save(); err != nil {
		return nil, &Error{Code: ErrInternal.Code, Message: err.Error()}
	}
	return &PairResult{
		DeviceToken:       tok,
		DaemonFingerprint: DaemonFingerprint(a.st.DaemonInstanceUUID),
		InstanceUUID:      a.st.DaemonInstanceUUID,
	}, nil
}

// HandleConnect implements Mode B. It verifies the presented deviceToken
// (constant-time) and, when supplied, the TOFU daemonFingerprint pin.
func (a *AuthHandlers) HandleConnect(ctx context.Context, p ConnectParams) (*ConnectResult, error) {
	peer, ok := a.st.PairedPeers[p.DeviceFingerprint]
	if !ok {
		return nil, ErrUnauthorized
	}
	if !pairing.VerifyDeviceToken(peer.DeviceToken, p.DeviceToken) {
		return nil, ErrUnauthorized
	}
	want := DaemonFingerprint(a.st.DaemonInstanceUUID)
	if p.ExpectedDaemonFingerprint != "" && p.ExpectedDaemonFingerprint != want {
		return nil, &Error{Code: ErrUnauthorized.Code,
			Message: "daemon fingerprint mismatch (TOFU)"}
	}
	a.st.Mutate(func(s *state.State) {
		p2 := s.PairedPeers[p.DeviceFingerprint]
		p2.LastSeenAt = time.Now().UnixMilli()
		s.PairedPeers[p.DeviceFingerprint] = p2
	})
	_ = a.st.Save()
	return &ConnectResult{OK: true, InstanceUUID: a.st.DaemonInstanceUUID}, nil
}

// HandleRevoke removes a paired peer from state. Used by future "remove
// device" UI on the desktop side.
func (a *AuthHandlers) HandleRevoke(ctx context.Context, fingerprint string) error {
	a.st.Mutate(func(s *state.State) {
		delete(s.PairedPeers, fingerprint)
	})
	return a.st.Save()
}

// guard rejects any handler call from an unauthenticated connection.
// Daemon wraps registry handlers in a guard for everything except auth.*
// methods.
func guard(c *Conn) error {
	if !c.Auth().Authenticated {
		return ErrUnauthorized
	}
	return nil
}
