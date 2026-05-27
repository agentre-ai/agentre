package remote_device_svc

import (
	"context"
)

//go:generate mockgen -source svc.go -destination mock_remote_device_svc/mock_svc.go

// ProviderSummary describes a single LLM provider configured on a remote daemon.
// Populated from health.ping responses; wire types are translated at the watcher layer.
type ProviderSummary struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// AddRequest 是 Add 的入参。DisplayName 可空，svc 自动派生。
type AddRequest struct {
	URL         string `json:"url"`
	PairingCode string `json:"pairingCode"`
	DisplayName string `json:"displayName,omitempty"`
	TLSMode     string `json:"tlsMode"`
	TLSCertPEM  string `json:"tlsCertPEM,omitempty"`
}

// DeviceView 返回给前端的视图（不含 keychain 秘密）。
type DeviceView struct {
	ID                int64  `json:"id"`
	Name              string `json:"name"`
	URL               string `json:"url"`
	DaemonFingerprint string `json:"daemonFingerprint"`
	InstanceUUID      string `json:"instanceUUID"`
	TLSMode           string `json:"tlsMode"`
	TLSCertPEM        string `json:"tlsCertPEM,omitempty"`
	PairedAt          int64  `json:"pairedAt"`
	LastSeenAt        int64  `json:"lastSeenAt"`
	LastError         string `json:"lastError"`
	Online            bool   `json:"online"`
}

// RemoteDeviceSvc 单例接口。
type RemoteDeviceSvc interface {
	List(ctx context.Context) ([]*DeviceView, error)
	Get(ctx context.Context, id int64) (*DeviceView, error)
	Add(ctx context.Context, req AddRequest) (*DeviceView, error)
	Remove(ctx context.Context, id int64) error
	UpdateTLS(ctx context.Context, id int64, mode, pem string) (*DeviceView, error)
	Refresh(ctx context.Context, id int64) (*DeviceView, error)
	Rename(ctx context.Context, id int64, name string) error
	// SetWatcher 注入 watcher port。生产由 bootstrap 在 watcher_svc 就绪后调用;
	// nil 注入也允许(单测里不关心 watcher 时跳过)。
	SetWatcher(w WatcherPort)
	// Pool 返回 chat_svc / agent_backend_svc 共享的 per-device 连接池。
	// 借出后必须 defer Lease.Release();池子负责 idle 回收 / daemon drop evict。
	Pool() ConnPool
	// RecordDeviceProviders overwrites the in-memory provider cache for deviceID.
	// Called by the watcher on each successful health.ping.
	RecordDeviceProviders(deviceID int64, ps []ProviderSummary)
	// ListDeviceProviders returns the cached provider list for deviceID.
	// Returns nil if no data has been recorded yet. Safe for concurrent use.
	ListDeviceProviders(deviceID int64) []ProviderSummary
	// SyncProvider copies one local LLM provider to the remote daemon state.
	// The raw API key is sent only for this explicit sync operation.
	SyncProvider(ctx context.Context, deviceID int64, providerKey string) error
}

var defaultSvc RemoteDeviceSvc

// Default 返回默认实现单例。
func Default() RemoteDeviceSvc { return defaultSvc }

// SetDefault 由 bootstrap 注入实现。
func SetDefault(s RemoteDeviceSvc) { defaultSvc = s }
