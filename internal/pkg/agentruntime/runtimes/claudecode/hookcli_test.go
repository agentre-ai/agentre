package claudecode

import (
	"os"
	"testing"
)

func TestHookBin_UsesInjectedPath(t *testing.T) {
	r := New()
	r.SetHookCLIPath("/data/bin/agrctl")
	got, err := r.hookBin()
	if err != nil {
		t.Fatalf("hookBin: %v", err)
	}
	if got != "/data/bin/agrctl" {
		t.Fatalf("hookBin = %q, want injected /data/bin/agrctl", got)
	}
}

func TestHookBin_FallsBackToExecutable(t *testing.T) {
	r := New() // no SetHookCLIPath
	got, err := r.hookBin()
	if err != nil {
		t.Fatalf("hookBin: %v", err)
	}
	exe, _ := os.Executable()
	if got != exe {
		t.Fatalf("hookBin = %q, want os.Executable() %q", got, exe)
	}
}
