package app

import (
	"path/filepath"
	"testing"
)

func TestResolveRelaunchTargetFromExecutablePath(t *testing.T) {
	t.Parallel()

	t.Run("mac app bundle", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("darwin", "/Applications/Agentre.app/Contents/MacOS/agentre")
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if got, want := filepath.ToSlash(target.appBundlePath), "/Applications/Agentre.app"; got != want {
			t.Fatalf("appBundlePath = %q, want %q", got, want)
		}
	})

	t.Run("mac app backup bundle", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("darwin", "/Applications/Agentre.app.backup/Contents/MacOS/agentre")
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if got, want := filepath.ToSlash(target.appBundlePath), "/Applications/Agentre.app"; got != want {
			t.Fatalf("appBundlePath = %q, want %q", got, want)
		}
	})

	t.Run("mac non bundle", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("darwin", "/tmp/agentre")
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if target.appBundlePath != "" {
			t.Fatalf("appBundlePath = %q, want empty", target.appBundlePath)
		}
		if target.executablePath != "/tmp/agentre" {
			t.Fatalf("executablePath = %q, want /tmp/agentre", target.executablePath)
		}
	})

	t.Run("mac non bundle backup executable", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("darwin", "/tmp/agentre.backup")
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if target.executablePath != "/tmp/agentre" {
			t.Fatalf("executablePath = %q, want /tmp/agentre", target.executablePath)
		}
	})

	t.Run("windows old executable", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("windows", `C:\Users\me\AppData\Local\Agentre\agentre.exe.old`)
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if want := `C:\Users\me\AppData\Local\Agentre\agentre.exe`; target.executablePath != want {
			t.Fatalf("executablePath = %q, want %q", target.executablePath, want)
		}
	})

	t.Run("linux deleted backup executable", func(t *testing.T) {
		t.Parallel()

		target, err := resolveRelaunchTargetFromExecutablePath("linux", "/opt/agentre/agentre.backup (deleted)")
		if err != nil {
			t.Fatalf("resolveRelaunchTargetFromExecutablePath() error = %v", err)
		}
		if target.executablePath != "/opt/agentre/agentre" {
			t.Fatalf("executablePath = %q, want /opt/agentre/agentre", target.executablePath)
		}
	})

	t.Run("empty executable path errors", func(t *testing.T) {
		t.Parallel()

		if _, err := resolveRelaunchTargetFromExecutablePath("darwin", "   "); err == nil {
			t.Fatal("resolveRelaunchTargetFromExecutablePath() expected error for empty path, got nil")
		}
	})
}
