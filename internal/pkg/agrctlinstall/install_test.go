package agrctlinstall

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeSource(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "agrctl-src")
	if err := os.WriteFile(src, []byte(content), 0o755); err != nil {
		t.Fatalf("write source: %v", err)
	}
	return src
}

func TestInstalledPath(t *testing.T) {
	got := InstalledPath("/data")
	want := filepath.Join("/data", "bin", binName())
	if got != want {
		t.Fatalf("InstalledPath = %q, want %q", got, want)
	}
	if runtime.GOOS == "windows" && filepath.Base(got) != "agrctl.exe" {
		t.Fatalf("windows bin name = %q, want agrctl.exe", filepath.Base(got))
	}
}

func TestEnsureInstalled_FreshInstall(t *testing.T) {
	data := t.TempDir()
	src := writeSource(t, "BINARY-V1")

	path, installed, err := EnsureInstalled(data, src, "v1")
	if err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}
	if !installed {
		t.Fatal("installed = false, want true on fresh install")
	}
	if path != InstalledPath(data) {
		t.Fatalf("path = %q, want %q", path, InstalledPath(data))
	}
	got, _ := os.ReadFile(path) // #nosec G304 -- path 由 InstalledPath 从 temp dataDir 拼接，测试内部构造。
	if string(got) != "BINARY-V1" {
		t.Fatalf("installed content = %q", got)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm()&0o100 == 0 {
			t.Fatalf("installed binary not executable: %v", info.Mode())
		}
	}
}

func TestEnsureInstalled_UpToDateIsNoop(t *testing.T) {
	data := t.TempDir()
	src := writeSource(t, "BINARY-V1")
	if _, _, err := EnsureInstalled(data, src, "v1"); err != nil {
		t.Fatalf("first install: %v", err)
	}
	// Source changes but version stamp is unchanged → must NOT reinstall.
	src2 := writeSource(t, "BINARY-V1-CHANGED")
	_, installed, err := EnsureInstalled(data, src2, "v1")
	if err != nil {
		t.Fatalf("second install: %v", err)
	}
	if installed {
		t.Fatal("installed = true, want false when version unchanged")
	}
	got, _ := os.ReadFile(InstalledPath(data))
	if string(got) != "BINARY-V1" {
		t.Fatalf("content changed on no-op: %q", got)
	}
}

func TestEnsureInstalled_StaleReinstalls(t *testing.T) {
	data := t.TempDir()
	if _, _, err := EnsureInstalled(data, writeSource(t, "OLD"), "v1"); err != nil {
		t.Fatalf("v1 install: %v", err)
	}
	path, installed, err := EnsureInstalled(data, writeSource(t, "NEW"), "v2")
	if err != nil {
		t.Fatalf("v2 install: %v", err)
	}
	if !installed {
		t.Fatal("installed = false, want true on version bump")
	}
	got, _ := os.ReadFile(path) // #nosec G304 -- path 由 EnsureInstalled 返回，测试内部构造。
	if string(got) != "NEW" {
		t.Fatalf("content = %q, want NEW after reinstall", got)
	}
}

func TestEnsureInstalled_MissingSourceErrors(t *testing.T) {
	data := t.TempDir()
	_, _, err := EnsureInstalled(data, filepath.Join(t.TempDir(), "nope"), "v1")
	if err == nil {
		t.Fatal("expected error for missing source, got nil")
	}
}

func TestBundledSourceFrom(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Agentre")
	// sibling absent → ok=false
	if _, ok := bundledSourceFrom(exe); ok {
		t.Fatal("ok = true with no sibling agrctl")
	}
	// create sibling → ok=true, path points at it
	sib := filepath.Join(dir, binName())
	if err := os.WriteFile(sib, []byte("x"), 0o755); err != nil {
		t.Fatalf("write sibling: %v", err)
	}
	got, ok := bundledSourceFrom(exe)
	if !ok || got != sib {
		t.Fatalf("bundledSourceFrom = (%q,%v), want (%q,true)", got, ok, sib)
	}
}
