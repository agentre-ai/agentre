package handlers

import (
	"context"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/blocks"
	"agentre/internal/service/chat_svc/turn"
)

// MarkRunningSubagentsCancelled 在 turn abort 收尾时把仍 running 的 SubagentStateBlock
// 改成 "canceled"。SubagentDoneHandler 是唯一改 Status 的正常路径,但 CLI 被
// interrupt 后 Done 事件不会到,导致 status 留在 "running" 被原样落 DB → 前端
// AgentSpawnCard 永远 spin。仅命中 *SubagentStateBlock(acc.AddBlock 加进来的
// 始终是指针;反序列化得到的 value 形态此处不出现)。已 completed/failed 的不动。
func MarkRunningSubagentsCancelled(finalBlocks []cagoblocks.ContentBlock) {
	for _, b := range finalBlocks {
		sb, ok := b.(*blocks.SubagentStateBlock)
		if !ok {
			continue
		}
		if sb.Status == "running" {
			sb.Status = "canceled"
		}
	}
}

type SubagentStartedHandler struct{}

func (SubagentStartedHandler) Apply(ctx context.Context, ev agentruntime.Event, acc *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	r := ev.(agentruntime.SubagentStarted)
	blk := &blocks.SubagentStateBlock{
		ParentToolCallID: r.ToolCallID,
		Status:           "running",
	}
	acc.AddBlock(blk, "subagent_state:"+r.ToolCallID)

	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind":      "subagent_started",
			"toolUseId": r.ToolCallID,
			"info":      r.Info,
		})
	}
	return nil
}

type SubagentProgressHandler struct{}

func (SubagentProgressHandler) Apply(ctx context.Context, ev agentruntime.Event, acc *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	r := ev.(agentruntime.SubagentProgress)
	turn.Mutate[blocks.SubagentStateBlock](acc, "subagent_state:"+r.ToolCallID, func(b *blocks.SubagentStateBlock) {
		b.TotalTokens = r.Info.TotalTokens
		b.LastToolName = r.Info.LastToolName
		b.ToolUses = r.Info.ToolUses
	})
	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind":      "subagent_progress",
			"toolUseId": r.ToolCallID,
			"info":      r.Info,
		})
	}
	return nil
}

type SubagentDoneHandler struct{}

func (SubagentDoneHandler) Apply(ctx context.Context, ev agentruntime.Event, acc *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	r := ev.(agentruntime.SubagentDone)
	turn.Mutate[blocks.SubagentStateBlock](acc, "subagent_state:"+r.ToolCallID, func(b *blocks.SubagentStateBlock) {
		status := r.Info.Status
		if status == "" {
			status = "completed"
		}
		b.Status = status
		b.TotalTokens = r.Info.TotalTokens
		b.DurationMs = r.Info.DurationMs
		b.ToolUses = r.Info.ToolUses
	})
	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind":      "subagent_done",
			"toolUseId": r.ToolCallID,
			"info":      r.Info,
		})
	}
	return nil
}
