package ccoauth

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
)

// FetcherConfig 描述一次 OAuth usage 拉取所需的环境信息。
// 桌面端与 agentred daemon 都通过 ccoauth.Fetch(ctx, cfg) 走同一份逻辑,
// 区别仅在于 cfg 里 ClaudeConfigDir / Username 来自各自所在机器。
type FetcherConfig struct {
	// Keychain 可空。空时跳过 Keychain 步骤,只走文件 fallback。
	Keychain KeychainGetter

	// ClaudeConfigDir 已展开的目录路径(用于定位 .credentials.json)。
	// 必填;空时只能走 keychain。
	ClaudeConfigDir string

	// EnvClaudeConfigDir 是 CLAUDE_CONFIG_DIR 环境变量的*原始字符串*。
	// 用于 KeychainServiceName 计算 hash 后缀。生产里通常等于 ClaudeConfigDir
	// 的来源 env 值(可能含 ~);默认配置下传空串。
	EnvClaudeConfigDir string

	// Username keychain 第一个候选 account。空时只用空 account 试一次。
	Username string

	// HTTPClient 注入 Client(测试时换 httptest server)。空时走 NewClient("")。
	HTTPClient *Client
}

// Fetch 是 ccoauth 的高级入口:Keychain → 文件 → HTTPS GET /api/oauth/usage,
// 返回标准化的 RateLimits 或 ErrNoCredentials / ErrAuthExpired / ErrRateLimited / ErrNetwork。
func Fetch(ctx context.Context, cfg FetcherConfig) (*RateLimits, error) {
	creds, err := readCredentialsChain(cfg)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = NewClient("")
	}
	return client.FetchUsage(ctx, creds.AccessToken)
}

// NewLocalFetcher 返回一个无参 fetcher,它从*当前进程所在机器*的环境读 OAuth 凭证
// 并调 Anthropic /api/oauth/usage。桌面 cc_usage_svc 用它探"local"设备,agentred
// daemon claudecode.usage handler 用它探它自己所在的机器,二者代码一致行为对称。
//
// 环境探测:
//   - CLAUDE_CONFIG_DIR env 决定 keychain service 名(KeychainServiceName 处理 hash)
//     和 .credentials.json 路径;为空时落 ~/.claude。
//   - 当前 OS 用户名作为 keychain 第一个候选 account。
func NewLocalFetcher() func(ctx context.Context) (*RateLimits, error) {
	envDir := os.Getenv("CLAUDE_CONFIG_DIR")
	dir := envDir
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".claude")
		}
	}
	var username string
	if u, err := user.Current(); err == nil {
		username = u.Username
	}
	cfg := FetcherConfig{
		Keychain:           ZalandoKeychain{},
		ClaudeConfigDir:    dir,
		EnvClaudeConfigDir: envDir,
		Username:           username,
	}
	return func(ctx context.Context) (*RateLimits, error) {
		return Fetch(ctx, cfg)
	}
}

func readCredentialsChain(cfg FetcherConfig) (*Credentials, error) {
	if cfg.Keychain != nil {
		service := KeychainServiceName(cfg.EnvClaudeConfigDir)
		accounts := []string{}
		if cfg.Username != "" {
			accounts = append(accounts, cfg.Username)
		}
		accounts = append(accounts, "") // 同时试无 account 的 entry,与 OMC 行为对齐
		// keychain 命中 → 直接返回。任何错误(未找到 / JSON 坏 / OS 调用失败)
		// 都降级到文件 fallback,避免一次性故障导致整个 HUD 挂掉。
		if creds, err := ReadKeychainCredentials(cfg.Keychain, service, accounts); err == nil {
			return creds, nil
		}
	}
	if cfg.ClaudeConfigDir == "" {
		return nil, ErrNoCredentials
	}
	return ReadFileCredentials(filepath.Join(cfg.ClaudeConfigDir, ".credentials.json"))
}
