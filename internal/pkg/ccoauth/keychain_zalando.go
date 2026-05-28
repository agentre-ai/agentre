package ccoauth

import (
	"errors"

	"github.com/zalando/go-keyring"
)

// ZalandoKeychain 用 github.com/zalando/go-keyring 实现 KeychainGetter,
// macOS / Windows / Linux Secret Service 全平台都能用,生产首选。
//
// 注意:zalando/go-keyring 要求非空 account,所以"无 account"的 keychain entry
// (OMC `find-generic-password -s ... -w` 不带 -a 的回退路径)这里命中不了。
// 实测主流 Claude Code 安装都是 user-scoped,可接受。需要兼容时改 shell 出 `security`。
type ZalandoKeychain struct{}

// Get 实现 KeychainGetter。空 account 直接返回 ErrNoCredentials。
func (ZalandoKeychain) Get(service, account string) (string, error) {
	if account == "" {
		return "", ErrNoCredentials
	}
	v, err := keyring.Get(service, account)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrNoCredentials
	}
	return v, err
}
