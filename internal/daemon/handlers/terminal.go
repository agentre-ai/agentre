// internal/daemon/handlers/terminal.go
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"

	"agentre/internal/pkg/pty"
	"agentre/pkg/agentred/protocol"
)

//go:generate mockgen -source=terminal.go -destination=mock_handlers/mock_terminal.go -package=mock_handlers

// PTYBackend / PTYHandle are named ports the daemon side speaks to. They
// mirror internal/pkg/pty.{Backend,Handle} but are declared here so
// mockgen can produce local mocks without crossing package boundaries.
type PTYBackend interface {
	Open(ctx context.Context, spec pty.Spec) (PTYHandle, error)
}

type PTYHandle interface {
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Close() error
	Data() <-chan []byte
	Exit() <-chan pty.ExitInfo
}

// Emitter is the daemon's push-event sink.
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

const (
	EventNameTerminalData = "terminal.data"
	EventNameTerminalExit = "terminal.exit"
)

var ErrTerminalNotFound = errors.New("terminal not found")

type TerminalHandlers struct {
	be      PTYBackend
	emitter Emitter

	mu        sync.Mutex
	terminals map[string]PTYHandle
}

func NewTerminalHandlers(be PTYBackend, emitter Emitter) *TerminalHandlers {
	return &TerminalHandlers{
		be:        be,
		emitter:   emitter,
		terminals: map[string]PTYHandle{},
	}
}

func (h *TerminalHandlers) Open(ctx context.Context, p protocol.TerminalOpenParams) (protocol.TerminalOpenResult, error) {
	hd, err := h.be.Open(ctx, pty.Spec{
		Cwd: p.Cwd, Shell: p.Shell, Env: p.Env,
		Cols: p.Cols, Rows: p.Rows,
	})
	if err != nil {
		return protocol.TerminalOpenResult{}, err
	}
	id := newTerminalID()
	h.mu.Lock()
	h.terminals[id] = hd
	h.mu.Unlock()
	go h.pump(ctx, id, hd)
	return protocol.TerminalOpenResult{TerminalID: id}, nil
}

func (h *TerminalHandlers) pump(ctx context.Context, id string, hd PTYHandle) {
	// 256-cap buffered channel: pump reads from hd.Data() and forwards to
	// this queue. If full, drop the oldest chunk, insert a throttle marker,
	// then enqueue the new chunk. Avoids blocking PTY stdout under
	// bursty/slow-consumer load.
	const bufCap = 256
	queue := make(chan []byte, bufCap)
	throttleMarker := []byte("\r\n[--- output throttled ---]\r\n")

	// forwarder goroutine: drains queue → emitter.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for data := range queue {
			h.emitter.Emit(ctx, EventNameTerminalData, protocol.TerminalDataEvent{
				TerminalID: id, Data: string(data),
			})
		}
	}()

	defer func() {
		close(queue)
		<-done // wait for forwarder to drain remaining items
	}()

	for {
		select {
		case data, ok := <-hd.Data():
			if !ok {
				return
			}
			select {
			case queue <- data:
				// enqueued normally
			default:
				// Queue full: drop oldest, insert marker, then enqueue current.
				select {
				case <-queue:
				default:
				}
				// Push marker (non-blocking; a racing consumer may have already
				// taken the freed slot — silently drop marker if still full).
				select {
				case queue <- throttleMarker:
				default:
				}
				// Try to enqueue the current chunk; drop if still full.
				select {
				case queue <- data:
				default:
				}
			}
		case info, ok := <-hd.Exit():
			h.mu.Lock()
			delete(h.terminals, id)
			h.mu.Unlock()
			if ok {
				h.emitter.Emit(ctx, EventNameTerminalExit, protocol.TerminalExitEvent{
					TerminalID: id, Code: info.Code, Reason: info.Reason, Msg: info.Msg,
				})
			}
			return
		}
	}
}

func newTerminalID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type TerminalAck struct{}

func (h *TerminalHandlers) Write(ctx context.Context, p protocol.TerminalWriteParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	_, err := hd.Write([]byte(p.Data))
	return TerminalAck{}, err
}

func (h *TerminalHandlers) Resize(ctx context.Context, p protocol.TerminalResizeParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	return TerminalAck{}, hd.Resize(p.Cols, p.Rows)
}

func (h *TerminalHandlers) Close(ctx context.Context, p protocol.TerminalCloseParams) (TerminalAck, error) {
	h.mu.Lock()
	hd, ok := h.terminals[p.TerminalID]
	h.mu.Unlock()
	if !ok {
		return TerminalAck{}, ErrTerminalNotFound
	}
	return TerminalAck{}, hd.Close()
}
