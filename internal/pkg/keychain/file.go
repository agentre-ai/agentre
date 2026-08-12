package keychain

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type fileKC struct {
	dir string
}

// NewFile 返回一个用 0600 文件存储的 keychain 后端。
//
// 只在 OS 原生 keychain 不可用 + 用户显式 opt-in 的环境用（例如 headless Linux）。
// dir 必须是当前用户专属目录（推荐 <AppDataDir>/keychain/）；调用方负责保证目录
// 不会被同机其他用户读到。
func NewFile(dir string) Keychain { return &fileKC{dir: dir} }

func (f *fileKC) path(account string) string {
	return filepath.Join(f.dir, account)
}

// ValidateFileDir 校验一个可作为 file keychain 存储目录的安全边界:
//   - 必须已存在且是目录(e2e runner 以 0700 预创建;缺失 = 配置错误);
//   - 权限必须严格 0700 —— 任何 group/other 位都视为不安全;
//   - 当前用户必须可写(真写一发 probe 再删,当场暴露只读挂载 / 他人属主 / 只读目录)。
//
// 任何一项失败都返回错误:调用方(e2e bootstrap)应直接终止启动,而不是回退其他后端。
func ValidateFileDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("keychain dir %q: %w", dir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("keychain dir %q is not a directory", dir)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		return fmt.Errorf("keychain dir %q permissions %#o are not isolated; want 0700", dir, perm)
	}
	probe, err := os.CreateTemp(dir, ".keychain-probe-")
	if err != nil {
		return fmt.Errorf("keychain dir %q not writable: %w", dir, err)
	}
	probePath := probe.Name()
	if err := probe.Chmod(0o600); err != nil {
		_ = probe.Close()
		_ = os.Remove(probePath)
		return fmt.Errorf("keychain dir %q probe permissions: %w", dir, err)
	}
	if err := probe.Close(); err != nil {
		_ = os.Remove(probePath)
		return fmt.Errorf("keychain dir %q probe close: %w", dir, err)
	}
	if err := os.Remove(probePath); err != nil {
		return fmt.Errorf("keychain dir %q probe cleanup: %w", dir, err)
	}
	return nil
}

func (f *fileKC) Get(account string) (string, error) {
	b, err := os.ReadFile(f.path(account))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (f *fileKC) Set(account, secret string) error {
	if err := os.MkdirAll(f.dir, 0o700); err != nil {
		return err
	}
	path := f.path(account)
	if err := os.WriteFile(path, []byte(secret), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (f *fileKC) Delete(account string) error {
	err := os.Remove(f.path(account))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
