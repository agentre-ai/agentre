package piagent

import (
	"github.com/cago-frame/agents/provider"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	pkgpi "github.com/agentre-ai/agentre/pkg/piagent"
)

func translate(ev pkgpi.Event) (events []agentruntime.Event, usage *provider.Usage, stopErr error) {
	switch ev.Kind {
	case pkgpi.EventTextDelta:
		if ev.Text != "" {
			events = append(events, agentruntime.TextDelta{Text: ev.Text})
		}
	case pkgpi.EventThinkingDelta:
		if ev.Text != "" {
			events = append(events, agentruntime.ThinkingDelta{Text: ev.Text})
		}
	case pkgpi.EventPreToolUse:
		events = append(events, agentruntime.ToolCall{ID: ev.Tool.ID, Name: ev.Tool.Name, Input: ev.Tool.Input})
	case pkgpi.EventPostToolUse:
		events = append(events, agentruntime.ToolResult{ToolCallID: ev.Tool.ID, Content: ev.Tool.Content, IsError: ev.Tool.IsError})
	case pkgpi.EventUsage:
		u := ev.Usage
		usage = &u
		events = append(events, agentruntime.UsageUpdate{Usage: usage, TotalInputTokens: u.PromptTokens + u.CachedTokens + u.CacheCreationTokens})
	case pkgpi.EventContextWindow:
		if ev.ContextWindow > 0 {
			events = append(events, agentruntime.ContextWindowUpdated{Tokens: ev.ContextWindow})
		}
	case pkgpi.EventCompactBoundary:
		events = append(events, agentruntime.CompactBoundary{Trigger: "manual"})
	case pkgpi.EventRuntimeStatus:
		events = append(events, agentruntime.RuntimeStatus{Status: ev.Text})
	case pkgpi.EventError:
		stopErr = ev.Err
	case pkgpi.EventDone:
		events = append(events, agentruntime.Done{})
	}
	return events, usage, stopErr
}
