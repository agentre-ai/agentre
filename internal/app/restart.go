package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// 旧进程收到 Quit 前预留的缓冲：留一拍让 startRelaunchHelper 已经 fork 出去,
// 同时 helper 内部会 kill -0 轮询直到旧进程真正退出才拉起新实例。
const relaunchQuitDelay = 100 * time.Millisecond

// relaunchTarget 描述重启时要拉起的目标。
// executablePath 始终是规整后的可执行文件路径;appBundlePath 仅 macOS 命中 .app 时填充。
type relaunchTarget struct {
	executablePath string
	appBundlePath  string
}

// RestartApp 安装完成后重启应用本体。
//
// 关键时序:必须先让旧进程退出、再拉起新实例,否则新实例会撞上生产构建的
// SingleInstanceLock(见 main.go),被 Wails 当作二次启动直接终止 → "重启"静默失效。
// 因此这里启动一个脱离本进程的 helper:它先等旧进程 PID 消失,再拉起新实例;
// 本进程随后短暂延迟后调用 Quit 退出。
func (a *App) RestartApp() error {
	target, err := currentRelaunchTarget()
	if err != nil {
		return err
	}
	if err := startRelaunchHelper(os.Getpid(), target); err != nil {
		return err
	}

	// 重启已交给 helper,旧进程必须无条件退出:标记已确认,绕过活跃会话二次确认,
	// 否则 OnBeforeClose 拦住旧进程,helper 永远等不到它退出 → 新实例拉不起来。
	a.quitConfirmed.Store(true)

	go func() {
		time.Sleep(relaunchQuitDelay)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// currentRelaunchTarget 解析当前进程对应的重启目标,解引用 symlink。
func currentRelaunchTarget() (relaunchTarget, error) {
	executablePath, err := os.Executable()
	if err != nil {
		return relaunchTarget{}, fmt.Errorf("get executable path failed: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executablePath); err == nil {
		executablePath = resolved
	}
	return resolveRelaunchTargetFromExecutablePath(runtime.GOOS, executablePath)
}

// resolveRelaunchTargetFromExecutablePath 从可执行文件路径推导重启目标(纯函数,便于测试)。
func resolveRelaunchTargetFromExecutablePath(goos, executablePath string) (relaunchTarget, error) {
	if strings.TrimSpace(executablePath) == "" {
		return relaunchTarget{}, fmt.Errorf("executable path is empty")
	}

	target := relaunchTarget{
		executablePath: normalizeExecutablePathForRelaunch(goos, executablePath),
	}
	if goos == "darwin" {
		if appBundlePath, ok := findMacAppBundlePath(executablePath); ok {
			target.appBundlePath = appBundlePath
		}
	}
	return target, nil
}

// normalizeExecutablePathForRelaunch 去除更新过程残留的备份后缀(.old / .backup)和
// "(deleted)" 标记,得到更新落地后应当存在的真实路径。
func normalizeExecutablePathForRelaunch(goos, executablePath string) string {
	path := strings.TrimSuffix(executablePath, " (deleted)")
	switch goos {
	case "windows":
		if strings.HasSuffix(strings.ToLower(path), ".exe.old") {
			return path[:len(path)-len(".old")]
		}
	case "darwin", "linux":
		if after, ok := strings.CutSuffix(path, ".backup"); ok {
			return after
		}
	}
	return path
}

// findMacAppBundlePath 沿目录向上查找 .app bundle;命中 .app.backup 时回退到正式 .app 路径。
func findMacAppBundlePath(executablePath string) (string, bool) {
	path := strings.TrimSuffix(executablePath, " (deleted)")
	for current := filepath.Clean(path); current != "." && current != string(filepath.Separator); current = filepath.Dir(current) {
		base := filepath.Base(current)
		if strings.HasSuffix(base, ".app") {
			return current, true
		}
		if strings.HasSuffix(base, ".app.backup") {
			return filepath.Join(filepath.Dir(current), strings.TrimSuffix(base, ".backup")), true
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	return "", false
}
