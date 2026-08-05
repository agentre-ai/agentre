package piagent

import (
	"context"
	"encoding/json"
	"errors"
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
	abortMu   sync.Mutex
	abortSent bool

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
	command, _ := cmd["type"].(string)
	if command != "abort" {
		return s.proc.writeJSON(cmd)
	}

	// Stop may reach the runtime interruptor just before or just after the turn
	// context is canceled. Serialize those paths so the accepted process receives
	// one abort frame, while a late duplicate remains idempotent even after exit.
	s.abortMu.Lock()
	defer s.abortMu.Unlock()
	if s.abortSent {
		return nil
	}
	if err := s.proc.writeJSON(cmd); err != nil {
		return err
	}
	s.abortSent = true
	return nil
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
	defer s.mu.RUnlock()
	return s.diagnostics
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

func (s *Stream) awaitPromptAcknowledgement(ctx context.Context) ([][]byte, error) {
	pending := make([][]byte, 0)
	for scanRPCLine(ctx, s.proc.lines) {
		line := append([]byte(nil), s.proc.lines.Bytes()...)
		emitRawFrame(s.proc, line)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		var response rpcResponse
		if err := json.Unmarshal(line, &response); err != nil || !isPromptResponse(response) {
			pending = append(pending, line)
			continue
		}
		if err := promptResponseError(response); err != nil {
			return nil, err
		}
		return pending, nil
	}
	return nil, awaitProcessExitOrScanError(ctx, s.proc)
}

const abortSettlementGrace = 500 * time.Millisecond

func (s *Stream) drain(ctx context.Context, pendingFrames ...[][]byte) {
	defer close(s.events)
	drainCtx, stopSettlementWindow := boundedSettlementContext(ctx, abortSettlementGrace, func() {
		_ = s.send(context.Background(), map[string]any{"type": "abort"})
	})
	defer stopSettlementWindow()

	var pending [][]byte
	if len(pendingFrames) > 0 {
		pending = pendingFrames[0]
	}
	for _, line := range pending {
		if !s.drainLine(drainCtx, line, false) {
			s.terminateCanceledProcess(ctx)
			return
		}
	}
	for scanRPCLine(drainCtx, s.proc.lines) {
		line := append([]byte(nil), s.proc.lines.Bytes()...)
		if !s.drainLine(drainCtx, line, true) {
			s.terminateCanceledProcess(ctx)
			return
		}
	}
	if err := ctx.Err(); err != nil {
		s.terminateCanceledProcess(ctx)
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
		return
	}
	if err := processDeadOrScanError(s.proc); err != nil {
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
	}
}

func (s *Stream) terminateCanceledProcess(ctx context.Context) {
	if s == nil || s.proc == nil || ctx.Err() == nil {
		return
	}
	// Settlement has either completed or exhausted its bound. Kill the whole
	// process group now rather than applying the ordinary graceful Close delay,
	// so descendant tool work cannot continue beyond the Stop window.
	_ = s.proc.terminate(context.Background(), 0)
}

func boundedSettlementContext(ctx context.Context, grace time.Duration, onCancel func()) (context.Context, func()) {
	settlementCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			if onCancel != nil {
				go onCancel()
			}
			timer := time.NewTimer(grace)
			defer timer.Stop()
			select {
			case <-timer.C:
				cancel()
			case <-done:
			}
		case <-done:
		}
	}()
	var once sync.Once
	return settlementCtx, func() {
		once.Do(func() {
			close(done)
			cancel()
		})
	}
}

func (s *Stream) drainLine(ctx context.Context, rawLine []byte, emitDiagnostic bool) bool {
	if emitDiagnostic {
		emitRawFrame(s.proc, rawLine)
	}
	select {
	case <-ctx.Done():
		s.setErr(ctx.Err())
		s.emit(Event{Kind: EventError, Err: ctx.Err()})
		return false
	default:
	}
	line := strings.TrimSpace(string(rawLine))
	if line == "" {
		return true
	}
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(rawLine, &probe); err != nil {
		return true
	}
	if probe.Type == "response" {
		var resp rpcResponse
		if err := json.Unmarshal(rawLine, &resp); err != nil {
			return true
		}
		var responseErr error
		if isPromptResponse(resp) {
			responseErr = promptResponseError(resp)
		} else if !resp.Success {
			responseErr = failureResponseError(resp)
		}
		if responseErr != nil {
			s.setErr(responseErr)
			s.emit(Event{Kind: EventError, Err: responseErr})
			return false
		}
		// compact turn 不发 agent_end —— compact response 即终止信号。
		if resp.Command == "compact" {
			s.finish(ctx)
			return false
		}
		return true
	}
	var ev rpcEvent
	if err := json.Unmarshal(rawLine, &ev); err != nil {
		return true
	}
	s.handleRPCEvent(ev)
	s.observeAgentEnd(ev, line)
	if isTerminalEvent(ev) {
		s.settle(ctx)
		return false
	}
	return true
}

func (s *Stream) finish(ctx context.Context) {
	s.emitTerminalMetadata(ctx)
	s.emit(Event{Kind: EventDone})
}

func (s *Stream) emitTerminalMetadata(ctx context.Context) {
	if s.captureUserAnchor {
		s.emitTrackedSessionMetadata(ctx)
	} else {
		s.emitSessionStats(ctx)
	}
}

func (s *Stream) emitFailedTurnAnchorMetadata(ctx context.Context) {
	if s.captureUserAnchor {
		// drain already replaced the canceled turn context with one bounded
		// settlement context. Keep that bound while using the same scanner/process.
		s.emitTrackedSessionMetadata(ctx)
	}
}

func (s *Stream) settle(ctx context.Context) {
	if candidate := s.pendingAgentEndError; candidate != nil {
		// Pi appends the accepted user entry before an assistant error/abort. Capture
		// that post-turn boundary even when the caller canceled its turn context.
		s.emitFailedTurnAnchorMetadata(ctx)
		s.recordFinalErrorDiagnostics(candidate.event, candidate.rawLine)
		s.setErr(candidate.err)
		s.emit(Event{Kind: EventError, Err: candidate.err})
		return
	}
	if err := s.pendingAssistantDeltaError; err != nil {
		s.emitFailedTurnAnchorMetadata(ctx)
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
		emitRawFrame(s.proc, s.proc.lines.Bytes())
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
			s.applyTrackedSessionMetadata(metadata)
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
		emitRawFrame(s.proc, s.proc.lines.Bytes())
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
		// errorMessage is an untrusted provider payload and can echo prompt or
		// credentials. The retry state itself is the useful downstream signal.
		s.emit(Event{Kind: EventRuntimeStatus, Text: "retrying"})
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
			// reason is an untrusted provider failure string. Preserve only the
			// stable stream-failure classification until agent_end settles it.
			s.pendingAssistantDeltaError = errors.New("piagent: assistant stream failed")
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
		return agentEndFailure(msg, "error")
	case "aborted":
		return agentEndFailure(msg, "aborted")
	default:
		return agentEndOutcome{kind: agentEndOutcomeUnknown}
	}
}

func agentEndFailure(msg *assistantMessage, classification string) agentEndOutcome {
	var err error
	switch classification {
	case "aborted":
		if strings.TrimSpace(msg.ErrorMessage) == "" {
			err = errors.New("piagent: aborted")
		} else {
			err = errors.New("piagent: user requested abort")
		}
	case "error":
		if strings.EqualFold(strings.TrimSpace(msg.ErrorMessage), "terminated") {
			err = errors.New("piagent: terminated")
		} else {
			err = errors.New("piagent: final provider failure")
		}
	default:
		err = errors.New("piagent: agent failed")
	}
	return agentEndOutcome{kind: agentEndOutcomeFailure, err: err}
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

func (s *Stream) recordFinalErrorDiagnostics(ev rpcEvent, rawLine string) {
	msg := lastAssistantFromAgentEnd(ev.Messages)
	if msg == nil {
		return
	}
	safeFrame := sanitizeDiagnosticFrame([]byte(rawLine))
	s.mu.Lock()
	defer s.mu.Unlock()
	s.diagnostics.FinalErrorEventType = ev.Type
	s.diagnostics.FinalErrorStopReason = strings.TrimSpace(msg.StopReason)
	s.diagnostics.FinalErrorFrame = string(safeFrame)
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
