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
// events. Each Subscribe call atomically installs one data/exit generation.
// Notification handlers only append to that generation's FIFO under mu; its
// sole delivery worker owns channel sends and closure.
type ClientAdapter struct {
	client DaemonClient

	mu     sync.Mutex
	subs   map[string]*terminalSubscription
	closed bool
}

type terminalSubscription struct {
	data   chan protocol.TerminalDataEvent
	exit   chan protocol.TerminalExitEvent
	wake   chan struct{}
	cancel chan struct{}
	done   chan struct{}

	head *terminalDataNode
	tail *terminalDataNode

	ending    bool
	canceled  bool
	hasExit   bool
	exitEvent protocol.TerminalExitEvent
}

type terminalDataNode struct {
	event protocol.TerminalDataEvent
	next  *terminalDataNode
}

type deliveryState uint8

const (
	deliveryWait deliveryState = iota
	deliveryData
	deliveryEnd
	deliveryCanceled
)

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
	var previousDone <-chan struct{}
	a.mu.Lock()
	closed := a.closed
	if closed {
		a.finishSubscriptionLocked(sub, nil)
	} else {
		previous := a.subs[terminalID]
		a.subs[terminalID] = sub
		if previous != nil {
			a.cancelSubscriptionLocked(previous)
			previousDone = previous.done
		}
	}
	a.mu.Unlock()
	if previousDone != nil {
		<-previousDone
	}
	go a.deliver(terminalID, sub)
	if closed {
		<-sub.done
	}
	return subscriptionView(sub)
}

// Unsubscribe removes and closes only the exact generation returned by
// Subscribe. It is safe after exit, connection loss, or replacement.
func (a *ClientAdapter) Unsubscribe(terminalID string, subscription Subscription) {
	var done <-chan struct{}
	a.mu.Lock()
	sub := a.subs[terminalID]
	if sub != nil && sub.data == subscription.Data && sub.exit == subscription.Exit {
		delete(a.subs, terminalID)
		a.cancelSubscriptionLocked(sub)
		done = sub.done
	}
	a.mu.Unlock()
	if done != nil {
		<-done
	}
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
		data:   make(chan protocol.TerminalDataEvent),
		exit:   make(chan protocol.TerminalExitEvent, 1),
		wake:   make(chan struct{}, 1),
		cancel: make(chan struct{}),
		done:   make(chan struct{}),
	}
}

func subscriptionView(sub *terminalSubscription) Subscription {
	return Subscription{Data: sub.data, Exit: sub.exit}
}

func (a *ClientAdapter) handleData(_ context.Context, raw json.RawMessage) (any, error) {
	var ev protocol.TerminalDataEvent
	if err := json.Unmarshal(raw, &ev); err != nil {
		return nil, nil //nolint:nilerr // push-event handler; malformed events are silently discarded
	}
	a.mu.Lock()
	sub := a.subs[ev.TerminalID]
	if sub != nil && !sub.ending && !sub.canceled {
		node := &terminalDataNode{event: ev}
		if sub.tail == nil {
			sub.head = node
		} else {
			sub.tail.next = node
		}
		sub.tail = node
		signalSubscription(sub)
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
		a.finishSubscriptionLocked(sub, &ev)
	}
	a.mu.Unlock()
	return nil, nil
}

func (a *ClientAdapter) watchClose(closed <-chan struct{}) {
	<-closed
	a.mu.Lock()
	a.closed = true
	for _, sub := range a.subs {
		// A connection close preserves bytes already accepted by the handler,
		// but carries no authoritative terminal.exit value.
		a.finishSubscriptionLocked(sub, nil)
	}
	a.mu.Unlock()
}

func signalSubscription(sub *terminalSubscription) {
	select {
	case sub.wake <- struct{}{}:
	default:
	}
}

func (a *ClientAdapter) finishSubscriptionLocked(
	sub *terminalSubscription,
	exitEvent *protocol.TerminalExitEvent,
) {
	if sub.ending || sub.canceled {
		return
	}
	sub.ending = true
	if exitEvent != nil {
		sub.hasExit = true
		sub.exitEvent = *exitEvent
	}
	signalSubscription(sub)
}

func (a *ClientAdapter) cancelSubscriptionLocked(sub *terminalSubscription) {
	if sub.canceled {
		return
	}
	sub.canceled = true
	sub.head = nil
	sub.tail = nil
	close(sub.cancel)
}

func (a *ClientAdapter) deliver(terminalID string, sub *terminalSubscription) {
	defer func() {
		close(sub.data)
		close(sub.exit)
		close(sub.done)
		a.mu.Lock()
		if a.subs[terminalID] == sub {
			delete(a.subs, terminalID)
		}
		a.mu.Unlock()
	}()

	for {
		state, dataEvent, hasExit, exitEvent := a.nextDelivery(sub)
		switch state {
		case deliveryData:
			select {
			case sub.data <- dataEvent:
			case <-sub.cancel:
				return
			}
		case deliveryEnd:
			if hasExit {
				select {
				case sub.exit <- exitEvent:
				case <-sub.cancel:
					return
				}
			}
			return
		case deliveryCanceled:
			return
		case deliveryWait:
			select {
			case <-sub.wake:
			case <-sub.cancel:
				return
			}
		}
	}
}

func (a *ClientAdapter) nextDelivery(
	sub *terminalSubscription,
) (deliveryState, protocol.TerminalDataEvent, bool, protocol.TerminalExitEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if sub.canceled {
		return deliveryCanceled, protocol.TerminalDataEvent{}, false, protocol.TerminalExitEvent{}
	}
	if sub.head != nil {
		node := sub.head
		sub.head = node.next
		if sub.head == nil {
			sub.tail = nil
		}
		return deliveryData, node.event, false, protocol.TerminalExitEvent{}
	}
	if sub.ending {
		return deliveryEnd, protocol.TerminalDataEvent{}, sub.hasExit, sub.exitEvent
	}
	return deliveryWait, protocol.TerminalDataEvent{}, false, protocol.TerminalExitEvent{}
}

// Compile-time assertion: ClientAdapter satisfies remote.Client.
var _ Client = (*ClientAdapter)(nil)
