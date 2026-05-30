package terminal_svc

import (
	"context"
	"fmt"
)

type Emitter interface {
	Emit(ctx context.Context, name string, payload any)
}

type EmitterFunc func(ctx context.Context, name string, payload any)

func (f EmitterFunc) Emit(ctx context.Context, name string, payload any) {
	if f != nil {
		f(ctx, name, payload)
	}
}

type NoopEmitter struct{}

func (NoopEmitter) Emit(context.Context, string, any) {}

// DataEventName is the canonical Wails event name for stdout chunks of a
// given terminalID. Frontend subscribes via EventsOn(DataEventName(id)).
func DataEventName(terminalID string) string {
	return fmt.Sprintf("terminal:%s:data", terminalID)
}

func ExitEventName(terminalID string) string {
	return fmt.Sprintf("terminal:%s:exit", terminalID)
}
