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

	mu                 sync.RWMutex
	sessionID          string
	userAnchorBoundary string
	userAnchor         string
	captureUserAnchor  bool
	model              string
	err                error
	diagnostics        StreamDiagnostics
	cur                Event

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

func (s *Stream) UserAnchor() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.userAnchor
}

func (s *Stream) setUserAnchorBoundary(leafID string) {
	s.mu.Lock()
	s.captureUserAnchor = true
	s.userAnchorBoundary = strings.TrimSpace(leafID)
	s.mu.Unlock()
}

func (s *Stream) setUserAnchor(anchor string) {
	s.mu.Lock()
	s.userAnchor = strings.TrimSpace(anchor)
	s.mu.Unlock()
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
		if s.proc.rawSink != nil {
			s.proc.rawSink(s.proc.lines.Bytes())
		}
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
			if !resp.Success {
				err := failureResponseError(resp)
				s.setErr(err)
				s.emit(Event{Kind: EventError, Err: err})
				return
			}
			if isAcceptedPromptResponse(resp) {
				promptAccepted = true
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
		s.handleRPCEvent(ev)
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
	if s.captureUserAnchor {
		s.emitTrackedSessionMetadata(ctx)
	} else {
		s.emitSessionStats(ctx)
	}
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

const sessionStatsTimeout = 2 * time.Second

func (s *Stream) emitSessionStats(ctx context.Context) {
	if s == nil || s.proc == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	if err := s.send(ctx, map[string]any{"type": "get_session_stats"}); err != nil {
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
		if s.proc.rawSink != nil {
			s.proc.rawSink(s.proc.lines.Bytes())
		}
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
		if !resp.Success {
			return 0
		}
		return contextWindowFromSessionStats(resp.Data)
	}
	return 0
}

type trackedSessionMetadata struct {
	userAnchor    string
	contextWindow int
	entriesSeen   bool
	statsSeen     bool
}

func (s *Stream) emitTrackedSessionMetadata(ctx context.Context) {
	if s == nil || s.proc == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	const entriesRequestID = "session-entries-after"
	if err := s.send(ctx, map[string]any{"id": entriesRequestID, "type": "get_entries"}); err != nil {
		return
	}
	wantStats := s.send(ctx, map[string]any{"type": "get_session_stats"}) == nil

	updates := make(chan trackedSessionMetadata, 2)
	go func() {
		s.readTrackedSessionMetadata(entriesRequestID, wantStats, updates)
		close(updates)
	}()

	metadata := trackedSessionMetadata{statsSeen: !wantStats}
	timer := time.NewTimer(sessionStatsTimeout)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.applyTrackedSessionMetadata(metadata)
			return
		case update, ok := <-updates:
			if !ok {
				s.applyTrackedSessionMetadata(metadata)
				return
			}
			metadata = update
			if metadata.entriesSeen && metadata.statsSeen {
				s.applyTrackedSessionMetadata(metadata)
				return
			}
		}
	}
}

func (s *Stream) applyTrackedSessionMetadata(metadata trackedSessionMetadata) {
	s.setUserAnchor(metadata.userAnchor)
	if metadata.contextWindow > 0 {
		s.emit(Event{Kind: EventContextWindow, ContextWindow: metadata.contextWindow})
	}
}

func (s *Stream) readTrackedSessionMetadata(
	entriesRequestID string,
	wantStats bool,
	updates chan<- trackedSessionMetadata,
) {
	metadata := trackedSessionMetadata{statsSeen: !wantStats}
	for s.proc.lines.Scan() {
		if s.proc.rawSink != nil {
			s.proc.rawSink(s.proc.lines.Bytes())
		}
		line := strings.TrimSpace(s.proc.lines.Text())
		if line == "" {
			continue
		}
		var response rpcResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil || response.Type != "response" {
			continue
		}
		switch {
		case response.Command == "get_entries" && response.ID == entriesRequestID && !metadata.entriesSeen:
			metadata.entriesSeen = true
			if response.Success {
				var entries sessionEntriesWire
				if err := json.Unmarshal(response.Data, &entries); err == nil {
					metadata.userAnchor = firstUserEntryAfterBoundary(
						entries.Entries,
						s.userAnchorBoundary,
						entries.LeafID,
					)
				}
			}
			updates <- metadata
		case response.Command == "get_session_stats" && !metadata.statsSeen:
			metadata.statsSeen = true
			if response.Success {
				metadata.contextWindow = contextWindowFromSessionStats(response.Data)
			}
			updates <- metadata
		}
		if metadata.entriesSeen && metadata.statsSeen {
			return
		}
	}
}

func firstUserEntryAfterBoundary(entries []sessionEntryWire, boundaryLeafID, currentLeafID string) string {
	boundaryLeafID = strings.TrimSpace(boundaryLeafID)
	currentLeafID = strings.TrimSpace(currentLeafID)
	if currentLeafID == "" || currentLeafID == boundaryLeafID {
		return ""
	}

	byID := make(map[string]sessionEntryWire, len(entries))
	for _, entry := range entries {
		id := strings.TrimSpace(entry.ID)
		if id == "" {
			continue
		}
		if _, duplicate := byID[id]; duplicate {
			return ""
		}
		entry.ID = id
		entry.ParentID = strings.TrimSpace(entry.ParentID)
		byID[id] = entry
	}

	path := make([]sessionEntryWire, 0)
	seen := make(map[string]struct{})
	for currentLeafID != boundaryLeafID {
		if currentLeafID == "" {
			if boundaryLeafID != "" {
				return ""
			}
			break
		}
		if _, cycle := seen[currentLeafID]; cycle {
			return ""
		}
		seen[currentLeafID] = struct{}{}
		entry, ok := byID[currentLeafID]
		if !ok {
			return ""
		}
		path = append(path, entry)
		currentLeafID = entry.ParentID
	}

	for i := len(path) - 1; i >= 0; i-- {
		entry := path[i]
		if entry.Type == "message" && entry.Message.Role == "user" {
			return entry.ID
		}
	}
	return ""
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

func (s *Stream) handleRPCEvent(ev rpcEvent) {
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
	case "tool_execution_end":
		content := toolResultText(ev.Result)
		s.emit(Event{Kind: EventPostToolUse, Tool: ToolEvent{ID: ev.ToolCallID, Name: ev.ToolName, Content: content, IsError: ev.IsError}})
	case "compaction_start":
		s.emit(Event{Kind: EventRuntimeStatus, Text: "compacting"})
	case "compaction_end":
		s.emit(Event{Kind: EventCompactBoundary})
	case "auto_retry_start":
		s.emit(Event{Kind: EventRuntimeStatus, Text: strings.TrimSpace(ev.ErrorMessage)})
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
	s.mu.Unlock()
	if u.PromptTokens > 0 || u.CompletionTokens > 0 {
		s.emit(Event{Kind: EventUsage, Usage: u, Model: msg.Model})
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

func toolResultText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var obj struct {
		Content []contentBlock `json:"content"`
	}
	if err := json.Unmarshal(raw, &obj); err != nil {
		return string(raw)
	}
	var b strings.Builder
	for _, c := range obj.Content {
		if c.Type == "text" {
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
