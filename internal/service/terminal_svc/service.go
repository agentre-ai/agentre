package terminal_svc

import (
	"context"
	"errors"
	"sync"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/pty"
	"agentre/pkg/agentred/protocol"
)

// SessionLookup decouples Service from chat_repo so it can be unit tested.
// Production binding lives in app.go and wraps chat_svc.ResolveSessionCwd
// plus chat_repo Find / agent_backend_repo Find.
type SessionLookup interface {
	Lookup(ctx context.Context, sessionID int64) (*chat_entity.Session, *agent_backend_entity.AgentBackend, string, error)
}

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrTerminalClosed  = errors.New("terminal closed")
	ErrTerminalNotOpen = errors.New("terminal not open for this session")
)

type Service struct {
	lookup   SessionLookup
	selector *BackendSelector
	emitter  Emitter

	mu       sync.Mutex
	sessions map[int64]pty.Handle
	inFlight map[int64]context.CancelFunc // cancel fns for pending Opens
}

func NewService(lookup SessionLookup, sel *BackendSelector, emitter Emitter) *Service {
	if emitter == nil {
		emitter = NoopEmitter{}
	}
	return &Service{
		lookup:   lookup,
		selector: sel,
		emitter:  emitter,
		sessions: map[int64]pty.Handle{},
		inFlight: map[int64]context.CancelFunc{},
	}
}

func (s *Service) Open(ctx context.Context, sessionID int64, cols, rows uint16) error {
	sess, be, cwd, err := s.lookup.Lookup(ctx, sessionID)
	if err != nil {
		return err
	}
	if sess == nil {
		return ErrSessionNotFound
	}
	backend, err := s.selector.Pick(be)
	if err != nil {
		return err
	}

	// 1. Evict any existing handle.
	s.mu.Lock()
	old, hasOld := s.sessions[sessionID]
	if hasOld {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if hasOld {
		_ = old.Close()
	}

	// 2. Register a cancel function so Close can preempt us while we wait on
	//    the (potentially slow) backend.Open call.
	openCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.inFlight[sessionID] = cancel
	s.mu.Unlock()

	h, err := backend.Open(openCtx, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})

	// 3. Atomically unregister inFlight and (on success) register handle.
	s.mu.Lock()
	delete(s.inFlight, sessionID)
	if err == nil {
		s.sessions[sessionID] = h
	}
	s.mu.Unlock()
	// Release the cancel goroutine resources; idempotent if already cancelled.
	cancel()

	if err != nil {
		return err
	}
	// Use the original ctx for the pump so it survives openCtx cancellation.
	go s.pump(ctx, sessionID, h)
	return nil
}

func (s *Service) Write(ctx context.Context, sessionID int64, data string) error {
	h := s.lookupHandle(sessionID)
	if h == nil {
		return ErrTerminalClosed
	}
	_, err := h.Write([]byte(data))
	return err
}

func (s *Service) Resize(ctx context.Context, sessionID int64, cols, rows uint16) error {
	h := s.lookupHandle(sessionID)
	if h == nil {
		return ErrTerminalClosed
	}
	return h.Resize(cols, rows)
}

func (s *Service) Close(ctx context.Context, sessionID int64) error {
	s.mu.Lock()
	cancel, hadInFlight := s.inFlight[sessionID]
	if hadInFlight {
		delete(s.inFlight, sessionID)
	}
	h, hadHandle := s.sessions[sessionID]
	if hadHandle {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()

	if hadInFlight {
		cancel() // preempt the in-flight Open
	}
	if !hadHandle && !hadInFlight {
		return ErrTerminalNotOpen
	}
	if hadHandle {
		return h.Close()
	}
	return nil // only inFlight was cancelled; no Handle to close
}

func (s *Service) Shutdown() {
	s.mu.Lock()
	hs := make([]pty.Handle, 0, len(s.sessions))
	for _, h := range s.sessions {
		hs = append(hs, h)
	}
	s.sessions = map[int64]pty.Handle{}
	s.mu.Unlock()
	for _, h := range hs {
		_ = h.Close()
	}
}

func (s *Service) lookupHandle(sessionID int64) pty.Handle {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[sessionID]
}

func (s *Service) pump(ctx context.Context, sessionID int64, h pty.Handle) {
	for {
		select {
		case data, ok := <-h.Data():
			if !ok {
				return
			}
			s.emitter.Emit(ctx, DataEventName(sessionID), map[string]string{"data": string(data)})
		case info, ok := <-h.Exit():
			s.mu.Lock()
			if cur, exists := s.sessions[sessionID]; exists && cur == h {
				delete(s.sessions, sessionID)
			}
			s.mu.Unlock()
			if ok {
				s.emitter.Emit(ctx, ExitEventName(sessionID), protocol.TerminalExitEvent{
					Code: info.Code, Reason: info.Reason, Msg: info.Msg,
				})
			}
			return
		}
	}
}
