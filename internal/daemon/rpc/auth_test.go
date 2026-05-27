package rpc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"agentre/internal/daemon/pairing"
	"agentre/internal/daemon/state"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAuthTest(t *testing.T) (*AuthHandlers, *state.State, *pairing.Manager) {
	t.Helper()
	dir := t.TempDir()
	st, err := state.Load(dir)
	require.NoError(t, err)
	pm := pairing.NewManager(pairing.ManagerOpts{TTL: time.Minute})
	rl := pairing.NewRateLimiter(pairing.RateLimitOpts{MaxAttempts: 3, Window: time.Minute})
	return NewAuthHandlers(st, pm, rl), st, pm
}

func TestAuth_PairThenConnect(t *testing.T) {
	ah, st, pm := setupAuthTest(t)
	code, err := pm.Generate()
	require.NoError(t, err)

	pairResp, err := ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
		Code:              code,
		DeviceName:        "mac-pro-m4",
		DeviceFingerprint: "sha256:test-fp",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, pairResp.DeviceToken)
	expectedDaemonFp := "sha256:" + hex.EncodeToString(testSha256Sum(st.DaemonInstanceUUID))
	assert.Equal(t, expectedDaemonFp, pairResp.DaemonFingerprint)

	peer, ok := st.PairedPeers["sha256:test-fp"]
	require.True(t, ok)
	assert.Equal(t, pairResp.DeviceToken, peer.DeviceToken)

	_, err = ah.HandleConnect(context.Background(), ConnectParams{
		DeviceFingerprint:         "sha256:test-fp",
		DeviceToken:               pairResp.DeviceToken,
		ExpectedDaemonFingerprint: expectedDaemonFp,
	})
	assert.NoError(t, err)
}

func TestAuth_BadCode(t *testing.T) {
	ah, _, _ := setupAuthTest(t)
	_, err := ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
		Code: "ZZZZZZ", DeviceName: "x", DeviceFingerprint: "sha256:y",
	})
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32004, rpcErr.Code)
}

func TestAuth_RateLimitTriggers(t *testing.T) {
	ah, _, _ := setupAuthTest(t)
	for i := 0; i < 3; i++ {
		_, _ = ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
			Code: "WRONG", DeviceName: "x", DeviceFingerprint: "sha256:y",
		})
	}
	_, err := ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
		Code: "WHATEVER", DeviceName: "x", DeviceFingerprint: "sha256:y",
	})
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32004, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "rate")
}

func TestAuth_ConnectFailsWithWrongToken(t *testing.T) {
	ah, _, pm := setupAuthTest(t)
	code, _ := pm.Generate()
	_, _ = ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
		Code: code, DeviceName: "x", DeviceFingerprint: "sha256:f",
	})
	_, err := ah.HandleConnect(context.Background(), ConnectParams{
		DeviceFingerprint: "sha256:f", DeviceToken: "nope",
	})
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32001, rpcErr.Code)
}

func TestAuth_TOFUFingerprintMismatch(t *testing.T) {
	ah, st, pm := setupAuthTest(t)
	code, _ := pm.Generate()
	resp, _ := ah.HandlePair(context.Background(), "1.2.3.4", PairParams{
		Code: code, DeviceName: "x", DeviceFingerprint: "sha256:f",
	})
	_ = st
	_, err := ah.HandleConnect(context.Background(), ConnectParams{
		DeviceFingerprint:         "sha256:f",
		DeviceToken:               resp.DeviceToken,
		ExpectedDaemonFingerprint: "sha256:tampered",
	})
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32001, rpcErr.Code)
	assert.Contains(t, rpcErr.Message, "fingerprint")
}

func TestAuth_GuardRejectsUnauthenticated(t *testing.T) {
	c := &Conn{}
	err := guard(c)
	var rpcErr *Error
	require.True(t, errors.As(err, &rpcErr))
	assert.Equal(t, -32001, rpcErr.Code)

	c.SetAuth(AuthState{Authenticated: true})
	assert.NoError(t, guard(c))
}

// testSha256Sum is a tiny helper to mirror what auth.go computes for the
// daemonFingerprint, used only to construct expected values in tests.
func testSha256Sum(s string) []byte { h := sha256.Sum256([]byte(s)); return h[:] }
