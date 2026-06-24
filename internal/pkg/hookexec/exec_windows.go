//go:build windows

package hookexec

import (
	"os/exec"
	"strconv"
)

func setSysProcAttr(cmd *exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// /T 连子进程树一起杀，/F 强制。
	return exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
