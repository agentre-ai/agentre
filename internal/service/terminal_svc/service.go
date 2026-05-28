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

	s.mu.Lock()
	old, hasOld := s.sessions[sessionID]
	if hasOld {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if hasOld {
		_ = old.Close()
	}

	h, err := backend.Open(ctx, pty.Spec{Cwd: cwd, Cols: cols, Rows: rows})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.sessions[sessionID] = h
	s.mu.Unlock()

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
	h, ok := s.sessions[sessionID]
	if ok {
		delete(s.sessions, sessionID)
	}
	s.mu.Unlock()
	if !ok {
		return ErrTerminalNotOpen
	}
	return h.Close()
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
