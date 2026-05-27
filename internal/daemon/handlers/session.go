package handlers

import (
	"context"
	"errors"
)

type SessionListResult struct {
	Sessions []SessionRow `json:"sessions"`
}

type SessionRow struct {
	SessionID   string `json:"sessionId"`
	BackendType string `json:"backendType"`
	Workdir     string `json:"workdir"`
	StartedAt   int64  `json:"startedAt"`
}

type SessionGetParams struct {
	SessionID string `json:"sessionId"`
}

type SessionGetResult struct {
	SessionRow
	Status      string `json:"status"`
	LastEventAt int64  `json:"lastEventAt"`
}

// ErrSessionNotFound is returned from session.get when the requested
// sessionId is not in the registry.
var ErrSessionNotFound = errors.New("session not found")

// SessionHandlers groups the session.* RPC methods.
type SessionHandlers struct{ reg SessionRegistryPort }

func NewSessionHandlers(r SessionRegistryPort) *SessionHandlers {
	return &SessionHandlers{reg: r}
}

func (h *SessionHandlers) List(ctx context.Context) (SessionListResult, error) {
	all := h.reg.List()
	out := make([]SessionRow, 0, len(all))
	for _, s := range all {
		out = append(out, SessionRow{
			SessionID:   s.SessionID,
			BackendType: s.BackendType,
			Workdir:     s.Workdir,
			StartedAt:   s.StartedAt,
		})
	}
	return SessionListResult{Sessions: out}, nil
}

func (h *SessionHandlers) Get(ctx context.Context, p SessionGetParams) (SessionGetResult, error) {
	s, ok := h.reg.Lookup(p.SessionID)
	if !ok {
		return SessionGetResult{}, ErrSessionNotFound
	}
	return SessionGetResult{
		SessionRow: SessionRow{
			SessionID:   s.SessionID,
			BackendType: s.BackendType,
			Workdir:     s.Workdir,
			StartedAt:   s.StartedAt,
		},
		Status: "active",
	}, nil
}
