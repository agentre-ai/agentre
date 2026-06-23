package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/app_setting_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/claudecode"
	_ "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote"
	_ "github.com/agentre-ai/agentre/internal/pkg/agentskill/claudeskill" // 触发 discoverer init 注册
	_ "github.com/agentre-ai/agentre/internal/pkg/agentskill/codexskill"  // 触发 discoverer init 注册
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/pkg/paths"
	"github.com/agentre-ai/agentre/internal/pkg/sysnotify"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/app_setting_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/department_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
	"github.com/agentre-ai/agentre/internal/service/agent_backend_svc"
	"github.com/agentre-ai/agentre/internal/service/app_settings_svc"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
	"github.com/agentre-ai/agentre/internal/service/notification_svc"
	"github.com/agentre-ai/agentre/internal/service/orgtool_svc"
	"github.com/agentre-ai/agentre/internal/service/project_svc"
	"github.com/agentre-ai/agentre/internal/service/skill_svc"
	"github.com/agentre-ai/agentre/internal/service/subagent_svc"
	"github.com/agentre-ai/agentre/internal/service/workflowtool_svc"
	"github.com/agentre-ai/agentre/migrations"

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
	cfg, err := configs.NewConfig(appName, configs.WithSource(memory.NewSource(defaultConfigValues(logsDir, sqliteDSN(dbPath)))))
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
	group_repo.RegisterGroup(group_repo.NewGroup())
	group_repo.RegisterMember(group_repo.NewMember())
	group_repo.RegisterMessage(group_repo.NewMessage())
	group_repo.RegisterTask(group_repo.NewTask())
	workflow_repo.RegisterWorkflow(workflow_repo.NewWorkflow())
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

	// 挂群聊 group_send MCP handler 到 gateway，并把 gateway base URL 注入 group_svc——
	// agent 子进程通过 <base>/mcp/group/ 回投消息。gateway 绑定失败时 BaseURL() 返回空串，
	// group MCP 不可达(软降级，App 继续)。
	gw.RegisterMCP("/mcp/group/", group_svc.Default().MCPHandler())
	group_svc.Default().SetGatewayBaseURL(gw.BaseURL())

	// 挂组织架构工具 MCP handler(/mcp/org/),并注册 TurnMCPProvider:
	// agent 开了 org 工具的会话 turn 注入该 MCP server(审批在服务端,见 orgtool_svc)。
	// 注意:RegisterDeps(含 chat_svc.Chat() 作为 ApprovalGateway)延迟到 app.go
	// registerChatService() 中 RegisterChat 之后执行——此时 chat_svc.Chat() 已非 nil。
	gw.RegisterMCP("/mcp/org/", orgtool_svc.Default().MCPHandler())
	orgtool_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(orgtool_svc.Default().BuildTurnMCP)
	// agent 开了流程库工具的会话 turn 注入该 MCP server(写操作审批在服务端,见 workflowtool_svc)。
	gw.RegisterMCP("/mcp/workflow/", workflowtool_svc.Default().MCPHandler())
	workflowtool_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(workflowtool_svc.Default().BuildTurnMCP)
	// group_create:单聊轮注入(群成员轮在 provider 内按 groupID 跳过)。
	chat_svc.RegisterTurnMCPProvider(group_svc.Default().BuildCreateTurnMCP)
	// 群成员 backing session 的群上下文(group_send MCP + 群 system-prompt 后缀):
	// 用户直接 Send/Edit/Regenerate(不经 scheduler)时补齐,修设计问题⑥。
	chat_svc.RegisterTurnExtrasProvider(group_svc.Default().BuildSendTurnExtras)
	// 挂「调用子 agent」工具 MCP handler(/mcp/subagent/) + 注册 TurnMCPProvider:
	// agent 开了 subagent 工具的会话 turn 注入该 MCP server(无审批门, 见 subagent_svc)。
	gw.RegisterMCP("/mcp/subagent/", subagent_svc.Default().MCPHandler())
	subagent_svc.Default().SetGatewayBaseURL(gw.BaseURL())
	chat_svc.RegisterTurnMCPProvider(subagent_svc.Default().BuildTurnMCP)
	// 远端执行(agentred):daemon 上 CLI 子进程访问内置工具 MCP(org/subagent/group/
	// workflow)会被 daemon 改写成 daemon 本地 URL,再经 WS 反向请求隧道回 desktop。这里
	// 装配把隧道请求重放到 desktop 本机 gateway 的 dispatcher。无 client 超时:approval 类
	// 工具可挂几分钟,由 MCP handler 自身上限收口(见 approvalTimeout)。
	remote.RegisterMCPProxyDispatcher(remote.NewLocalGatewayDispatcher(gw.BaseURL, &http.Client{}))

	// 技能包(skill pack)注入:skill_svc 组合 agent 授权 + 发现,chat_svc 按 CapSkills
	// 在 runTurn 注入 RunRequest.EnabledPlugins(runtime 各自渲染到 CLI 配置)。
	skill_svc.Register(agent_repo.Agent(), agent_backend_repo.AgentBackend(), agent_backend_svc.NewRemoteSkillDiscoverer())
	chat_svc.RegisterEnabledPluginsProvider(func(ctx context.Context, a *agent_entity.Agent) map[string]bool {
		m, err := skill_svc.Default().EnabledPluginsMap(ctx, a.ID)
		if err != nil {
			return nil // 发现/查询失败 → 软降级(本轮不约束技能集),不阻断对话
		}
		return m
	})

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
	// 环境变量覆盖(最高优先级):e2e 用 AGENTRE_PROXY_PORT=0 绑 OS 临时端口,与已运行的正式
	// Agentre(固定 52401)互不抢端口,保证 gateway 在 e2e 中可靠起来(否则 BaseURL 为空、
	// group_send 之类经 gateway 的回投全部失效)。生产不设此变量,行为不变。
	if p, ok := proxyPortFromEnv(); ok {
		port = p
	}
	return host, port
}

// proxyPortFromEnv 解析 AGENTRE_PROXY_PORT 覆盖值。未设置 / 非数字 / 越界 → ok=false(回退默认)。
// 0 合法,表示让 OS 选一个空闲端口(e2e 隔离用)。
func proxyPortFromEnv() (int, bool) {
	raw := strings.TrimSpace(os.Getenv("AGENTRE_PROXY_PORT"))
	if raw == "" {
		return 0, false
	}
	p, err := strconv.Atoi(raw)
	if err != nil || p < 0 || p > 65535 {
		return 0, false
	}
	return p, true
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

// sqliteDSN 给 SQLite 文件路径挂上连接 pragma。busy_timeout: 并发 turn 流式写库
// 时另一条写会撞 SQLITE_BUSY(默认 0 立即失败,实测 0.5ms 即报 database is locked),
// 改为等锁最多 5s。glebarez 驱动按 DSN _pragma 参数对每个池化连接生效;启动后
// Exec("PRAGMA ...") 只作用单个连接,不可用。
func sqliteDSN(dbPath string) string {
	return dbPath + "?_pragma=busy_timeout(5000)"
}

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
