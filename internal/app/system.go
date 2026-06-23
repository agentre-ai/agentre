package app

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/agentre-ai/agentre/internal/bootstrap"
	"github.com/agentre-ai/agentre/internal/pkg/procattr"
)

// runOpenCmd is the test seam for exec.Command. Tests swap it; production code
// uses the real exec.
var runOpenCmd = func(name string, args ...string) error {
	cmd := exec.Command(name, args...) //nolint:gosec
	procattr.ApplyNoConsoleWindow(cmd)
	return cmd.Run()
}

var lineSuffixRe = regexp.MustCompile(`:\d+(?::\d+)?$`)

// OpenPath 用系统默认应用打开 path。
// path 必须是绝对路径；包含 ".." 时拒绝（防御性，AI 输出基本不会有）。
// 末尾 :line[:col] 后缀会被剥离 —— macOS open / xdg-open 不识别这种语法。
// 行号未来若要支持，由"编辑器 URL scheme"设置项接管（见 spec 未来工作）。
func (a *App) OpenPath(path string) error {
	cleaned, err := validateOpenPath(path)
	if err != nil {
		return err
	}
	return runOpenPlatform(cleaned)
}

func validateOpenPath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("OpenPath: path is empty")
	}
	if !isAbsolutePath(path) {
		return "", fmt.Errorf("OpenPath: path must be absolute: %s", path)
	}
	cleaned := lineSuffixRe.ReplaceAllString(path, "")
	for _, part := range strings.FieldsFunc(cleaned, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return "", fmt.Errorf("OpenPath: path contains '..' segment: %s", path)
		}
	}
	return cleaned, nil
}

func isAbsolutePath(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	// Windows: C:\ 或 C:/
	if len(p) >= 3 && p[1] == ':' && (p[2] == '\\' || p[2] == '/') {
		c := p[0]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
			return true
		}
	}
	return false
}

// OpenLogsDir 在系统文件管理器中打开 Agentre 的日志目录（不存在时先创建）。
// 用于「设置 → 版本 & 更新 → 打开日志」，方便用户取日志附到 Bug 反馈里。
func (a *App) OpenLogsDir() error {
	dir, err := bootstrap.LogsDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("OpenLogsDir: create logs dir: %w", err)
	}
	return runOpenPlatform(dir)
}

func runOpenPlatform(path string) error {
	switch runtime.GOOS {
	case "darwin":
		return runOpenCmd("open", path)
	case "windows":
		return runOpenCmd("cmd", "/c", "start", "", path)
	default:
		return runOpenCmd("xdg-open", path)
	}
}
