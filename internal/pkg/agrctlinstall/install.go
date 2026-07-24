// Package agrctlinstall installs the companion `agrctl` CLI (which owns the
// `claudecode` hook shim and the `ctl` control commands) into a writable,
// deterministic location under AppDataDir, from the copy the desktop ships in
// its app bundle. The desktop points the Claude Code PostToolUse hook at this
// installed path, so the hook never needs to exec the fat app binary.
//
// A version marker file keeps the installed copy in sync with the running app:
// on version change the binary is re-copied. On macOS this deliberately avoids
// executing the binary from inside the (possibly read-only / quarantined) app
// bundle by copying it out to AppDataDir first.
package agrctlinstall

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// binName 安装后的可执行文件名（Windows 带 .exe）。
func binName() string {
	if runtime.GOOS == "windows" {
		return "agrctl.exe"
	}
	return "agrctl"
}

// InstalledPath 返回 agrctl 的确定安装路径 <dataDir>/bin/agrctl(.exe)。
// hook 与终端共用此路径。
func InstalledPath(dataDir string) string {
	return filepath.Join(dataDir, "bin", binName())
}

// verPath 版本标记文件路径。
func verPath(dataDir string) string {
	return filepath.Join(dataDir, "bin", "agrctl.ver")
}

// EnsureInstalled 确保 <dataDir>/bin/agrctl 存在且与 version 一致：缺失或版本标记不符时
// 从 sourcePath 拷贝（0755）并写标记；已是最新则 no-op。返回安装路径与本次是否发生拷贝。
func EnsureInstalled(dataDir, sourcePath, version string) (path string, installed bool, err error) {
	dest := InstalledPath(dataDir)
	if upToDate(dest, verPath(dataDir), version) {
		return dest, false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return "", false, fmt.Errorf("agrctlinstall: mkdir bin dir: %w", err)
	}
	if err := copyFile(sourcePath, dest); err != nil {
		return "", false, err
	}
	if err := os.WriteFile(verPath(dataDir), []byte(version), 0o644); err != nil {
		return "", false, fmt.Errorf("agrctlinstall: write version marker: %w", err)
	}
	return dest, true, nil
}

// upToDate 目标存在且版本标记等于 version。
func upToDate(dest, ver, version string) bool {
	if _, err := os.Stat(dest); err != nil {
		return false
	}
	got, err := os.ReadFile(ver) // #nosec G304 -- ver 是 <AppDataDir>/bin/agrctl.ver，应用内部拼接，非用户输入。
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(got)) == strings.TrimSpace(version)
}

// copyFile 原子拷贝 src→dst（临时文件 + rename），dst 置 0755。
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src) // #nosec G304 -- src 是应用从 os.Executable() 解析的 bundle 兄弟路径，非用户输入。
	if err != nil {
		return fmt.Errorf("agrctlinstall: read source %q: %w", src, err)
	}
	tmp := dst + ".tmp"
	// #nosec G304,G703 -- tmp/dst 都在 <AppDataDir>/bin 内、由应用拼接，非用户输入；0755 是可执行文件必需。
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return fmt.Errorf("agrctlinstall: write temp: %w", err)
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("agrctlinstall: chmod temp: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("agrctlinstall: rename into place: %w", err)
	}
	return nil
}
