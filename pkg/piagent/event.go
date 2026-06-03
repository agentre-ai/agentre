package piagent

import "github.com/cago-frame/agents/provider"

type EventKind string

const (
	EventTextDelta       EventKind = "text_delta"
	EventThinkingDelta   EventKind = "thinking_delta"
	EventPreToolUse      EventKind = "pre_tool_use"
	EventPostToolUse     EventKind = "post_tool_use"
	EventUsage           EventKind = "usage"
	EventContextWindow   EventKind = "context_window"
	EventCompactBoundary EventKind = "compact_boundary"
	EventRuntimeStatus   EventKind = "runtime_status"
	// EventUserMessage Pi 把用户消息（首条 prompt 或 mid-turn steer 注入）回显
	// 成一条 user message。Text 是该消息文本，runtime 用它对照 pending steer
	// FIFO 配对，命中即 emit SteerConsumed（与 codex EventUserMessage 同构）。
	EventUserMessage EventKind = "user_message"
	EventError       EventKind = "error"
	EventDone        EventKind = "done"
)

type Event struct {
	Kind          EventKind
	Text          string
	Tool          ToolEvent
	Usage         provider.Usage
	Model         string
	ContextWindow int
	SessionID     string
	Err           error
}

type ToolEvent struct {
	ID      string
	Name    string
	Input   []byte
	Content string
	IsError bool
}
