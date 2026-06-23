package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cago-frame/agents/provider"
)

func (s *Stream) drain(ctx context.Context) {
	defer close(s.events)
	defer s.clearActiveTurn()
	defer s.closeOnce.Do(func() {
		if s.closeAppOnDrain {
			_ = s.app.terminate(context.Background(), s.killGrace)
		}
	})

	preSeen := map[string]struct{}{}
	doneCh := s.app.Done()
	for {
		select {
		case in, ok := <-s.app.Incoming():
			if !ok {
				if err := s.app.Err(); err != nil {
					s.emitError(err, nil)
				} else if !s.app.isStopping() {
					s.emitError(ErrProcessDead, nil)
				}
				return
			}
			done := s.handleInbound(ctx, in, preSeen)
			if done {
				return
			}
		case <-doneCh:
			if err := s.app.Err(); err != nil {
				s.emitError(err, nil)
			} else if !s.app.isStopping() {
				s.emitError(ErrProcessDead, nil)
			}
			return
		case <-ctx.Done():
			s.emitError(ctx.Err(), nil)
			return
		}
	}
}

func (s *Stream) handleInbound(ctx context.Context, in appInbound, preSeen map[string]struct{}) bool {
	if in.Kind == appInboundRequest {
		_ = s.handleServerRequest(ctx, in)
		return false
	}
	n, err := parseNotification(in.Params)
	if err != nil {
		s.emitError(err, in.Params)
		return false
	}
	if n.ThreadID != "" {
		s.setSessionID(n.ThreadID)
	}
	switch in.Method {
	case appMethodItemAgentMessageDelta:
		if n.ItemID != "" {
			// If an agentMessage later completes with full text, avoid re-emitting it.
			preSeen["partial:"+n.ItemID] = struct{}{}
		}
		s.emit(Event{Kind: EventTextDelta, SessionID: s.SessionID(), Text: n.Delta, Raw: in.Params})
	case appMethodItemReasoningTextDelta, appMethodItemReasoningSummaryTextDelta:
		s.emit(Event{Kind: EventThinkingDelta, SessionID: s.SessionID(), Text: n.Delta, Raw: in.Params})
	case appMethodThreadTokenUsageUpdated:
		usage := appUsageToProvider(n.Usage)
		s.setUsage(usage)
		if cw := appContextWindow(n); cw > 0 {
			s.setContextWindow(cw)
		}
		s.emit(Event{
			Kind:          EventUsage,
			SessionID:     s.SessionID(),
			Usage:         usage,
			ContextWindow: s.currentContextWindow(),
			Raw:           in.Params,
		})
	case appMethodTurnPlanUpdated:
		s.emitPlan(n, in.Params)
	case appMethodRawResponseItemCompleted:
		if isCompactItem(n.Item) {
			s.emitCompactBoundary(n, in.Params)
		}
	case appMethodItemStarted:
		s.handleItemStarted(n, in.Params, preSeen)
	case appMethodItemFileChangePatchUpdated:
		if n.ItemID != "" && len(n.Changes) > 0 {
			s.emitPreToolUseIfMissing(&appThreadItem{Type: appItemFileChange, ID: n.ItemID, Changes: n.Changes}, in.Params, preSeen)
		}
	case appMethodItemCompleted:
		s.handleItemCompleted(n, in.Params, preSeen)
	case appMethodThreadCompacted:
		s.emitCompactBoundary(n, in.Params)
		if s.isManualCompactStream() {
			s.emit(Event{
				Kind:          EventDone,
				SessionID:     s.SessionID(),
				Usage:         s.currentUsage(),
				ContextWindow: s.currentContextWindow(),
				Raw:           in.Params,
			})
			return true
		}
	case appMethodTurnCompleted:
		if n.Turn != nil && s.turnID != "" && n.Turn.ID != s.turnID {
			return false
		}
		if err := appTurnErr(n.Turn); err != nil {
			s.emitError(err, in.Params)
		}
		s.emit(Event{
			Kind:          EventDone,
			SessionID:     s.SessionID(),
			Usage:         s.currentUsage(),
			ContextWindow: s.currentContextWindow(),
			Raw:           in.Params,
		})
		return true
	case appMethodError:
		if n.WillRetry {
			s.emit(Event{Kind: EventRetry, SessionID: s.SessionID(), Retry: appRetryEvent(n), Raw: in.Params})
			return false
		}
		s.emitError(fmt.Errorf("codex app-server: %s", string(in.Params)), in.Params)
	}
	return false
}

func (s *Stream) isManualCompactStream() bool {
	return s.turnID == "" && strings.TrimSpace(s.compactTrigger) != ""
}

func (s *Stream) handleItemStarted(n appNotification, raw json.RawMessage, preSeen map[string]struct{}) {
	item := n.Item
	if item == nil || item.ID == "" {
		return
	}
	if isCompactItem(item) {
		s.emitCompactBoundary(n, raw)
		return
	}
	switch item.Type {
	case appItemUserMessage:
		s.emitUserMessageIfMissing(item, raw, preSeen)
		return
	case appItemAgentMessage, appItemReasoning, appItemPlan:
		return
	case appItemFileChange:
		if len(item.Changes) == 0 {
			return
		}
	}
	s.emitPreToolUseIfMissing(item, raw, preSeen)
}

func (s *Stream) handleItemCompleted(n appNotification, raw json.RawMessage, preSeen map[string]struct{}) {
	item := n.Item
	if item == nil {
		return
	}
	if isCompactItem(item) {
		s.emitCompactBoundary(n, raw)
		return
	}
	switch item.Type {
	case appItemUserMessage:
		s.emitUserMessageIfMissing(item, raw, preSeen)
	case appItemAgentMessage:
		if _, partial := preSeen["partial:"+item.ID]; !partial && item.Text != "" {
			s.emit(Event{Kind: EventTextDelta, SessionID: s.SessionID(), Text: item.Text, Raw: raw})
		}
	case appItemPlan:
		if strings.TrimSpace(item.Text) != "" {
			s.emit(Event{Kind: EventPlanUpdated, SessionID: s.SessionID(), PlanText: item.Text, Raw: raw})
		}
	case appItemCommandExecution, appItemFileChange, appItemMCPToolCall, appItemDynamicToolCall, appItemCollabAgentTool:
		s.emitPreToolUseIfMissing(item, raw, preSeen)
		s.emit(Event{
			Kind:      EventPostToolUse,
			SessionID: s.SessionID(),
			Tool: &ToolEvent{
				ID:       item.ID,
				Name:     toolNameForItem(item),
				Input:    toolInputForItem(item),
				Response: toolResponseForItem(item),
				Err:      toolErrForItem(item),
				Source:   toolSourceForItem(item),
			},
			Raw: raw,
		})
	default:
		if _, startedAsTool := preSeen[item.ID]; startedAsTool {
			s.emit(Event{
				Kind:      EventPostToolUse,
				SessionID: s.SessionID(),
				Tool: &ToolEvent{
					ID:       item.ID,
					Name:     toolNameForItem(item),
					Input:    toolInputForItem(item),
					Response: toolResponseForItem(item),
					Err:      toolErrForItem(item),
					Source:   toolSourceForItem(item),
				},
				Raw: raw,
			})
		}
	}
}

func (s *Stream) emitCompactBoundary(n appNotification, raw json.RawMessage) {
	threadID := n.ThreadID
	if threadID == "" {
		threadID = s.SessionID()
	}
	turnID := n.TurnID
	if turnID == "" && n.Turn != nil {
		turnID = n.Turn.ID
	}
	key := threadID + ":" + turnID
	if key == ":" && n.Item != nil {
		key = "item:" + n.Item.ID
	}
	if key == ":" {
		key = string(raw)
	}
	if _, ok := s.compactSeen[key]; ok {
		return
	}
	s.compactSeen[key] = struct{}{}

	trigger := strings.TrimSpace(s.compactTrigger)
	if trigger == "" {
		trigger = "auto"
	}
	s.emit(Event{
		Kind:      EventCompactBoundary,
		SessionID: threadID,
		Compact:   &CompactEvent{Trigger: trigger},
		Raw:       raw,
	})
}

func isCompactItem(item *appThreadItem) bool {
	if item == nil {
		return false
	}
	switch item.Type {
	case appItemContextCompaction, "context_compaction", "compaction":
		return true
	default:
		return false
	}
}

func (s *Stream) emitUserMessageIfMissing(item *appThreadItem, raw json.RawMessage, seen map[string]struct{}) {
	if item == nil || item.ID == "" {
		return
	}
	text := userTextForItem(item)
	if strings.TrimSpace(text) == "" {
		return
	}
	key := "user:" + item.ID
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	s.emit(Event{Kind: EventUserMessage, SessionID: s.SessionID(), Text: text, Raw: raw})
}

func (s *Stream) emitPreToolUseIfMissing(item *appThreadItem, raw json.RawMessage, preSeen map[string]struct{}) {
	if item == nil || item.ID == "" {
		return
	}
	if _, ok := preSeen[item.ID]; ok {
		return
	}
	preSeen[item.ID] = struct{}{}
	s.emit(Event{
		Kind:      EventPreToolUse,
		SessionID: s.SessionID(),
		Tool: &ToolEvent{
			ID:     item.ID,
			Name:   toolNameForItem(item),
			Input:  toolInputForItem(item),
			Source: toolSourceForItem(item),
		},
		Raw: raw,
	})
}

func (s *Stream) emitPlan(n appNotification, raw json.RawMessage) {
	if len(n.Plan) == 0 {
		return
	}
	steps := make([]PlanStep, 0, len(n.Plan))
	for _, p := range n.Plan {
		steps = append(steps, PlanStep(p))
	}
	s.emit(Event{Kind: EventPlanUpdated, SessionID: s.SessionID(), Plan: steps, Raw: raw})

	id := n.TurnID + ":plan"
	tool := &ToolEvent{
		ID:       id,
		Name:     appToolUpdatePlan,
		Input:    append(json.RawMessage(nil), raw...),
		Response: append(json.RawMessage(nil), raw...),
		Source:   ToolSourceBuiltin,
	}
	s.emit(Event{Kind: EventPreToolUse, SessionID: s.SessionID(), Tool: tool, Raw: raw})
	s.emit(Event{Kind: EventPostToolUse, SessionID: s.SessionID(), Tool: tool, Raw: raw})
}

func (s *Stream) handleServerRequest(ctx context.Context, in appInbound) error {
	app := s.app
	if app == nil {
		return ErrNoActiveTurn
	}
	switch in.Method {
	case appMethodItemCommandApprovalRequest, appMethodItemFileApprovalRequest:
		return s.handleApprovalRequest(ctx, in)
	case appMethodItemPermissionsRequest:
		return s.handleApprovalRequest(ctx, in)
	case appMethodItemToolRequestUserInput:
		ev, err := parseRequestUserInputParams(in.Params)
		if err != nil {
			s.emitError(err, in.Params)
			return app.Respond(ctx, in.ID, map[string]any{"answers": map[string]any{}})
		}
		ev.RequestID = s.registerUserInputRequest(in.ID)
		if ev.RequestID == "" {
			err := ErrNoActiveTurn
			s.emitError(err, in.Params)
			return app.Respond(ctx, in.ID, map[string]any{"answers": map[string]any{}})
		}
		s.emit(Event{
			Kind:             EventRequestUserInput,
			SessionID:        s.SessionID(),
			RequestUserInput: &ev,
			Raw:              in.Params,
		})
		return nil
	case appMethodItemToolCall:
		return app.Respond(ctx, in.ID, map[string]any{"contentItems": []any{}, "success": false})
	default:
		return app.Respond(ctx, in.ID, map[string]any{})
	}
}

func (s *Stream) handleApprovalRequest(ctx context.Context, in appInbound) error {
	app := s.app
	if app == nil {
		return ErrNoActiveTurn
	}
	ev, err := parseApprovalRequest(in.Method, in.Params)
	if err != nil {
		s.emitError(err, in.Params)
		return app.Respond(ctx, in.ID, approvalResponse(approvalRequest{method: in.Method, params: in.Params}, false, false))
	}
	ev.RequestID = s.registerApprovalRequest(in.ID, in.Method, in.Params)
	if ev.RequestID == "" {
		err := ErrNoActiveTurn
		s.emitError(err, in.Params)
		return app.Respond(ctx, in.ID, approvalResponse(approvalRequest{method: in.Method, params: in.Params}, false, false))
	}
	s.emit(Event{
		Kind:      EventApprovalRequest,
		SessionID: s.SessionID(),
		Approval:  &ev,
		Raw:       in.Params,
	})
	return nil
}

func (s *Stream) emit(ev Event) {
	select {
	case s.events <- ev:
	default:
		s.events <- ev
	}
}

func (s *Stream) emitError(err error, raw json.RawMessage) {
	if err == nil {
		return
	}
	s.setErr(err)
	s.emit(Event{Kind: EventError, SessionID: s.SessionID(), Err: err, Raw: raw})
}

func (s *Stream) setSessionID(id string) {
	s.mu.Lock()
	s.sessionID = id
	s.mu.Unlock()
}

func (s *Stream) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *Stream) setUsage(u provider.Usage) {
	s.mu.Lock()
	s.usage = u
	s.mu.Unlock()
}

func (s *Stream) clearActiveTurn() {
	s.mu.Lock()
	s.turnID = ""
	s.mu.Unlock()
}

func (s *Stream) currentUsage() provider.Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.usage
}

func (s *Stream) setContextWindow(cw int) {
	s.mu.Lock()
	s.contextWindow = cw
	s.mu.Unlock()
}

func (s *Stream) currentContextWindow() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.contextWindow
}
