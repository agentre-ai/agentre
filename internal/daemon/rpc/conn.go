package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/gorilla/websocket"
)

// connCtxKey is the unexported context key used by handleRequest to attach
// the *Conn before dispatching. Use ConnFromContext to retrieve.
type connCtxKey struct{}

// ConnFromContext returns the *Conn associated with this dispatch, or nil
// if called outside of Conn.handleRequest (e.g. unit tests dispatching
// directly through Registry).
func ConnFromContext(ctx context.Context) *Conn {
	v, _ := ctx.Value(connCtxKey{}).(*Conn)
	return v
}

// Conn wraps one WS connection with bidirectional JSON-RPC.
// Inbound requests are dispatched through the registry; outbound Call sends
// a request and blocks until the peer's matching response arrives.
type Conn struct {
	ws  *websocket.Conn
	reg *Registry

	writeMu sync.Mutex

	pendingMu sync.Mutex
	pending   map[string]chan Frame

	nextID atomic.Uint64

	authMu sync.RWMutex
	auth   AuthState

	done      chan struct{}
	closeOnce sync.Once
}

// AuthState captures who is on the other end of this connection after the
// handshake completes (Mode A pair or Mode B connect).
type AuthState struct {
	Authenticated     bool
	DeviceFingerprint string
	DeviceName        string
}

// NewConn wraps an already-upgraded *websocket.Conn. The caller owns the
// goroutine running Serve.
func NewConn(ws *websocket.Conn, reg *Registry) *Conn {
	return &Conn{
		ws:      ws,
		reg:     reg,
		pending: map[string]chan Frame{},
		done:    make(chan struct{}),
	}
}

// ErrConnClosed is returned by Call when the connection closes (peer
// disconnect / Close / read-loop EOF) while a request is still awaiting its
// response. Without it, Call would block until the caller's own ctx deadline
// — and reverse requests like the MCP tunnel carry the CLI's long-lived HTTP
// ctx (~285s), so a mid-request WS death would hang that long. Callers can
// errors.Is against this to retry / re-borrow a fresh connection.
var ErrConnClosed = errors.New("rpc: connection closed")

// Done returns a channel that is closed when Close is called or when Serve
// exits (read loop EOF). Multiple consumers may receive on it.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) markDone() { c.closeOnce.Do(func() { close(c.done) }) }

func (c *Conn) Auth() AuthState {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.auth
}

func (c *Conn) SetAuth(a AuthState) {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.auth = a
}

// Serve runs the read loop until the underlying WS closes or ctx is done.
//
// Requests are dispatched per-goroutine — different RPC calls have no
// inter-call ordering semantics, so a slow handler shouldn't stall others.
//
// Notifications are dispatched SYNCHRONOUSLY in the read loop to preserve
// peer-emit order. The earlier per-goroutine path let the Go scheduler
// reorder notifications: TextDelta chunks landed out of order in the
// remote runtime's session events channel, mangling streamed text like
// "/root/.config/agentre/agents/5" → "//.rootconfig/agent/reagents/5",
// and runtime.runResultDone could close the events channel before
// in-flight runtime.event goroutines drained, silently dropping events.
// Notification handlers MUST therefore be non-blocking (or bounded) —
// they share back-pressure with the read loop.
func (c *Conn) Serve(ctx context.Context) {
	defer c.markDone()
	defer func() { _ = c.ws.Close() }()
	for {
		var f Frame
		if err := c.ws.ReadJSON(&f); err != nil {
			return
		}
		switch {
		case f.IsResponse():
			c.deliverResponse(f)
		case f.IsRequest():
			go c.handleRequest(ctx, f)
		case f.IsNotification():
			nctx := context.WithValue(ctx, connCtxKey{}, c)
			_, _ = c.reg.Dispatch(nctx, f.Method, f.Params)
		}
	}
}

func (c *Conn) handleRequest(ctx context.Context, f Frame) {
	ctx = context.WithValue(ctx, connCtxKey{}, c)
	res, err := c.reg.Dispatch(ctx, f.Method, f.Params)
	out := Frame{JSONRPC: "2.0", ID: f.ID}
	if err != nil {
		var rpcErr *Error
		if errors.As(err, &rpcErr) {
			out.Error = rpcErr
		} else {
			out.Error = &Error{Code: ErrInternal.Code, Message: err.Error()}
		}
	} else {
		b, mErr := json.Marshal(res)
		if mErr != nil {
			out.Error = &Error{Code: ErrInternal.Code, Message: mErr.Error()}
		} else {
			out.Result = b
		}
	}
	_ = c.write(out)
}

func (c *Conn) write(f Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteJSON(f)
}

// Notify sends a fire-and-forget JSON-RPC notification to the peer.
func (c *Conn) Notify(method string, params any) error {
	b, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return c.write(Frame{JSONRPC: "2.0", Method: method, Params: b})
}

// Call sends a request and blocks until the peer responds or ctx is done.
// result may be nil if the caller does not care about the reply payload.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	id := c.nextID.Add(1)
	idJSON, _ := json.Marshal(id)
	pb, err := json.Marshal(params)
	if err != nil {
		return err
	}
	ch := make(chan Frame, 1)
	idStr := string(idJSON)
	c.pendingMu.Lock()
	c.pending[idStr] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, idStr)
		c.pendingMu.Unlock()
	}()
	if err := c.write(Frame{JSONRPC: "2.0", ID: idJSON, Method: method, Params: pb}); err != nil {
		return err
	}
	select {
	case f := <-ch:
		if f.Error != nil {
			return f.Error
		}
		if result == nil {
			return nil
		}
		return json.Unmarshal(f.Result, result)
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		// 连接已断(peer 掉线 / Close / read-loop EOF):响应永不会来,及时返回而
		// 不是干等 ctx deadline。反向请求(MCP 隧道)携 CLI 的长寿命 HTTP ctx,少了
		// 这一路 case,WS 中途断会把隧道 goroutine + pending 挂到 CLI ~285s 超时。
		return ErrConnClosed
	}
}

func (c *Conn) deliverResponse(f Frame) {
	c.pendingMu.Lock()
	ch, ok := c.pending[string(f.ID)]
	c.pendingMu.Unlock()
	if ok {
		ch <- f
	}
}

// Close closes the underlying WS connection.
func (c *Conn) Close() error {
	err := c.ws.Close()
	c.markDone()
	return err
}
