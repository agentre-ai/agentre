// internal/daemon/handlers/terminal.go
package handlers

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"sync"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
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

	maxTerminalIDLength             = 128
	terminalCancelTombstoneCapacity = 256
)

var (
	ErrTerminalNotFound       = errors.New("terminal not found")
	ErrTerminalIDInvalid      = errors.New("terminal id is invalid")
	ErrTerminalIDInUse        = errors.New("terminal id is already active or pending")
	ErrTerminalOpenCanceled   = errors.New("terminal open canceled")
	ErrTerminalHandlerClosed  = errors.New("terminal handler is closed")
	ErrTerminalCancelCapacity = errors.New("terminal pending cancellation capacity reached")
)

type pendingTerminalOpen struct {
	ctx      context.Context
	cancel   context.CancelFunc
	canceled bool
}

type TerminalHandlers struct {
	be      PTYBackend
	emitter Emitter

	mu               sync.Mutex
	terminals        map[string]PTYHandle
	pending          map[string]*pendingTerminalOpen
	cancelTombstones map[string]struct{}
	closed           bool
}

func NewTerminalHandlers(be PTYBackend, emitter Emitter) *TerminalHandlers {
	return &TerminalHandlers{
		be:               be,
		emitter:          emitter,
		terminals:        map[string]PTYHandle{},
		pending:          map[string]*pendingTerminalOpen{},
		cancelTombstones: map[string]struct{}{},
	}
}

func (h *TerminalHandlers) Open(ctx context.Context, p protocol.TerminalOpenParams) (protocol.TerminalOpenResult, error) {
	id := p.TerminalID
	if id == "" {
		id = newTerminalID()
	} else if err := validateTerminalID(id); err != nil {
		return protocol.TerminalOpenResult{}, err
	}

	attempt, err := h.claimPendingOpen(ctx, id)
	if err != nil {
		return protocol.TerminalOpenResult{}, err
	}
	hd, openErr := h.be.Open(attempt.ctx, pty.Spec{
		Cwd: p.Cwd, Shell: p.Shell, Command: p.Command, Env: p.Env,
		Cols: p.Cols, Rows: p.Rows,
	})

	h.mu.Lock()
	current, stillPending := h.pending[id]
	ownsClaim := stillPending && current == attempt
	if ownsClaim {
		delete(h.pending, id)
	}
	canceled := attempt.canceled
	closed := h.closed
	openCtxErr := attempt.ctx.Err()
	register := openErr == nil && hd != nil && ownsClaim && !canceled && !closed && openCtxErr == nil
	if register {
		h.terminals[id] = hd
	}
	h.mu.Unlock()
	attempt.cancel()

	if register {
		go h.pump(ctx, id, hd)
		return protocol.TerminalOpenResult{TerminalID: id}, nil
	}
	if hd != nil {
		_ = hd.Close()
	}
	switch {
	case closed:
		return protocol.TerminalOpenResult{}, ErrTerminalHandlerClosed
	case canceled || !ownsClaim:
		return protocol.TerminalOpenResult{}, ErrTerminalOpenCanceled
	case openErr != nil:
		return protocol.TerminalOpenResult{}, openErr
	case openCtxErr != nil:
		return protocol.TerminalOpenResult{}, openCtxErr
	default:
		return protocol.TerminalOpenResult{}, ErrTerminalOpenCanceled
	}
}

func (h *TerminalHandlers) claimPendingOpen(ctx context.Context, id string) (*pendingTerminalOpen, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, ErrTerminalHandlerClosed
	}
	if _, canceled := h.cancelTombstones[id]; canceled {
		delete(h.cancelTombstones, id)
		return nil, ErrTerminalOpenCanceled
	}
	if _, active := h.terminals[id]; active {
		return nil, ErrTerminalIDInUse
	}
	if _, pending := h.pending[id]; pending {
		return nil, ErrTerminalIDInUse
	}
	openCtx, cancel := context.WithCancel(ctx)
	attempt := &pendingTerminalOpen{ctx: openCtx, cancel: cancel}
	h.pending[id] = attempt
	return attempt, nil
}

func validateTerminalID(id string) error {
	if len(id) == 0 || len(id) > maxTerminalIDLength || !isTerminalIDAlphaNumeric(id[0]) {
		return ErrTerminalIDInvalid
	}
	for i := 1; i < len(id); i++ {
		if !isTerminalIDAlphaNumeric(id[i]) && id[i] != '-' && id[i] != '_' {
			return ErrTerminalIDInvalid
		}
	}
	return nil
}

func isTerminalIDAlphaNumeric(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
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
			// base64 so multibyte UTF-8 split across PTY reads survives the
			// WebSocket JSON hop instead of being mangled to U+FFFD. The desktop
			// remote backend decodes it back to raw bytes.
			h.emitter.Emit(ctx, EventNameTerminalData, protocol.TerminalDataEvent{
				TerminalID: id, Data: base64.StdEncoding.EncodeToString(data),
			})
		}
	}()

	enqueue := func(data []byte) {
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
	}

	// Data() and Exit() are independent channels with no ordering guarantee.
	// Drain every data chunk AND read the single exit value before tearing
	// down — a naive select that returns on a closed Data() channel races the
	// buffered Exit() value and drops the exit ~50% of the time (remote
	// terminal stuck "open"), or returns on Exit() while data is still
	// buffered and drops the trailing output.
	dataCh := hd.Data()
	exitCh := hd.Exit()
	var exitInfo pty.ExitInfo
stream:
	for {
		select {
		case data, ok := <-dataCh:
			if !ok {
				// Data closed before we observed exit; block for the single
				// exit value (real handles always deliver it).
				exitInfo = <-exitCh
				break stream
			}
			enqueue(data)
		case info := <-exitCh:
			exitInfo = info
			// Drain any already-buffered data so trailing output is queued
			// before the exit event.
			for drained := false; !drained; {
				select {
				case data, ok := <-dataCh:
					if !ok {
						drained = true
					} else {
						enqueue(data)
					}
				default:
					drained = true
				}
			}
			break stream
		}
	}

	// Flush all queued data through the forwarder before emitting exit so
	// trailing output never arrives after the exit event.
	close(queue)
	<-done

	h.mu.Lock()
	delete(h.terminals, id)
	h.mu.Unlock()
	h.emitter.Emit(ctx, EventNameTerminalExit, protocol.TerminalExitEvent{
		TerminalID: id, Code: exitInfo.Code, Reason: exitInfo.Reason, Msg: exitInfo.Msg,
	})
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

// CloseAll terminates every live PTY and pending open owned by this connection.
func (h *TerminalHandlers) CloseAll() {
	h.mu.Lock()
	h.closed = true
	hs := make([]PTYHandle, 0, len(h.terminals))
	for _, hd := range h.terminals {
		hs = append(hs, hd)
	}
	cancels := make([]context.CancelFunc, 0, len(h.pending))
	for _, attempt := range h.pending {
		attempt.canceled = true
		cancels = append(cancels, attempt.cancel)
	}
	h.terminals = map[string]PTYHandle{}
	h.pending = map[string]*pendingTerminalOpen{}
	h.cancelTombstones = map[string]struct{}{}
	h.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
	for _, hd := range hs {
		_ = hd.Close()
	}
}

func (h *TerminalHandlers) Close(ctx context.Context, p protocol.TerminalCloseParams) (TerminalAck, error) {
	if p.CancelPendingOpen {
		if err := validateTerminalID(p.TerminalID); err != nil {
			return TerminalAck{}, err
		}
	}

	h.mu.Lock()
	if hd, ok := h.terminals[p.TerminalID]; ok {
		h.mu.Unlock()
		return TerminalAck{}, hd.Close()
	}
	if !p.CancelPendingOpen {
		h.mu.Unlock()
		return TerminalAck{}, ErrTerminalNotFound
	}
	if h.closed {
		h.mu.Unlock()
		return TerminalAck{}, ErrTerminalHandlerClosed
	}
	if attempt, ok := h.pending[p.TerminalID]; ok {
		if !attempt.canceled {
			attempt.canceled = true
			cancel := attempt.cancel
			h.mu.Unlock()
			cancel()
			return TerminalAck{}, nil
		}
		h.mu.Unlock()
		return TerminalAck{}, nil
	}
	if _, ok := h.cancelTombstones[p.TerminalID]; ok {
		h.mu.Unlock()
		return TerminalAck{}, nil
	}
	if len(h.cancelTombstones) >= terminalCancelTombstoneCapacity {
		h.mu.Unlock()
		return TerminalAck{}, ErrTerminalCancelCapacity
	}
	h.cancelTombstones[p.TerminalID] = struct{}{}
	h.mu.Unlock()
	return TerminalAck{}, nil
}
