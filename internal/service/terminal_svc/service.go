package terminal_svc

import (
	"context"
	"errors"
	"sync"

	"agentre/internal/pkg/pty"
	"agentre/pkg/agentred/protocol"
)

var (
	ErrTerminalClosed  = errors.New("terminal closed")
	ErrTerminalNotOpen = errors.New("terminal not open")
)

type Service struct {
	selector *BackendSelector
	emitter  Emitter

	mu       sync.Mutex
	sessions map[string]pty.Handle
	inFlight map[string]*openAttempt // pending Opens, keyed by terminalID
}

// openAttempt tracks one in-flight backend.Open. Close cancels it; the Open
// itself uses pointer identity to detect that it was preempted (its entry
// removed or replaced) before registering the resulting handle.
type openAttempt struct {
	cancel context.CancelFunc
}

func NewService(sel *BackendSelector, emitter Emitter) *Service {
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	return &Service{
		selector: sel,
		emitter:  emitter,
		sessions: map[string]pty.Handle{},
		inFlight: map[string]*openAttempt{},
	}
}

func (s *Service) Open(ctx context.Context, terminalID string, deviceID string, cwd string, cols, rows uint16) error {
	backend, err := s.selector.Pick(deviceID)
	if err != nil {
		return err
	}

	// 1. Evict any existing handle.
	s.mu.Lock()
	old, hasOld := s.sessions[terminalID]
	if hasOld {
		delete(s.sessions, terminalID)
	}
	s.mu.Unlock()
	if hasOld {
		_ = old.Close()
	}

	// 2. Register a cancel function so Close can preempt us while we wait on
	//    the (potentially slow) backend.Open call.
	openCtx, cancel := context.WithCancel(ctx)
	attempt := &openAttempt{cancel: cancel}
	s.mu.Lock()
	s.inFlight[terminalID] = attempt
	s.mu.Unlock()

	h, err := backend.Open(openCtx, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})

	// 3. Atomically unregister inFlight and (on success) register handle —
	//    unless a concurrent Close (or newer Open) already removed/replaced our
	//    attempt while backend.Open was running.
	s.mu.Lock()
	preempted := s.inFlight[terminalID] != attempt
	if !preempted {
		delete(s.inFlight, terminalID)
		if err == nil {
			s.sessions[terminalID] = h
		}
	}
	s.mu.Unlock()
	// Release the cancel goroutine resources; idempotent if already canceled.
	cancel()

	if err != nil {
		return err
	}
	if preempted {
		// Close already returned to the caller, so it never saw this handle.
		// Tear it down here so the PTY — and any remote daemon-side shell —
		// does not leak.
		_ = h.Close()
		return nil
	}
	// Use the original ctx for the pump so it survives openCtx cancellation.
	go s.pump(ctx, terminalID, h)
	return nil
}

func (s *Service) Write(ctx context.Context, terminalID string, data string) error {
	h := s.lookupHandle(terminalID)
	if h == nil {
		return ErrTerminalClosed
	}
	_, err := h.Write([]byte(data))
	return err
}

func (s *Service) Resize(ctx context.Context, terminalID string, cols, rows uint16) error {
	h := s.lookupHandle(terminalID)
	if h == nil {
		return ErrTerminalClosed
	}
	return h.Resize(cols, rows)
}

func (s *Service) Close(ctx context.Context, terminalID string) error {
	s.mu.Lock()
	attempt, hadInFlight := s.inFlight[terminalID]
	if hadInFlight {
		delete(s.inFlight, terminalID)
	}
	h, hadHandle := s.sessions[terminalID]
	if hadHandle {
		delete(s.sessions, terminalID)
	}
	s.mu.Unlock()

	if hadInFlight {
		attempt.cancel() // preempt the in-flight Open
	}
	if !hadHandle && !hadInFlight {
		return ErrTerminalNotOpen
	}
	if hadHandle {
		return h.Close()
	}
	return nil // only inFlight was canceled; no Handle to close
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	hs := make([]pty.Handle, 0, len(s.sessions))
	for _, h := range s.sessions {
		hs = append(hs, h)
	}
	s.sessions = map[string]pty.Handle{}
	// Clear and cancel in-flight Opens too: clearing inFlight makes each pending
	// Open observe itself as preempted (so a handle returned after Shutdown is
	// torn down, not registered), and canceling unblocks a backend.Open that is
	// waiting on its context. Without this, a slow Open completing after Shutdown
	// would leak a PTY and a pump goroutine past app shutdown.
	attempts := make([]*openAttempt, 0, len(s.inFlight))
	for _, a := range s.inFlight {
		attempts = append(attempts, a)
	}
	s.inFlight = map[string]*openAttempt{}
	s.mu.Unlock()
	for _, a := range attempts {
		a.cancel()
	}
	for _, h := range hs {
		_ = h.Close()
	}
}

func (s *Service) lookupHandle(terminalID string) pty.Handle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[terminalID]
}

func (s *Service) pump(ctx context.Context, terminalID string, h pty.Handle) {
	// Data() and Exit() are independent channels with no ordering guarantee
	// between them. We must drain every data chunk AND read the single exit
	// value before emitting the exit event — otherwise a naive select that
	// returns on a closed Data() channel races the buffered Exit() value and
	// drops the exit ~50% of the time (terminal stuck "open"), or returns on
	// Exit() while data is still buffered and drops the trailing output.
	dataCh := h.Data()
	exitCh := h.Exit()
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
			s.emitter.Emit(ctx, DataEventName(terminalID), map[string]string{"data": string(data)})
		case info := <-exitCh:
			exitInfo = info
			// Drain any already-buffered data so trailing output is emitted
			// before the exit event.
			for drained := false; !drained; {
				select {
				case data, ok := <-dataCh:
					if !ok {
						drained = true
					} else {
						s.emitter.Emit(ctx, DataEventName(terminalID), map[string]string{"data": string(data)})
					}
				default:
					drained = true
				}
			}
			break stream
		}
	}

	s.mu.Lock()
	if cur, exists := s.sessions[terminalID]; exists && cur == h {
		delete(s.sessions, terminalID)
	}
	s.mu.Unlock()
	s.emitter.Emit(ctx, ExitEventName(terminalID), protocol.TerminalExitEvent{
		Code: exitInfo.Code, Reason: exitInfo.Reason, Msg: exitInfo.Msg,
	})
}
