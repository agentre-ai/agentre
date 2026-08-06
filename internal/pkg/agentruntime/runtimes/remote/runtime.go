// Package remote 是 agentre 桌面端连远端 agentred daemon 的 agent runtime
// 客户端。daemon 端跑真正的 claudecode / codex / builtin runtime,本包通过
// WebSocket + JSON-RPC(runtime.* 命名空间)把整个 agentruntime.Runtime
// 接口 + 7 个可选子接口透明代理过去:
//
//   - Run / Steer / CancelSteer / DrainPending / Abort / SetPermissionMode /
//     SubmitAnswer / SubmitToolPermission → existing runtime.* RPC forms
//   - Pi PrepareRun → phased runtime.run registration / preparation / Start
//   - daemon → client 反向 push 用两条 notification:
//     runtime.event(每个 sealed Event 一条)+ runtime.runResultDone(终态)
//
// chat_svc 拿到 *Runtime 后只用接口方法,看不到本地 / 远端区别。
//
// 协议层 sentinel 错误(ErrNoActiveTurn / ErrSteerNotFound / ErrUnsupported /
// ErrAborted)通过 wire.FromJSONRPCError 反向 rehydrate,让 errors.Is 跨进程
// 继续工作。
package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	piagentrt "github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/piagent"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// remoteSession 一个远端 daemon 上跑的 chat session 在本地的镜像。sessionID
// 是 client/daemon 共享的 int64(daemon 侧不再分配额外的 string sid),所以一个 map 就够。
type remoteSession struct {
	id          int64
	backendType agent_backend_entity.BackendType
	events      chan agentruntime.Event
	result      *agentruntime.RunResult
	ctx         context.Context
	cancel      context.CancelFunc

	mu                sync.Mutex
	providerSessionID string
	started           bool
	closed            bool
	registrationDone  chan struct{}
	registrationOnce  sync.Once
	abortOnce         sync.Once
	abortErr          error
}

// Runtime 包装 DaemonClientPort 把 chat session 委托给远端 daemon。生命周期:
//   - New(client) 立即向 client 注册两条 server-push handler
//   - Run() 调 runtime.run 注册 session,后续 runtime.event / runtime.runResultDone
//     按 sessionID 路由；Pi PrepareRun 复用同一方法分三阶段完成
//   - Prefetch(ctx, backendType) 主动拉一次 daemon 的 capability 矩阵缓存到本地,
//     之后 Capabilities() 同步返(chat_svc UI gating 依赖它是同步的)
type Runtime struct {
	client agentruntime.DaemonClientPort

	mu              sync.RWMutex
	sessions        map[int64]*remoteSession
	generationGates map[int64]*generationGate
	caps            map[agent_backend_entity.BackendType]capability.Capabilities
	// autoSessions 是「自主续轮」(AutonomousTurnSource)的会话级镜像,**独立于**
	// per-Run 的 sessions(后者在 runResultDone 时删除,而自主续轮发生在 Run 收尾
	// *之后*)。按 sessionID 持久(跨 turn / 子进程 evict 复用),conn close 时统一拆。
	// 见 autoturn.go。
	autoSessions    map[int64]*autoSession
	piGenerationSeq atomic.Uint64
}

type generationGate struct {
	mu   sync.Mutex
	refs int
}

// New 构造一个 remote.Runtime,并把 runtime.event / runtime.runResultDone
// 两个 server-push handler 注册到 client。调用方负责管理 client 的生命周期(通常
// 是 Pool.Lease)。
//
// 额外起一个 goroutine 监 client.Closed():daemon 进程崩溃 / 网络断 / TLS 失
// 败等情况下,在飞的 run session 永远等不到 runResultDone,events channel 不
// 关 → chat_svc.runTurn 卡在 `for ev := range events`,前端会话一直停在「生
// 成中」。conn 关闭时给所有 live session 注入一条 ErrDaemonDisconnected 的
// StopErr 并 close events,chat_svc 走 StreamError 解锁前端。
func New(c agentruntime.DaemonClientPort) *Runtime {
	r := &Runtime{
		client:          c,
		sessions:        map[int64]*remoteSession{},
		generationGates: map[int64]*generationGate{},
		caps:            map[agent_backend_entity.BackendType]capability.Capabilities{},
		autoSessions:    map[int64]*autoSession{},
	}
	c.Handle(wire.NotifyEvent, r.handleEvent)
	c.Handle(wire.NotifyRunResultDone, r.handleRunResultDone)
	c.Handle(wire.NotifyAutonomousTurnStarted, r.handleAutonomousTurnStarted)
	c.Handle(wire.NotifyAutonomousTurnEvent, r.handleAutonomousTurnEvent)
	c.Handle(wire.NotifyAutonomousTurnDone, r.handleAutonomousTurnDone)
	// daemon 上的 CLI 子进程访问内置工具 MCP(org/subagent/group/workflow)时,经此反向
	// 请求隧道回 desktop 执行(真 /mcp/* handler 在 desktop)。见 mcpproxy.go。
	c.Handle(wire.MethodMCPProxy, r.handleMCPProxy)
	if closed := c.Closed(); closed != nil {
		go r.watchClose(closed)
	}
	return r
}

// ErrDaemonDisconnected 当远端 daemon 连接断开(进程崩 / 网络断 / 主动 Close)
// 时,remote.Runtime 注入到在飞 session 的 StopErr。chat_svc 拿到后映射为
// StreamError,前端就能解锁「生成中」并显示一条提示。
var ErrDaemonDisconnected = errors.New("agentruntime/runtimes/remote: daemon connection closed")

const generationControlTimeout = 5 * time.Second

// watchClose 阻塞读 client.Closed(),触发时把所有未结束的 session 用
// ErrDaemonDisconnected 收尾。幂等 - session 已经被 handleRunResultDone 关闭
// 的不会被二次关。
func (r *Runtime) watchClose(closed <-chan struct{}) {
	<-closed
	r.mu.Lock()
	live := make([]*remoteSession, 0, len(r.sessions))
	liveSids := make([]int64, 0, len(r.sessions))
	for sid, sess := range r.sessions {
		live = append(live, sess)
		liveSids = append(liveSids, sid)
		delete(r.sessions, sid)
	}
	r.mu.Unlock()
	// 关键失败模式:daemon 进程崩 / 网络断 / TLS 失败,客户端单方面感知到 conn close,
	// 给在飞 session 注 ErrDaemonDisconnected 解锁前端「生成中」。同步落一条 Warn
	// 让运维事后能在日志里看到"哪几个 session 是被 daemon 断连兜底关掉的",而不是
	// 误以为 runtime 正常收尾。空 live 列表也打一条 Debug,方便区分"daemon 主动断
	// 但没在飞 session" 与日志缺失。
	if len(live) > 0 {
		// goroutine 无请求 ctx,按 CLAUDE.md 日志规范用 logger.Default()。
		logger.Default().Warn("remote runtime: daemon disconnected, injecting StopErr to live sessions",
			zap.Int("liveCount", len(live)),
			zap.Int64s("sids", liveSids))
	} else {
		logger.Default().Debug("remote runtime: daemon disconnected, no live sessions")
	}
	for _, sess := range live {
		sess.mu.Lock()
		if sess.cancel != nil {
			sess.cancel()
		}
		if !sess.closed {
			sess.closed = true
			if sess.result != nil && sess.result.StopErr == nil {
				sess.result.StopErr = ErrDaemonDisconnected
			}
			close(sess.events)
		}
		sess.mu.Unlock()
	}
	// 自主续轮镜像也随 conn close 拆掉:close 每个 out → chat_svc 的 watcher 退出;
	// 在飞的那轮 events 也 close,driveAutonomousTurn 干净收尾。见 autoturn.go。
	r.closeAllAutoSessions()
}

// Close 关掉与 daemon 的 client 连接。
func (r *Runtime) Close() error {
	if r.client == nil {
		return nil
	}
	return r.client.Close()
}

// ── Capabilities ───────────────────────────────────────────────────────────

// Prefetch 主动拉一次 daemon 端 backendType 对应 runtime 的 capability 矩阵
// 并缓存,后续 Capabilities() 同步返。chat_svc.borrowRemoteRuntime 在 Pool
// borrow 完成后调一次,避免 turn 启动时再走异步 RPC。
//
// 已缓存的 backendType 重复调直接 noop。
func (r *Runtime) Prefetch(ctx context.Context, bt agent_backend_entity.BackendType) error {
	r.mu.RLock()
	_, ok := r.caps[bt]
	r.mu.RUnlock()
	if ok {
		return nil
	}
	var res wire.CapabilitiesResult
	if err := r.client.Call(ctx, wire.MethodCapabilities, wire.CapabilitiesParams{
		BackendType: string(bt),
	}, &res); err != nil {
		return wire.FromJSONRPCError(err)
	}
	r.mu.Lock()
	r.caps[bt] = res.Capabilities
	r.mu.Unlock()
	return nil
}

// Capabilities 返回最近一次 Prefetch 的结果(任意 backendType 第一个命中的);
// 没 Prefetch 时返默认占位矩阵让 UI gating 不挂死。
//
// 一台远端 daemon 通常只跑一种 backend type(claudecode 或 codex),所以单值
// 返回足够;真要同 device 多 backend,chat_svc 拿到 runtime 后立即 Prefetch
// 当前 turn 的 backendType,再调 Capabilities() 命中刚写的 cache。
func (r *Runtime) Capabilities() capability.Capabilities {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, c := range r.caps {
		return c
	}
	return defaultCapsBeforePrefetch
}

// defaultCapsBeforePrefetch 占位矩阵 —— Prefetch 之前 UI 才不会一片灰。
// claudecode 是 daemon 最常见的 backend,所以默认对齐它已知能力子集。
var defaultCapsBeforePrefetch = capability.Capabilities{
	Set: map[capability.Capability]bool{
		capability.CapSteer:              true,
		capability.CapAbort:              true,
		capability.CapStopBackgroundTask: true,
		capability.CapAnswerUserAsk:      true,
		capability.CapToolPermission:     true,
		capability.CapSkills:             true,
	},
	PermissionModeMeta: capability.PermissionModeMeta{
		AllowedModes:         []string{"default", "acceptEdits", "plan", "bypassPermissions"},
		DefaultMode:          "acceptEdits",
		Order:                []string{"default", "acceptEdits", "plan", "bypassPermissions"},
		SwitchableDuringTurn: false,
	},
}

// ── Run ─────────────────────────────────────────────────────────────────────

// Run 在远端 daemon 上启动一轮 chat session;本地返回 sealed Event 流 +
// 一个会异步被填充的 *RunResult。channel close 之后调用方才能读 RunResult,
// 这一契约由 daemon 的 runtime.runResultDone 通知保证:终态帧到达时先填
// result,再 close channel。
func (r *Runtime) Run(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if req.Backend != nil && req.Backend.IsPiAgent() {
		prepared, err := r.PrepareRun(ctx, req)
		if err != nil {
			return nil, nil, err
		}
		return prepared.Start(ctx)
	}
	return r.runDirect(ctx, req)
}

// PrepareRun mirrors Pi's existing local pre-prompt boundary without adding a
// wire method or field. The first runtime.run request prepares/forks on
// agentred and returns RunAck.ProviderSessionID. Start sends the same request a
// second time with that provider identity, which agentred uses to start the
// exact prepared generation only after chat_svc has durably activated it.
func (r *Runtime) PrepareRun(ctx context.Context, req agentruntime.RunRequest) (piagentrt.PreparedRun, error) {
	if req.Backend == nil || !req.Backend.IsPiAgent() {
		return nil, errors.New("remote runtime: prepared runs are only supported for Pi Agent")
	}
	params, err := buildRunParams(req)
	if err != nil {
		return nil, err
	}
	// Pi does not consume PermissionMode. Reuse that existing request field as
	// an opaque per-runtime generation owner, and strip it on agentred before
	// constructing the real Pi RunRequest. This avoids a new wire field while
	// letting delayed preparation/start requests fail closed against a retry.
	params.PermissionMode = "remote-pi-generation-" + strconv.FormatUint(r.piGenerationSeq.Add(1), 10)
	generationCtx, cancel := context.WithCancel(ctx)
	sess := &remoteSession{
		id:               req.SessionID,
		backendType:      agent_backend_entity.TypePiAgent,
		events:           make(chan agentruntime.Event, 64),
		result:           &agentruntime.RunResult{},
		ctx:              generationCtx,
		cancel:           cancel,
		registrationDone: make(chan struct{}),
	}
	releaseGenerationGate := r.acquireGenerationGate(req.SessionID)
	r.mu.Lock()
	if _, exists := r.sessions[req.SessionID]; exists {
		r.mu.Unlock()
		releaseGenerationGate()
		cancel()
		return nil, errors.New("remote runtime: session already has an active generation")
	}
	r.sessions[req.SessionID] = sess
	r.mu.Unlock()

	registrationCtx, stopRegistration := context.WithTimeout(context.WithoutCancel(ctx), generationControlTimeout)
	var registrationAck wire.RunAck
	registrationErr := r.client.Call(registrationCtx, wire.MethodRun, params, &registrationAck)
	stopRegistration()
	sess.registrationOnce.Do(func() { close(sess.registrationDone) })
	releaseGenerationGate()
	if registrationErr != nil {
		_ = r.abortGeneration(ctx, sess)
		return nil, wire.FromJSONRPCError(registrationErr)
	}
	if registrationAck.SessionID != req.SessionID {
		_ = r.abortGeneration(ctx, sess)
		return nil, fmt.Errorf("remote runtime: Pi registration returned session %d for %d", registrationAck.SessionID, req.SessionID)
	}
	if err := generationCtx.Err(); err != nil {
		_ = r.abortGeneration(ctx, sess)
		return nil, err
	}

	var ack wire.RunAck
	if err := r.client.Call(generationCtx, wire.MethodRun, params, &ack); err != nil {
		_ = r.abortGeneration(ctx, sess)
		logger.Ctx(ctx).Warn("remote runtime: Pi preparation RPC failed",
			zap.Int64("sessionId", req.SessionID),
			zap.String("errorType", fmt.Sprintf("%T", err)))
		return nil, wire.FromJSONRPCError(err)
	}
	if ack.SessionID != req.SessionID {
		_ = r.abortGeneration(ctx, sess)
		return nil, fmt.Errorf("remote runtime: Pi preparation returned session %d for %d", ack.SessionID, req.SessionID)
	}
	providerSessionID := strings.TrimSpace(ack.ProviderSessionID)
	if providerSessionID == "" {
		_ = r.abortGeneration(ctx, sess)
		return nil, errors.New("remote runtime: Pi preparation returned empty provider session id")
	}
	sess.mu.Lock()
	sess.providerSessionID = providerSessionID
	sess.result.ProviderSessionID = providerSessionID
	sess.result.LaunchPermissionMode = ack.LaunchPermissionMode
	sess.mu.Unlock()
	return &remotePreparedRun{runtime: r, session: sess, params: params}, nil
}

type remotePreparedRun struct {
	runtime *Runtime
	session *remoteSession
	params  wire.RunParams

	mu      sync.Mutex
	started bool
	closed  bool
}

func (p *remotePreparedRun) ProviderSessionID() string {
	if p == nil || p.session == nil {
		return ""
	}
	p.session.mu.Lock()
	defer p.session.mu.Unlock()
	return p.session.providerSessionID
}

func (p *remotePreparedRun) Start(ctx context.Context) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	if p == nil || p.runtime == nil || p.session == nil {
		return nil, nil, errors.New("remote runtime: nil prepared Pi generation")
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, nil, errors.New("remote runtime: prepared Pi generation closed")
	}
	if p.started {
		p.mu.Unlock()
		return nil, nil, errors.New("remote runtime: prepared Pi generation already started")
	}
	p.started = true
	p.mu.Unlock()

	p.params.ProviderSessionID = p.ProviderSessionID()
	p.session.mu.Lock()
	p.session.started = true
	p.session.mu.Unlock()
	startCtx, cancelStart := context.WithCancel(p.session.ctx)
	stopCallerCancel := context.AfterFunc(ctx, cancelStart)
	defer func() {
		stopCallerCancel()
		cancelStart()
	}()
	var ack wire.RunAck
	if err := p.runtime.client.Call(startCtx, wire.MethodRun, p.params, &ack); err != nil {
		_ = p.runtime.abortGeneration(ctx, p.session)
		return nil, nil, wire.FromJSONRPCError(err)
	}
	if ack.SessionID != p.session.id || strings.TrimSpace(ack.ProviderSessionID) != p.ProviderSessionID() {
		_ = p.runtime.abortGeneration(ctx, p.session)
		return nil, nil, errors.New("remote runtime: Pi start acknowledged a different prepared generation")
	}
	p.session.mu.Lock()
	p.session.result.LaunchPermissionMode = ack.LaunchPermissionMode
	p.session.mu.Unlock()
	logger.Ctx(ctx).Info("remote runtime: Pi generation started",
		zap.Int64("sessionId", ack.SessionID),
		zap.String("providerSessionId", ack.ProviderSessionID))
	return p.session.events, p.session.result, nil
}

func (p *remotePreparedRun) Close(ctx context.Context) error {
	if p == nil || p.runtime == nil || p.session == nil {
		return nil
	}
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	return p.runtime.abortGeneration(ctx, p.session)
}

func (r *Runtime) runDirect(ctx context.Context, req agentruntime.RunRequest) (<-chan agentruntime.Event, *agentruntime.RunResult, error) {
	params, err := buildRunParams(req)
	if err != nil {
		return nil, nil, err
	}
	generationCtx, cancel := context.WithCancel(ctx)
	sess := &remoteSession{
		id:          req.SessionID,
		backendType: agent_backend_entity.BackendType(req.Backend.Type),
		events:      make(chan agentruntime.Event, 64),
		result:      &agentruntime.RunResult{},
		ctx:         generationCtx,
		cancel:      cancel,
		started:     true,
	}
	r.mu.Lock()
	r.sessions[req.SessionID] = sess
	r.mu.Unlock()

	var ack wire.RunAck
	if err := r.client.Call(generationCtx, wire.MethodRun, params, &ack); err != nil {
		r.finishSession(sess, nil)
		logger.Ctx(ctx).Error("remote runtime: Run RPC failed",
			zap.Int64("requestedSid", req.SessionID), zap.Error(err))
		return nil, nil, wire.FromJSONRPCError(err)
	}

	sess.mu.Lock()
	sess.id = ack.SessionID
	sess.providerSessionID = strings.TrimSpace(ack.ProviderSessionID)
	sess.result.ProviderSessionID = ack.ProviderSessionID
	sess.result.LaunchPermissionMode = ack.LaunchPermissionMode
	sess.mu.Unlock()
	if ack.SessionID != req.SessionID {
		r.mu.Lock()
		if r.sessions[req.SessionID] == sess {
			delete(r.sessions, req.SessionID)
			r.sessions[ack.SessionID] = sess
		}
		r.mu.Unlock()
	}
	logger.Ctx(ctx).Info("remote runtime: session started",
		zap.Int64("sid", ack.SessionID),
		zap.String("backend", req.Backend.Type))
	return sess.events, sess.result, nil
}

// buildRunParams 序列化 agentruntime.RunRequest 成 wire.RunParams。Backend
// 走 json.RawMessage 透传(避免 wire 硬依赖 entity 内部结构),History 通过
// blocks.EncodeAll 转成 StoredBlock 形式。
//
// 故意不发 req.Provider / GatewayURL / GatewayToken —— 见 wire.RunParams 注释:
// daemon 端在 handlers/runtime.go 里自家 ProviderLookup + 自家 Gateway 解出来,
// desktop 那份是本机 127.0.0.1 + 含 APIKey 的明文,跨进程发过去既不可达也不安全。
func buildRunParams(req agentruntime.RunRequest) (wire.RunParams, error) {
	backendJSON, err := json.Marshal(req.Backend)
	if err != nil {
		return wire.RunParams{}, fmt.Errorf("marshal backend: %w", err)
	}
	history, err := encodeHistory(req.History)
	if err != nil {
		return wire.RunParams{}, err
	}
	userBlocks, err := blocks.EncodeAll(req.UserBlocks)
	if err != nil {
		return wire.RunParams{}, fmt.Errorf("encode user blocks: %w", err)
	}
	return wire.RunParams{
		Backend:           backendJSON,
		AgentID:           req.AgentID,
		SessionID:         req.SessionID,
		Cwd:               req.Cwd,
		SystemPrompt:      req.SystemPrompt,
		ProviderSessionID: req.ProviderSessionID,
		UserText:          req.UserText,
		UserBlocks:        userBlocks,
		History:           history,
		Compact:           req.Compact,
		ForkAnchor:        req.ForkAnchor,
		PermissionMode:    req.PermissionMode,
		CollaborationMode: req.CollaborationMode,
		MCPServers:        req.MCPServers,
		EnabledPlugins:    req.EnabledPlugins,
		LLMProviderKey:    req.LLMProviderKey,
	}, nil
}

func encodeHistory(in []agentruntime.HistoryMessage) ([]wire.HistoryMessageWire, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]wire.HistoryMessageWire, 0, len(in))
	for _, m := range in {
		sbs, err := blocks.EncodeAll(m.Blocks)
		if err != nil {
			return nil, fmt.Errorf("encode history blocks: %w", err)
		}
		out = append(out, wire.HistoryMessageWire{Role: m.Role, Blocks: sbs})
	}
	return out, nil
}

func usageFromWire(u *wire.UsageWire) *provider.Usage {
	if u == nil {
		return nil
	}
	return &provider.Usage{
		PromptTokens:        u.PromptTokens,
		CompletionTokens:    u.CompletionTokens,
		ReasoningTokens:     u.ReasoningTokens,
		CachedTokens:        u.CachedTokens,
		CacheCreationTokens: u.CacheCreationTokens,
		TotalTokens:         u.TotalTokens,
	}
}

// ── server-push handlers ───────────────────────────────────────────────────

func (r *Runtime) handleEvent(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.EventFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: event frame unmarshal failed",
			zap.Int("rawBytes", len(raw)), zap.String("errorType", fmt.Sprintf("%T", err)))
		return nil, nil
	}
	r.mu.RLock()
	sess := r.sessions[frame.SessionID]
	knownCount := len(r.sessions)
	r.mu.RUnlock()
	kind := eventKind(frame.Event)
	if sess == nil {
		logger.Ctx(ctx).Warn("remote runtime: event for unknown session dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.Int("knownSessionCount", knownCount),
			zap.String("eventKind", kind))
		return nil, nil
	}
	ev, err := agentruntime.UnmarshalEvent(frame.Event)
	if err != nil {
		logger.Ctx(ctx).Warn("remote runtime: event decode failed and was dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.String("eventKind", kind),
			zap.Int("eventBytes", len(frame.Event)),
			zap.String("errorType", fmt.Sprintf("%T", err)))
		return nil, nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if !sess.started {
		logger.Ctx(ctx).Warn("remote runtime: event before prepared generation start dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.String("eventType", fmt.Sprintf("%T", ev)))
		return nil, nil
	}
	if sess.closed {
		logger.Ctx(ctx).Warn("remote runtime: event after session close dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.String("eventType", fmt.Sprintf("%T", ev)))
		return nil, nil
	}
	logger.Ctx(ctx).Debug("remote runtime: event delivered",
		zap.Int64("sessionId", frame.SessionID),
		zap.String("eventType", fmt.Sprintf("%T", ev)))
	sess.events <- ev
	return nil, nil
}

func (r *Runtime) handleRunResultDone(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.RunResultDoneFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: runResultDone unmarshal failed",
			zap.Int("rawBytes", len(raw)), zap.String("errorType", fmt.Sprintf("%T", err)))
		return nil, nil
	}
	r.mu.RLock()
	sess, ok := r.sessions[frame.SessionID]
	r.mu.RUnlock()
	if !ok {
		logger.Ctx(ctx).Warn("remote runtime: late runResultDone for unknown generation dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.Int("stopErrCode", frame.StopErrCode))
		return nil, nil
	}
	sess.mu.Lock()
	expectedProviderSessionID := sess.providerSessionID
	sess.mu.Unlock()
	if expectedProviderSessionID != "" && frame.ProviderSessionID != "" &&
		frame.ProviderSessionID != expectedProviderSessionID {
		logger.Ctx(ctx).Warn("remote runtime: stale runResultDone generation dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.String("expectedProviderSessionId", expectedProviderSessionID),
			zap.String("frameProviderSessionId", frame.ProviderSessionID),
			zap.Int("stopErrCode", frame.StopErrCode))
		return nil, nil
	}
	r.mu.Lock()
	if r.sessions[frame.SessionID] != sess {
		r.mu.Unlock()
		logger.Ctx(ctx).Warn("remote runtime: replaced runResultDone generation dropped",
			zap.Int64("sessionId", frame.SessionID),
			zap.Int("stopErrCode", frame.StopErrCode))
		return nil, nil
	}
	delete(r.sessions, frame.SessionID)
	r.mu.Unlock()

	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	sess.result.ProviderSessionID = frame.ProviderSessionID
	sess.result.UserAnchor = frame.UserAnchor
	sess.result.Model = frame.Model
	sess.result.ContextWindow = frame.ContextWindow
	if frame.Usage != nil {
		// provider.Usage 没 JSON tag,wire 端用 UsageWire 中转,这里 1:1 拷回。
		sess.result.Usage = usageFromWire(frame.Usage)
	}
	sess.result.StopErr = stopErrFromFrame(frame)
	if !sess.closed {
		sess.closed = true
		close(sess.events)
	}
	sess.mu.Unlock()
	logger.Ctx(ctx).Info("remote runtime: session ended",
		zap.Int64("sessionId", frame.SessionID),
		zap.Int("stopErrCode", frame.StopErrCode),
		zap.String("model", frame.Model))
	return nil, nil
}

func stopErrFromFrame(f wire.RunResultDoneFrame) error {
	if f.StopErrCode == 0 && f.StopErrMsg == "" {
		return nil
	}
	if sent := wire.SentinelFromCode(f.StopErrCode); sent != nil {
		return sent
	}
	return errors.New(f.StopErrMsg)
}

// ── control RPCs ────────────────────────────────────────────────────────────

func (r *Runtime) Steer(ctx context.Context, sessionID int64, queuedID, text string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSentinel(ctx, wire.MethodSteer, wire.SteerParams{
		SessionID: sessionID, QueuedID: queuedID, Text: text,
	}, &wire.OK{})
}

func (r *Runtime) CancelSteer(ctx context.Context, sessionID int64, queuedID string) ([]string, error) {
	if !r.hasSession(sessionID) {
		return nil, agentruntime.ErrNoActiveTurn
	}
	var res wire.CancelSteerResult
	if err := r.callSentinel(ctx, wire.MethodCancelSteer, wire.CancelSteerParams{
		SessionID: sessionID, QueuedID: queuedID,
	}, &res); err != nil {
		return nil, err
	}
	return res.Removed, nil
}

func (r *Runtime) DrainPending(ctx context.Context, sessionID int64) []agentruntime.ConsumedSteer {
	if !r.hasSession(sessionID) {
		return nil
	}
	var res wire.DrainResult
	if err := r.callSentinel(ctx, wire.MethodDrainPending, wire.DrainParams{
		SessionID: sessionID,
	}, &res); err != nil {
		return nil
	}
	return res.Steers
}

func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	r.mu.RLock()
	sess := r.sessions[sessionID]
	r.mu.RUnlock()
	if sess == nil {
		return agentruntime.ErrNoActiveTurn
	}
	if sess.backendType == agent_backend_entity.TypePiAgent {
		return r.abortGeneration(ctx, sess)
	}
	return r.callSentinel(ctx, wire.MethodAbort, wire.AbortParams{SessionID: sessionID}, &wire.OK{})
}

func (r *Runtime) StopBackgroundTask(ctx context.Context, sessionID int64, taskID string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSentinel(ctx, wire.MethodStopBackgroundTask, wire.StopBackgroundTaskParams{
		SessionID: sessionID,
		TaskID:    taskID,
	}, &wire.OK{})
}

func (r *Runtime) SetPermissionMode(ctx context.Context, sessionID int64, mode string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSentinel(ctx, wire.MethodSetPermissionMode, wire.SetPermissionModeParams{
		SessionID: sessionID, Mode: mode,
	}, &wire.OK{})
}

func (r *Runtime) SubmitAnswer(ctx context.Context, sessionID int64, requestID string, questions []agentruntime.AskQuestion, answers []agentruntime.AskAnswer, skipped bool) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSentinel(ctx, wire.MethodSubmitAnswer, wire.SubmitAnswerParams{
		SessionID: sessionID, RequestID: requestID,
		Questions: questions, Answers: answers, Skipped: skipped,
	}, &wire.OK{})
}

func (r *Runtime) SubmitToolPermission(ctx context.Context, sessionID int64, requestID string, allow, alwaysAllowSession bool, denyReason string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSentinel(ctx, wire.MethodSubmitToolPermission, wire.SubmitToolPermissionParams{
		SessionID: sessionID, RequestID: requestID,
		Allow: allow, AlwaysAllowSession: alwaysAllowSession, DenyReason: denyReason,
	}, &wire.OK{})
}

func (r *Runtime) GetGoal(ctx context.Context, req agentruntime.GoalRequest) (*agentruntime.Goal, error) {
	var res wire.GoalResult
	params, err := goalParams(req)
	if err != nil {
		return nil, err
	}
	if err := r.callSentinel(ctx, wire.MethodGetGoal, params, &res); err != nil {
		return nil, err
	}
	return res.Goal, nil
}

func (r *Runtime) SetGoal(ctx context.Context, req agentruntime.GoalRequest) (*agentruntime.Goal, error) {
	var res wire.GoalResult
	params, err := goalParams(req)
	if err != nil {
		return nil, err
	}
	if err := r.callSentinel(ctx, wire.MethodSetGoal, params, &res); err != nil {
		return nil, err
	}
	return res.Goal, nil
}

func (r *Runtime) ClearGoal(ctx context.Context, req agentruntime.GoalRequest) (bool, error) {
	var res wire.GoalClearResult
	params, err := goalParams(req)
	if err != nil {
		return false, err
	}
	if err := r.callSentinel(ctx, wire.MethodClearGoal, params, &res); err != nil {
		return false, err
	}
	return res.Cleared, nil
}

func goalParams(req agentruntime.GoalRequest) (wire.GoalParams, error) {
	var backendJSON json.RawMessage
	if req.Backend != nil {
		raw, err := json.Marshal(req.Backend)
		if err != nil {
			return wire.GoalParams{}, fmt.Errorf("marshal backend: %w", err)
		}
		backendJSON = raw
	}
	return wire.GoalParams{
		SessionID:         req.SessionID,
		AgentID:           req.AgentID,
		ProviderSessionID: req.ProviderSessionID,
		Backend:           backendJSON,
		Cwd:               req.Cwd,
		Objective:         req.Objective,
		Status:            req.Status,
		TokenBudget:       req.TokenBudget,
	}, nil
}

// ── helpers ─────────────────────────────────────────────────────────────────

func eventKind(raw json.RawMessage) string {
	var head struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(raw, &head); err != nil || strings.TrimSpace(head.Kind) == "" {
		return "unknown"
	}
	return head.Kind
}

func (r *Runtime) abortGeneration(ctx context.Context, sess *remoteSession) error {
	if sess == nil {
		return agentruntime.ErrNoActiveTurn
	}
	r.mu.RLock()
	current := r.sessions[sess.id] == sess
	r.mu.RUnlock()
	if !current {
		return nil
	}

	// Cancellation is owner-local and must reach preparation immediately. Only
	// the SessionID-only daemon Abort call waits behind the generation gate.
	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	registrationDone := sess.registrationDone
	sess.mu.Unlock()
	if registrationDone != nil {
		<-registrationDone
	}

	releaseGenerationGate := r.acquireGenerationGate(sess.id)
	defer releaseGenerationGate()
	r.mu.RLock()
	current = r.sessions[sess.id] == sess
	r.mu.RUnlock()
	if !current {
		return nil
	}
	sess.abortOnce.Do(func() {
		base := ctx
		if base == nil {
			base = context.Background()
		}
		abortCtx, cancel := context.WithTimeout(context.WithoutCancel(base), generationControlTimeout)
		defer cancel()
		err := r.callSentinel(abortCtx, wire.MethodAbort, wire.AbortParams{SessionID: sess.id}, &wire.OK{})
		if errors.Is(err, agentruntime.ErrNoActiveTurn) {
			err = nil
		}
		sess.abortErr = err
		r.finishSession(sess, agentruntime.ErrAborted)
	})
	return sess.abortErr
}

func (r *Runtime) finishSession(sess *remoteSession, stopErr error) {
	if sess == nil {
		return
	}
	r.mu.Lock()
	if r.sessions[sess.id] == sess {
		delete(r.sessions, sess.id)
	}
	r.mu.Unlock()
	sess.mu.Lock()
	if sess.cancel != nil {
		sess.cancel()
	}
	if stopErr != nil && sess.result != nil && sess.result.StopErr == nil {
		sess.result.StopErr = stopErr
	}
	if !sess.closed {
		sess.closed = true
		close(sess.events)
	}
	sess.mu.Unlock()
}

func (r *Runtime) acquireGenerationGate(sid int64) func() {
	r.mu.Lock()
	gate := r.generationGates[sid]
	if gate == nil {
		gate = &generationGate{}
		r.generationGates[sid] = gate
	}
	gate.refs++
	r.mu.Unlock()

	gate.mu.Lock()
	return func() {
		gate.mu.Unlock()
		r.mu.Lock()
		gate.refs--
		if gate.refs == 0 && r.generationGates[sid] == gate {
			delete(r.generationGates, sid)
		}
		r.mu.Unlock()
	}
}

func (r *Runtime) hasSession(sid int64) bool {
	r.mu.RLock()
	_, ok := r.sessions[sid]
	r.mu.RUnlock()
	return ok
}

func (r *Runtime) callSentinel(ctx context.Context, method string, params, result any) error {
	if err := r.client.Call(ctx, method, params, result); err != nil {
		return wire.FromJSONRPCError(err)
	}
	return nil
}
