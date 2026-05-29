package app

import (
	"context"
	"testing"
)

func TestResetStaleSessionsOnStartup(t *testing.T) {
	called := 0
	orig := resetStaleActiveSessions
	resetStaleActiveSessions = func(context.Context) error {
		called++
		return nil
	}
	t.Cleanup(func() { resetStaleActiveSessions = orig })

	a := &App{}
	a.resetStaleSessionsOnStartup(context.Background())

	if called != 1 {
		t.Fatalf("resetStaleActiveSessions called %d times, want 1", called)
	}
}
