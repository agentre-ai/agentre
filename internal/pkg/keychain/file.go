package keychain

import (
	"errors"
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
	return os.WriteFile(f.path(account), []byte(secret), 0o600)
}

func (f *fileKC) Delete(account string) error {
	err := os.Remove(f.path(account))
	if errors.Is(err, fs.ErrNotExist) {
		return ErrNotFound
	}
	return err
}
