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

	mu        sync.RWMutex
	sessionID string
	model     string
	err       error
	cur       Event

	closeOnce sync.Once
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

func (s *Stream) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
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
				s.emit(Event{Kind: EventDone})
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
		if isTerminalEvent(ev.Type) {
			s.emit(Event{Kind: EventDone})
			return
		}
	}
	if err := processDeadOrScanError(s.proc); err != nil {
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
	}
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
		err := fmt.Errorf("piagent: %s", strings.TrimSpace(delta.Reason))
		s.setErr(err)
		s.emit(Event{Kind: EventError, Err: err})
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
