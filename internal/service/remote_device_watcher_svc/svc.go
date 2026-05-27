package remote_device_watcher_svc

import "context"

// Service 由 bootstrap 注入唯一实例;remote_device_svc 通过 WatcherPort
// 接口(在 remote_device_svc 包内声明)反向消费它。
type Service interface {
	// StartAll 按当前 ACTIVE 设备启动 watcher。bootstrap 阶段调用。
	StartAll(ctx context.Context) error
	// Start 启动单台 watcher(已存在则 no-op)。
	Start(ctx context.Context, deviceID int64) error
	// Stop 停掉单台 watcher(不存在则 no-op)。
	Stop(deviceID int64)
	// Restart = Stop + Start。用于 TLS 配置变化等需要重新拨号的场景。
	Restart(ctx context.Context, deviceID int64) error
	// StopAll 关停全部 watcher。app Shutdown 调用。
	StopAll()
}

var defaultSvc Service

// Default 返回默认实现。
func Default() Service { return defaultSvc }

// SetDefault 由 bootstrap 注入。
func SetDefault(s Service) { defaultSvc = s }
