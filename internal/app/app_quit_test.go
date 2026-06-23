package app

import (
	"context"
	"errors"
	"testing"

	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

func TestShouldPreventQuit(t *testing.T) {
	ctx := context.Background()

	t.Run("confirmed quit is allowed without counting or emitting", func(t *testing.T) {
		countCalls, emitCalls := 0, 0
		prevent := shouldPreventQuit(ctx, true,
			func(context.Context) (int, error) { countCalls++; return 3, nil },
			func(int) { emitCalls++ })
		if prevent {
			t.Fatal("confirmed quit must be allowed (prevent=false)")
		}
		if countCalls != 0 {
			t.Fatalf("count called %d times, want 0 (short-circuit on confirmed)", countCalls)
		}
		if emitCalls != 0 {
			t.Fatalf("emit called %d times, want 0", emitCalls)
		}
	})

	t.Run("no active sessions is allowed without emitting", func(t *testing.T) {
		emitCalls := 0
		prevent := shouldPreventQuit(ctx, false,
			func(context.Context) (int, error) { return 0, nil },
			func(int) { emitCalls++ })
		if prevent {
			t.Fatal("zero active sessions must be allowed (prevent=false)")
		}
		if emitCalls != 0 {
			t.Fatalf("emit called %d times, want 0", emitCalls)
		}
	})

	t.Run("active sessions are prevented and emit the count", func(t *testing.T) {
		emitCalls, emitted := 0, 0
		prevent := shouldPreventQuit(ctx, false,
			func(context.Context) (int, error) { return 2, nil },
			func(n int) { emitCalls++; emitted = n })
		if !prevent {
			t.Fatal("active sessions must prevent quit (prevent=true)")
		}
		if emitCalls != 1 {
			t.Fatalf("emit called %d times, want 1", emitCalls)
		}
		if emitted != 2 {
			t.Fatalf("emitted count = %d, want 2", emitted)
		}
	})

	t.Run("count error fails open (allow) without emitting", func(t *testing.T) {
		emitCalls := 0
		prevent := shouldPreventQuit(ctx, false,
			func(context.Context) (int, error) { return 0, errors.New("db down") },
			func(int) { emitCalls++ })
		if prevent {
			t.Fatal("count error must fail open (prevent=false) so the user is never trapped")
		}
		if emitCalls != 0 {
			t.Fatalf("emit called %d times, want 0", emitCalls)
		}
	})
}

// TestOnBeforeClose_NilChatServiceFailsOpen guards the race where the user quits
// before Startup finishes wiring the chat service. Wails runs OnStartup in a
// goroutine concurrent with the window run loop (darwin/windows/linux all do), so
// cmd+Q / close button can fire OnBeforeClose while chat_svc.Chat() is still nil.
// That must fail open (allow quit), never panic on a nil-interface dereference.
func TestOnBeforeClose_NilChatServiceFailsOpen(t *testing.T) {
	prev := chat_svc.Chat()
	t.Cleanup(func() { chat_svc.RegisterChat(prev) })
	chat_svc.RegisterChat(nil) // simulate the pre-registration window

	a := NewApp()
	// quitConfirmed defaults to false → OnBeforeClose has to count active sessions.
	if prevent := a.OnBeforeClose(context.Background()); prevent {
		t.Fatal("early quit before chat service registration must fail open (prevent=false)")
	}
}
