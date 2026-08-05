package piagent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/agentre-ai/agentre/internal/pkg/paths"
)

// providerExtensionWriter 物化 provider 扩展写盘的可注入点：生产实现
// materializeProviderExtension；单测可换成 fake，使会话装配测试不依赖真实磁盘。
var providerExtensionWriter = func(source string) (string, error) {
	return materializeProviderExtension(source)
}

// SetProviderExtensionWriterForTest 替换 providerExtensionWriter，返回恢复函数。
func SetProviderExtensionWriterForTest(fn func(string) (string, error)) func() {
	old := providerExtensionWriter
	providerExtensionWriter = fn
	return func() { providerExtensionWriter = old }
}

// materializeProviderExtension 把 provider 扩展源文本按内容哈希写到
// <AppDataDir>/piagent/ext/agentre-provider-<hash>.mjs（幂等：同哈希已存在则不
// 重写），返回绝对路径。扩展文件只含 $ENV_VAR 的 apiKey 引用、不含密钥，无需按
// 会话清理，也不设 0600。
func materializeProviderExtension(source string) (string, error) {
	root, err := paths.AppDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "piagent", "ext")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(source))
	// 取 sha256 前 16 hex 作内容指纹（版本隔离 + 幂等，与 mcpbridge 同款）。
	name := fmt.Sprintf("agentre-provider-%s.mjs", hex.EncodeToString(sum[:])[:16])
	path := filepath.Join(dir, name)
	if _, statErr := os.Stat(path); statErr == nil {
		return path, nil
	} else if !os.IsNotExist(statErr) {
		return "", statErr
	}
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		return "", err
	}
	return path, nil
}
