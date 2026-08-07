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
	ws, resp, err := d.DialContext(ctx, u.String(), nil)
	closeHandshakeBody(resp)
	if err != nil {
		return nil, err
	}
	reg := rpc.NewRegistry()
	c := &Client{reg: reg}
	c.conn = rpc.NewConn(rpc.NewWebSocketFrameConn(ws), reg)
	go c.conn.Serve(ctx)
	return c, nil
}

// closeHandshakeBody releases the WebSocket handshake response. gorilla replaces
// the body with a no-op reader on both outcomes (success and ErrBadHandshake),
// so this closes nothing that the live connection needs — it keeps the HTTP
// contract explicit and leaves the status line, which classifyRelayDialError
// reads, untouched.
func closeHandshakeBody(resp *http.Response) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
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
	// Dial establishes the connection. It receives a per-path context that is
	// canceled as soon as *another* path wins; a conforming Dial must return
	// promptly after cancellation (closing any half-open connection) rather than
	// block. The winning path's context is never canceled by Race — Dial hands
	// it to the connection's Serve read loop, which outlives the race.
	Dial func(ctx context.Context) (*Client, error)
}

// Race concurrently dials every path and returns the first Client whose Dial
// succeeds (R6: first to succeed wins, the rest are closed immediately). The
// winner is returned to the caller; every losing path is canceled and any
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

	type outcome struct {
		idx int
		c   *Client
		err error
	}
	results := make(chan outcome, len(paths))
	// 每条路径一个**独立**的可取消 ctx,而不是一个共享的 raceCtx:Dial 会把拿到的
	// ctx 交给连接自己的 Serve 读循环(client.Dial / DialRelay 里的
	// `go c.conn.Serve(ctx)`),而赢家那条连接活得比这场竞速久得多 —— ConnPool 按
	// device 缓存它,daemon 之后所有反向请求(内置工具 MCP 隧道)都在它下面派发。
	// 共享一个 `defer cancel()` 的 raceCtx 会让胜出的长连接一返回就带着已取消的 ctx。
	cancels := make([]context.CancelFunc, len(paths))
	for i, p := range paths {
		//nolint:gosec // G118: 赢家那条的 cancel 故意不调用——它的 ctx 归胜出的长连接
		// 所有(见 Path.Dial 注释),随父 ctx 结束而释放;败者的在 cancelExcept 里全部调用。
		pathCtx, pathCancel := context.WithCancel(ctx)
		cancels[i] = pathCancel
		go func() {
			c, err := p.Dial(pathCtx)
			results <- outcome{idx: i, c: c, err: err}
		}()
	}
	// cancelExcept 取消除 keep 之外的每条路径;keep = -1 时全取消(无人胜出)。
	cancelExcept := func(keep int) {
		for i, cancel := range cancels {
			if i != keep {
				cancel()
			}
		}
	}

	var (
		won    bool
		winner *Client
		losers []*Client
		errs   []error
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
			cancelExcept(r.idx) // stop in-flight losing dials; the winner keeps its ctx
			continue
		}
		losers = append(losers, r.c)
	}

	for _, l := range losers {
		_ = l.Close()
	}
	if won {
		return winner, nil
	}
	// 无人胜出:每条路径都已返回,把它们的 ctx 一并释放,不留悬挂的子 context。
	cancelExcept(-1)
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

// The three relay dial failures R21 requires the caller to be able to tell
// apart, because the user's next step differs for each: claim the machine,
// power it on, or retry later. agentre-server distinguishes them on the wire
// (404 / 409 / 502, see relay_ctr.relayError); DialRelay maps them back so the
// distinction survives the websocket handshake instead of collapsing into
// gorilla's generic "bad handshake".
var (
	// ErrRelayDaemonNotFound: the daemon was never registered under this
	// account — the user has to claim that machine first.
	ErrRelayDaemonNotFound = errors.New("relay: daemon is not registered under this account")
	// ErrRelayDaemonOffline: registered, but not currently connected to the
	// relay — the user has to power that machine on.
	ErrRelayDaemonOffline = errors.New("relay: daemon is registered but currently offline")
	// ErrRelayForwardFailed: online, but the server could not forward to it —
	// transient, worth retrying.
	ErrRelayForwardFailed = errors.New("relay: daemon is online but the relay could not forward to it")
)

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
	ws, resp, err := d.DialContext(ctx, opts.URL, headers)
	closeHandshakeBody(resp)
	if err != nil {
		// classifyRelayDialError only reads the status line, which stays valid
		// after the body is closed.
		return nil, classifyRelayDialError(err, resp)
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

// classifyRelayDialError turns a failed relay handshake into one of R21's three
// distinguishable outcomes. gorilla hands back the HTTP response alongside
// ErrBadHandshake, and agentre-server answers the pre-upgrade cases with a
// distinct status each; anything else (401 on a stale token, a dead TCP dial, a
// proxy in the way) is deliberately left unclassified rather than guessed into
// one of the three — a wrong classification sends the user down the wrong fix.
func classifyRelayDialError(err error, resp *http.Response) error {
	if resp == nil || !errors.Is(err, websocket.ErrBadHandshake) {
		return err
	}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %w", ErrRelayDaemonNotFound, err)
	case http.StatusConflict:
		return fmt.Errorf("%w: %w", ErrRelayDaemonOffline, err)
	case http.StatusBadGateway:
		return fmt.Errorf("%w: %w", ErrRelayForwardFailed, err)
	}
	return fmt.Errorf("relay rejected the connection with %s: %w", resp.Status, err)
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
