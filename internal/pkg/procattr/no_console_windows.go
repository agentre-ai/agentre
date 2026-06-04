//go:build windows

package procattr

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// ApplyNoConsoleWindow prevents console windows from flashing when the Wails
// GUI process starts background CLI subprocesses on Windows.
func ApplyNoConsoleWindow(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
