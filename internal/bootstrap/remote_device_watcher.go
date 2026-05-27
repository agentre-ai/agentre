package bootstrap

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/daemon/client"
	"agentre/internal/model/entity/paired_agentred_entity"
	"agentre/internal/pkg/keychain"
	"agentre/internal/repository/remote_device_repo"
	"agentre/internal/service/remote_device_svc"
	watcher "agentre/internal/service/remote_device_watcher_svc"
)

// dialAdapter 把 remote_device_svc.DaemonDialPort 的 Open 桥到 watcher port。
type dialAdapter struct {
	inner remote_device_svc.DaemonDialPort
}

func (a *dialAdapter) Open(ctx context.Context, args watcher.OpenArgs) (*client.Client, error) {
	return a.inner.Open(ctx, remote_device_svc.ConnectArgs{
		URL:                       args.URL,
		TLSMode:                   args.TLSMode,
		TLSCertPEM:                args.TLSCertPEM,
		DeviceFingerprint:         args.DeviceFingerprint,
		DeviceToken:               args.DeviceToken,
		ExpectedDaemonFingerprint: args.ExpectedDaemonFingerprint,
	})
}

// repoAdapter 把 remote_device_repo.PairedAgentredRepo 的方法集暴露给 watcher
// (watcher 只需要 List/Get/UpdateLastSeen,不需要 Insert/Update/Delete)。
type repoAdapter struct {
	inner remote_device_repo.PairedAgentredRepo
}

func (a *repoAdapter) List(ctx context.Context) ([]*paired_agentred_entity.PairedAgentred, error) {
	return a.inner.List(ctx)
}

func (a *repoAdapter) Get(ctx context.Context, id int64) (*paired_agentred_entity.PairedAgentred, error) {
	return a.inner.Get(ctx, id)
}

func (a *repoAdapter) UpdateLastSeen(ctx context.Context, id int64, lastSeenAtMs int64, lastError string) error {
	return a.inner.UpdateLastSeen(ctx, id, lastSeenAtMs, lastError)
}

// providerRecorderAdapter 把 remote_device_svc.RemoteDeviceSvc 的 provider cache
// API 适配成 watcher.ProviderRecorder(两边的 ProviderSummary 类型同形但不同包)。
type providerRecorderAdapter struct {
	inner remote_device_svc.RemoteDeviceSvc
}

func (a *providerRecorderAdapter) RecordDeviceProviders(deviceID int64, ps []watcher.ProviderSummary) {
	translated := make([]remote_device_svc.ProviderSummary, len(ps))
	for i, p := range ps {
		translated[i] = remote_device_svc.ProviderSummary{Key: p.Key, Name: p.Name, Type: p.Type}
	}
	a.inner.RecordDeviceProviders(deviceID, translated)
}

// InitRemoteDeviceWatcher 必须在 InitRemoteDevice 之后调。注入 watcher,
// 顺带把 watcher 反向挂到 remote_device_svc。emit 必须非 nil(调用方强契约)。
func InitRemoteDeviceWatcher(_ context.Context, emit watcher.Emitter) {
	devSvc := remote_device_svc.Default()
	svc := watcher.New(
		&repoAdapter{inner: remote_device_repo.PairedAgentred()},
		&dialAdapter{inner: remote_device_svc.NewDaemonDial()},
		keychain.Default(),
		emit,
		watcher.DefaultWatcherConfig(),
		watcher.NewRealClock(),
		&providerRecorderAdapter{inner: devSvc},
	)
	watcher.SetDefault(svc)

	// 反向挂到 remote_device_svc:Add/Remove/UpdateTLS 触发 watcher 生命周期。
	devSvc.SetWatcher(&watcherPortAdapter{inner: svc})
}

// watcherPortAdapter 把 watcher.Service 适配成 remote_device_svc.WatcherPort
// (两个接口签名 1:1,但定义在各自的包里,所以要 thin adapter)。
type watcherPortAdapter struct {
	inner watcher.Service
}

func (a *watcherPortAdapter) Start(ctx context.Context, deviceID int64) error {
	return a.inner.Start(ctx, deviceID)
}

func (a *watcherPortAdapter) Stop(deviceID int64) { a.inner.Stop(deviceID) }

func (a *watcherPortAdapter) Restart(ctx context.Context, deviceID int64) error {
	return a.inner.Restart(ctx, deviceID)
}

// RemoteDeviceWatcherBoot 替代旧 RemoteDeviceBoot:对所有 ACTIVE 设备启 watcher。
// 用 Background ctx 让 watcher 生命周期跟 svc 走,不被 startup ctx 提前 cancel。
func RemoteDeviceWatcherBoot(_ context.Context) {
	if err := watcher.Default().StartAll(context.Background()); err != nil {
		logger.Default().Warn("remote_device_watcher StartAll", zap.Error(err))
	}
}
