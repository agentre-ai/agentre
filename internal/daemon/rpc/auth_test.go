package rpc

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/pairing"
	"github.com/agentre-ai/agentre/internal/daemon/state"

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

func TestAuth_AccountCredential_GivenClaimedDaemon_WhenValidCredential_ThenAuthenticates(t *testing.T) {
	ah, st, _ := setupAuthTest(t)
	privateKey, publicKeyPEM := testRSAKeyPair(t)
	st.Claim("42", publicKeyPEM, state.AccountCredential{})
	credential := testAccountCredential(t, privateKey, int64(42), time.Now().Add(time.Hour))

	result, err := ah.HandleAccount(context.Background(), AccountParams{
		Credential:        credential,
		DeviceFingerprint: "sha256:account-client",
	})

	require.NoError(t, err)
	assert.Equal(t, &ConnectResult{OK: true, InstanceUUID: st.DaemonInstanceUUID}, result)
}

func TestAuth_AccountCredential_GivenCredentialWithinClockSkew_WhenAuthenticating_ThenAuthenticates(t *testing.T) {
	ah, st, _ := setupAuthTest(t)
	privateKey, publicKeyPEM := testRSAKeyPair(t)
	st.Claim("42", publicKeyPEM, state.AccountCredential{})
	credential := testAccountCredential(t, privateKey, int64(42), time.Now().Add(-30*time.Second))

	result, err := ah.HandleAccount(context.Background(), AccountParams{Credential: credential, DeviceFingerprint: "sha256:account-client"})

	require.NoError(t, err)
	assert.Equal(t, &ConnectResult{OK: true, InstanceUUID: st.DaemonInstanceUUID}, result)
}

func TestAuth_AccountCredential_GivenExpiredCredential_WhenAuthenticating_ThenRejectsExpiry(t *testing.T) {
	ah, st, _ := setupAuthTest(t)
	privateKey, publicKeyPEM := testRSAKeyPair(t)
	st.Claim("42", publicKeyPEM, state.AccountCredential{})
	credential := testAccountCredential(t, privateKey, int64(42), time.Now().Add(-61*time.Second))

	_, err := ah.HandleAccount(context.Background(), AccountParams{Credential: credential, DeviceFingerprint: "sha256:account-client"})

	assertAccountCredentialRejection(t, err, "account credential expired")
}

func TestAuth_AccountCredential_GivenWrongSignature_WhenAuthenticating_ThenRejectsSignature(t *testing.T) {
	ah, st, _ := setupAuthTest(t)
	_, publicKeyPEM := testRSAKeyPair(t)
	wrongPrivateKey, _ := testRSAKeyPair(t)
	st.Claim("42", publicKeyPEM, state.AccountCredential{})
	credential := testAccountCredential(t, wrongPrivateKey, int64(42), time.Now().Add(time.Hour))

	_, err := ah.HandleAccount(context.Background(), AccountParams{Credential: credential, DeviceFingerprint: "sha256:account-client"})

	assertAccountCredentialRejection(t, err, "account credential signature invalid")
}

func TestAuth_AccountCredential_GivenDifferentAccount_WhenAuthenticating_ThenRejectsMismatch(t *testing.T) {
	ah, st, _ := setupAuthTest(t)
	privateKey, publicKeyPEM := testRSAKeyPair(t)
	st.Claim("42", publicKeyPEM, state.AccountCredential{})
	credential := testAccountCredential(t, privateKey, int64(99), time.Now().Add(time.Hour))

	_, err := ah.HandleAccount(context.Background(), AccountParams{Credential: credential, DeviceFingerprint: "sha256:account-client"})

	assertAccountCredentialRejection(t, err, "account credential account mismatch")
}

func TestAuth_AccountCredential_GivenUnclaimedDaemon_WhenAuthenticating_ThenRejectsMissingCachedKey(t *testing.T) {
	ah, _, _ := setupAuthTest(t)
	privateKey, _ := testRSAKeyPair(t)
	credential := testAccountCredential(t, privateKey, int64(42), time.Now().Add(time.Hour))

	_, err := ah.HandleAccount(context.Background(), AccountParams{Credential: credential, DeviceFingerprint: "sha256:account-client"})

	assertAccountCredentialRejection(t, err, "account credential rejected: cached verification key unavailable")
}

func assertAccountCredentialRejection(t *testing.T, err error, reason string) {
	t.Helper()
	var rpcErr *Error
	require.ErrorAs(t, err, &rpcErr)
	assert.Equal(t, ErrUnauthorized.Code, rpcErr.Code)
	assert.Equal(t, reason, rpcErr.Message)
}

func testRSAKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	der, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	require.NoError(t, err)
	return privateKey, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

func testAccountCredential(t *testing.T, privateKey *rsa.PrivateKey, accountID any, expiresAt time.Time) string {
	t.Helper()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, err := json.Marshal(map[string]any{"uid": accountID, "exp": expiresAt.Unix()})
	require.NoError(t, err)
	payload := base64.RawURLEncoding.EncodeToString(claims)
	signingInput := header + "." + payload
	digest := sha256.Sum256([]byte(signingInput))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	require.NoError(t, err)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature)
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
