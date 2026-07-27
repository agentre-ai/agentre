package agrctlinstall

import (
	"os"
	"path/filepath"
)

// BundledSourcePath 返回随 app 一起 build 进 bundle 的 agrctl 源路径（与当前可执行文件同目录）。
// ok=false 表示没找到（dev 模式、或该构建没把 agrctl 放进 bundle）——此时调用方跳过安装。
func BundledSourcePath() (string, bool) {
	exe, err := os.Executable()
	if err != nil {
		return "", false
	}
	if resolved, rerr := filepath.EvalSymlinks(exe); rerr == nil {
		exe = resolved
	}
	return bundledSourceFrom(exe)
}

// bundledSourceFrom 计算 exe 同目录下的 agrctl 兄弟文件路径，存在才返回 ok=true（可注入测试）。
func bundledSourceFrom(exePath string) (string, bool) {
	sibling := filepath.Join(filepath.Dir(exePath), binName())
	if _, err := os.Stat(sibling); err != nil {
		return "", false
	}
	return sibling, true
}
