// Package paths 提供 Agentre 桌面端的根目录定位。
// 单独成包是为了让 bootstrap、agentruntime 等都能引用，避免与 bootstrap 形成 import 环。
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AppName 是 AppDataDir 的应用名段（同时被 cago.Default() / Wails build / ldflags 引用）。
const AppName = "agentre"

// AppNameAgentred is the AppDataDir leaf for the agentred daemon binary.
// 与 AppName 分开，保证 agentre desktop 与 agentred daemon 在同机运行时文件系统完全隔离。
const AppNameAgentred = "agentred"

// AppNameDev 是 wails dev 模式下 AppDataDir 的应用名段。与 AppName(生产) 分开，
// 让 `make dev` / `wails dev` 的 DB / 日志 / 配置 / agent 工作副本与已安装的 App 完全隔离，
// 调试不会污染真实数据，且两者可同时运行。
const AppNameDev = AppName + "-dev"

// IsDevMode 判断当前进程是否跑在 wails dev 下：wails dev 会给它编译并启动的二进制注入
// devserver 环境变量(指向 vite dev server)。单独成函数是为了让 paths(叶子包) 与 main.go
// 共用同一份判定，避免两处各写一遍。
func IsDevMode() bool {
	return strings.TrimSpace(os.Getenv("devserver")) != ""
}

// AppDataDir 返回 Agentre 本地状态的根目录。
//
//	macOS    ~/Library/Application Support/agentre/
//	Windows  %LOCALAPPDATA%\agentre\
//	Linux    ~/.config/agentre/
//
// 优先级(从高到低)：
//  1. AGENTRE_DATA_DIR 显式覆盖(测试 / 排查 / 自定义)——永远最高，dev 模式下也照样赢。
//  2. wails dev(IsDevMode)→ <base>/agentre-dev,与已安装 App 隔离。
//  3. 默认 → <base>/agentre。
func AppDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("AGENTRE_DATA_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	name := AppName
	if IsDevMode() {
		name = AppNameDev
	}
	return filepath.Join(base, name), nil
}

// AgentredDataDir 返回 agentred daemon 的本地状态根目录。
//
//	macOS    ~/Library/Application Support/agentred/
//	Windows  %LOCALAPPDATA%\agentred\
//	Linux    ~/.config/agentred/
//
// 测试 / 排查可用 AGENTRED_DATA_DIR 覆盖（绕过平台默认）。
func AgentredDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("AGENTRED_DATA_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, AppNameAgentred), nil
}
