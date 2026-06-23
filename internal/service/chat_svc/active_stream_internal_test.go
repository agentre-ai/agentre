package chat_svc

import (
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
)

// activeStreamName backs LoadSession.ActiveStream: when a turn is in-flight, a frontend
// opening the session mid-turn must be able to reattach to the live per-turn stream. The
// stream name is reconstructed from the in-flight (last) assistant message.
func TestActiveStreamName(t *testing.T) {
	msgs := []*chat_entity.Message{
		{ID: 1, Role: "user"},
		{ID: 2, Role: "assistant"},
		{ID: 3, Role: "user"},
		{ID: 4, Role: "assistant"},
	}

	t.Run("active turn points at the last assistant message", func(t *testing.T) {
		if got, want := activeStreamName(true, 7, msgs), StreamName(7, 4); got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	})

	t.Run("no active turn returns empty", func(t *testing.T) {
		if got := activeStreamName(false, 7, msgs); got != "" {
			t.Fatalf("got %q want empty", got)
		}
	})

	t.Run("active turn with no assistant message yet returns empty", func(t *testing.T) {
		only := []*chat_entity.Message{{ID: 1, Role: "user"}}
		if got := activeStreamName(true, 7, only); got != "" {
			t.Fatalf("got %q want empty", got)
		}
	})
}
