// Package client is a wails-free JSON-RPC over WebSocket client. Used by
// daemon/integration_test.go and intended as the foundation of future
// desktop-remote-device UI in agentre's Wails frontend.
package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net/url"

	"agentre/internal/daemon/rpc"
	"agentre/internal/pkg/agentruntime"

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
	c.conn = rpc.NewConn(ws, reg)
	go c.conn.Serve(ctx)
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
