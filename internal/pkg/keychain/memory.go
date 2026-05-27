package keychain

import "sync"

type memKC struct {
	mu sync.RWMutex
	m  map[string]string
}

// NewMemory 返回一个进程内的 keychain 实现（仅供测试使用）。
func NewMemory() Keychain {
	return &memKC{m: map[string]string{}}
}

func (k *memKC) Get(account string) (string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	v, ok := k.m[account]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func (k *memKC) Set(account, secret string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[account] = secret
	return nil
}

func (k *memKC) Delete(account string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, account)
	return nil
}
