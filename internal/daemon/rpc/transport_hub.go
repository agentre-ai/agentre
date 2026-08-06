package rpc

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultHubHeartbeatInterval = 15 * time.Second
	defaultHubRetryInitial      = time.Second
	defaultHubRetryMax          = time.Minute
	hubFrameBuffer              = 64
)

// ErrHubUnavailable reports that the relay is not currently connected. Callers
// must wait for OnDial before retrying; HubLink reconnects in the background.
var ErrHubUnavailable = errors.New("rpc: hub relay unavailable")

// HubFrame is one raw WebSocket frame arriving from the relay. The future hub
// multiplexer owns interpreting its payload and must select on Frames instead
// of reading the underlying socket directly.
type HubFrame struct {
	MessageType int
	Payload     []byte
}

// HubLinkOptions configures a daemon-owned outbound relay connection.
type HubLinkOptions struct {
	ServerURL         string
	AccessToken       string
	HeartbeatInterval time.Duration
	RetryInitial      time.Duration
	RetryMax          time.Duration

	// RetryWait is the clock seam for retry tests. Production waits for delay
	// or context cancellation; a test can advance a fake clock immediately.
	RetryWait func(context.Context, time.Duration) error
	// Random produces [0,1] jitter. A test supplies a stable value.
	Random func() float64

	// OnDial and OnDisconnect let the future multiplexer observe link lifecycle.
	// Hooks must return promptly because they run on the link's goroutine.
	OnDial       func()
	OnDisconnect func(error)
}

// HubLink maintains the daemon's one outbound WebSocket to the account server.
// It intentionally has no virtual-channel or JSON-RPC knowledge: task 5 layers
// that protocol on Send and Frames.
type HubLink struct {
	opts HubLinkOptions

	mu      sync.RWMutex
	conn    *websocket.Conn
	writeMu sync.Mutex
	frames  chan HubFrame

	lifecycleMu        sync.RWMutex
	lifecycleListeners []hubLifecycleListener
}

type hubLifecycleListener struct {
	onDial       func()
	onDisconnect func(error)
}

// NewHubLink creates an outbound relay manager. Run starts its lifetime.
func NewHubLink(opts HubLinkOptions) *HubLink {
	if opts.HeartbeatInterval <= 0 {
		opts.HeartbeatInterval = defaultHubHeartbeatInterval
	}
	if opts.RetryInitial <= 0 {
		opts.RetryInitial = defaultHubRetryInitial
	}
	if opts.RetryMax <= 0 {
		opts.RetryMax = defaultHubRetryMax
	}
	if opts.RetryMax < opts.RetryInitial {
		opts.RetryMax = opts.RetryInitial
	}
	if opts.RetryWait == nil {
		opts.RetryWait = waitForHubRetry
	}
	if opts.Random == nil {
		opts.Random = rand.Float64
	}
	return &HubLink{opts: opts, frames: make(chan HubFrame, hubFrameBuffer)}
}

// Run connects until ctx is canceled. A relay failure is intentionally not
// returned to the daemon: it is logged, backed off, and retried while LAN work
// and local sessions continue independently.
func (l *HubLink) Run(ctx context.Context) error {
	failures := 0
	for {
		if ctx.Err() != nil {
			return nil
		}
		conn, err := l.dial(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			log.Printf("rpc.HubLink: relay dial failed; retrying: %v", err)
			if err := l.wait(ctx, failures); err != nil {
				return nil
			}
			failures++
			continue
		}

		l.setConn(conn)
		l.notifyDial()
		var renewed bool
		err, renewed = l.serve(ctx, conn)
		l.clearConn(conn)
		_ = conn.Close()
		if ctx.Err() != nil {
			l.notifyDisconnect(ctx.Err())
			return nil
		}
		if renewed {
			failures = 0
		}
		l.notifyDisconnect(err)
		log.Printf("rpc.HubLink: relay disconnected; retrying: %v", err)
		if err := l.wait(ctx, failures); err != nil {
			return nil
		}
		failures++
	}
}

// Send writes one raw relay frame. It does not queue while disconnected so the
// future multiplexer can fail or retry each virtual-channel operation honestly.
func (l *HubLink) Send(messageType int, payload []byte) error {
	l.mu.RLock()
	conn := l.conn
	l.mu.RUnlock()
	if conn == nil {
		return ErrHubUnavailable
	}
	if err := l.writeMessage(conn, messageType, payload); err != nil {
		_ = conn.Close()
		return fmt.Errorf("write relay frame: %w", err)
	}
	return nil
}

// Receive returns the raw inbound frame stream. It remains open across a
// reconnect; callers use lifecycle hooks or their parent context to delimit a
// particular physical connection.
func (l *HubLink) Receive() <-chan HubFrame { return l.frames }

// Connected reports whether the relay currently has a physical WebSocket.
func (l *HubLink) Connected() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.conn != nil
}

// AddLifecycleListener registers a transport observer without replacing the
// composition-root callbacks supplied in HubLinkOptions. A listener added
// after a successful dial is immediately told about that live connection.
func (l *HubLink) AddLifecycleListener(onDial func(), onDisconnect func(error)) {
	l.lifecycleMu.Lock()
	defer l.lifecycleMu.Unlock()
	l.lifecycleListeners = append(l.lifecycleListeners, hubLifecycleListener{
		onDial:       onDial,
		onDisconnect: onDisconnect,
	})
	if onDial != nil && l.Connected() {
		onDial()
	}
}

func (l *HubLink) notifyDial() {
	if l.opts.OnDial != nil {
		l.opts.OnDial()
	}
	l.lifecycleMu.RLock()
	listeners := append([]hubLifecycleListener(nil), l.lifecycleListeners...)
	l.lifecycleMu.RUnlock()
	for _, listener := range listeners {
		if listener.onDial != nil {
			listener.onDial()
		}
	}
}

func (l *HubLink) notifyDisconnect(err error) {
	if l.opts.OnDisconnect != nil {
		l.opts.OnDisconnect(err)
	}
	l.lifecycleMu.RLock()
	listeners := append([]hubLifecycleListener(nil), l.lifecycleListeners...)
	l.lifecycleMu.RUnlock()
	for _, listener := range listeners {
		if listener.onDisconnect != nil {
			listener.onDisconnect(err)
		}
	}
}

func (l *HubLink) dial(ctx context.Context) (*websocket.Conn, error) {
	endpoint, err := hubEndpoint(l.opts.ServerURL)
	if err != nil {
		return nil, err
	}
	headers := make(http.Header)
	headers.Set("Authorization", "Bearer "+l.opts.AccessToken)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint, headers)
	if err != nil {
		return nil, fmt.Errorf("dial relay websocket: %w", err)
	}
	return conn, nil
}

func (l *HubLink) serve(ctx context.Context, conn *websocket.Conn) (error, bool) {
	readDeadline := 2 * l.opts.HeartbeatInterval
	if err := conn.SetReadDeadline(time.Now().Add(readDeadline)); err != nil {
		return fmt.Errorf("set relay read deadline: %w", err), false
	}
	var renewed atomic.Bool
	conn.SetPongHandler(func(string) error {
		renewed.Store(true)
		return conn.SetReadDeadline(time.Now().Add(readDeadline))
	})

	stopHeartbeat := make(chan struct{})
	defer close(stopHeartbeat)
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-stopHeartbeat:
		}
	}()
	go l.heartbeat(ctx, conn, stopHeartbeat)

	for {
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read relay frame: %w", err), renewed.Load()
		}
		frame := HubFrame{MessageType: messageType, Payload: append([]byte(nil), payload...)}
		select {
		case l.frames <- frame:
		case <-ctx.Done():
			return ctx.Err(), renewed.Load()
		default:
			_ = conn.Close()
			return errors.New("relay frame consumer is not keeping up"), renewed.Load()
		}
	}
}

func (l *HubLink) heartbeat(ctx context.Context, conn *websocket.Conn, stop <-chan struct{}) {
	ticker := time.NewTicker(l.opts.HeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-ticker.C:
			if err := l.writeControl(conn, websocket.PingMessage, nil,
				time.Now().Add(l.opts.HeartbeatInterval)); err != nil {
				_ = conn.Close()
				return
			}
		}
	}
}

func (l *HubLink) writeMessage(conn *websocket.Conn, messageType int, payload []byte) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return conn.WriteMessage(messageType, payload)
}

func (l *HubLink) writeControl(conn *websocket.Conn, messageType int, payload []byte, deadline time.Time) error {
	l.writeMu.Lock()
	defer l.writeMu.Unlock()
	return conn.WriteControl(messageType, payload, deadline)
}

func (l *HubLink) setConn(conn *websocket.Conn) {
	l.mu.Lock()
	l.conn = conn
	l.mu.Unlock()
}

func (l *HubLink) clearConn(conn *websocket.Conn) {
	l.mu.Lock()
	if l.conn == conn {
		l.conn = nil
	}
	l.mu.Unlock()
}

func (l *HubLink) wait(ctx context.Context, failures int) error {
	return l.opts.RetryWait(ctx, l.backoff(failures))
}

func (l *HubLink) backoff(failures int) time.Duration {
	delay := l.opts.RetryInitial
	for range failures {
		if delay >= l.opts.RetryMax/2 {
			delay = l.opts.RetryMax
			break
		}
		delay *= 2
	}
	jitter := l.opts.Random()
	if jitter < 0 {
		jitter = 0
	} else if jitter > 1 {
		jitter = 1
	}
	return time.Duration(float64(delay) * (0.5 + jitter/2))
}

func waitForHubRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func hubEndpoint(serverURL string) (string, error) {
	parsed, err := url.Parse(serverURL)
	if err != nil || parsed.Host == "" {
		return "", errors.New("relay server URL must be an http(s) base URL")
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", errors.New("relay server URL must be an http(s) base URL")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/relay/daemon"
	parsed.RawPath = ""
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}
