//go:build !windows

package hookexec

import (
	"os/exec"
	"syscall"
)

func setSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// 负 PID = 杀整个进程组。
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
