package remote_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"agentre/internal/pkg/pty/remote"
	"agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDaemonClient struct {
	mu       sync.Mutex
	handlers map[string]func(context.Context, json.RawMessage) (any, error)
	closed   chan struct{}
}

func newStubDaemonClient() *stubDaemonClient {
	return &stubDaemonClient{
		handlers: map[string]func(context.Context, json.RawMessage) (any, error){},
		closed:   make(chan struct{}),
	}
}

func (s *stubDaemonClient) Call(_ context.Context, _ string, _ any, _ any) error { return nil }

func (s *stubDaemonClient) Handle(method string, fn func(context.Context, json.RawMessage) (any, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = fn
}

func (s *stubDaemonClient) Closed() <-chan struct{} { return s.closed }

func (s *stubDaemonClient) push(t *testing.T, method string, payload any) {
	s.mu.Lock()
	fn := s.handlers[method]
	s.mu.Unlock()
	require.NotNil(t, fn, "handler not registered for %s", method)
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	_, err = fn(context.Background(), raw)
	require.NoError(t, err)
}

func TestClientAdapter_Subscribe_DemuxesDataByTerminalID(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	chA := a.SubscribeData("term-a")
	chB := a.SubscribeData("term-b")

	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "term-a", Data: "alpha"})
	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "term-b", Data: "beta"})

	select {
	case ev := <-chA:
		assert.Equal(t, "alpha", ev.Data)
	case <-time.After(time.Second):
		t.Fatal("no data for term-a")
	}
	select {
	case ev := <-chB:
		assert.Equal(t, "beta", ev.Data)
	case <-time.After(time.Second):
		t.Fatal("no data for term-b")
	}
}

func TestClientAdapter_Exit_DeliversAndClosesChannels(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	dataCh := a.SubscribeData("term-x")
	exitCh := a.SubscribeExit("term-x")

	c.push(t, "terminal.exit", protocol.TerminalExitEvent{TerminalID: "term-x", Code: 0, Reason: "natural"})

	select {
	case ev := <-exitCh:
		assert.Equal(t, "natural", ev.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit event")
	}

	// Both channels should be closed after exit.
	_, ok := <-exitCh
	assert.False(t, ok, "exit chan should be closed")
	_, ok = <-dataCh
	assert.False(t, ok, "data chan should be closed")
}

func TestClientAdapter_ConnClose_ClosesAllSubscriptions(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	exit1 := a.SubscribeExit("t1")
	exit2 := a.SubscribeExit("t2")

	close(c.closed)

	for _, ch := range []<-chan protocol.TerminalExitEvent{exit1, exit2} {
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "exit chan should be closed on conn close")
		case <-time.After(time.Second):
			t.Fatal("exit chan not closed within 1s")
		}
	}
}

func TestClientAdapter_UnknownTerminal_DropsSilently(t *testing.T) {
	c := newStubDaemonClient()
	_ = remote.NewClientAdapter(c)
	// No SubscribeData call → no subscription → push should be silently dropped.
	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "ghost", Data: "ignored"})
	// If we got here without panic/error, drop worked.
}
