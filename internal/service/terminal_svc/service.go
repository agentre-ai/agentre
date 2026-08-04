package terminal_svc

import (
	"context"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/cago-frame/cago/pkg/gogo"

	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/pkg/agentred/protocol"
)

// Output coalescing: PTY stdout accumulates and is flushed to the emitter at
// most every flushInterval, or sooner once flushThreshold bytes pile up. This
// keeps a high-frequency full-screen TUI (claude, vim) from flooding the Wails
// event bridge with hundreds of tiny events per second — a flood that drops or
// reorders events and desyncs xterm's parser into the garbled output this fixes.
// Mirrors the opskat terminal pipeline.
const (
	flushInterval  = 10 * time.Millisecond
	flushThreshold = 32 * 1024
)

var (
	ErrTerminalClosed  = errors.New("terminal closed")
	ErrTerminalNotOpen = errors.New("terminal not open")
)

type Service struct {
	selector             *BackendSelector
	emitter              Emitter
	commandScopeResolver CommandScopeResolver

	mu       sync.Mutex
	sessions map[string]pty.Handle
	inFlight map[string]*openAttempt // pending starts, keyed by terminalID
}

// openAttempt owns one terminal start continuously from the first blocking
// boundary through backend.Open registration. Close or a newer start cancels
// it and removes/replaces its map entry; pointer identity rejects every stale
// result returned by a cancellation-ignoring dependency.
type openAttempt struct {
	ctx    context.Context
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

// Open opens an interactive login shell (original behavior).
func (s *Service) Open(ctx context.Context, terminalID string, deviceID string, cwd string, cols, rows uint16) error {
	attempt := s.claimStart(ctx, terminalID)
	defer s.releaseStart(terminalID, attempt)
	return s.open(ctx, attempt, terminalID, deviceID, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows}, nil, false)
}

// OpenCommand runs a one-shot command under cwd, reusing the same
// streaming/exit/kill machinery as Open.
func (s *Service) OpenCommand(ctx context.Context, terminalID string, deviceID string, cwd string, command string, cols, rows uint16) error {
	attempt := s.claimStart(ctx, terminalID)
	defer s.releaseStart(terminalID, attempt)
	return s.openCommand(ctx, attempt, terminalID, deviceID, cwd, command, cols, rows, nil)
}

func (s *Service) openCommand(
	ctx context.Context,
	attempt *openAttempt,
	terminalID string,
	deviceID string,
	cwd string,
	command string,
	cols, rows uint16,
	lifecycle *commandLifecycle,
) error {
	return s.open(ctx, attempt, terminalID, deviceID, pty.Spec{
		Cwd: cwd, Command: command, Cols: cols, Rows: rows,
	}, lifecycle, true)
}

func (s *Service) open(
	ctx context.Context,
	attempt *openAttempt,
	terminalID string,
	deviceID string,
	spec pty.Spec,
	lifecycle *commandLifecycle,
	annotateStartFailure bool,
) error {
	backend, err := s.selector.Pick(deviceID)
	if !s.ownsStart(terminalID, attempt) {
		return preemptedStartError(lifecycle)
	}
	if err != nil {
		if annotateStartFailure {
			return annotateCommandStartError(commandStartStageBackendSelect, err)
		}
		return err
	}

	// Keep the attempt registered while evicting an existing handle. Handle
	// Close is an external, non-context-aware boundary and may block; ownership
	// must be rechecked after it returns before backend.Open can launch.
	s.mu.Lock()
	if s.inFlight[terminalID] != attempt {
		s.mu.Unlock()
		return preemptedStartError(lifecycle)
	}
	old, hasOld := s.sessions[terminalID]
	if hasOld {
		delete(s.sessions, terminalID)
	}
	s.mu.Unlock()
	if hasOld {
		_ = old.Close()
		if !s.ownsStart(terminalID, attempt) {
			return preemptedStartError(lifecycle)
		}
	}

	// The already-allocated desktop identity must reach a remote backend before
	// terminal.open so it can subscribe and cancel under that same ID. Local
	// backends intentionally ignore this runtime-only field.
	spec.TerminalID = terminalID
	h, err := backend.Open(attempt.ctx, spec)

	// Atomically hand ownership from the start attempt to the live session.
	// A stale handle returned by a cancellation-ignoring backend is never
	// registered and therefore never gets a listener/pump.
	s.mu.Lock()
	preempted := s.inFlight[terminalID] != attempt
	if !preempted {
		delete(s.inFlight, terminalID)
		if err == nil {
			s.sessions[terminalID] = h
		}
	}
	s.mu.Unlock()

	if err != nil {
		if preempted && lifecycle != nil {
			return ErrCommandStartPreempted
		}
		if annotateStartFailure {
			return annotateCommandStartError(commandStartStagePTYOpen, err)
		}
		return err
	}
	if preempted {
		// Close already returned to the caller, so it never saw this handle.
		// Tear it down here so the PTY — and any remote daemon-side shell —
		// does not leak.
		_ = h.Close()
		return preemptedStartError(lifecycle)
	}
	// Log before starting the pump so even an already-exited handle preserves
	// the command lifecycle order.
	if lifecycle != nil {
		lifecycle.logStarted(ctx)
	}
	// Detach from caller cancellation while preserving logger values so exit
	// cleanup and lifecycle events complete after Open returns.
	pumpCtx := context.WithoutCancel(ctx)
	gogo.Go(func() error {
		s.pump(pumpCtx, terminalID, h, lifecycle)
		return nil
	}, gogo.WithIgnorePanic())
	return nil
}

func (s *Service) claimStart(ctx context.Context, terminalID string) *openAttempt {
	attemptCtx, cancel := context.WithCancel(ctx)
	attempt := &openAttempt{ctx: attemptCtx, cancel: cancel}

	s.mu.Lock()
	previous := s.inFlight[terminalID]
	s.inFlight[terminalID] = attempt
	s.mu.Unlock()

	if previous != nil {
		previous.cancel()
	}
	return attempt
}

func (s *Service) ownsStart(terminalID string, attempt *openAttempt) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.inFlight[terminalID] == attempt
}

func (s *Service) releaseStart(terminalID string, attempt *openAttempt) {
	s.mu.Lock()
	if s.inFlight[terminalID] == attempt {
		delete(s.inFlight, terminalID)
	}
	s.mu.Unlock()
	attempt.cancel()
}

func preemptedStartError(lifecycle *commandLifecycle) error {
	if lifecycle != nil {
		return ErrCommandStartPreempted
	}
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
	s.mu.Unlock()

	if hadInFlight {
		attempt.cancel() // preempt the in-flight Open
	}
	if !hadHandle && !hadInFlight {
		return ErrTerminalNotOpen
	}
	if hadHandle {
		if err := h.Close(); err != nil {
			return err
		}
		// Close is an external boundary, so a new Open may have installed a
		// replacement while it was in flight. Settle only the handle we closed.
		s.mu.Lock()
		if cur, exists := s.sessions[terminalID]; exists && cur == h {
			delete(s.sessions, terminalID)
		}
		s.mu.Unlock()
	}
	return nil // only inFlight was canceled, or the captured Handle settled
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	hs := make([]pty.Handle, 0, len(s.sessions))
	for _, h := range s.sessions {
		hs = append(hs, h)
	}
	s.sessions = map[string]pty.Handle{}
	// Clear and cancel in-flight starts too: clearing inFlight makes each pending
	// start observe itself as preempted (so stale resolver/selector results stop,
	// and a late handle is torn down instead of registered). Cancellation also
	// unblocks context-aware resolver and backend boundaries.
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

func (s *Service) pump(
	ctx context.Context,
	terminalID string,
	h pty.Handle,
	lifecycle *commandLifecycle,
) {
	// Data() and Exit() are independent channels with no ordering guarantee
	// between them. We must drain every data chunk AND read the single exit
	// value before emitting the exit event — otherwise a naive select that
	// returns on a closed Data() channel races the buffered Exit() value and
	// drops the exit ~50% of the time (terminal stuck "open"), or returns on
	// Exit() while data is still buffered and drops the trailing output.
	dataCh := h.Data()
	exitCh := h.Exit()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	var pending []byte
	flush := func() {
		if len(pending) == 0 {
			return
		}
		s.emitter.Emit(ctx, DataEventName(terminalID),
			map[string]string{"data": base64.StdEncoding.EncodeToString(pending)})
		pending = pending[:0]
	}

	var exitInfo pty.ExitInfo
stream:
	for {
		select {
		case data, ok := <-dataCh:
			if !ok {
				// Data closed before we observed exit; flush trailing output,
				// then block for the single exit value (real handles always
				// deliver it).
				flush()
				exitInfo = <-exitCh
				break stream
			}
			pending = append(pending, data...)
			if len(pending) >= flushThreshold {
				flush()
			}
		case <-ticker.C:
			flush()
		case info := <-exitCh:
			exitInfo = info
			// Drain any already-buffered data so trailing output is flushed
			// before the exit event.
			for drained := false; !drained; {
				select {
				case data, ok := <-dataCh:
					if !ok {
						drained = true
					} else {
						pending = append(pending, data...)
					}
				default:
					drained = true
				}
			}
			break stream
		}
	}
	// Flush whatever remains so no trailing output arrives after the exit event.
	flush()

	s.mu.Lock()
	if cur, exists := s.sessions[terminalID]; exists && cur == h {
		delete(s.sessions, terminalID)
	}
	s.mu.Unlock()
	if lifecycle != nil {
		lifecycle.logExited(ctx, exitInfo.Code, exitInfo.Reason)
	}
	s.emitter.Emit(ctx, ExitEventName(terminalID), protocol.TerminalExitEvent{
		Code: exitInfo.Code, Reason: exitInfo.Reason, Msg: exitInfo.Msg,
	})
}
