package piagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Stream struct {
	proc      *rpcProcess
	killGrace time.Duration
	events    chan Event

	mu                  sync.RWMutex
	sessionID           string
	model               string
	contextWindow       int
	usageObserved       bool
	initialStatsPending bool
	err                 error
	diagnostics         StreamDiagnostics
	cur                 Event

	closeOnce sync.Once

	pendingAgentEndError       *agentEndError
	pendingAssistantDeltaError error
}

type agentEndError struct {
	err     error
	event   rpcEvent
	rawLine string
}

type agentEndOutcomeKind uint8

const (
	agentEndOutcomeUnknown agentEndOutcomeKind = iota
	agentEndOutcomeSuccess
	agentEndOutcomeFailure
)

type agentEndOutcome struct {
	kind agentEndOutcomeKind
	err  error
}

func newStream(proc *rpcProcess, killGrace time.Duration) *Stream {
	return &Stream{proc: proc, killGrace: killGrace, events: make(chan Event, 64)}
}

func (s *Stream) send(ctx context.Context, cmd map[string]any) error {
	if s == nil || s.proc == nil {
		return errStreamClosed
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return s.proc.writeJSON(cmd)
}

func (s *Stream) Next() bool {
	ev, ok := <-s.events
	if !ok {
		return false
	}
	s.cur = ev
	return true
}

func (s *Stream) Event() Event { return s.cur }

func (s *Stream) SessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

func (s *Stream) setSessionID(sessionID string) {
	s.mu.Lock()
	s.sessionID = strings.TrimSpace(sessionID)
	s.mu.Unlock()
}

func (s *Stream) setContextWindow(contextWindow int) (changed, usageObserved bool) {
	if contextWindow <= 0 {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed = s.contextWindow != contextWindow
	if changed {
		s.contextWindow = contextWindow
	}
	return changed, s.usageObserved
}

func (s *Stream) markInitialSessionStatsPending() {
	s.mu.Lock()
	s.initialStatsPending = true
	s.mu.Unlock()
}

func (s *Stream) consumeInitialSessionStatsPending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.initialStatsPending {
		return false
	}
	s.initialStatsPending = false
	return true
}

func (s *Stream) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

func (s *Stream) Diagnostics() StreamDiagnostics {
	s.mu.RLock()
	out := s.diagnostics
	s.mu.RUnlock()
	if s.proc != nil && s.proc.stderr != nil {
		out.StderrTail = tailString(strings.TrimSpace(s.proc.stderr.String()), diagnosticStderrTailLimit)
	}
	return out
}

func (s *Stream) Close(ctx context.Context) error {
	var err error
	s.closeOnce.Do(func() {
		err = s.proc.terminate(ctx, s.killGrace)
	})
	if err != nil {
		return err
	}
	return s.Err()
}

func (s *Stream) drain(ctx context.Context) {
	defer close(s.events)
	promptAccepted := false
	for s.proc.lines.Scan() {
		s.proc.captureRawFrame(s.proc.lines.Bytes())
		select {
		case <-ctx.Done():
			s.setErr(ctx.Err())
			s.emit(Event{Kind: EventError, Err: ctx.Err()})
			return
		default:
		}
		line := strings.TrimSpace(s.proc.lines.Text())
		if line == "" {
			continue
		}
		var probe struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil {
			continue
		}
		if probe.Type == "response" {
			var resp rpcResponse
			if err := json.Unmarshal([]byte(line), &resp); err != nil {
				continue
			}
			if resp.Command == "get_session_stats" {
				if resp.ID != "" && resp.ID != initialSessionStatsRequestID {
					continue
				}
				// Initial stats are optional capability data. Retain a valid
				// authoritative window for each usage snapshot, but never fail the
				// prompt when an older or degraded Pi RPC rejects the request.
				s.consumeInitialSessionStatsPending()
				if resp.Success {
					cw := contextWindowFromSessionStats(resp.Data)
					if changed, usageObserved := s.setContextWindow(cw); changed && usageObserved {
						// A late pre-prompt correction arrived after the last usage.
						// Surface it immediately so runtime/session state cannot stay stale
						// when the round-end refresh is absent.
						s.emit(Event{Kind: EventContextWindow, ContextWindow: cw})
					}
				}
				continue
			}
			if !resp.Success {
				err := failureResponseError(resp)
				s.setErr(err)
				s.emit(Event{Kind: EventError, Err: err})
				return
			}
			if isAcceptedPromptResponse(resp) {
				promptAccepted = true
				// Pi guarantees correlated command responses. If the optional
				// initial stats response was omitted, prompt acceptance closes that
				// outstanding compatibility slot so a later no-ID final response is
				// not discarded as stale.
				s.consumeInitialSessionStatsPending()
			}
			// compact turn 不发 agent_end —— compact response 即终止信号。
			if resp.Command == "compact" {
				s.finish(ctx)
				return
			}
			continue
		}
		if !promptAccepted && probe.Type != "extension_ui_request" {
			// Pi can emit startup UI notifications before prompt response. Other events
			// before prompt acceptance are still safe to process, so this is only a marker.
			promptAccepted = true
		}
		var ev rpcEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		if err := s.handleRPCEvent(ctx, ev); err != nil {
			s.setErr(err)
			s.emit(Event{Kind: EventError, Err: err})
			return
		}
		s.observeAgentEnd(ev, line)
		if isTerminalEvent(ev) {
			s.settle(ctx)
			return
		}
	}
	if err := processDeadOrScanError(s.proc); err != nil {
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
	}
}

func (s *Stream) finish(ctx context.Context) {
	s.emitSessionStats(ctx)
	s.emit(Event{Kind: EventDone})
}

func (s *Stream) settle(ctx context.Context) {
	if candidate := s.pendingAgentEndError; candidate != nil {
		s.recordFinalErrorDiagnostics(candidate.event, candidate.rawLine)
		s.setErr(candidate.err)
		s.emit(Event{Kind: EventError, Err: candidate.err})
		return
	}
	if err := s.pendingAssistantDeltaError; err != nil {
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
		return
	}
	s.finish(ctx)
}

const (
	initialSessionStatsRequestID = "initial-session-stats"
	finalSessionStatsRequestID   = "final-session-stats"
	sessionStatsTimeout          = 2 * time.Second
)

func (s *Stream) emitSessionStats(ctx context.Context) {
	if s == nil || s.proc == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	if err := s.send(ctx, map[string]any{
		"id": finalSessionStatsRequestID, "type": "get_session_stats",
	}); err != nil {
		return
	}

	// get_session_stats 是增强信息，不能因为旧版/异常 Pi RPC 没有及时返回而卡住
	// terminal Done。超时后 runtime 会照常结束 turn 并关闭进程，下面的扫描 goroutine
	// 会随 stdout 关闭退出；它只写本地 buffered channel，不直接 emit，避免 late send 到
	// 已关闭的 events channel。
	resultC := make(chan int, 1)
	go func() {
		cw := s.readSessionStatsContextWindow()
		select {
		case resultC <- cw:
		default:
		}
	}()

	timer := time.NewTimer(sessionStatsTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		return
	case cw := <-resultC:
		if cw > 0 {
			s.emit(Event{Kind: EventContextWindow, ContextWindow: cw})
		}
	}
}

func (s *Stream) readSessionStatsContextWindow() int {
	for s.proc.lines.Scan() {
		s.proc.captureRawFrame(s.proc.lines.Bytes())
		line := strings.TrimSpace(s.proc.lines.Text())
		if line == "" {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		if resp.Type != "response" || resp.Command != "get_session_stats" {
			continue
		}
		// A delayed pre-prompt response must not be mistaken for the
		// authoritative round-end refresh. Correlated responses are explicit;
		// for ID-stripping implementations, command order identifies the first
		// outstanding no-ID response as the initial request.
		switch resp.ID {
		case initialSessionStatsRequestID:
			s.consumeInitialSessionStatsPending()
			continue
		case finalSessionStatsRequestID:
			// Authoritative response; decode below.
		case "":
			if s.consumeInitialSessionStatsPending() {
				continue
			}
		default:
			continue
		}
		if !resp.Success {
			return 0
		}
		return contextWindowFromSessionStats(resp.Data)
	}
	return 0
}

func contextWindowFromSessionStats(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var stats sessionStatsWire
	if err := json.Unmarshal(raw, &stats); err != nil || stats.ContextUsage == nil {
		return 0
	}
	return stats.ContextUsage.ContextWindow
}

func (s *Stream) handleRPCEvent(ctx context.Context, ev rpcEvent) error {
	switch ev.Type {
	case "message_start":
		// 只有 user 消息回显才 surface（首条 prompt + mid-turn steer 注入）；
		// runtime 据此对照 pending steer emit SteerConsumed。
		if text, ok := userEchoText(ev.Message); ok {
			s.emit(Event{Kind: EventUserMessage, Text: text})
		}
	case "message_update":
		s.handleAssistantDelta(ev.AssistantMessageEvent)
	case "message_end":
		s.handleMessageEnd(ev.Message)
	case "agent_end":
		if msg := lastAssistantFromAgentEnd(ev.Messages); msg != nil {
			s.recordAssistantMessage(msg)
		}
	case "tool_execution_start":
		s.emit(Event{Kind: EventPreToolUse, Tool: ToolEvent{ID: ev.ToolCallID, Name: ev.ToolName, Input: ev.Args}})
	case "tool_execution_update":
		s.emit(Event{Kind: EventToolUseUpdate, Tool: ToolEvent{ID: ev.ToolCallID, Name: ev.ToolName, PartialResult: ev.PartialResult}})
	case "tool_execution_end":
		content, details := toolResult(ev.Result)
		s.emit(Event{Kind: EventPostToolUse, Tool: ToolEvent{ID: ev.ToolCallID, Name: ev.ToolName, Content: content, Details: details, IsError: ev.IsError}})
	case "extension_ui_request":
		if isBlockingExtensionUIMethod(ev.Method) {
			if err := s.send(ctx, map[string]any{
				"type": "extension_ui_response", "id": ev.ID, "cancelled": true, //nolint:misspell // Pi RPC wire contract requires this JSON key.
			}); err != nil {
				return err
			}
		}
	case "compaction_start":
		s.emit(Event{Kind: EventRuntimeStatus, Text: "compacting"})
	case "compaction_end":
		s.emit(Event{Kind: EventCompactBoundary})
	case "auto_retry_start":
		s.emit(Event{Kind: EventRuntimeStatus, Text: strings.TrimSpace(ev.ErrorMessage)})
	}
	return nil
}

func isBlockingExtensionUIMethod(method string) bool {
	switch strings.TrimSpace(method) {
	case "confirm", "select", "input", "editor":
		return true
	default:
		return false
	}
}

func (s *Stream) handleAssistantDelta(delta assistantDelta) {
	switch delta.Type {
	case "text_delta":
		s.emit(Event{Kind: EventTextDelta, Text: delta.Delta})
	case "thinking_delta":
		s.emit(Event{Kind: EventThinkingDelta, Text: delta.Delta})
	// 注意：toolcall_end 不再 emit PreToolUse。Pi 对一次工具调用会同时发
	// message_update/toolcall_end 和后续的 tool_execution_start（同一个
	// toolCallId），PreToolUse 只从 tool_execution_start 出，避免下游工具卡重复。
	case "error":
		if s.pendingAgentEndError == nil {
			s.pendingAssistantDeltaError = fmt.Errorf("piagent: %s", strings.TrimSpace(delta.Reason))
		}
	}
}

func (s *Stream) handleMessageEnd(raw json.RawMessage) {
	msg, err := parseAssistantMessage(raw)
	if err != nil || msg == nil {
		return
	}
	s.recordAssistantMessage(msg)
}

func (s *Stream) recordAssistantMessage(msg *assistantMessage) {
	s.mu.Lock()
	if strings.TrimSpace(msg.Model) != "" {
		s.model = strings.TrimSpace(msg.Model)
	}
	u := usageFromMessage(msg)
	contextWindow := s.contextWindow
	hasUsage := u.PromptTokens > 0 || u.CompletionTokens > 0
	if hasUsage {
		s.usageObserved = true
	}
	s.mu.Unlock()
	if hasUsage {
		// Keep the standalone event for older desktop consumers, then carry the
		// same denominator atomically on usage for current clients. Both repeat at
		// each API-call boundary so a later snapshot repairs an earlier Wails miss.
		if contextWindow > 0 {
			s.emit(Event{Kind: EventContextWindow, ContextWindow: contextWindow})
		}
		s.emit(Event{Kind: EventUsage, Usage: u, Model: msg.Model, ContextWindow: contextWindow})
	}
}

func (s *Stream) observeAgentEnd(ev rpcEvent, rawLine string) {
	if ev.Type != "agent_end" {
		return
	}
	outcome := classifyFinalAgentEnd(ev)
	switch outcome.kind {
	case agentEndOutcomeFailure:
		// agent_end carries the authoritative terminal message. It supersedes
		// any preceding streaming error delta and is confirmed at settlement.
		s.pendingAgentEndError = &agentEndError{err: outcome.err, event: ev, rawLine: rawLine}
		s.pendingAssistantDeltaError = nil
	case agentEndOutcomeSuccess:
		// A completed low-level run clears retry candidates. Other outcomes
		// deliberately leave a staged delta intact until agent_settled decides it.
		s.pendingAgentEndError = nil
		s.pendingAssistantDeltaError = nil
	}
}

func classifyFinalAgentEnd(ev rpcEvent) agentEndOutcome {
	msg := lastAssistantFromAgentEnd(ev.Messages)
	if msg == nil {
		return agentEndOutcome{kind: agentEndOutcomeUnknown}
	}
	switch strings.TrimSpace(msg.StopReason) {
	case "stop", "length", "toolUse":
		return agentEndOutcome{kind: agentEndOutcomeSuccess}
	case "error":
		return agentEndFailure(msg, "unknown error")
	case "aborted":
		return agentEndFailure(msg, "aborted")
	default:
		return agentEndOutcome{kind: agentEndOutcomeUnknown}
	}
}

func agentEndFailure(msg *assistantMessage, fallback string) agentEndOutcome {
	errMsg := strings.TrimSpace(msg.ErrorMessage)
	if errMsg == "" {
		errMsg = fallback
	}
	return agentEndOutcome{
		kind: agentEndOutcomeFailure,
		err:  fmt.Errorf("piagent: %s", errMsg),
	}
}

func (s *Stream) emit(ev Event) {
	select {
	case s.events <- ev:
	default:
		s.events <- ev
	}
}

func (s *Stream) setErr(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

const diagnosticStderrTailLimit = 4 * 1024

func (s *Stream) recordFinalErrorDiagnostics(ev rpcEvent, rawLine string) {
	msg := lastAssistantFromAgentEnd(ev.Messages)
	if msg == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diagnostics.FinalErrorEventType = ev.Type
	s.diagnostics.FinalErrorStopReason = strings.TrimSpace(msg.StopReason)
	s.diagnostics.FinalErrorMessage = strings.TrimSpace(msg.ErrorMessage)
	s.diagnostics.FinalErrorFrame = strings.TrimSpace(rawLine)
}

func tailString(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

func toolResult(raw json.RawMessage) (string, json.RawMessage) {
	if len(raw) == 0 {
		return "", nil
	}
	var obj struct {
		Content []contentBlock  `json:"content"`
		Details json.RawMessage `json:"details"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw), nil
	}
	var b strings.Builder
	for _, c := range obj.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String(), obj.Details
}
