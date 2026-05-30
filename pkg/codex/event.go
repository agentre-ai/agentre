package codex

import (
	"encoding/json"

	"github.com/cago-frame/agents/provider"
)

type EventKind string

const (
	EventTextDelta        EventKind = "text_delta"
	EventThinkingDelta    EventKind = "thinking_delta"
	EventUserMessage      EventKind = "user_message"
	EventPreToolUse       EventKind = "pre_tool_use"
	EventPostToolUse      EventKind = "post_tool_use"
	EventApprovalRequest  EventKind = "approval_request"
	EventRequestUserInput EventKind = "request_user_input"
	EventPlanUpdated      EventKind = "plan_updated"
	EventUsage            EventKind = "usage"
	EventCompactBoundary  EventKind = "compact_boundary"
	EventRetry            EventKind = "retry"
	EventDone             EventKind = "done"
	EventError            EventKind = "error"
)

type ToolSource string

const (
	ToolSourceUnknown ToolSource = ""
	ToolSourceMCP     ToolSource = "mcp"
	ToolSourceBuiltin ToolSource = "builtin"
)

type ToolEvent struct {
	ID       string
	Name     string
	Input    json.RawMessage
	Response json.RawMessage
	Err      error
	Source   ToolSource
}

type ApprovalRequestEvent struct {
	RequestID string
	ThreadID  string
	TurnID    string
	ItemID    string
	ToolName  string
	Input     json.RawMessage
}

type RetryEvent struct {
	Message           string
	AdditionalDetails string
	Attempt           int
	MaxAttempts       int
}

type CompactEvent struct {
	PreTokens int
	Trigger   string
}

type RequestUserInputOption struct {
	Label       string
	Description string
}

type RequestUserInputQuestion struct {
	ID       string
	Header   string
	Question string
	IsOther  bool
	IsSecret bool
	Options  []RequestUserInputOption
}

type RequestUserInputEvent struct {
	RequestID string
	ThreadID  string
	TurnID    string
	ItemID    string
	Questions []RequestUserInputQuestion
}

type PlanStep struct {
	Step   string
	Status string
}

type Event struct {
	Kind             EventKind
	SessionID        string
	Text             string
	Tool             *ToolEvent
	Approval         *ApprovalRequestEvent
	RequestUserInput *RequestUserInputEvent
	Plan             []PlanStep
	PlanText         string
	Retry            *RetryEvent
	Compact          *CompactEvent
	Usage            provider.Usage
	// ContextWindow 是 runtime 上报的模型上下文窗口大小（tokens）。
	// 仅在 EventUsage / EventDone 上有值；0 表示 codex app-server 在本轮没报，
	// 调用方按 cago catalog / 用户配置 fallback。
	ContextWindow int
	Err           error
	Raw           json.RawMessage
}
