// Package client is a wails-free JSON-RPC over WebSocket client. Used by
// daemon/integration_test.go and intended as the foundation of future
// desktop-remote-device UI in agentre's Wails frontend.
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"

	"github.com/gorilla/websocket"
)

// 编译期断言:*Client 实现 agentruntime.DaemonClientPort。断言写在实现侧
// (daemon/client),避免 agentruntime 抽象层反向依赖具体实现。
var _ agentruntime.DaemonClientPort = (*Client)(nil)

// Options configures a client dial.
type Options struct {
	URL       string // ws[s]://host:port/rpc
	TLSConfig *tls.Config
}

// Client wraps an *rpc.Conn with the dial + handle ergonomics callers expect.
type Client struct {
	conn *rpc.Conn
	reg  *rpc.Registry
}

// Dial opens a WebSocket connection to a daemon and starts its read loop.
// Caller is responsible for calling Close when done.
func Dial(ctx context.Context, opts Options) (*Client, error) {
	u, err := url.Parse(opts.URL)
	if err != nil {
		return nil, err
	}
	d := *websocket.DefaultDialer
	d.TLSClientConfig = opts.TLSConfig
	d.Subprotocols = []string{rpc.Subprotocol}
	ws, _, err := d.DialContext(ctx, u.String(), nil)
	if err != nil {
		return nil, err
	}
	reg := rpc.NewRegistry()
	c := &Client{reg: reg}
	c.conn = rpc.NewConn(rpc.NewWebSocketFrameConn(ws), reg)
	go c.conn.Serve(ctx)
	return c, nil
}

// Path is one candidate way to reach the same daemon peer. Race dials every
// path concurrently and the first to succeed wins.
type Path struct {
	// Name labels this path in error messages (e.g. "direct", "relay").
	// Empty names fall back to "path <n>".
	Name string
	// Fingerprint is the canonical peer identity this path resolves to. Every
	// path in one Race must resolve to the same value — the hard invariant
	// behind R5: switching path must not change the peer identity the daemon
	// resolves to.
	Fingerprint string
	// Dial establishes the connection. It receives a context that is cancelled
	// as soon as another path wins; a conforming Dial must return promptly
	// after cancellation (closing any half-open connection) rather than block.
	Dial func(ctx context.Context) (*Client, error)
}

// Race concurrently dials every path and returns the first Client whose Dial
// succeeds (R6: first to succeed wins, the rest are closed immediately). The
// winner is returned to the caller; every losing path is cancelled and any
// client it already produced is closed before Race returns.
//
// It is a hard invariant that every path resolves to the same peer fingerprint:
// a mismatch is rejected before any Dial is invoked. If every path fails, the
// returned error names each path's reason separately.
func Race(ctx context.Context, paths ...Path) (*Client, error) {
	if len(paths) == 0 {
		return nil, errors.New("client.Race: no paths")
	}
	first := paths[0].Fingerprint
	for _, p := range paths[1:] {
		if p.Fingerprint != first {
			return nil, errors.New("client.Race: peer fingerprint mismatch")
		}
	}

	raceCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type outcome struct {
		idx int
		c   *Client
		err error
	}
	results := make(chan outcome, len(paths))
	for i, p := range paths {
		i, p := i, p
		go func() {
			c, err := p.Dial(raceCtx)
			results <- outcome{idx: i, c: c, err: err}
		}()
	}

	var (
		won       bool
		winner    *Client
		winnerIdx int
		losers    []*Client
		errs      []error
	)
	for range paths {
		r := <-results
		if r.c == nil {
			err := r.err
			if err == nil {
				err = errors.New("returned no connection")
			}
			errs = append(errs, fmt.Errorf("%s path: %w", pathLabel(paths[r.idx], r.idx), err))
			continue
		}
		if r.err != nil {
			// 一个同时返回了连接与错误的 Dial——关掉连接,按错误上报。
			_ = r.c.Close()
			errs = append(errs, fmt.Errorf("%s path: %w", pathLabel(paths[r.idx], r.idx), r.err))
			continue
		}
		if !won {
			won = true
			winner = r.c
			winnerIdx = r.idx
			cancel() // stop in-flight losing dials
		} else if r.idx != winnerIdx {
			losers = append(losers, r.c)
		}
	}

	for _, l := range losers {
		_ = l.Close()
	}
	if won {
		return winner, nil
	}
	if len(errs) == 1 {
		return nil, errs[0]
	}
	return nil, errors.Join(errs...)
}

func pathLabel(p Path, idx int) string {
	if p.Name != "" {
		return p.Name
	}
	return fmt.Sprintf("path %d", idx+1)
}

// RelayOptions configures DialRelay.
type RelayOptions struct {
	// URL is the account relay endpoint for the target daemon, e.g.
	// "wss://hub/v1/relay/client?daemon_fingerprint=sha256:...".
	URL string
	// AccessToken authenticates against the relay endpoint (device JWT Bearer).
	AccessToken string
	// DeviceFingerprint is the desktop's own peer identity, presented to
	// auth.account — the same value the LAN path presents to auth.connect (R5).
	DeviceFingerprint string
	// TLSConfig is optional for the websocket dial.
	TLSConfig *tls.Config
}

// DialRelay connects to a daemon through the account relay: it dials the relay
// websocket authenticated with the account access token, wraps it as a JSON-RPC
// Conn, and completes the auth.account handshake presenting DeviceFingerprint.
// The returned Client is authenticated and ready for Call/Notify. Caller must
// call Close when done.
func DialRelay(ctx context.Context, opts RelayOptions) (*Client, error) {
	d := *websocket.DefaultDialer
	d.TLSClientConfig = opts.TLSConfig
	d.Subprotocols = []string{rpc.Subprotocol}
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+opts.AccessToken)
	ws, _, err := d.DialContext(ctx, opts.URL, headers)
	if err != nil {
		return nil, err
	}
	reg := rpc.NewRegistry()
	c := &Client{reg: reg}
	c.conn = rpc.NewConn(rpc.NewWebSocketFrameConn(ws), reg)
	go c.conn.Serve(ctx)
	var res rpc.ConnectResult
	if err := c.Call(ctx, "auth.account", rpc.AccountParams{
		Credential:        opts.AccessToken,
		DeviceFingerprint: opts.DeviceFingerprint,
	}, &res); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

// Call invokes a server method and waits for the response.
func (c *Client) Call(ctx context.Context, method string, params, result any) error {
	return c.conn.Call(ctx, method, params, result)
}

// Notify sends a fire-and-forget message.
func (c *Client) Notify(method string, params any) error {
	return c.conn.Notify(method, params)
}

// Handle registers a handler the server can invoke (server-initiated
// requests like approval.request and notifications like chat.event).
func (c *Client) Handle(method string, fn func(ctx context.Context, params json.RawMessage) (any, error)) {
	c.reg.Register(method, fn)
}

// Close shuts the underlying websocket. Idempotent.
func (c *Client) Close() error {
	if c.conn == nil {
		return errors.New("not connected")
	}
	return c.conn.Close()
}

// Closed returns a channel that is closed when the underlying connection is
// closed (either by Close, ctx cancel during Serve, or read loop EOF).
// Consumers can select on it to detect daemon drop.
//
// If the Client was constructed without an active connection (i.e. not via
// Dial), the returned channel is nil — selecting on it blocks forever. There
// is no underlying conn to ever fire a close event.
func (c *Client) Closed() <-chan struct{} {
	if c.conn == nil {
		return nil
	}
	return c.conn.Done()
}
