//go:build e2e

package bootstrap

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
)

// E2EKeychainDirEnv 是 e2e 构建专用的隔离 keychain 目录环境变量。带 e2e build tag 且
// 设置它时,bootstrap 在装配 Server / Remote Device 之前就建立 file keychain,让
// Server Add、ConnPool、watcher 与 e2e seeding 共享同一个后端,生产 system keychain
// 永远不是 fallback。
const E2EKeychainDirEnv = "AGENTRE_E2E_KEYCHAIN_DIR"

// initKeychain 覆盖生产默认:设置 E2EKeychainDirEnv 时建立并校验 file keychain;
// 目录缺失 / 权限不安全 / 不可写都会让启动失败,绝不回退 NewSystem()。
// 未设置时与生产构建一致,继续使用平台 system keychain。
func initKeychain(_ context.Context) error {
	dir := strings.TrimSpace(os.Getenv(E2EKeychainDirEnv))
	if dir == "" {
		keychain.SetDefault(keychain.NewSystem())
		return nil
	}
	if err := keychain.ValidateFileDir(dir); err != nil {
		return fmt.Errorf("e2e keychain dir not usable: %w", err)
	}
	keychain.SetDefault(keychain.NewFile(dir))
	return nil
}
