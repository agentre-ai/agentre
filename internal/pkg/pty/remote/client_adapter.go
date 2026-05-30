package remote

import (
	"context"
	"encoding/json"
	"sync"

	"agentre/pkg/agentred/protocol"
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

// ClientAdapter wraps a single daemon client and demuxes per-terminal
// push events. Construction registers terminal.data and terminal.exit
// handlers exactly once on the wrapped client. SubscribeData/SubscribeExit
// allocate per-terminalID channel records on first call.
type ClientAdapter struct {
	client DaemonClient

	mu   sync.Mutex
	subs map[string]*terminalSubscription
}

type terminalSubscription struct {
	data chan protocol.TerminalDataEvent
	exit chan protocol.TerminalExitEvent
}

// NewClientAdapter wires up the push-event demux. Spawns one goroutine for
// connection-close detection. The handler registrations are register-once;
// constructing a second ClientAdapter against the same client would
// overwrite them — callers are expected to keep at most one adapter per
// client instance.
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

// SubscribeData returns the read end of the data channel for a given
// terminalID. Channel is buffered (cap 32) so a slow consumer doesn't
// drop chunks unless we're truly behind.
func (a *ClientAdapter) SubscribeData(terminalID string) <-chan protocol.TerminalDataEvent {
	return a.ensureSubscription(terminalID).data
}

// SubscribeExit returns the read end of the exit channel for a given
// terminalID. Channel is cap 1; per protocol exit fires at most once.
func (a *ClientAdapter) SubscribeExit(terminalID string) <-chan protocol.TerminalExitEvent {
	return a.ensureSubscription(terminalID).exit
}

func (a *ClientAdapter) ensureSubscription(terminalID string) *terminalSubscription {
	a.mu.Lock()
	defer a.mu.Unlock()
	sub, ok := a.subs[terminalID]
	if !ok {
		sub = &terminalSubscription{
			data: make(chan protocol.TerminalDataEvent, 32),
			exit: make(chan protocol.TerminalExitEvent, 1),
		}
		a.subs[terminalID] = sub
	}
	return sub
}

func (a *ClientAdapter) handleData(_ context.Context, raw json.RawMessage) (any, error) {
	var ev protocol.TerminalDataEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, nil //nolint:nilerr // push-event handler; malformed events are silently discarded
	}
	a.mu.Lock()
	sub := a.subs[ev.TerminalID]
	a.mu.Unlock()
	if sub == nil {
		return nil, nil
	}
	select {
	case sub.data <- ev:
	default:
		// drop on full buffer; matches "slow consumer = lost chunks" tradeoff
	}
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
	}
	a.mu.Unlock()
	if sub == nil {
		return nil, nil
	}
	select {
	case sub.exit <- ev:
	default:
	}
	close(sub.exit)
	close(sub.data)
	return nil, nil
}

func (a *ClientAdapter) watchClose(closed <-chan struct{}) {
	<-closed
	a.mu.Lock()
	subs := a.subs
	a.subs = map[string]*terminalSubscription{}
	a.mu.Unlock()
	for _, sub := range subs {
		// close exit unbuffered-style so the per-handle pump sees !ok and
		// synthesizes ExitInfo{Reason: "connection_lost"} (see remote.go pump)
		close(sub.exit)
		close(sub.data)
	}
}

// Compile-time assertion: ClientAdapter satisfies remote.Client.
var _ Client = (*ClientAdapter)(nil)
