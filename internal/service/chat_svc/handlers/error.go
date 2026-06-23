package handlers

import (
	"context"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

// ErrorWriter handler 通过这个把 error text patch 到 assistantMsg。
type ErrorWriter interface {
	WriteErrorText(msg any, errText string)
}

type ErrorHandler struct {
	Writer ErrorWriter
}

// Apply 把错误信息写到 assistantMsg.ErrorText 并 emit StreamError。
// dispatcher 调完后,chat.go runTurn 会断开 stream(dispatcher 不直接负责关闭)。
func (h ErrorHandler) Apply(ctx context.Context, ev agentruntime.Event, _ *turn.Accumulator, emit turn.Emitter, _ turn.View, tc *turn.TurnContext) error {
	e := ev.(agentruntime.ErrorEvent)
	msg := ""
	if e.Err != nil {
		msg = e.Err.Error()
	}
	if tc != nil && h.Writer != nil && tc.AssistantMsg != nil && msg != "" {
		h.Writer.WriteErrorText(tc.AssistantMsg, msg)
	}
	if tc != nil && tc.MessageUpdater != nil && tc.AssistantMsg != nil {
		_ = tc.MessageUpdater.Update(context.WithoutCancel(ctx), tc.AssistantMsg)
	}
	if emit != nil {
		emit.Emit(ctx, streamOf(tc), map[string]any{
			"kind":  "error",
			"error": msg,
		})
	}
	return nil
}
