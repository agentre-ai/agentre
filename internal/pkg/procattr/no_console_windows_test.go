//go:build windows

package procattr

import (
	"os/exec"
	"testing"
)

func TestApplyNoConsoleWindow(t *testing.T) {
	cmd := exec.Command("cmd", "/c", "ver")

	ApplyNoConsoleWindow(cmd)

	if cmd.SysProcAttr == nil {
		t.Fatal("ApplyNoConsoleWindow must configure SysProcAttr")
	}
	if !cmd.SysProcAttr.HideWindow {
		t.Fatal("ApplyNoConsoleWindow must hide the child console window")
	}
	if got, want := cmd.SysProcAttr.CreationFlags, uint32(createNoWindow); got&want != want {
		t.Fatalf("ApplyNoConsoleWindow CreationFlags = %#x, want CREATE_NO_WINDOW %#x", got, want)
	}
}
