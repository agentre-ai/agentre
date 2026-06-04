// internal/service/chat_svc/emitter.go
package chat_svc

import (
	"context"
	"fmt"
)

// Emitter abstracts wails runtime.EventsEmit so service tests can inject a
// recorder. Production wiring is in app.go.
type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

// EmitterFunc is the convenience adapter.
type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

// NoopEmitter discards events (used by service constructors when not yet wired).
type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, string, any) {}

// StreamName returns the canonical wails event name for a given turn.
func StreamName(sessionID, assistantMessageID int64) string {
	return fmt.Sprintf("%s:%d:%d", StreamEventPrefix, sessionID, assistantMessageID)
}

// AutonomousStreamName 是会话级(非 per-turn)的 wails 事件名:
// "chat:autonomous:<sessionID>"。前端 ChatPanel 在挂载某会话时常驻订阅它,接到
// StreamAutonomousStarted 后插入新 assistant 行并 openStream 订阅 Stream 携带的
// per-turn 流。per-turn 流(StreamName)只在 user 主动发起时由 Send 响应给出,自主轮
// 没有这个入口,所以需要这条会话级旁路把 stream 名推给前端。
func AutonomousStreamName(sessionID int64) string {
	return fmt.Sprintf("%s:%d", AutonomousEventPrefix, sessionID)
}
