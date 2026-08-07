//go:build windows

package claudecode

import "os/exec"

// setProcessGroup 在 Windows 上是 no-op：没有 POSIX 进程组，SIGKILL 单进程即足够，
// 孙进程由系统的作业对象回收。
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessGroup 在 Windows 上退化为杀单进程。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
