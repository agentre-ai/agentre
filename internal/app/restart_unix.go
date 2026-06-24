//go:build !windows && !darwin

package app

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

// startRelaunchHelper 拉起一个脱离本进程组(Setsid)的 sh 脚本:
// 先轮询等待旧进程 PID 消失,再延迟一拍 nohup 拉起新实例。
func startRelaunchHelper(pid int, target relaunchTarget) error {
	launchPath := target.executablePath
	if launchPath == "" {
		return fmt.Errorf("restart target executable is empty")
	}

	script := `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done
sleep 0.3
nohup "$2" >/dev/null 2>&1 &`
	// #nosec G204 -- script 为常量字符串;launchPath 是应用自身经内部解析的可执行路径,非用户输入。
	cmd := exec.Command("/bin/sh", "-c", script, "agentre-restart", strconv.Itoa(pid), launchPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
