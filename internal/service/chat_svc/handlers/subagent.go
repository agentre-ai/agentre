package handlers

import (
	"context"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
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

// subagentKindLocalBash 是 CLI task_type 里后台 bash 的取值(对应 SubagentStateBlock.Kind)。
const subagentKindLocalBash = "local_bash"

// trackSubagentState 判定这次 task 帧是否该建/维护 SubagentStateBlock overlay。
// 真实 CLI 对*每一次* Bash 都发 task_type:"local_bash" 帧,但只有 run_in_background
// 的 bash 才是真正的后台任务;普通前台 bash 不该有 overlay(否则污染后台任务面板 +
// 白存一堆无意义持久化块)。subagent(local_agent)与空 kind 一律 track。找不到对应
// tool_use 块时保守 track —— 真实流里 Bash tool_use 总先于 task_started 到达,找不到
// 属异常,宁可多挂一个也不漏掉真后台任务。
func trackSubagentState(acc *turn.Accumulator, toolCallID, kind string) bool {
	if kind != subagentKindLocalBash {
		return true
	}
	input, ok := acc.ToolUseInput(toolCallID)
	if !ok {
		return true
	}
	bg, _ := input["run_in_background"].(bool)
	return bg
}

type SubagentStartedHandler struct{}

func (SubagentStartedHandler) Apply(ctx context.Context, ev agentruntime.Event, acc *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	r := ev.(agentruntime.SubagentStarted)
	if !trackSubagentState(acc, r.ToolCallID, r.Info.Kind) {
		return nil // 前台 bash:不建 overlay,也不 emit(后续 progress/done 经 Mutate 未命中自然静默)
	}
	blk := &blocks.SubagentStateBlock{
		ParentToolCallID: r.ToolCallID,
		Kind:             r.Info.Kind,
		Description:      r.Info.TaskDescription,
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
	// task_progress 帧不带 task_type,无法自己判前台/后台;靠 Mutate 是否命中既有
	// overlay 来判定 —— 前台 bash 在 Started 已被跳过,这里命中不到 → 不 emit 孤儿事件。
	hit := turn.Mutate[blocks.SubagentStateBlock](acc, "subagent_state:"+r.ToolCallID, func(b *blocks.SubagentStateBlock) {
		b.TotalTokens = r.Info.TotalTokens
		b.LastToolName = r.Info.LastToolName
		b.ToolUses = r.Info.ToolUses
	})
	if !hit {
		return nil
	}
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
	// 同 Progress:命中不到既有 overlay(前台 bash)→ 不 emit 孤儿事件。后台 bash 的
	// 完成是跨轮的(autonomous turn 经 FlipSubagentStatus 定向翻转),不走这条 handler。
	hit := turn.Mutate[blocks.SubagentStateBlock](acc, "subagent_state:"+r.ToolCallID, func(b *blocks.SubagentStateBlock) {
		status := r.Info.Status
		if status == "" {
			status = "completed"
		}
		b.Status = status
		b.TotalTokens = r.Info.TotalTokens
		b.DurationMs = r.Info.DurationMs
		b.ToolUses = r.Info.ToolUses
	})
	if !hit {
		return nil
	}
	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind":      "subagent_done",
			"toolUseId": r.ToolCallID,
			"info":      r.Info,
		})
	}
	return nil
}
