package remote

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

// DaemonClient is the subset of internal/daemon/client.Client and
// agentruntime.DaemonClientPort that ClientAdapter consumes. Declared here
// to avoid this package depending on daemon/client; production wires the
// real *client.Client.
type DaemonClient interface {
	Call(ctx context.Context, method string, params any, result any) error
	Handle(method string, fn func(ctx context.Context, params json.RawMessage) (any, error))
	Closed() <-chan struct{}
}

// ClientAdapter wraps a single daemon client and demuxes per-terminal push
// events. Each Subscribe call atomically installs one data/exit generation;
// replacement, delivery, exit, unsubscribe, and connection close all serialize
// under mu so no goroutine can send on a channel another goroutine just closed.
type ClientAdapter struct {
	client DaemonClient

	mu     sync.Mutex
	subs   map[string]*terminalSubscription
	closed bool
}

type terminalSubscription struct {
	data chan protocol.TerminalDataEvent
	exit chan protocol.TerminalExitEvent
}

// NewClientAdapter wires up the push-event demux. Spawns one goroutine for
// connection-close detection. The handler registrations are register-once;
// constructing a second ClientAdapter against the same client would overwrite
// them, so callers keep at most one adapter per client instance.
func NewClientAdapter(c DaemonClient) *ClientAdapter {
	a := &ClientAdapter{client: c, subs: map[string]*terminalSubscription{}}
	c.Handle("terminal.data", a.handleData)
	c.Handle("terminal.exit", a.handleExit)
	if closed := c.Closed(); closed != nil {
		go a.watchClose(closed)
	}
	return a
}

// Call passes through to the underlying client.
func (a *ClientAdapter) Call(ctx context.Context, method string, params any, out any) error {
	return a.client.Call(ctx, method, params, out)
}

// Subscribe atomically creates and registers one data/exit channel pair for a
// terminal ID. A newer registration replaces and closes the older generation;
// its later Unsubscribe cannot remove the replacement because identity is
// checked against both exact channel references.
func (a *ClientAdapter) Subscribe(terminalID string) Subscription {
	sub := newTerminalSubscription()
	a.mu.Lock()
	if a.closed {
		closeTerminalSubscription(sub)
		a.mu.Unlock()
		return subscriptionView(sub)
	}
	previous := a.subs[terminalID]
	a.subs[terminalID] = sub
	if previous != nil {
		closeTerminalSubscription(previous)
	}
	a.mu.Unlock()
	return subscriptionView(sub)
}

// Unsubscribe removes and closes only the exact generation returned by
// Subscribe. It is safe after exit, connection loss, or replacement.
func (a *ClientAdapter) Unsubscribe(terminalID string, subscription Subscription) {
	a.mu.Lock()
	sub := a.subs[terminalID]
	if sub != nil && sub.data == subscription.Data && sub.exit == subscription.Exit {
		delete(a.subs, terminalID)
		closeTerminalSubscription(sub)
	}
	a.mu.Unlock()
}

// Abort closes the wrapped shared connection when a pending-open cleanup RPC
// cannot be acknowledged. Production daemon clients implement Close; keeping
// it as an optional narrow assertion avoids coupling this demux interface to
// unrelated client operations.
func (a *ClientAdapter) Abort() {
	if closer, ok := a.client.(interface{ Close() error }); ok {
		_ = closer.Close()
	}
}

func newTerminalSubscription() *terminalSubscription {
	return &terminalSubscription{
		data: make(chan protocol.TerminalDataEvent, 32),
		exit: make(chan protocol.TerminalExitEvent, 1),
	}
}

func subscriptionView(sub *terminalSubscription) Subscription {
	return Subscription{Data: sub.data, Exit: sub.exit}
}

func closeTerminalSubscription(sub *terminalSubscription) {
	close(sub.exit)
	close(sub.data)
}

func (a *ClientAdapter) handleData(_ context.Context, raw json.RawMessage) (any, error) {
	var ev protocol.TerminalDataEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, nil //nolint:nilerr // push-event handler; malformed events are silently discarded
	}
	a.mu.Lock()
	sub := a.subs[ev.TerminalID]
	if sub != nil {
		select {
		case sub.data <- ev:
		default:
			// Drop on full buffer; matches the existing slow-consumer tradeoff.
		}
	}
	a.mu.Unlock()
	return nil, nil
}

func (a *ClientAdapter) handleExit(_ context.Context, raw json.RawMessage) (any, error) {
	var ev protocol.TerminalExitEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, nil //nolint:nilerr // push-event handler; malformed events are silently discarded
	}
	a.mu.Lock()
	sub := a.subs[ev.TerminalID]
	if sub != nil {
		delete(a.subs, ev.TerminalID)
		select {
		case sub.exit <- ev:
		default:
		}
		closeTerminalSubscription(sub)
	}
	a.mu.Unlock()
	return nil, nil
}

func (a *ClientAdapter) watchClose(closed <-chan struct{}) {
	<-closed
	a.mu.Lock()
	a.closed = true
	subs := a.subs
	a.subs = map[string]*terminalSubscription{}
	for _, sub := range subs {
		// Close exit before data so the per-handle pump sees a missing
		// authoritative outcome and synthesizes connection_lost.
		closeTerminalSubscription(sub)
	}
	a.mu.Unlock()
}

// Compile-time assertion: ClientAdapter satisfies remote.Client.
var _ Client = (*ClientAdapter)(nil)
