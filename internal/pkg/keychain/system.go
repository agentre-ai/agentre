package keychain

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// serviceName 是 keychain 里的「应用程序」槽位。同一台机器上 agentre 所有账号都挂在它下面。
const serviceName = "agentre"

type systemKC struct{}

// NewSystem 返回 OS 原生 keychain 实现：macOS Keychain / Windows Credential Manager /
// Linux Secret Service。生产构建挂这个；headless / 部分容器环境若不可用，应改挂 NewFile()。
func NewSystem() Keychain { return &systemKC{} }

func (s *systemKC) Get(account string) (string, error) {
	v, err := keyring.Get(serviceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNotFound
	}
	return v, err
}

func (s *systemKC) Set(account, secret string) error {
	return keyring.Set(serviceName, account, secret)
}

func (s *systemKC) Delete(account string) error {
	err := keyring.Delete(serviceName, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return ErrNotFound
	}
	return err
}
