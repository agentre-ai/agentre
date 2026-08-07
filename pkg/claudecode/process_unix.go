//go:build !windows

package claudecode

import (
	"os/exec"
	"syscall"
)

// setProcessGroup 让 CLI 子进程成为其自有进程组组长（Setpgid），这样 kill() 可以按
// 组 SIGKILL，把 CLI 派生的孙进程（MCP server、git、node 等）一起杀掉。否则孙进程
// 会继承并握住 stdout pipe 的写端，reaper 的 io.Copy 永远等不到 EOF → cmd.Wait 到
// 不了 → exit channel 永不关闭，卡死的会话永远收不了尾。
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessGroup 给整个子进程组发 SIGKILL（不可被忽略）。负 PID = 按组投递。
// 组已不存在（子进程可能刚退出）时退化为杀单进程，幂等。
func killProcessGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
