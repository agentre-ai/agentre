package rpc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLAN_ServerAcceptsWS(t *testing.T) {
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, p json.RawMessage) (any, error) { return "pong", nil })

	srv := NewLANServer(LANOpts{
		Host:     "127.0.0.1",
		Port:     0, // ephemeral
		Registry: reg,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()
	require.Eventually(t, func() bool { return srv.Addr() != "" }, time.Second, 10*time.Millisecond)

	u := url.URL{Scheme: "ws", Host: srv.Addr(), Path: "/rpc"}
	c, hsResp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.NoError(t, err)
	if hsResp != nil {
		_ = hsResp.Body.Close()
	}
	defer func() { _ = c.Close() }()

	require.NoError(t, c.WriteJSON(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "ping",
	}))
	var resp Frame
	require.NoError(t, c.ReadJSON(&resp))
	assert.Equal(t, `"pong"`, string(resp.Result))
}

// TestLAN_ServeCtxNotCanceledAfterUpgrade pins the fix for
// transport_lan.go:74 — Serve must NOT inherit r.Context(), since
// net/http cancels the request ctx as soon as the HTTP handler
// returns (which happens immediately after upgrader.Upgrade
// hijacks the connection). If that ctx leaked into Serve, every
// subsequent RPC handler would see context.Canceled on entry,
// breaking chat.start in the first message of a new session.
func TestLAN_ServeCtxNotCanceledAfterUpgrade(t *testing.T) {
	reg := NewRegistry()
	ctxSeen := make(chan error, 1)
	reg.Register("probe", func(ctx context.Context, _ json.RawMessage) (any, error) {
		ctxSeen <- ctx.Err()
		return "ok", nil
	})
	srv := NewLANServer(LANOpts{Host: "127.0.0.1", Port: 0, Registry: reg})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()
	require.Eventually(t, func() bool { return srv.Addr() != "" }, time.Second, 10*time.Millisecond)

	u := url.URL{Scheme: "ws", Host: srv.Addr(), Path: "/rpc"}
	c, hsResp, err := websocket.DefaultDialer.Dial(u.String(), nil)
	require.NoError(t, err)
	if hsResp != nil {
		_ = hsResp.Body.Close()
	}
	defer func() { _ = c.Close() }()

	// Give net/http a beat to drop the request ctx — the bug
	// surfaces because Upgrade hijacks the conn and the HTTP
	// handler returns, after which the request ctx is canceled
	// even though the WS read loop keeps running.
	time.Sleep(100 * time.Millisecond)

	require.NoError(t, c.WriteJSON(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "probe",
	}))
	var resp Frame
	require.NoError(t, c.ReadJSON(&resp))

	select {
	case got := <-ctxSeen:
		assert.NoError(t, got, "Serve ctx must outlive the HTTP request that upgraded it")
	case <-time.After(time.Second):
		t.Fatal("probe handler never fired")
	}
}

func TestLAN_TLSMisconfigFails(t *testing.T) {
	srv := NewLANServer(LANOpts{
		Host:        "127.0.0.1",
		Port:        0,
		TLSCertFile: "/no/such/cert.pem",
		// TLSKeyFile intentionally missing -> mismatched pair
		Registry: NewRegistry(),
	})
	err := srv.Run(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tls")
}

func TestLAN_WithTempTLS(t *testing.T) {
	certFile, keyFile := writeSelfSignedPair(t, "127.0.0.1")
	reg := NewRegistry()
	reg.Register("ping", func(ctx context.Context, p json.RawMessage) (any, error) { return "pong", nil })

	srv := NewLANServer(LANOpts{
		Host:        "127.0.0.1",
		Port:        0,
		TLSCertFile: certFile,
		TLSKeyFile:  keyFile,
		Registry:    reg,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = srv.Run(ctx) }()
	require.Eventually(t, func() bool { return srv.Addr() != "" }, time.Second, 10*time.Millisecond)
	assert.True(t, strings.HasPrefix(srv.URL(), "wss://"))
	// Verified end-to-end via the daemon integration TLS test (T22).
}

// writeSelfSignedPair writes a self-signed ECDSA cert + key into t.TempDir()
// and returns their paths. Used by this transport test and (later) reused
// by the daemon-level TLS sub-tests.
func writeSelfSignedPair(t *testing.T, host string) (certPath, keyPath string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(host)},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)

	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")

	certBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NotNil(t, certBytes)
	require.NoError(t, os.WriteFile(certPath, certBytes, 0o600))

	kb, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	keyBytes := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb})
	require.NotNil(t, keyBytes)
	require.NoError(t, os.WriteFile(keyPath, keyBytes, 0o600))
	return
}
