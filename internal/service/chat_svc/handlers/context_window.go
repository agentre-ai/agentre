package handlers

import (
	"context"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

// ContextWindowWriter handler 通过这个把 tokens 写到 session.ContextWindow。
type ContextWindowWriter interface {
	WriteContextWindow(sess any, tokens int)
}

type ContextWindowUpdatedHandler struct {
	Writer ContextWindowWriter
}

// Apply 把 runtime 探到的 model context window 写回 session 字段 + emit patch。
// Tokens=0 视为"未探到",no-op。
func (h ContextWindowUpdatedHandler) Apply(ctx context.Context, ev agentruntime.Event, _ *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	r := ev.(agentruntime.ContextWindowUpdated)
	if r.Tokens <= 0 {
		return nil
	}
	if tc != nil && h.Writer != nil && tc.Session != nil {
		h.Writer.WriteContextWindow(tc.Session, r.Tokens)
	}
	if tc != nil && tc.SessionUpdater != nil && tc.Session != nil {
		_ = tc.SessionUpdater.Update(context.WithoutCancel(ctx), tc.Session)
	}
	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind": "session_status",
			"sessionStatus": map[string]any{
				"contextWindow": r.Tokens,
			},
		})
	}
	return nil
}
