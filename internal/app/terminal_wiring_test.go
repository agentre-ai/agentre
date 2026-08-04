package app

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/remote"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"

	"github.com/stretchr/testify/require"
)

type terminalWiringDaemonClient struct {
	mu           sync.Mutex
	handlers     map[string]func(context.Context, json.RawMessage) (any, error)
	handlerCalls map[string]int
	closed       chan struct{}
	closeOnce    sync.Once
	openErr      error
	closeErrors  []error
	closeCalls   int
}

func newTerminalWiringDaemonClient() *terminalWiringDaemonClient {
	return &terminalWiringDaemonClient{
		handlers:     map[string]func(context.Context, json.RawMessage) (any, error){},
		handlerCalls: map[string]int{},
		closed:       make(chan struct{}),
	}
}

func (c *terminalWiringDaemonClient) Call(_ context.Context, method string, params any, result any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch method {
	case "terminal.open":
		if c.openErr != nil {
			return c.openErr
		}
		result.(*protocol.TerminalOpenResult).TerminalID = params.(protocol.TerminalOpenParams).TerminalID
	case "terminal.close":
		c.closeCalls++
		if c.closeCalls <= len(c.closeErrors) {
			return c.closeErrors[c.closeCalls-1]
		}
	}
	return nil
}

func (c *terminalWiringDaemonClient) Handle(
	method string,
	fn func(context.Context, json.RawMessage) (any, error),
) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.handlerCalls[method]++
	c.handlers[method] = fn
}

func (c *terminalWiringDaemonClient) Closed() <-chan struct{} { return c.closed }

func (c *terminalWiringDaemonClient) dispatch(method string, payload any) error {
	c.mu.Lock()
	fn := c.handlers[method]
	c.mu.Unlock()
	if fn == nil {
		return errors.New("handler not registered")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fn(context.Background(), raw)
	return err
}

func (c *terminalWiringDaemonClient) push(t *testing.T, method string, payload any) {
	t.Helper()
	require.NoError(t, c.dispatch(method, payload), "dispatch %s", method)
}

func (c *terminalWiringDaemonClient) handlerCallCount(method string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.handlerCalls[method]
}

func (c *terminalWiringDaemonClient) closeConnection() {
	c.closeOnce.Do(func() { close(c.closed) })
}

type terminalWiringBorrower struct {
	mu       sync.Mutex
	client   remote.DaemonClient
	borrows  int
	releases int
}

func (b *terminalWiringBorrower) Borrow(
	context.Context,
	int64,
) (remote.DaemonClient, func(), error) {
	b.mu.Lock()
	b.borrows++
	client := b.client
	b.mu.Unlock()
	var once sync.Once
	return client, func() {
		once.Do(func() {
			b.mu.Lock()
			b.releases++
			b.mu.Unlock()
		})
	}, nil
}

func (b *terminalWiringBorrower) setClient(client remote.DaemonClient) {
	b.mu.Lock()
	b.client = client
	b.mu.Unlock()
}

func (b *terminalWiringBorrower) counts() (int, int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.borrows, b.releases
}

func requireTerminalWiringData(
	t *testing.T,
	client *terminalWiringDaemonClient,
	h pty.Handle,
	terminalID string,
	data string,
) {
	t.Helper()
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	require.Eventually(t, func() bool {
		if err := client.dispatch("terminal.data", protocol.TerminalDataEvent{
			TerminalID: terminalID,
			Data:       encoded,
		}); err != nil {
			return false
		}
		select {
		case got := <-h.Data():
			return string(got) == data
		default:
			return false
		}
	}, time.Second, time.Millisecond)
}

func requireTerminalWiringOutcome(t *testing.T, h pty.Handle, reason string) {
	t.Helper()
	select {
	case info, ok := <-h.Exit():
		require.True(t, ok)
		require.Equal(t, reason, info.Reason)
	case <-time.After(time.Second):
		t.Fatal("terminal outcome not delivered")
	}
}

func requireTerminalWiringExit(
	t *testing.T,
	client *terminalWiringDaemonClient,
	h pty.Handle,
	terminalID string,
) {
	t.Helper()
	client.push(t, "terminal.exit", protocol.TerminalExitEvent{
		TerminalID: terminalID,
		Reason:     "natural",
	})
	requireTerminalWiringOutcome(t, h, "natural")
}

func terminalWiringCachedGeneration(
	wiring *terminalRemoteWiring,
	deviceID int64,
) (int, <-chan struct{}) {
	wiring.adapters.mu.Lock()
	defer wiring.adapters.mu.Unlock()
	entry := wiring.adapters.entries[deviceID]
	if entry == nil {
		return len(wiring.adapters.entries), nil
	}
	return len(wiring.adapters.entries), entry.closed
}

func TestTerminalProductionWiring_GivenTwoLiveTerminalsOnOneDaemonClient_WhenOpened_ThenSharesDemuxAndOwnsOneLeaseEach(t *testing.T) {
	client := newTerminalWiringDaemonClient()
	t.Cleanup(client.closeConnection)
	borrower := &terminalWiringBorrower{client: client}
	wiring := newTerminalRemoteWiring(borrower.Borrow)

	firstBackend, err := wiring.Backend("42")
	require.NoError(t, err)
	secondBackend, err := wiring.Backend("42")
	require.NoError(t, err)
	borrows, releases := borrower.counts()
	require.Zero(t, borrows, "backend selection must not borrow before PTYBackend.Open")
	require.Zero(t, releases)

	first, err := firstBackend.Open(context.Background(), pty.Spec{TerminalID: "terminal-first", Cwd: "/repo"})
	require.NoError(t, err)
	second, err := secondBackend.Open(context.Background(), pty.Spec{
		TerminalID: "terminal-second", Cwd: "/repo", Command: "go test",
	})
	require.NoError(t, err)
	borrows, releases = borrower.counts()
	require.Equal(t, 2, borrows)
	require.Zero(t, releases, "live handles must retain their independent leases")
	require.Equal(t, 1, client.handlerCallCount("terminal.data"))
	require.Equal(t, 1, client.handlerCallCount("terminal.exit"))

	requireTerminalWiringData(t, client, first, "terminal-first", "first")
	requireTerminalWiringData(t, client, second, "terminal-second", "second")
	requireTerminalWiringExit(t, client, first, "terminal-first")
	requireTerminalWiringExit(t, client, second, "terminal-second")
	require.Eventually(t, func() bool {
		_, released := borrower.counts()
		return released == 2
	}, time.Second, time.Millisecond)

	require.NoError(t, first.Close())
	require.NoError(t, second.Close())
	_, releases = borrower.counts()
	require.Equal(t, 2, releases, "settled handles must not release twice")
}

func TestTerminalProductionWiring_GivenConcurrentOpensOnSharedClient_WhenStarted_ThenRegistersHandlersOnceAndReleasesEveryLease(t *testing.T) {
	const opens = 32
	client := newTerminalWiringDaemonClient()
	t.Cleanup(client.closeConnection)
	borrower := &terminalWiringBorrower{client: client}
	wiring := newTerminalRemoteWiring(borrower.Borrow)
	type openResult struct {
		handle pty.Handle
		err    error
	}
	results := make(chan openResult, opens)

	for i := range opens {
		go func() {
			backend, err := wiring.Backend("42")
			if err != nil {
				results <- openResult{err: err}
				return
			}
			h, err := backend.Open(context.Background(), pty.Spec{
				TerminalID: "terminal-" + strconv.Itoa(i), Cwd: "/repo",
			})
			results <- openResult{handle: h, err: err}
		}()
	}

	handles := make([]pty.Handle, 0, opens)
	for range opens {
		result := <-results
		require.NoError(t, result.err)
		handles = append(handles, result.handle)
	}
	require.Equal(t, 1, client.handlerCallCount("terminal.data"))
	require.Equal(t, 1, client.handlerCallCount("terminal.exit"))
	borrows, releases := borrower.counts()
	require.Equal(t, opens, borrows)
	require.Zero(t, releases)

	for _, h := range handles {
		require.NoError(t, h.Close())
	}
	for _, h := range handles {
		requireTerminalWiringOutcome(t, h, "killed")
	}
	require.Eventually(t, func() bool {
		_, released := borrower.counts()
		return released == opens
	}, time.Second, time.Millisecond)
}

func TestTerminalProductionWiring_GivenBackendSelected_WhenStartStopsBeforeOpen_ThenBorrowsNothing(t *testing.T) {
	borrower := &terminalWiringBorrower{client: newTerminalWiringDaemonClient()}
	wiring := newTerminalRemoteWiring(borrower.Borrow)

	backend, err := wiring.Backend("42")

	require.NoError(t, err)
	require.NotNil(t, backend)
	borrows, releases := borrower.counts()
	require.Zero(t, borrows)
	require.Zero(t, releases)
}

func TestTerminalProductionWiring_GivenRemoteOpenFails_WhenOpened_ThenReleasesBorrowImmediately(t *testing.T) {
	openErr := errors.New("terminal.open failed")
	client := newTerminalWiringDaemonClient()
	client.openErr = openErr
	t.Cleanup(client.closeConnection)
	borrower := &terminalWiringBorrower{client: client}
	wiring := newTerminalRemoteWiring(borrower.Borrow)
	backend, err := wiring.Backend("42")
	require.NoError(t, err)

	h, err := backend.Open(context.Background(), pty.Spec{TerminalID: "terminal-open-failure", Cwd: "/repo"})

	require.Nil(t, h)
	require.ErrorIs(t, err, openErr)
	borrows, releases := borrower.counts()
	require.Equal(t, 1, borrows)
	require.Equal(t, 1, releases)
}

func TestTerminalProductionWiring_GivenCloseFails_WhenRetried_ThenRetainsLeaseUntilConfirmedOutcome(t *testing.T) {
	closeErr := errors.New("terminal.close failed")
	client := newTerminalWiringDaemonClient()
	client.closeErrors = []error{closeErr, nil}
	t.Cleanup(client.closeConnection)
	borrower := &terminalWiringBorrower{client: client}
	wiring := newTerminalRemoteWiring(borrower.Borrow)
	backend, err := wiring.Backend("42")
	require.NoError(t, err)
	h, err := backend.Open(context.Background(), pty.Spec{TerminalID: "terminal-close-retry", Cwd: "/repo"})
	require.NoError(t, err)

	require.ErrorIs(t, h.Close(), closeErr)
	_, releases := borrower.counts()
	require.Zero(t, releases)

	require.NoError(t, h.Close())
	select {
	case info := <-h.Exit():
		require.Equal(t, "killed", info.Reason)
	case <-time.After(time.Second):
		t.Fatal("confirmed close did not publish a killed outcome")
	}
	require.Eventually(t, func() bool {
		_, released := borrower.counts()
		return released == 1
	}, time.Second, time.Millisecond)
}

func TestTerminalProductionWiring_GivenReplacementClientForSameDevice_WhenOpened_ThenUsesNewAdapterWithoutDisruptingOldHandle(t *testing.T) {
	oldClient := newTerminalWiringDaemonClient()
	newClient := newTerminalWiringDaemonClient()
	t.Cleanup(oldClient.closeConnection)
	t.Cleanup(newClient.closeConnection)
	borrower := &terminalWiringBorrower{client: oldClient}
	wiring := newTerminalRemoteWiring(borrower.Borrow)
	oldBackend, err := wiring.Backend("42")
	require.NoError(t, err)
	oldHandle, err := oldBackend.Open(context.Background(), pty.Spec{TerminalID: "terminal-old", Cwd: "/old"})
	require.NoError(t, err)
	requireTerminalWiringData(t, oldClient, oldHandle, "terminal-old", "old-ready")

	borrower.setClient(newClient)
	newBackend, err := wiring.Backend("42")
	require.NoError(t, err)
	newHandle, err := newBackend.Open(context.Background(), pty.Spec{TerminalID: "terminal-new", Cwd: "/new"})
	require.NoError(t, err)

	require.Equal(t, 1, oldClient.handlerCallCount("terminal.data"))
	require.Equal(t, 1, oldClient.handlerCallCount("terminal.exit"))
	require.Equal(t, 1, newClient.handlerCallCount("terminal.data"))
	require.Equal(t, 1, newClient.handlerCallCount("terminal.exit"))
	cacheSize, generation := terminalWiringCachedGeneration(wiring, 42)
	require.Equal(t, 1, cacheSize, "the cache keeps only the current generation per device")
	require.Equal(t, (<-chan struct{})(newClient.closed), generation)
	requireTerminalWiringData(t, oldClient, oldHandle, "terminal-old", "old-still-live")
	requireTerminalWiringData(t, newClient, newHandle, "terminal-new", "new-live")

	oldClient.closeConnection()
	requireTerminalWiringOutcome(t, oldHandle, "connection_lost")
	require.Eventually(t, func() bool {
		size, current := terminalWiringCachedGeneration(wiring, 42)
		return size == 1 && current == newClient.closed
	}, time.Second, time.Millisecond, "closing the old generation must not evict the replacement")
	requireTerminalWiringData(t, newClient, newHandle, "terminal-new", "new-still-live")
	requireTerminalWiringExit(t, newClient, newHandle, "terminal-new")
	require.Eventually(t, func() bool {
		_, released := borrower.counts()
		return released == 2
	}, time.Second, time.Millisecond)
}
