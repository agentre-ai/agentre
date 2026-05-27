package codex

import (
	"context"
	"errors"
	"io"
	"strings"
)

// ErrNoActiveTurn is returned by Stream.Steer when the stream has no active
// turn/thread to steer.
var ErrNoActiveTurn = errors.New("codex: no active turn to steer")

// Steer injects a user message into the active turn via the codex app-server
// turn/steer RPC. The CLI integrates the message into the same turn and the
// agent decides when to respond to it (typically at the next safe boundary).
func (s *Stream) Steer(ctx context.Context, text string) error {
	s.mu.RLock()
	threadID := s.sessionID
	turnID := s.turnID
	app := s.app
	s.mu.RUnlock()
	if threadID == "" || turnID == "" || app == nil {
		return ErrNoActiveTurn
	}
	_, err := app.Call(ctx, appMethodTurnSteer, map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input":          userInput(text),
	})
	if isNoActiveTurnSteerError(err) {
		return ErrNoActiveTurn
	}
	return err
}

func isNoActiveTurnSteerError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrNoActiveTurn) ||
		errors.Is(err, ErrProcessDead) ||
		errors.Is(err, io.ErrClosedPipe) {
		return true
	}
	var rpc *rpcError
	if !errors.As(err, &rpc) {
		return false
	}
	msg := strings.ToLower(rpc.Message + " " + string(rpc.Data))
	return strings.Contains(msg, "expectedturnid") ||
		strings.Contains(msg, "active turn") ||
		strings.Contains(msg, "no active turn")
}
