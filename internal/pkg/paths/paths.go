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

// AppDataDir 返回 Agentre 本地状态的根目录。
//
//	macOS    ~/Library/Application Support/agentre/
//	Windows  %LOCALAPPDATA%\agentre\
//	Linux    ~/.config/agentre/
//
// 测试 / 排查可用 AGENTRE_DATA_DIR 覆盖。
func AppDataDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("AGENTRE_DATA_DIR")); dir != "" {
		return dir, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(base, AppName), nil
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
