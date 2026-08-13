//go:build !e2e

package bootstrap

import (
	"context"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
)

// initKeychain 在 bootstrap 装配任何依赖 keychain 的服务之前确立默认 keychain 后端。
// 生产构建固定用平台 system keychain(NewSystem),行为与历史一致;e2e 构建在
// keychain_e2e.go 覆盖这个实现。
func initKeychain(context.Context) error {
	keychain.SetDefault(keychain.NewSystem())
	return nil
}
