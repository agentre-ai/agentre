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
	callFn    func(context.Context, string, any, any) error
	closed    chan struct{}
	closeOnce sync.Once
}

func newStubDaemonClient() *stubDaemonClient {
	return &stubDaemonClient{
		handlers: map[string]func(context.Context, json.RawMessage) (any, error){},
		closed:   make(chan struct{}),
	}
}

func (s *stubDaemonClient) Call(ctx context.Context, method string, params any, out any) error {
	s.mu.Lock()
	fn := s.callFn
	s.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(ctx, method, params, out)
}

func (s *stubDaemonClient) setCall(fn func(context.Context, string, any, any) error) {
	s.mu.Lock()
	s.callFn = fn
	s.mu.Unlock()
}

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

func TestClientAdapter_GivenFastStartupBurstBeforeConsumerStartsWhenExitArrivesThenDeliversEveryFrameFIFOFirst(t *testing.T) {
	const frameCount = 128
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	sub := a.Subscribe("term-fast-startup")

	producerDone := make(chan error, 1)
	go func() {
		for i := 0; i < frameCount; i++ {
			if err := c.dispatch("terminal.data", protocol.TerminalDataEvent{
				TerminalID: "term-fast-startup",
				Data:       fmt.Sprintf("frame-%03d", i),
			}); err != nil {
				producerDone <- err
				return
			}
		}
		producerDone <- c.dispatch("terminal.exit", protocol.TerminalExitEvent{
			TerminalID: "term-fast-startup",
			Code:       0,
			Reason:     "natural",
		})
	}()

	select {
	case err := <-producerDone:
		require.NoError(t, err, "notification handlers must not wait for the consumer")
	case <-time.After(time.Second):
		t.Fatal("startup notification handlers blocked on an unread subscription")
	}
	select {
	case ev, ok := <-sub.Exit:
		t.Fatalf("exit became observable before accepted data drained: ok=%v event=%+v", ok, ev)
	default:
	}

	for i := 0; i < frameCount; i++ {
		select {
		case ev, ok := <-sub.Data:
			require.Truef(t, ok, "data closed after %d of %d frames", i, frameCount)
			require.Equal(t, fmt.Sprintf("frame-%03d", i), ev.Data)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for frame %d of %d", i, frameCount)
		}
	}
	select {
	case _, ok := <-sub.Data:
		require.False(t, ok, "data channel must close after the accepted burst")
	case <-time.After(time.Second):
		t.Fatal("data channel did not close after the accepted burst")
	}
	select {
	case ev, ok := <-sub.Exit:
		require.True(t, ok, "exit channel closed without the daemon outcome")
		require.Equal(t, "natural", ev.Reason)
	case <-time.After(time.Second):
		t.Fatal("exit did not follow the accepted burst")
	}
	_, ok := <-sub.Exit
	require.False(t, ok, "exit channel must close after exactly one outcome")
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

func TestClientAdapter_GivenUnknownTerminalFloodWhenDeliveredThenDoesNotReplayOrAllocateSubscriptions(t *testing.T) {
	const unknownCount = 1000
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	for i := 0; i < unknownCount; i++ {
		terminalID := fmt.Sprintf("ghost-%04d", i)
		c.push(t, "terminal.data", protocol.TerminalDataEvent{TerminalID: terminalID, Data: "ignored"})
		c.push(t, "terminal.exit", protocol.TerminalExitEvent{TerminalID: terminalID, Reason: "natural"})
	}

	sub := a.Subscribe("ghost-0500")
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

func TestClientAdapter_GivenUnreadSpoolWhenUnsubscribedThenCancelsWorkerAndDiscardsQueuedFrames(t *testing.T) {
	const frameCount = 128
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	t.Cleanup(func() { a.Abort() })
	sub := a.Subscribe("term-unsubscribe")
	for i := 0; i < frameCount; i++ {
		c.push(t, "terminal.data", protocol.TerminalDataEvent{
			TerminalID: "term-unsubscribe",
			Data:       fmt.Sprintf("queued-%03d", i),
		})
	}
	c.push(t, "terminal.exit", protocol.TerminalExitEvent{
		TerminalID: "term-unsubscribe",
		Reason:     "natural",
	})

	// An open failure can unsubscribe after the daemon has already pushed an
	// exit; that exact generation must still be cancelable without a consumer.
	a.Unsubscribe("term-unsubscribe", sub)

	select {
	case ev, ok := <-sub.Data:
		require.Falsef(t, ok, "unsubscribe leaked queued data: %+v", ev)
	case <-time.After(time.Second):
		t.Fatal("blocked delivery worker did not stop after unsubscribe")
	}
	select {
	case ev, ok := <-sub.Exit:
		require.Falsef(t, ok, "unsubscribe published an exit value: %+v", ev)
	case <-time.After(time.Second):
		t.Fatal("exit channel did not close after unsubscribe")
	}
}

func TestClientAdapter_GivenAcceptedBurstWhenConnectionClosesThenDrainsDataBeforeClosingExitWithoutValue(t *testing.T) {
	const frameCount = 128
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	sub := a.Subscribe("term-connection-close")
	for i := 0; i < frameCount; i++ {
		c.push(t, "terminal.data", protocol.TerminalDataEvent{
			TerminalID: "term-connection-close",
			Data:       fmt.Sprintf("accepted-%03d", i),
		})
	}

	a.Abort()
	probe := a.Subscribe("connection-close-probe")
	select {
	case _, ok := <-probe.Exit:
		require.False(t, ok, "connection-close probe published an exit value")
	case <-time.After(time.Second):
		t.Fatal("connection close was not observed")
	}
	select {
	case ev, ok := <-sub.Exit:
		t.Fatalf("connection close ended the subscription before accepted data drained: ok=%v event=%+v", ok, ev)
	default:
	}

	for i := 0; i < frameCount; i++ {
		select {
		case ev, ok := <-sub.Data:
			require.Truef(t, ok, "data closed after %d of %d accepted frames", i, frameCount)
			require.Equal(t, fmt.Sprintf("accepted-%03d", i), ev.Data)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for accepted frame %d of %d", i, frameCount)
		}
	}
	select {
	case _, ok := <-sub.Data:
		require.False(t, ok, "data channel must close after draining accepted frames")
	case <-time.After(time.Second):
		t.Fatal("data channel did not close after connection-close drain")
	}
	select {
	case ev, ok := <-sub.Exit:
		require.Falsef(t, ok, "connection close published an exit value: %+v", ev)
	case <-time.After(time.Second):
		t.Fatal("exit channel did not close after connection-close drain")
	}
}

func TestClientAdapter_GivenDeliveryExitUnsubscribeAndWatchCloseRacesThenNeverUsesOrLeaksClosedGeneration(t *testing.T) {
	const (
		iterations     = 300
		framesPerBurst = 64
	)
	c := newStubDaemonClient()
	a := remote.NewClientAdapter(c)
	dataHandler := func(id string) error {
		return c.dispatch("terminal.data", protocol.TerminalDataEvent{TerminalID: id, Data: "chunk"})
	}
	exitHandler := func(id string) error {
		return c.dispatch("terminal.exit", protocol.TerminalExitEvent{TerminalID: id, Reason: "natural"})
	}

	var operations sync.WaitGroup
	var consumers sync.WaitGroup
	errs := make(chan error, iterations*2)
	for i := 0; i < iterations; i++ {
		id := fmt.Sprintf("race-%03d", i)
		sub := a.Subscribe(id)
		consumers.Add(1)
		go func() {
			defer consumers.Done()
			for range sub.Data {
			}
			for range sub.Exit {
			}
		}()
		start := make(chan struct{})
		operations.Add(3)
		go func() {
			defer operations.Done()
			<-start
			for range framesPerBurst {
				if err := dataHandler(id); err != nil {
					errs <- err
					return
				}
			}
		}()
		go func() {
			defer operations.Done()
			<-start
			if err := exitHandler(id); err != nil {
				errs <- err
			}
		}()
		go func() {
			defer operations.Done()
			<-start
			a.Unsubscribe(id, sub)
		}()
		close(start)
	}
	operations.Add(1)
	go func() {
		defer operations.Done()
		a.Abort()
	}()
	operations.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	consumerDone := make(chan struct{})
	go func() {
		consumers.Wait()
		close(consumerDone)
	}()
	select {
	case <-consumerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("subscription delivery workers did not all terminate")
	}
}
