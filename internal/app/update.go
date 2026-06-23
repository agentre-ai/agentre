package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/logger"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/pkg/procattr"
	"github.com/agentre-ai/agentre/internal/service/update_svc"
)

// 启动后延迟若干秒再触发自动检查，避开前端冷启动 / DB 迁移高峰。
const autoUpdateCheckDelay = 5 * time.Second

// 自动检查最短间隔，距上次检查不足 24h 时跳过。
const autoUpdateCheckInterval = 24 * time.Hour

// CheckForUpdate 检查最新版本。channel / mirror 从持久化设置读取。
func (a *App) CheckForUpdate() (*update_svc.UpdateInfo, error) {
	channel, err := update_svc.Update().GetChannel(a.ctx)
	if err != nil {
		return nil, err
	}
	mirror, err := update_svc.Update().GetMirror(a.ctx)
	if err != nil {
		return nil, err
	}
	info, err := update_svc.Update().CheckForUpdate(channel, mirror)
	if err != nil {
		return nil, err
	}
	if err := update_svc.Update().SetLastUpdateCheck(a.ctx, time.Now().Unix()); err != nil {
		logger.Ctx(a.ctx).Warn("persist last update check", zap.Error(err))
	}
	return info, nil
}

// DownloadAndInstallUpdate 下载并安装最新版本；进度通过 "update:progress" 事件推送。
// skipChecksum=true 用于 SHA256SUMS.txt 获取失败但用户选择继续的场景。
func (a *App) DownloadAndInstallUpdate(skipChecksum bool) error {
	channel, err := update_svc.Update().GetChannel(a.ctx)
	if err != nil {
		return err
	}
	mirror, err := update_svc.Update().GetMirror(a.ctx)
	if err != nil {
		return err
	}
	ctx := a.ctx
	onProgress := func(downloaded, total int64) {
		wailsruntime.EventsEmit(ctx, "update:progress", map[string]int64{
			"downloaded": downloaded,
			"total":      total,
		})
	}
	return update_svc.Update().DownloadAndUpdate(channel, mirror, skipChecksum, onProgress)
}

// GetAvailableMirrors 返回内置可用下载镜像列表。
func (a *App) GetAvailableMirrors() []update_svc.MirrorInfo {
	return update_svc.Update().GetAvailableMirrors()
}

// GetUpdateChannel 返回当前更新通道。
func (a *App) GetUpdateChannel() (string, error) {
	return update_svc.Update().GetChannel(a.ctx)
}

// SetUpdateChannel 更新通道（stable / beta / nightly）。
func (a *App) SetUpdateChannel(channel string) error {
	return update_svc.Update().SetChannel(a.ctx, channel)
}

// GetDownloadMirror 返回当前下载镜像前缀；空串表示直连 GitHub。
func (a *App) GetDownloadMirror() (string, error) {
	return update_svc.Update().GetMirror(a.ctx)
}

// SetDownloadMirror 更新下载镜像前缀；空串恢复直连。
func (a *App) SetDownloadMirror(mirror string) error {
	return update_svc.Update().SetMirror(a.ctx, mirror)
}

// RestartApp 安装完成后重启应用本体。
// 不同平台启动方式不同：macOS 用 open -n 启动新进程；Linux/Windows 直接 fork-exec。
// 当前进程随后由 Quit 终止；wails OnShutdown 钩子会被触发完成收尾。
func (a *App) RestartApp() error {
	execPath, err := os.Executable()
	if err != nil {
		return err
	}
	execPath, err = filepath.EvalSymlinks(execPath)
	if err != nil {
		return err
	}

	switch runtime.GOOS {
	case "darwin":
		appDir := execPath
		for !strings.HasSuffix(appDir, ".app") && appDir != "/" {
			appDir = filepath.Dir(appDir)
		}
		if strings.HasSuffix(appDir, ".app") {
			cmd := exec.Command("open", "-n", appDir) //nolint:gosec
			procattr.ApplyNoConsoleWindow(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}
		} else {
			cmd := exec.Command(execPath) //nolint:gosec
			procattr.ApplyNoConsoleWindow(cmd)
			if err := cmd.Start(); err != nil {
				return err
			}
		}
	default:
		cmd := exec.Command(execPath) //nolint:gosec
		procattr.ApplyNoConsoleWindow(cmd)
		if err := cmd.Start(); err != nil {
			return err
		}
	}

	// 重启已拉起新进程,旧进程必须无条件退出:标记已确认,绕过活跃会话二次确认,
	// 否则 OnBeforeClose 拦住旧进程、新进程撞单实例锁 → 更新静默失败。
	a.quitConfirmed.Store(true)

	go func() {
		// 留一拍时间让 wails Quit 走完 OnShutdown。
		time.Sleep(500 * time.Millisecond)
		wailsruntime.Quit(a.ctx)
	}()
	return nil
}

// startAutoUpdateCheck 启动 5s 后做一次更新检查；24h 节流。
// 有新版本时发送 "update:available" 事件供前端弹横幅。
//
// 仅在 Startup 中由 goroutine 调用，不阻塞主流程。
func (a *App) startAutoUpdateCheck() {
	time.Sleep(autoUpdateCheckDelay)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	last, err := update_svc.Update().GetLastUpdateCheck(ctx)
	if err != nil {
		logger.Default().Warn("read last update check", zap.Error(err))
		return
	}
	if last > 0 && time.Since(time.Unix(last, 0)) < autoUpdateCheckInterval {
		return
	}

	channel, err := update_svc.Update().GetChannel(ctx)
	if err != nil {
		logger.Default().Warn("read update channel", zap.Error(err))
		return
	}
	mirror, err := update_svc.Update().GetMirror(ctx)
	if err != nil {
		logger.Default().Warn("read update mirror", zap.Error(err))
		return
	}

	info, err := update_svc.Update().CheckForUpdate(channel, mirror)
	if err != nil {
		logger.Default().Info("auto update check failed", zap.Error(err))
		return
	}

	if err := update_svc.Update().SetLastUpdateCheck(ctx, time.Now().Unix()); err != nil {
		logger.Default().Warn("persist last update check", zap.Error(err))
	}

	if info.HasUpdate {
		wailsruntime.EventsEmit(a.ctx, "update:available", info)
	}
}
