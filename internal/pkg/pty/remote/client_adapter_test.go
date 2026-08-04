package remote_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubDaemonClient struct {
	mu        sync.Mutex
	handlers  map[string]func(context.Context, json.RawMessage) (any, error)
	closed    chan struct{}
	closeOnce sync.Once
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

func (s *stubDaemonClient) Close() error {
	s.closeOnce.Do(func() { close(s.closed) })
	return nil
}

func (s *stubDaemonClient) dispatch(method string, payload any) error {
	s.mu.Lock()
	fn := s.handlers[method]
	s.mu.Unlock()
	if fn == nil {
		return fmt.Errorf("handler not registered for %s", method)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fn(context.Background(), raw)
	return err
}

func (s *stubDaemonClient) push(t *testing.T, method string, payload any) {
	t.Helper()
	require.NoError(t, s.dispatch(method, payload))
}

func TestClientAdapter_GivenAtomicSubscriptionsWhenDataArrivesThenDemuxesByTerminalID(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	subA := a.Subscribe("term-a")
	subB := a.Subscribe("term-b")

	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "term-a", Data: "alpha"})
	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "term-b", Data: "beta"})

	select {
	case ev := <-subA.Data:
		assert.Equal(t, "alpha", ev.Data)
	case <-time.After(time.Second):
		t.Fatal("no data for term-a")
	}
	select {
	case ev := <-subB.Data:
		assert.Equal(t, "beta", ev.Data)
	case <-time.After(time.Second):
		t.Fatal("no data for term-b")
	}
}

func TestClientAdapter_GivenExitWhenDeliveredThenClosesTheSameGenerationPair(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	sub := a.Subscribe("term-x")

	c.push(t, "terminal.exit", protocol.TerminalExitEvent{TerminalID: "term-x", Code: 0, Reason: "natural"})

	select {
	case ev := <-sub.Exit:
		assert.Equal(t, "natural", ev.Reason)
	case <-time.After(time.Second):
		t.Fatal("no exit event")
	}
	_, ok := <-sub.Exit
	assert.False(t, ok, "exit channel should be closed")
	_, ok = <-sub.Data
	assert.False(t, ok, "data channel should be closed")
}

func TestClientAdapter_GivenStaleUnsubscribeWhenANewGenerationExistsThenKeepsNewPairRegistered(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	first := a.Subscribe("term-generation")
	second := a.Subscribe("term-generation")
	require.NotEqual(t, first.Data, second.Data)
	require.NotEqual(t, first.Exit, second.Exit)

	a.Unsubscribe("term-generation", first)
	c.push(t, "terminal.data", protocol.TerminalDataEvent{
		TerminalID: "term-generation",
		Data:       "current",
	})

	select {
	case ev := <-second.Data:
		assert.Equal(t, "current", ev.Data)
	case <-time.After(time.Second):
		t.Fatal("stale unsubscribe removed the current generation")
	}
	_, ok := <-first.Data
	assert.False(t, ok, "replacement must close the stale data generation")
	_, ok = <-first.Exit
	assert.False(t, ok, "replacement must close the stale exit generation")
}

func TestClientAdapter_GivenConnectionCloseWhenSubscriptionsExistThenClosesAllAndFuturePairs(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	sub1 := a.Subscribe("t1")
	sub2 := a.Subscribe("t2")

	a.Abort()

	for _, ch := range []<-chan protocol.TerminalExitEvent{sub1.Exit, sub2.Exit} {
		select {
		case _, ok := <-ch:
			assert.False(t, ok, "exit channel should be closed on connection close")
		case <-time.After(time.Second):
			t.Fatal("exit channel not closed within 1s")
		}
	}
	require.Eventually(t, func() bool {
		sub := a.Subscribe("after-close")
		select {
		case _, ok := <-sub.Exit:
			return !ok
		default:
			return false
		}
	}, time.Second, time.Millisecond, "subscription registered after close must already be closed")
}

func TestClientAdapter_GivenUnknownTerminalEventsWhenDeliveredThenDoesNotReplayOrAllocateForLaterSubscribe(t *testing.T) {
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: "ghost", Data: "ignored"})
	c.push(t, "terminal.exit", protocol.TerminalExitEvent{TerminalID: "ghost", Reason: "natural"})

	sub := a.Subscribe("ghost")
	select {
	case ev, ok := <-sub.Data:
		t.Fatalf("unknown event was retained: ok=%v event=%+v", ok, ev)
	default:
	}
	select {
	case ev, ok := <-sub.Exit:
		t.Fatalf("unknown exit was retained: ok=%v event=%+v", ok, ev)
	default:
	}
}

func TestClientAdapter_GivenDeliveryExitUnsubscribeAndWatchCloseRacesThenNeverUsesClosedGeneration(t *testing.T) {
	const iterations = 300
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	dataHandler := func(id string) error {
		return c.dispatch("terminal.data", protocol.TerminalDataEvent{TerminalID: id, Data: "chunk"})
	}
	exitHandler := func(id string) error {
		return c.dispatch("terminal.exit", protocol.TerminalExitEvent{TerminalID: id, Reason: "natural"})
	}

	var wg sync.WaitGroup
	errs := make(chan error, iterations*2)
	for i := 0; i < iterations; i++ {
		id := fmt.Sprintf("race-%03d", i)
		sub := a.Subscribe(id)
		start := make(chan struct{})
		wg.Add(3)
		go func() {
			defer wg.Done()
			<-start
			if err := dataHandler(id); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			if err := exitHandler(id); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer wg.Done()
			<-start
			a.Unsubscribe(id, sub)
		}()
		close(start)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		a.Abort()
	}()
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
}
