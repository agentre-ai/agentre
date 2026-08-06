package client

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildTLSConfig_Default(t *testing.T) {
	c, err := BuildTLSConfig(TLSDefault, "")
	require.NoError(t, err)
	assert.False(t, c.InsecureSkipVerify)
	assert.Nil(t, c.VerifyPeerCertificate)
}

func TestBuildTLSConfig_DefaultEmptyMode(t *testing.T) {
	// Empty string mode is treated as default for backward-compat with
	// callers that don't set mode explicitly.
	c, err := BuildTLSConfig("", "")
	require.NoError(t, err)
	assert.False(t, c.InsecureSkipVerify)
}

func TestBuildTLSConfig_SkipVerify(t *testing.T) {
	c, err := BuildTLSConfig(TLSSkipVerify, "")
	require.NoError(t, err)
	assert.True(t, c.InsecureSkipVerify)
}

func TestBuildTLSConfig_PinCert_Bad(t *testing.T) {
	_, err := BuildTLSConfig(TLSPinCert, "not a pem")
	assert.Error(t, err)
}

func TestBuildTLSConfig_CABundle_Bad(t *testing.T) {
	_, err := BuildTLSConfig(TLSCABundle, "not a pem")
	assert.Error(t, err)
}

func TestBuildTLSConfig_Unknown(t *testing.T) {
	_, err := BuildTLSConfig("nonsense", "")
	assert.Error(t, err)
}

type closeTrackingFrameConn struct {
	done chan struct{}
	once sync.Once
}

func newCloseTrackingClient() (*Client, *closeTrackingFrameConn) {
	transport := &closeTrackingFrameConn{done: make(chan struct{})}
	return &Client{conn: rpc.NewConn(transport, rpc.NewRegistry())}, transport
}

func (*closeTrackingFrameConn) WriteFrame(rpc.Frame) error { return nil }
func (*closeTrackingFrameConn) ReadFrame(*rpc.Frame) error { return io.EOF }
func (c *closeTrackingFrameConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}
func (c *closeTrackingFrameConn) Done() <-chan struct{} { return c.done }

func TestRace_GivenRelayWins_WhenBothPathsStart_ThenCancelsAndClosesLANLoser(t *testing.T) {
	directStarted := make(chan struct{})
	winner, _ := newCloseTrackingClient()
	loser, loserTransport := newCloseTrackingClient()

	got, err := Race(context.Background(),
		Path{Name: "direct", Fingerprint: "sha256:desktop", Dial: func(ctx context.Context) (*Client, error) {
			close(directStarted)
			<-ctx.Done()
			return loser, nil
		}},
		Path{Name: "relay", Fingerprint: "sha256:desktop", Dial: func(context.Context) (*Client, error) {
			<-directStarted
			return winner, nil
		}},
	)
	require.NoError(t, err)
	assert.Same(t, winner, got, "the first successful relay path must win")
	select {
	case <-loserTransport.Done():
	case <-time.After(time.Second):
		t.Fatal("the losing LAN connection was not closed")
	}
}

func TestRace_GivenDifferentFingerprints_WhenSelectingPath_ThenRejectsBeforeDialing(t *testing.T) {
	called := false
	_, err := Race(context.Background(),
		Path{Name: "direct", Fingerprint: "sha256:lan", Dial: func(context.Context) (*Client, error) { called = true; return nil, nil }},
		Path{Name: "relay", Fingerprint: "sha256:relay", Dial: func(context.Context) (*Client, error) { called = true; return nil, nil }},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "peer fingerprint mismatch")
	assert.False(t, called, "a mismatched peer identity must never be dialed")
}

func TestRace_GivenBothPathsFail_WhenSelectingPath_ThenReportsEachReason(t *testing.T) {
	_, err := Race(context.Background(),
		Path{Name: "direct", Fingerprint: "sha256:desktop", Dial: func(context.Context) (*Client, error) { return nil, errors.New("LAN unavailable") }},
		Path{Name: "relay", Fingerprint: "sha256:desktop", Dial: func(context.Context) (*Client, error) { return nil, errors.New("relay unauthorized") }},
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "direct path: LAN unavailable")
	assert.ErrorContains(t, err, "relay path: relay unauthorized")
}

// relayDaemonServer 起一个假的中转目标:校验 Bearer 头,升级为 websocket,对
// auth.account 请求回成功。返回 server 与它收到的握手参数(经 channel 传回)。
func relayDaemonServer(t *testing.T, bearer string) (*httptest.Server, chan rpc.AccountParams) {
	t.Helper()
	got := make(chan rpc.AccountParams, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotH := r.Header.Get("Authorization"); gotH != "Bearer "+bearer {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		up := &websocket.Upgrader{}
		ws, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer ws.Close()
		for {
			var f rpc.Frame
			if err := ws.ReadJSON(&f); err != nil {
				return
			}
			if f.Method == "auth.account" {
				var p rpc.AccountParams
				_ = json.Unmarshal(f.Params, &p)
				got <- p
			}
			_ = ws.WriteJSON(rpc.Frame{JSONRPC: "2.0", ID: f.ID, Result: json.RawMessage(`{"ok":true,"instanceUUID":"uuid-1"}`)})
		}
	}))
	return srv, got
}

func TestDialRelay_GivenAccountToken_WhenAuthAccountSucceeds_ThenReturnsUsableClient(t *testing.T) {
	srv, got := relayDaemonServer(t, "relay-tok")
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	c, err := DialRelay(context.Background(), RelayOptions{
		URL:               wsURL + "/v1/relay/client?daemon_fingerprint=sha256:abc",
		AccessToken:       "relay-tok",
		DeviceFingerprint: "sha256:desktop",
	})
	require.NoError(t, err)
	defer c.Close()

	select {
	case p := <-got:
		assert.Equal(t, "relay-tok", p.Credential)
		assert.Equal(t, "sha256:desktop", p.DeviceFingerprint, "the relay path presents the same peer fingerprint as the LAN path")
	case <-time.After(time.Second):
		t.Fatal("auth.account was never received by the relay target")
	}
	select {
	case <-c.Closed():
		t.Fatal("client closed after successful auth.account")
	default:
	}
}

// R21:「客户端请求连接一台 daemon 而失败时，错误需区分三种情形：该 daemon 从未
// 登记过（不属于此账号）、登记过但当前离线、在线但转发失败。」server 已经把三者
// 分成 404 / 409 / 502(relay_ctr.relayError),这里守的是客户端一侧——中转拨号
// 必须把它们解析成各自可判定的错误,而不是塞回一个通用的 "bad handshake"。
func TestDialRelay_GivenRelayRefuses_WhenClassifyingFailure_ThenDistinguishesThreeCases(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"never registered", http.StatusNotFound, `{"code":30400,"msg":"relay daemon not found"}`, ErrRelayDaemonNotFound},
		{"registered but offline", http.StatusConflict, `{"code":30401,"msg":"relay daemon offline"}`, ErrRelayDaemonOffline},
		{"online but forward failed", http.StatusBadGateway, `{"code":30402,"msg":"relay forwarding failed"}`, ErrRelayForwardFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			_, err := DialRelay(context.Background(), RelayOptions{
				URL:               "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/relay/client?daemon_fingerprint=sha256:abc",
				AccessToken:       "relay-tok",
				DeviceFingerprint: "sha256:desktop",
			})
			require.Error(t, err)
			assert.ErrorIs(t, err, tc.want)
			for _, other := range []error{ErrRelayDaemonNotFound, ErrRelayDaemonOffline, ErrRelayForwardFailed} {
				if other != tc.want {
					assert.NotErrorIs(t, err, other, "the three relay failures must stay distinguishable from each other")
				}
			}
		})
	}
}

// 不属于 R21 三种情形的中转失败(例如鉴权 401)不得被误判成其中任何一种——
// 「先去认领」「去把机器开起来」「稍后重试」都不是它的正确处置。
func TestDialRelay_GivenUnrelatedHTTPFailure_WhenClassifyingFailure_ThenClaimsNoneOfTheThree(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := DialRelay(context.Background(), RelayOptions{
		URL:               "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/relay/client?daemon_fingerprint=sha256:abc",
		AccessToken:       "stale-tok",
		DeviceFingerprint: "sha256:desktop",
	})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrRelayDaemonNotFound)
	assert.NotErrorIs(t, err, ErrRelayDaemonOffline)
	assert.NotErrorIs(t, err, ErrRelayForwardFailed)
}
