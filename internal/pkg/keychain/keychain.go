// Package keychain 抽象一层 OS 级别的密钥存储。
//
// 桌面端用它来持久化 Hub refresh_token：生产构建挂 NewSystem() （C5 提供）；
// 没有 OS keychain 的环境（CI / 部分 Linux 容器）显式 opt-in NewFile() （C6 提供）；
// 单测一律用 NewMemory()。
package keychain

import "errors"

// ErrNotFound 表示账号下没有对应密钥。
var ErrNotFound = errors.New("keychain: secret not found")

// Keychain 抽象一层 OS 级别的密钥存储。
type Keychain interface {
	Get(account string) (string, error)
	Set(account, secret string) error
	Delete(account string) error
}

var defaultKC Keychain

// Default 返回当前注册的实现。
func Default() Keychain { return defaultKC }

// SetDefault 由 bootstrap 注入一个实现。
func SetDefault(k Keychain) { defaultKC = k }
