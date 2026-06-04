package bootstrap

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"agentre/internal/model/entity/app_setting_entity"
	"agentre/internal/pkg/agentruntime/runtimes/claudecode"
	_ "agentre/internal/pkg/agentruntime/runtimes/piagent"
	"agentre/internal/pkg/httpgateway"
	"agentre/internal/pkg/paths"
	"agentre/internal/pkg/sysnotify"
	"agentre/internal/repository/agent_backend_repo"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/app_setting_repo"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/repository/department_repo"
	"agentre/internal/repository/hook_repo"
	"agentre/internal/repository/issue_repo"
	"agentre/internal/repository/llm_provider_repo"
	"agentre/internal/repository/project_location_repo"
	"agentre/internal/repository/project_repo"
	"agentre/internal/service/agent_backend_svc"
	"agentre/internal/service/app_settings_svc"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/issue_svc"
	"agentre/internal/service/notification_svc"
	"agentre/internal/service/project_svc"
	"agentre/migrations"

	"github.com/cago-frame/cago"
	"github.com/cago-frame/cago/configs"
	"github.com/cago-frame/cago/configs/memory"
	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	// 注册 SQLite 驱动
	_ "github.com/cago-frame/cago/database/db/sqlite"
)

// appName 仍保留作为兼容包级常量；权威定义在 paths.AppName。
const appName = paths.AppName

// dbFileName 桌面端 SQLite 数据库文件名（位于 AppDataDir 根目录）
const dbFileName = "agentre.db"

var runtime *Runtime

// Runtime owns the cago config and lifecycle hooks used by the desktop app.
type Runtime struct {
	config  *configs.Config
	dataDir string
}

// Init initializes the cago config/logger/database stack for the process.
// 启动顺序：dataDir → logger → SQLite(db.Database 组件) → migrations。
func Init(ctx context.Context) (*Runtime, error) {
	dataDir, err := AppDataDir()
	if err != nil {
		return nil, err
	}

	logsDir, err := LogsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("create logs dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, dbFileName)
	cfg, err := configs.NewConfig(appName, configs.WithSource(memory.NewSource(defaultConfigValues(logsDir, dbPath))))
	if err != nil {
		return nil, fmt.Errorf("create cago config: %w", err)
	}
	if err := logger.Logger(ctx, cfg); err != nil {
		return nil, fmt.Errorf("init cago logger: %w", err)
	}

	// 注册 SQLite 数据库组件。cago 启动 db 失败时会 panic，由调用方 recover/log。
	cago.New(ctx, cfg).Registry(db.Database())

	if err := migrations.RunMigrations(db.Default()); err != nil {
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// 注入 repository 默认实现，service 层调 llm_provider_repo.LLMProvider() 直接拿到 GORM 版。
	llm_provider_repo.RegisterLLMProvider(llm_provider_repo.NewLLMProvider())
	agent_backend_repo.RegisterAgentBackend(agent_backend_repo.NewAgentBackend())
	app_setting_repo.RegisterAppSetting(app_setting_repo.NewAppSetting())
	department_repo.RegisterDepartment(department_repo.NewDepartment())
	agent_repo.RegisterAgent(agent_repo.NewAgent())
	hook_repo.RegisterHookSource(hook_repo.NewHookSource())
	hook_repo.RegisterHookRule(hook_repo.NewHookRule())
	hook_repo.RegisterHookEvent(hook_repo.NewHookEvent())
	chat_repo.RegisterSession(chat_repo.NewSession())
	chat_repo.RegisterMessage(chat_repo.NewMessage())
	project_repo.RegisterProject(project_repo.NewProject())
	project_repo.RegisterProjectAgent(project_repo.NewProjectAgent())
	project_location_repo.RegisterProjectLocation(project_location_repo.NewProjectLocation())
	project_svc.SetDefault(project_svc.New())
	issue_repo.RegisterIssue(issue_repo.NewIssue())
	issue_repo.RegisterLabel(issue_repo.NewLabel())
	issue_repo.RegisterIssueLabel(issue_repo.NewIssueLabel())
	issue_svc.SetDefault(issue_svc.New())
	// 把 project_svc 的 cwd 解析注入 chat_svc —— chat_svc 不直接 import project_svc，
	// 避免 project_svc → chat_repo 与 chat_svc → project_svc 形成环。
	chat_svc.RegisterCwdResolver(project_svc.Default().ResolveSessionCwd)

	// 启动时按持久化的开关恢复 Debug 日志级别（取代旧 AGENTRE_DEBUG 环境变量）。
	applyDebugLoggingOnBoot(ctx)

	// Server 接入：注册 keychain + server_state_repo + server_svc 默认实现。
	// server_svc 此时的 emit 为 nil；app.go.startup 在 wails ctx 就绪后调 SetEmitter 绑定事件源。
	if err := InitServer(ctx); err != nil {
		return nil, fmt.Errorf("init server: %w", err)
	}
	if err := InitRemoteDevice(ctx); err != nil {
		return nil, fmt.Errorf("init remote device: %w", err)
	}

	// 装配本地 HTTP 代理。启动失败软降级——只记日志、不阻断 App。
	host, port := loadProxyAddr(ctx)
	gw := httpgateway.New(host, port, llm_provider_repo.LLMProvider())
	if err := gw.Start(ctx); err != nil {
		logger.Default().Warn("httpgateway start", zap.Error(err))
	}
	if st := gw.Status(); st.State != "running" {
		logger.Default().Warn("httpgateway degraded", zap.String("reason", st.Reason))
	}
	agent_backend_svc.RegisterGateway(gw)
	app_settings_svc.RegisterGateway(gw)
	chat_svc.RegisterGateway(gw)

	// 注入平台原生通知实现，供前端 App.ShowNotification 调用。
	notification_svc.RegisterNotifier(sysnotify.New())

	// 把 gateway 的 SteerInbox 注入到 claudecode runner，让 Steer 能 Push 进去；
	// 之后 PostToolUse hook 子进程会 GET /hook/v1/inbox 拉走，turn 结束时
	// chat_svc 还会调 runner.DrainPending 把残留转成下一轮的 user msg。
	claudecode.Default().SetSteerInbox(gw.Steer())

	runtime = &Runtime{config: cfg, dataDir: dataDir}
	return runtime, nil
}

// ResetStaleActiveSessions turns persisted running/waiting sessions left by a
// dead previous desktop process into error. Call this only after the Wails
// single-instance lock has admitted the process as the primary instance.
func ResetStaleActiveSessions(ctx context.Context) error {
	n, err := chat_repo.Session().ResetActiveSessions(ctx)
	if err != nil {
		logger.Default().Warn("reset stale active sessions", zap.Error(err))
		return err
	}
	if n > 0 {
		logger.Default().Info("reset stale active sessions", zap.Int64("count", n))
	}
	return nil
}

// loadProxyAddr 从 app_settings 表读监听地址 / 端口；缺失走默认 127.0.0.1:DefaultProxyListenPort。
func loadProxyAddr(ctx context.Context) (string, int) {
	host := app_setting_entity.DefaultProxyListenHost
	port := app_setting_entity.DefaultProxyListenPort
	if got, err := app_setting_repo.AppSetting().Get(ctx, app_setting_entity.KeyProxyListenHost); err == nil && got != nil && strings.TrimSpace(got.Value) != "" {
		host = strings.TrimSpace(got.Value)
	}
	if got, err := app_setting_repo.AppSetting().Get(ctx, app_setting_entity.KeyProxyListenPort); err == nil && got != nil && strings.TrimSpace(got.Value) != "" {
		port = app_setting_entity.ParseProxyPort(got.Value)
	}
	return host, port
}

// Default returns the initialized runtime, if Init has already been called.
func Default() *Runtime {
	return runtime
}

// Config returns the cago config associated with this runtime.
func (r *Runtime) Config() *configs.Config {
	if r == nil {
		return nil
	}
	return r.config
}

// DataDir returns the resolved data directory for this runtime.
func (r *Runtime) DataDir() string {
	if r == nil {
		return ""
	}
	return r.dataDir
}

// Close flushes logger buffers.
func (r *Runtime) Close() {
	if err := logger.Default().Sync(); err != nil {
		logger.Default().Debug("sync logger", zap.Error(err))
	}
}

// AppDataDir returns the directory for local Agentre state.
// 实际实现在 paths.AppDataDir；保留 wrapper 是为了让现有 internal/bootstrap.AppDataDir
// 调用点（main.go 等）零改动。
func AppDataDir() (string, error) { return paths.AppDataDir() }

func defaultConfigValues(logsDir, dbPath string) map[string]interface{} {
	// 启动默认 info 级别；debug 日志改由「设置 → 版本 & 更新」开关在 Init 末尾按
	// app_settings.logger.debug_enabled 热重载（见 applyDebugLoggingOnBoot）。
	return map[string]interface{}{
		"env":    string(appEnv()),
		"debug":  false,
		"source": "file",
		"logger": map[string]interface{}{
			"level":          "info",
			"disableConsole": false,
			"logFile": map[string]interface{}{
				"enable":        true,
				"filename":      filepath.Join(logsDir, "agentre.log"),
				"errorFilename": filepath.Join(logsDir, "error.log"),
			},
		},
		"db": map[string]interface{}{
			"driver": string(db.SQLite),
			"dsn":    dbPath,
			"debug":  false,
		},
	}
}

func appEnv() configs.Env {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AGENTRE_ENV"))) {
	case string(configs.PROD):
		return configs.PROD
	case string(configs.PRE):
		return configs.PRE
	case string(configs.TEST):
		return configs.TEST
	default:
		return configs.DEV
	}
}
