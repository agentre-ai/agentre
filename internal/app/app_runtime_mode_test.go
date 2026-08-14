package app

import "testing"

func TestAppInfoReportsExplicitRuntimeMode(t *testing.T) {
	t.Run("Given an interactive desktop entry When Info is requested Then interactive is reported", func(t *testing.T) {
		got := NewApp(RuntimeModeInteractive).Info().RuntimeMode
		if got != RuntimeModeInteractive {
			t.Fatalf("RuntimeMode = %q, want %q", got, RuntimeModeInteractive)
		}
	})

	t.Run("Given a headless desktop entry When Info is requested Then headless is reported", func(t *testing.T) {
		got := NewApp(RuntimeModeHeadless).Info().RuntimeMode
		if got != RuntimeModeHeadless {
			t.Fatalf("RuntimeMode = %q, want %q", got, RuntimeModeHeadless)
		}
	})

	t.Run("Given an unknown runtime mode When App is constructed Then it remains unknown for fail-closed clients", func(t *testing.T) {
		got := NewApp(RuntimeMode("unexpected")).Info().RuntimeMode
		if got != RuntimeModeUnknown {
			t.Fatalf("RuntimeMode = %q, want %q", got, RuntimeModeUnknown)
		}
	})
}
