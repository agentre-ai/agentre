package server_svc

import (
	"context"
	"errors"
	"net/url"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo"
)

// AccessToken returns the current hub access token (empty when not logged in).
// The relay dial uses it to authenticate GET /v1/relay/client (device JWT Bearer).
func (s *service) AccessToken() string {
	return s.getClient().AccessToken()
}

// DialDaemonRelay 经账号中转连接指定指纹的 daemon(R6 的 relay 路径)。目标 daemon
// 指纹进 URL 的 daemon_fingerprint;peerFingerprint 是桌面端自身的设备指纹,在
// auth.account 中呈现——与 LAN 路径 auth.connect 呈现的是同一值(R5 硬不变量)。
func (s *service) DialDaemonRelay(ctx context.Context, daemonFingerprint, peerFingerprint string) (*client.Client, error) {
	row, err := server_state_repo.ServerState().Get(ctx)
	if err != nil {
		return nil, err
	}
	if row == nil || !row.IsLoggedIn() {
		return nil, ErrNotLoggedIn
	}
	if daemonFingerprint == "" || peerFingerprint == "" {
		return nil, errors.New("server_svc.DialDaemonRelay: empty fingerprint")
	}
	c := s.getClient()
	if c.AccessToken() == "" {
		return nil, ErrNotLoggedIn
	}
	return client.DialRelay(ctx, client.RelayOptions{
		URL:               relayClientURL(c.baseURL, daemonFingerprint),
		AccessToken:       c.AccessToken(),
		DeviceFingerprint: peerFingerprint,
	})
}

// relayClientURL 把 server 的 HTTP baseURL 换成 ws(s):// 并拼上客户端接入端点
// /v1/relay/client?daemon_fingerprint=<fp>。
func relayClientURL(baseURL, daemonFingerprint string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return ""
	}
	q := u.Query()
	q.Set("daemon_fingerprint", daemonFingerprint)
	u.Path = "/v1/relay/client"
	u.RawQuery = q.Encode()
	return u.String()
}
