package ccoauth

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// KeychainGetter 抽象一层 OS keychain 读取。生产用 NewSecurityCLIKeychain()
// 调 `/usr/bin/security`(macOS);测试用 fake。其他平台保留口子但通常返回 ErrNoCredentials。
//
// 不复用 internal/pkg/keychain 的原因:那里把 serviceName 写死成 "agentre",
// 我们需要读 Anthropic 自家的 "Claude Code-credentials" service。
type KeychainGetter interface {
	Get(service, account string) (string, error)
}

// KeychainServiceName 计算应该读哪个 keychain service。
// 与 OMC 约定一致(usage-api.js:getKeychainServiceName):
//   - 默认 CLAUDE_CONFIG_DIR 为空 -> "Claude Code-credentials"
//   - 否则 -> "Claude Code-credentials-<sha256(env_value)[:8]>"
//
// 注意 hash 取的是 *原始 env 字符串*(不展开 ~),与 OMC 行为一致。
func KeychainServiceName(claudeConfigDir string) string {
	if claudeConfigDir == "" {
		return "Claude Code-credentials"
	}
	sum := sha256.Sum256([]byte(claudeConfigDir))
	return "Claude Code-credentials-" + hex.EncodeToString(sum[:])[:8]
}

// ReadKeychainCredentials 按 accounts 顺序探每个候选,命中第一个有效 access token 即返回。
// 全部缺失 / 全部内容无 accessToken -> ErrNoCredentials。
// JSON 损坏的 entry 跳过,继续探下一个候选。
func ReadKeychainCredentials(getter KeychainGetter, service string, accounts []string) (*Credentials, error) {
	if getter == nil {
		return nil, ErrNoCredentials
	}
	if len(accounts) == 0 {
		accounts = []string{""}
	}
	var lastErr error
	for _, account := range accounts {
		blob, err := getter.Get(service, account)
		if err != nil {
			if errors.Is(err, ErrNoCredentials) {
				continue
			}
			lastErr = err
			continue
		}
		creds, perr := parseCredentialsBlob([]byte(blob))
		if perr != nil {
			lastErr = perr
			continue
		}
		return creds, nil
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrNoCredentials
}
