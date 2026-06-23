package remote_device_svc

import (
	"sync"

	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
)

type service struct {
	repo     remote_device_repo.PairedAgentredRepo
	dial     DaemonDialPort
	keychain KeychainPort
	pool     ConnPool
	watcher  WatcherPort // 可空:测试不注入时 Add/Remove/UpdateTLS 跳过 watcher 调用

	providerCacheMu sync.RWMutex
	providerCache   map[int64][]ProviderSummary
}

// New constructs a service. Production wiring lives in bootstrap; tests
// inject mock ports directly. pool 由 bootstrap 用 NewConnPool 构造后注入。
func New(repo remote_device_repo.PairedAgentredRepo, dial DaemonDialPort, kc KeychainPort, pool ConnPool) RemoteDeviceSvc {
	return &service{
		repo:          repo,
		dial:          dial,
		keychain:      kc,
		pool:          pool,
		providerCache: make(map[int64][]ProviderSummary),
	}
}

// RecordDeviceProviders overwrites the cached provider list for deviceID.
func (s *service) RecordDeviceProviders(deviceID int64, ps []ProviderSummary) {
	cp := make([]ProviderSummary, len(ps))
	copy(cp, ps)
	s.providerCacheMu.Lock()
	s.providerCache[deviceID] = cp
	s.providerCacheMu.Unlock()
}

// ListDeviceProviders returns a defensive copy of the cached provider list for
// deviceID, or nil if none has been recorded.
func (s *service) ListDeviceProviders(deviceID int64) []ProviderSummary {
	s.providerCacheMu.RLock()
	ps, ok := s.providerCache[deviceID]
	s.providerCacheMu.RUnlock()
	if !ok {
		return nil
	}
	cp := make([]ProviderSummary, len(ps))
	copy(cp, ps)
	return cp
}

// SetWatcher 注入 watcher port。
func (s *service) SetWatcher(w WatcherPort) {
	s.watcher = w
}

// Pool 返回 chat_svc / agent_backend_svc 共享的 per-device 连接池。
func (s *service) Pool() ConnPool { return s.pool }

// keychainAccountForToken returns the keychain account name for a paired
// device's deviceToken.
func keychainAccountForToken(id int64) string {
	return "agentre-daemon-token-" + itoa(id)
}

// accountForDeviceFingerprint is the app-level singleton keychain account.
const accountForDeviceFingerprint = "agentre-device-fingerprint"
