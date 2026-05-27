package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

func requestIDKey(id json.RawMessage) string {
	return strings.Trim(string(id), `"`)
}

// SubmitUserInput answers a pending item/tool/requestUserInput server request.
// The answers map is keyed by the Codex question id; each value is the list of
// selected or free-form answers for that question.
func (s *Stream) SubmitUserInput(ctx context.Context, requestID string, answers map[string][]string) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("codex: empty request user input id")
	}
	s.userInputMu.Lock()
	rawID := s.userInputRequests[requestID]
	s.userInputMu.Unlock()
	if len(rawID) == 0 {
		return ErrNoActiveTurn
	}
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return ErrNoActiveTurn
	}
	payload := make(map[string]any, len(answers))
	for id, values := range answers {
		payload[id] = map[string]any{"answers": append([]string(nil), values...)}
	}
	if err := app.Respond(ctx, rawID, map[string]any{"answers": payload}); err != nil {
		return err
	}
	s.userInputMu.Lock()
	delete(s.userInputRequests, requestID)
	s.userInputMu.Unlock()
	return nil
}

func (s *Stream) registerUserInputRequest(id json.RawMessage) string {
	key := requestIDKey(id)
	if key == "" {
		return ""
	}
	s.userInputMu.Lock()
	if s.userInputRequests == nil {
		s.userInputRequests = map[string]json.RawMessage{}
	}
	s.userInputRequests[key] = append(json.RawMessage(nil), id...)
	s.userInputMu.Unlock()
	return key
}
