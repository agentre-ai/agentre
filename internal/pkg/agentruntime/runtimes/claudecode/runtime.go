// Package claudecode 是 Anthropic Claude Code CLI 的 agent runtime,emit sealed
// agentruntime.Event。本包 init() 时把 *Runtime 注册到 agentruntime.RuntimeFor。
package claudecode

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/pkg/httpgateway"
	"agentre/pkg/claudecode"
)

// defaultRuntime 是包级单例,init() 时登记到 agentruntime.RuntimeFor 注册表。
var defaultRuntime = NewWithPool(agentruntime.DefaultCLISessionPool())

func init() {
	agentruntime.RegisterRuntime(agent_backend_entity.TypeClaudeCode, defaultRuntime)
}

// Default 返回包级默认 *Runtime,供 bootstrap 注入 SteerInbox / app shutdown
// 调 CloseAllSessions / chat_svc 删 session 调 CloseSession 等。
func Default() *Runtime { return defaultRuntime }

// sessionCacheCap = 8:与顶层 claudecode.go.claudeSessionCacheCap 一致。
const sessionCacheCap = 8

// Runtime claudecode runtime 实现。
type Runtime struct {
	// mu 仅用于 acquireSession 的 get-or-spawn 串行化兜底。
	mu    sync.Mutex
	cache *agentruntime.CLISessionPool
	steer *httpgateway.SteerInbox
}

// New 默认 8 上限 LRU。
func New() *Runtime {
	return NewWithCap(sessionCacheCap)
}

// NewWithCap 仅供测试覆盖 LRU 触发场景(capacity=2 之类)。
func NewWithCap(capacity int) *Runtime {
	return NewWithPool(agentruntime.NewCLISessionPool(capacity))
}

func NewWithPool(pool *agentruntime.CLISessionPool) *Runtime {
	if pool == nil {
		pool = agentruntime.NewCLISessionPool(sessionCacheCap)
	}
	return &Runtime{cache: pool}
}

// SetSteerInbox 由 bootstrap 在 gateway.Start() 后注入。
func (r *Runtime) SetSteerInbox(ib *httpgateway.SteerInbox) { r.steer = ib }

// sessionKey 把 chat session ID 翻成 cache key。
func sessionKey(id int64) string { return strconv.FormatInt(id, 10) }

// Capabilities 返回 claudecode runtime 的能力矩阵 + permission mode 元数据。
func (r *Runtime) Capabilities() capability.Capabilities {
	return capability.Capabilities{
		Set: map[capability.Capability]bool{
			capability.CapSteer:          true,
			capability.CapCancelSteer:    true,
			capability.CapDrainSteer:     true,
			capability.CapAbort:          true,
			capability.CapSetPermission:  true,
			capability.CapAnswerUserAsk:  true,
			capability.CapToolPermission: true,
			capability.CapForkSession:    true,
			// translator.EventInit 路径用 llmcatalog 兜底 emit ContextWindowUpdated;
			// Claude Code SDK 协议本身不报窗口,这里靠 catalog 给前端 turn 内总量。
			capability.CapReportContextWindow: true,
			// user frame 携带 base64 image content block(CLI stream-json 原生支持);
			// extractImages 从 RunRequest.UserBlocks 抽 inline 图片经 handle.Stream 透传。
			capability.CapImageInput: true,
			// CLI 在 run_in_background Bash 任务完成后自主跑续轮;实现 AutonomousTurnSource。
			capability.CapAutonomousTurn: true,
		},
		PermissionModeMeta: capability.PermissionModeMeta{
			AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			DefaultMode:          "acceptEdits",
			SwitchableDuringTurn: true,
			Order:                []string{"default", "acceptEdits", "plan", "bypassPermissions"},
			// 空串 = chat_svc 不显式落库,pkg/claudecode args.go:86 再兜底成 acceptEdits。
			LaunchDefaultMode: "",
		},
	}
}

// Steer 把 (queuedID, text) 投入到当前 turn 对应的 claude session UUID 的
// SteerInbox。语义同顶层 claudecode.go.Steer。
func (r *Runtime) Steer(ctx context.Context, sessionID int64, queuedID, text string) error {
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		return agentruntime.ErrNoActiveTurn
	}
	a := v.(*claudeActive)
	if !a.inTurn.Load() {
		return agentruntime.ErrNoActiveTurn
	}
	if r.steer == nil {
		return errors.New("agentruntime/runtimes/claudecode: steer inbox not configured")
	}
	r.steer.Push(a.sessionUUID, queuedID, text)
	// 诊断 steer 投递路径: 消息 push 进 inbox 的 key(inboxKey)。要和
	// httpgateway.serveHookInbox 的 sid 完全一致, hook 才能在工具边界 drain 到。
	// 不一致 → mid-turn 永远 drain 不到 → 只能等 turn 末 DrainPending。
	logger.Ctx(ctx).Debug("claudecode runtime: steer enqueued to inbox",
		zap.Int64("sessionID", sessionID),
		zap.String("inboxKey", a.sessionUUID),
		zap.String("queuedID", queuedID))
	return nil
}

// CancelSteer 撤回一条尚未被 hook 拉走的排队消息。语义同顶层 CancelSteer。
func (r *Runtime) CancelSteer(_ context.Context, sessionID int64, queuedID string) ([]string, error) {
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		return nil, agentruntime.ErrNoActiveTurn
	}
	a := v.(*claudeActive)
	if r.steer == nil || a.sessionUUID == "" {
		return nil, agentruntime.ErrNoActiveTurn
	}
	if queuedID == "" {
		items := r.steer.Drain(a.sessionUUID)
		out := make([]string, 0, len(items))
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out, nil
	}
	if r.steer.Remove(a.sessionUUID, queuedID) {
		return []string{queuedID}, nil
	}
	return nil, agentruntime.ErrSteerNotFound
}

// markIdle Run() 的 drain 完成后调用:清除 inTurn 标记。语义同顶层 markIdle。
func (r *Runtime) markIdle(sessionID int64) {
	if sessionID <= 0 {
		return
	}
	key := sessionKey(sessionID)
	v, ok := r.cache.Get(key)
	if ok {
		v.(*claudeActive).inTurn.Store(false)
	}
	r.cache.MarkIdle(key)
}

// DrainPending 取走未消费的排队消息并清空,返非空时把 inTurn 重新置为 true
// (关 markIdle→acquireSession 之间的 race 窗口)。语义同顶层 DrainPending。
func (r *Runtime) DrainPending(ctx context.Context, sessionID int64) []agentruntime.ConsumedSteer {
	if sessionID <= 0 || r.steer == nil {
		return nil
	}
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		return nil
	}
	a := v.(*claudeActive)
	if a.sessionUUID == "" {
		return nil
	}
	out := consumedSteersFromInbox(r.steer.Drain(a.sessionUUID))
	if len(out) > 0 {
		a.inTurn.Store(true)
		// 诊断 steer "等整轮才发出去" 的烟枪: 走到这里 = 消息整轮都没被 PostToolUse
		// hook drain 到(纯文字轮 / 子 agent 期 / 末工具之后发), 直到 turn 收尾才被
		// 扫出来当下一轮 prompt。count>0 出现一次 = 用户体感的那次"延迟"。
		logger.Ctx(ctx).Debug("claudecode runtime: steer drained at TURN END (was not delivered mid-turn)",
			zap.Int64("sessionID", sessionID),
			zap.String("inboxKey", a.sessionUUID),
			zap.Int("count", len(out)))
	}
	return out
}

// Abort 软中断当前 turn。语义同顶层 Abort。
func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		return agentruntime.ErrNoActiveTurn
	}
	a := v.(*claudeActive)
	if !a.inTurn.Load() {
		return agentruntime.ErrNoActiveTurn
	}
	if r.steer != nil && a.sessionUUID != "" {
		_ = r.steer.Drain(a.sessionUUID)
	}
	if a.handle != nil {
		if err := a.handle.Interrupt(ctx); err != nil {
			_ = a.handle.Close(ctx)
			r.cache.Remove(sessionKey(sessionID))
		}
	}
	return nil
}

// SetPermissionMode 实现 PermissionModeSetter。语义同顶层 SetPermissionMode。
func (r *Runtime) SetPermissionMode(ctx context.Context, sessionID int64, mode string) error {
	if sessionID <= 0 {
		return fmt.Errorf("agentruntime/runtimes/claudecode: invalid sessionID %d", sessionID)
	}
	v, ok := r.cache.Get(sessionKey(sessionID))
	if !ok {
		return agentruntime.ErrNoActiveTurn
	}
	a := v.(*claudeActive)
	if a.handle == nil {
		return agentruntime.ErrNoActiveTurn
	}
	return a.handle.SetPermissionMode(ctx, mode)
}

// CloseSession 显式释放某个 chat session 的常驻进程。
func (r *Runtime) CloseSession(_ context.Context, sessionID int64) {
	if sessionID <= 0 {
		return
	}
	r.cache.Remove(sessionKey(sessionID))
}

// CloseAllSessions app shutdown 时调,关掉所有常驻 claude 子进程。
func (r *Runtime) CloseAllSessions(_ context.Context) {
	r.cache.RemoveAll()
}

// Run 启动一轮 claudecode CLI 发送。语义同顶层 claudecode.go.Run,emit 类型
// 从 RuntimeEvent 改为 sealed agentruntime.Event。
func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if req.Backend == nil {
		return nil, nil, fmt.Errorf("agentruntime/runtimes/claudecode: nil backend")
	}

	a, launchMode, err := r.acquireSession(ctx, req)
	if err != nil {
		logger.Ctx(ctx).Error("claudecode runtime: acquireSession failed",
			zap.Int64("sessionID", req.SessionID),
			zap.Int64("agentID", req.AgentID),
			zap.String("cwd", req.Cwd),
			zap.Error(err))
		return nil, nil, err
	}
	logger.Ctx(ctx).Info("claudecode runtime: turn starting",
		zap.Int64("sessionID", req.SessionID),
		zap.String("launchPermissionMode", launchMode),
		zap.String("providerSessionID", a.handle.ID()))

	stream, err := a.handle.Stream(ctx, req.UserText, extractImages(req.UserBlocks))
	if err != nil {
		logger.Ctx(ctx).Error("claudecode runtime: handle.Stream failed",
			zap.Int64("sessionID", req.SessionID),
			zap.String("providerSessionID", a.handle.ID()),
			zap.Error(err))
		a.inTurn.Store(false)
		if req.SessionID > 0 {
			r.cache.Remove(sessionKey(req.SessionID))
		} else {
			_ = a.Close(context.Background())
		}
		return nil, nil, err
	}

	out := make(chan agentruntime.Event, 32)
	result := &agentruntime.RunResult{
		ProviderSessionID:    a.handle.ID(),
		LaunchPermissionMode: launchMode,
	}

	a.setOut(out)

	go func() {
		var (
			steerDrain  <-chan []httpgateway.SteerItem
			cancelDrain func()
			steerDone   = make(chan struct{})
		)
		if r.steer != nil && a.sessionUUID != "" {
			steerDrain, cancelDrain = r.steer.SubscribeDrain(a.sessionUUID)
		}
		if steerDrain != nil {
			go func() {
				defer close(steerDone)
				for items := range steerDrain {
					if len(items) == 0 {
						continue
					}
					out <- agentruntime.SteerConsumed{Steers: consumedSteersFromInbox(items)}
				}
			}()
		} else {
			close(steerDone)
		}

		drainStream(stream, out, result, a)
		if cancelDrain != nil {
			cancelDrain()
		}
		<-steerDone
		a.clearOut()
		if sid := stream.SessionID(); sid != "" {
			result.ProviderSessionID = sid
		}
		// 0-frame 兜底:CLI spawn 起来但立刻退出。语义同顶层 Run。
		if result.Usage == nil && result.StopErr == nil && ctx.Err() == nil {
			if exitErr := a.handle.ExitErr(); exitErr != nil {
				result.StopErr = exitErr
			} else {
				result.StopErr = errors.New("agentruntime/runtimes/claudecode: subprocess produced no events (likely exited on startup)")
			}
			logger.Ctx(ctx).Warn("claudecode runtime: subprocess produced no events",
				zap.Int64("sessionID", req.SessionID),
				zap.String("providerSessionID", result.ProviderSessionID),
				zap.Error(result.StopErr))
			if req.SessionID > 0 {
				r.cache.Remove(sessionKey(req.SessionID))
			}
		}
		// 最佳努力地从 JSONL 抽 UserAnchor。
		if root := projectsRoot(); root != "" && result.ProviderSessionID != "" {
			if msgs, err := claudecode.ReadSessionJSONL(root, result.ProviderSessionID); err == nil {
				result.UserAnchor = claudecode.FindUserAnchorByText(msgs, req.UserText)
			}
		}
		// 生命周期边界:turn 收尾。结构化打 model / usage 让排查
		// 「provider 没识别 / usage 抽空了」这类 third-party provider 接入问题
		// 不用再起 raw dump (--include-partial-messages stream_event 那次踩坑)。
		logTurnDone(ctx, req, result)
		r.markIdle(req.SessionID)
		close(out)
	}()
	return out, result, nil
}

// logTurnDone 把一轮 claudecode turn 收尾时拿到的关键状态结构化写日志。
// usage 不直接 spread 是因为 *provider.Usage 可能 nil (0-frame 兜底分支);
// 这里把每个字段单独抽出来打,grep 时不需要展开嵌套对象。
func logTurnDone(ctx context.Context, req agentruntime.RunRequest, result *agentruntime.RunResult) {
	fields := []zap.Field{
		zap.Int64("sessionID", req.SessionID),
		zap.String("providerSessionID", result.ProviderSessionID),
		zap.String("model", result.Model),
	}
	if result.Usage != nil {
		fields = append(fields,
			zap.Int("promptTokens", result.Usage.PromptTokens),
			zap.Int("completionTokens", result.Usage.CompletionTokens),
			zap.Int("cachedTokens", result.Usage.CachedTokens),
			zap.Int("cacheCreationTokens", result.Usage.CacheCreationTokens),
		)
	} else {
		fields = append(fields, zap.Bool("usageNil", true))
	}
	if result.StopErr != nil {
		fields = append(fields, zap.Error(result.StopErr))
		logger.Ctx(ctx).Warn("claudecode runtime: turn done with stopErr", fields...)
		return
	}
	logger.Ctx(ctx).Info("claudecode runtime: turn done", fields...)
}

func consumedSteersFromInbox(items []httpgateway.SteerItem) []agentruntime.ConsumedSteer {
	if len(items) == 0 {
		return nil
	}
	out := make([]agentruntime.ConsumedSteer, 0, len(items))
	for _, it := range items {
		out = append(out, agentruntime.ConsumedSteer{QueuedID: it.ID, Text: it.Text})
	}
	return out
}

// acquireSession 拿到 chat session 对应的常驻 claude handle,必要时 spawn 新
// 进程。返回 (active, launchMode, err):launchMode 是本次实际下发给 CLI 的
// --permission-mode 值,Run() 写回 RunResult.LaunchPermissionMode,由 chat_svc
// 在主进程侧落库到 session.PermissionModeAtLaunch。
//
// 历史:旧实现直接调 chat_repo.Session().UpdatePermissionModeAtLaunch 写库,
// 在 agentred daemon 进程(不 bootstrap cago/chat_repo)里会 nil panic。
func (r *Runtime) acquireSession(ctx context.Context, req agentruntime.RunRequest) (*claudeActive, string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := sessionKey(req.SessionID)
	var cur *claudeActive
	if req.SessionID > 0 {
		if v, ok := r.cache.Get(key); ok {
			cur = v.(*claudeActive)
		}
	}

	if req.ForkAnchor != "" && cur != nil {
		r.cache.Remove(key)
		cur = nil
	}

	if cur != nil && cur.launchedEffort != req.Backend.ReasoningEffort {
		r.cache.Remove(key)
		cur = nil
	}

	if cur != nil {
		// 复用现有 CLI 子进程:不重新 spawn,因此本轮没有新的 --permission-mode
		// 下发。回吐当前缓存的 mode 让 chat_svc 写库幂等(值不变即 noop)。
		cur.inTurn.Store(true)
		r.cache.MarkActive(key)
		return cur, cur.permissionMode, nil
	}

	isolationUUID := newUUIDv4()
	bin, err := os.Executable()
	if err != nil {
		return nil, "", fmt.Errorf("agentruntime/runtimes/claudecode: resolve executable path: %w", err)
	}
	settingsJSON, err := buildHookSettingsJSONString(bin)
	if err != nil {
		return nil, "", fmt.Errorf("agentruntime/runtimes/claudecode: build hook settings: %w", err)
	}

	pureResume := req.ProviderSessionID != "" && req.ForkAnchor == ""
	var cliSessionUUID, inboxKey string
	if pureResume {
		cliSessionUUID = ""
		inboxKey = req.ProviderSessionID
	} else {
		cliSessionUUID = isolationUUID
		inboxKey = isolationUUID
	}

	cwd := req.Cwd
	if cwd == "" {
		cwd, err = agentruntime.AgentCwd(req.AgentID)
		if err != nil {
			return nil, "", err
		}
	}
	env, err := BuildClaudeCodeEnv(req.Backend, CLIDeps{Token: req.GatewayToken, GatewayURL: req.GatewayURL})
	if err != nil {
		return nil, "", err
	}
	handle, err := ccSessionFactory(ccLaunchSpec{
		Req:                   req,
		Env:                   env,
		Cwd:                   cwd,
		Settings:              settingsJSON,
		SessionUUID:           cliSessionUUID,
		PermissionMode:        req.PermissionMode,
		DefaultPermissionMode: req.Backend.DefaultPermissionMode,
	})
	if err != nil {
		return nil, "", err
	}
	resolvedLaunchMode := resolveLaunchMode(req.PermissionMode, req.Backend.DefaultPermissionMode)
	// stored mode != launch mode 时(目前唯一触发条件: backendDefault=bypass +
	// req.PermissionMode != bypass, 见 resolveLaunchMode 注释), spawn 完成的瞬间
	// 发一次 control_request set_permission_mode 把 CLI 校准到 stored mode 一
	// 让前端 pill 看到的 mode 和 CLI 实际行为一致。失败仅记 warn 不阻 spawn ——
	// launch mode (bypass) 已是最宽松, 用户可以在 pill 上手动重试切换。
	//
	// 必须在 cache.Put + drainStream goroutine 启动前调用同步 SetPermissionMode,
	// 否则与 EventPermissionModeChanged 写 active.permissionMode 的路径有 race。
	runtimeMode := resolvedLaunchMode
	if req.PermissionMode != "" && req.PermissionMode != resolvedLaunchMode {
		if err := handle.SetPermissionMode(ctx, req.PermissionMode); err != nil {
			logger.Ctx(ctx).Warn("claudecode runtime: spawn-after SetPermissionMode failed",
				zap.Int64("sessionID", req.SessionID),
				zap.String("launchMode", resolvedLaunchMode),
				zap.String("storedMode", req.PermissionMode),
				zap.Error(err))
		} else {
			runtimeMode = req.PermissionMode
		}
	}
	cur = &claudeActive{
		sessionUUID:    inboxKey,
		handle:         handle,
		steer:          r.steer,
		pool:           r.cache,
		poolKey:        key,
		launchedEffort: req.Backend.ReasoningEffort,
		permissionMode: runtimeMode,
		tasks:          newTaskAggregator(),
	}
	if req.SessionID > 0 {
		r.cache.Put(key, cur)
		r.cache.MarkActive(key)
	}
	cur.inTurn.Store(true)
	return cur, resolvedLaunchMode, nil
}

// drainStream 把 claudecode.Event 翻成 sealed agentruntime.Event;同步累 usage
// / stopErr;control_request 走 handleControlRequest 分派。
//
// 额外:active.tasks 聚合 TaskCreate / TaskUpdate 增量为 canonical.PlanUpdate
// 完整快照(TodoWrite 已经在 translator 里直接出 EventPlanUpdated),emit 顺序
// 是"先翻译完原始 event,再吐 PlanUpdated 快照",保证前端看到的 plan 更新
// 永远在对应 tool_use / tool_result 之后(消费方的 mutation order 不被打破)。
func drainStream(stream ccStream, out chan<- agentruntime.Event, result *agentruntime.RunResult, active *claudeActive) {
	for stream.Next() {
		ev := stream.Event()
		if result.StopErr != nil && claudeEventShowsProgressAfterError(ev.Kind) {
			result.StopErr = nil
		}
		if ev.Kind == claudecode.EventControlRequest && ev.ControlRequest != nil && active != nil {
			handleControlRequest(ev.ControlRequest, active, out)
			continue
		}
		// 同步 in-process mode 快照 —— ExitPlanMode 之后 CLI 会切到 default,
		// handleControlRequest 里的 bypassPermissions 短路判断要看新值。
		if ev.Kind == claudecode.EventPermissionModeChanged && ev.PermissionMode != "" && active != nil {
			active.permissionMode = ev.PermissionMode
		}
		translated, usage, stopErr := translate(ev)
		for _, t := range translated {
			out <- t
		}
		if active != nil && active.tasks != nil {
			switch ev.Kind {
			case claudecode.EventPreToolUse:
				emitSnapshot(out, active.tasks.observePreToolUse(ev))
			case claudecode.EventPostToolUse:
				emitSnapshot(out, active.tasks.observePostToolUse(ev))
			}
		}
		if usage != nil {
			result.Usage = usage
		}
		if stopErr != nil {
			result.StopErr = stopErr
		}
		if ev.Kind == claudecode.EventDone && ev.Model != "" {
			result.Model = ev.Model
		}
	}
}

func claudeEventShowsProgressAfterError(kind claudecode.EventKind) bool {
	switch kind {
	case claudecode.EventTextDelta,
		claudecode.EventThinkingDelta,
		claudecode.EventPreToolUse,
		claudecode.EventPostToolUse,
		claudecode.EventTaskStarted,
		claudecode.EventTaskProgress,
		claudecode.EventTaskNotification,
		claudecode.EventRetry,
		claudecode.EventCompactBoundary,
		claudecode.EventControlRequest:
		return true
	default:
		return false
	}
}
