package codex

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type approvalRequest struct {
	rawID  json.RawMessage
	method string
	params json.RawMessage
}

type approvalRequestParams struct {
	ThreadID    string          `json:"threadId"`
	TurnID      string          `json:"turnId"`
	ItemID      string          `json:"itemId"`
	Permissions json.RawMessage `json:"permissions,omitempty"`
}

func parseApprovalRequest(method string, params json.RawMessage) (ApprovalRequestEvent, error) {
	var p approvalRequestParams
	if len(params) > 0 {
		if err := json.Unmarshal(params, &p); err != nil {
			return ApprovalRequestEvent{}, err
		}
	}
	return ApprovalRequestEvent{
		ThreadID: p.ThreadID,
		TurnID:   p.TurnID,
		ItemID:   p.ItemID,
		ToolName: approvalToolName(method),
		Input:    append(json.RawMessage(nil), params...),
	}, nil
}

func approvalToolName(method string) string {
	switch method {
	case appMethodItemCommandApprovalRequest:
		return "Bash"
	case appMethodItemFileApprovalRequest:
		return "FileChange"
	case appMethodItemPermissionsRequest:
		return "Permissions"
	default:
		return "Approval"
	}
}

func (s *Stream) SubmitApproval(ctx context.Context, requestID string, allow, alwaysAllowSession bool) error {
	if strings.TrimSpace(requestID) == "" {
		return errors.New("codex: empty approval request id")
	}
	s.userInputMu.Lock()
	req := s.approvalRequests[requestID]
	s.userInputMu.Unlock()
	if len(req.rawID) == 0 {
		return ErrNoActiveTurn
	}
	s.mu.RLock()
	app := s.app
	s.mu.RUnlock()
	if app == nil {
		return ErrNoActiveTurn
	}
	if err := app.Respond(ctx, req.rawID, approvalResponse(req, allow, alwaysAllowSession)); err != nil {
		return err
	}
	s.userInputMu.Lock()
	delete(s.approvalRequests, requestID)
	s.userInputMu.Unlock()
	return nil
}

func approvalResponse(req approvalRequest, allow, alwaysAllowSession bool) map[string]any {
	switch req.method {
	case appMethodItemPermissionsRequest:
		if !allow {
			return map[string]any{"permissions": map[string]any{}, "scope": "turn"}
		}
		return map[string]any{
			"permissions": requestedPermissions(req.params),
			"scope":       approvalScope(alwaysAllowSession),
		}
	case appMethodItemCommandApprovalRequest, appMethodItemFileApprovalRequest:
		return map[string]any{"decision": approvalDecision(allow, alwaysAllowSession)}
	default:
		return map[string]any{}
	}
}

func approvalDecision(allow, alwaysAllowSession bool) string {
	if !allow {
		return "decline"
	}
	if alwaysAllowSession {
		return "acceptForSession"
	}
	return "accept"
}

func approvalScope(alwaysAllowSession bool) string {
	if alwaysAllowSession {
		return "session"
	}
	return "turn"
}

func requestedPermissions(params json.RawMessage) map[string]any {
	var p approvalRequestParams
	if len(params) == 0 || json.Unmarshal(params, &p) != nil || len(p.Permissions) == 0 {
		return map[string]any{}
	}
	var out map[string]any
	if json.Unmarshal(p.Permissions, &out) != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func (s *Stream) registerApprovalRequest(id json.RawMessage, method string, params json.RawMessage) string {
	key := requestIDKey(id)
	if key == "" {
		return ""
	}
	s.userInputMu.Lock()
	if s.approvalRequests == nil {
		s.approvalRequests = map[string]approvalRequest{}
	}
	s.approvalRequests[key] = approvalRequest{
		rawID:  append(json.RawMessage(nil), id...),
		method: method,
		params: append(json.RawMessage(nil), params...),
	}
	s.userInputMu.Unlock()
	return key
}
