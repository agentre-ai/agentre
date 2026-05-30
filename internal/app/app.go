// Package app contains the Wails binding layer. Methods on App are exposed to
// the frontend via wails generation under frontend/wailsjs/go/app/App.*.
// Each method should remain a thin pass-through to the corresponding service
// singleton — keep business logic in internal/service/<domain>_svc/.
package app

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"agentre/internal/bootstrap"
	"agentre/internal/buildinfo"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/code"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/data_svc"
	"agentre/internal/service/hook_svc"
	"agentre/internal/service/remote_device_svc"
	watcher "agentre/internal/service/remote_device_watcher_svc"
	"agentre/internal/service/server_svc"
	"agentre/internal/service/terminal_svc"

	"github.com/cago-frame/cago/configs"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
	"go.uber.org/zap"
)

// App is the Wails binding root. Each exported method becomes a frontend RPC.
type App struct {
	ctx              context.Context
	hookPollerCancel context.CancelFunc
	ccUsageStop      func()
	terminalSvc      *terminal_svc.Service

	lastImportPath   string
	lastImportPathMu sync.Mutex
}

// AppInfo contains build and runtime metadata exposed to the frontend.
type AppInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Env     string `json:"env"`
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{}
}

var resetStaleActiveSessions = bootstrap.ResetStaleActiveSessions

// Startup is wired to wails OnStartup. The context is saved so we can call
// the runtime methods.
func (a *App) Startup(ctx context.Context) {
	a.ctx = ctx
	a.resetStaleSessionsOnStartup(ctx)
	a.registerChatService()
	a.hookPollerCancel = hook_svc.StartEmailPoller(ctx)

	// Server 联机：绑定 wails 事件源后启动 boot 协程（最长一次刷新）。
	server_svc.Server().SetEmitter(func(payload any) {
		wailsruntime.EventsEmit(a.ctx, "server.state", payload)
	})
	bootstrap.ServerBoot(context.Background())

	// Remote device watcher：注入 wails 事件 emitter,Boot 拉起所有 ACTIVE 设备的 watcher。
	// 顺带把 device online/offline 事件接到 cc_usage_svc(动态起/停 per-device 配额 ticker)。
	remoteDeviceEmit := watcher.EmitterFunc(func(p watcher.StateEvent) {
		wailsruntime.EventsEmit(a.ctx, watcher.EventName, p)
		a.onRemoteDeviceState(p.ID, p.Online)
	})
	bootstrap.InitRemoteDeviceWatcher(context.Background(), remoteDeviceEmit)
	bootstrap.RemoteDeviceWatcherBoot(context.Background())

	// Claude Code OAuth usage HUD:启动后台 60s 轮询,wails event "cc_usage:update"
	// 推送给前端 QuotaMeter。Shutdown 时停所有 ticker。
	a.ccUsageStop = a.startCCUsage()

	a.terminalSvc = newTerminalService(a.ctx)

	//nolint:gosec // G118: background poll deliberately outlives request scope
	go a.startAutoUpdateCheck()

	logger.Default().Info("app startup", zap.Any("info", a.Info()))
}

func (a *App) resetStaleSessionsOnStartup(ctx context.Context) {
	if err := resetStaleActiveSessions(ctx); err != nil {
		logger.Ctx(ctx).Warn("app startup: reset stale active sessions", zap.Error(err))
	}
}

// Shutdown is wired to wails OnShutdown.
func (a *App) Shutdown(ctx context.Context) {
	if a.hookPollerCancel != nil {
		a.hookPollerCancel()
		a.hookPollerCancel = nil
	}
	if a.ccUsageStop != nil {
		a.ccUsageStop()
		a.ccUsageStop = nil
	}
	// 关停 remote device watcher：让长连守护 goroutine 全部退出。
	if w := watcher.Default(); w != nil {
		w.StopAll()
	}
	// 关闭 device-shared ConnPool:guarantee 所有活 entry 的 client.Close 被调,
	// chat_svc / agent_backend_svc 持有的 lease 自动失效。
	if rd := remote_device_svc.Default(); rd != nil {
		if p := rd.Pool(); p != nil {
			if err := p.Close(); err != nil {
				logger.Ctx(ctx).Warn("conn pool close", zap.Error(err))
			}
		}
	}
	// 收尾常驻 CLI 子进程；pool.RemoveAll 异步 close，不阻塞 wails 退出。
	agentruntime.DefaultCLISessionPool().RemoveAll()
	if a.terminalSvc != nil {
		a.terminalSvc.Shutdown()
	}
	logger.Ctx(ctx).Info("app shutdown")
}

// registerChatService wires the chat service singleton with a real wails-runtime
// emitter so chat_svc.Send-triggered chunks reach the frontend via EventsEmit.
func (a *App) registerChatService() {
	emitter := chat_svc.EmitterFunc(func(_ context.Context, name string, payload any) {
		wailsruntime.EventsEmit(a.ctx, name, payload)
	})
	chat_svc.RegisterChat(chat_svc.NewChat(emitter))
}

// Greet returns a greeting for the given name.
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// Info returns app build and runtime metadata.
func (a *App) Info() AppInfo {
	info := AppInfo{
		Name:    "agentre",
		Version: configs.Version,
		Commit:  buildinfo.ShortCommitID(),
		Env:     string(configs.DEV),
	}

	if runtime := bootstrap.Default(); runtime != nil && runtime.Config() != nil {
		cfg := runtime.Config()
		info.Name = cfg.AppName
		info.Version = cfg.Version
		info.Env = string(cfg.Env)
	}

	return info
}

// OpenExternalURL opens url in the user's system browser. The frontend can't use
// window.open() — Wails's embedded webview silently drops it — so any "open in
// browser" action from JS must go through this binding.
func (a *App) OpenExternalURL(url string) {
	wailsruntime.BrowserOpenURL(a.ctx, url)
}

// SelectDirectory 弹出系统目录选择器并返回用户选中的绝对路径；用户取消时返回空串。
//
// 用于新建项目模态 / 设置抽屉的「浏览…」按钮。沿用 wails 自带 runtime API，
// 不引入额外 CGO 依赖；macOS / Windows / Linux 行为一致。
func (a *App) SelectDirectory(title string) (string, error) {
	if strings.TrimSpace(title) == "" {
		title = "选择项目目录"
	}
	return wailsruntime.OpenDirectoryDialog(a.ctx, wailsruntime.OpenDialogOptions{
		Title:                title,
		CanCreateDirectories: true,
	})
}

// ExportFileResult 是 ExportData 的返回值。
type ExportFileResult struct {
	Path     string         `json:"path"`
	Canceled bool           `json:"canceled"`
	Summary  map[string]int `json:"summary,omitempty"`
}

// ExportData 收集所选 scopes，弹保存对话框，写入用户选择的路径。
func (a *App) ExportData(req data_svc.ExportRequest) (*ExportFileResult, error) {
	ctx := a.ctx
	res, err := data_svc.Default().Export(ctx, &req)
	if err != nil {
		return nil, err
	}
	defaultName := "agentre-export-" + time.Now().Format("20060102-150405") + ".json"
	path, err := wailsruntime.SaveFileDialog(ctx, wailsruntime.SaveDialogOptions{
		Title:           "导出 Agentre 数据",
		DefaultFilename: defaultName,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return &ExportFileResult{Canceled: true}, nil
	}
	if err := os.WriteFile(path, res.JSON, 0o600); err != nil {
		logger.Ctx(ctx).Error("app.ExportData: write file failed", zap.String("path", path), zap.Error(err))
		return nil, i18n.NewError(ctx, code.DataExportWriteFailed)
	}
	return &ExportFileResult{Path: path, Summary: res.Summary}, nil
}

// PreviewImportData 弹打开对话框，读文件，缓存 path，返回 preview。
// 用户取消 → 返回 (nil, nil)。
func (a *App) PreviewImportData() (*data_svc.ImportPreview, error) {
	ctx := a.ctx
	path, err := wailsruntime.OpenFileDialog(ctx, wailsruntime.OpenDialogOptions{
		Title:   "选择 Agentre 导出文件",
		Filters: []wailsruntime.FileFilter{{DisplayName: "JSON (*.json)", Pattern: "*.json"}},
	})
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	//nolint:gosec // G304: path is user-selected via OS open dialog
	raw, err := os.ReadFile(path)
	if err != nil {
		logger.Ctx(ctx).Warn("app.PreviewImportData: read failed", zap.String("path", path), zap.Error(err))
		return nil, i18n.NewError(ctx, code.DataImportReadFailed)
	}
	pv, err := data_svc.Default().PreviewImport(ctx, raw)
	if err != nil {
		return nil, err
	}
	a.lastImportPathMu.Lock()
	a.lastImportPath = path
	a.lastImportPathMu.Unlock()
	return pv, nil
}

// ApplyImportFrontendRequest 是 ApplyImportData 的请求体。
type ApplyImportFrontendRequest struct {
	Actions          map[string]data_svc.ItemAction `json:"actions"`
	FallbackStrategy data_svc.ItemAction            `json:"fallbackStrategy"`
}

// ApplyImportData 读取缓存 path，重载文件，调用 ApplyImport。
func (a *App) ApplyImportData(req ApplyImportFrontendRequest) (*data_svc.ApplyImportResult, error) {
	ctx := a.ctx
	a.lastImportPathMu.Lock()
	path := a.lastImportPath
	a.lastImportPathMu.Unlock()
	if strings.TrimSpace(path) == "" {
		return nil, i18n.NewError(ctx, code.DataImportReadFailed)
	}
	//nolint:gosec // G304: path was previously cached from a user-selected OS open dialog
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, i18n.NewError(ctx, code.DataImportReadFailed)
	}
	return data_svc.Default().ApplyImport(ctx, &data_svc.ApplyImportRequest{
		Raw:              raw,
		Actions:          req.Actions,
		FallbackStrategy: req.FallbackStrategy,
	})
}
