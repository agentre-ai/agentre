//go:build darwin

package app

import (
	"fmt"
	"os/exec"
	"strconv"
	"syscall"
)

// startRelaunchHelper 拉起一个脱离本进程组(Setsid)的 sh 脚本:
// 先轮询等待旧进程 PID 消失,再延迟一拍拉起新实例。
// 命中 .app bundle 时用 open -n(macOS 原生启动),否则裸 nohup exec。
func startRelaunchHelper(pid int, target relaunchTarget) error {
	launchPath := target.executablePath
	mode := "exec"
	if target.appBundlePath != "" {
		launchPath = target.appBundlePath
		mode = "app"
	}
	if launchPath == "" {
		return fmt.Errorf("restart target path is empty")
	}

	script := `while kill -0 "$1" 2>/dev/null; do sleep 0.1; done
sleep 0.3
if [ "$3" = "app" ]; then
  open -n "$2"
else
  nohup "$2" >/dev/null 2>&1 &
fi`
	// #nosec G204 -- script 为常量字符串;launchPath 是应用自身经内部解析的可执行/包路径,非用户输入。
	cmd := exec.Command("/bin/sh", "-c", script, "agentre-restart", strconv.Itoa(pid), launchPath, mode)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
