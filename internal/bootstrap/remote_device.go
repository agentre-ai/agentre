package bootstrap

import (
	"context"
	"time"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

// InitRemoteDevice wires the repo + svc default impls. Must run after the
// SQLite DB component is registered (i.e., inside bootstrap.Init after
// migrations.RunMigrations) and after keychain.SetDefault has been called
// (done inside InitHub).
//
// 构造 device-shared ConnPool 一并注入 svc:idle=30s。app.Shutdown 通过
// remote_device_svc.Default().Pool().Close() 平滑回收所有 entries(见
// internal/app/app.go)。
func InitRemoteDevice(_ context.Context) error {
	remote_device_repo.RegisterPairedAgentred(remote_device_repo.NewPairedAgentred())
	repo := remote_device_repo.PairedAgentred()
	dial := remote_device_svc.NewDaemonDial()
	kc := keychain.Default()
	pool := remote_device_svc.NewConnPool(repo, kc, dial,
		remote_device_svc.WithIdleTimeout(30*time.Second))
	remote_device_svc.SetDefault(remote_device_svc.New(repo, dial, kc, pool))
	return nil
}
