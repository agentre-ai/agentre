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
