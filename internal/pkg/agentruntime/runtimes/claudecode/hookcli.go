package claudecode

import "os"

// SetHookCLIPath 由 bootstrap 注入 PostToolUse hook 要 exec 的 CLI 路径
// （桌面端 = <AppDataDir>/bin/agrctl）。为空则 hookBin 回落 os.Executable()。
func (r *Runtime) SetHookCLIPath(p string) { r.hookCLIPath = p }

// hookBin 返回注册进 PostToolUse hook 的可执行文件路径：优先注入路径，否则回落当前可执行文件
// （agentred 守护进程走此回落，它自带 claudecode 子命令）。
func (r *Runtime) hookBin() (string, error) {
	if r.hookCLIPath != "" {
		return r.hookCLIPath, nil
	}
	return os.Executable()
}
