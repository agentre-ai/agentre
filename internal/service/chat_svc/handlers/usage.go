package handlers

import (
	"context"

	"agentre/internal/pkg/agentruntime"
	"agentre/internal/service/chat_svc/turn"
)

// UsageWriter handler 通过这个把 per-call usage 写到 assistantMsg。
// chat_svc 在 wire 时实现:把 ev.Usage 字段往 *chat_entity.ChatMessage 上 patch。
// MessageID 返回 assistantMsg.ID,emit 时附带让前端按消息匹配。
type UsageWriter interface {
	WriteUsage(msg any, u *agentruntime.UsageUpdate)
	MessageID(msg any) int64
}

type UsageUpdateHandler struct {
	// Writer 可选,nil 时仅 emit。chat_svc 在 dispatcher Register 时注入。
	Writer UsageWriter
}

// Apply 把 per-call usage 写回 assistantMsg 并 emit StreamUsage 中间形态。
// MessageUpdater 也由 chat_svc 注入(context.WithoutCancel 抗 abort,spec §1.4)。
func (h UsageUpdateHandler) Apply(ctx context.Context, ev agentruntime.Event, _ *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	u := ev.(agentruntime.UsageUpdate)
	if u.Usage == nil {
		return nil
	}
	if tc != nil && h.Writer != nil && tc.AssistantMsg != nil {
		h.Writer.WriteUsage(tc.AssistantMsg, &u)
	}
	if tc != nil && tc.MessageUpdater != nil && tc.AssistantMsg != nil {
		_ = tc.MessageUpdater.Update(context.WithoutCancel(ctx), tc.AssistantMsg)
	}
	if emit != nil {
		var msgID int64
		if tc != nil && h.Writer != nil && tc.AssistantMsg != nil {
			msgID = h.Writer.MessageID(tc.AssistantMsg)
		}
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind": "usage",
			"usage": map[string]any{
				"messageId":           msgID,
				"promptTokens":        u.Usage.PromptTokens,
				"completionTokens":    u.Usage.CompletionTokens,
				"cachedTokens":        u.Usage.CachedTokens,
				"cacheCreationTokens": u.Usage.CacheCreationTokens,
				"reasoningTokens":     u.Usage.ReasoningTokens,
				"totalInputTokens":    u.TotalInputTokens,
			},
		})
	}
	return nil
}
