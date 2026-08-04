// Package remote 是 agentre 桌面端连远端 agentred daemon 的 agent runtime
// 客户端。daemon 端跑真正的 claudecode / codex / builtin runtime,本包通过
// WebSocket + JSON-RPC(runtime.* 命名空间)把整个 agentruntime.Runtime
// 接口 + 7 个可选子接口透明代理过去:
//
//   - Run / Steer / CancelSteer / DrainPending / Abort / SetPermissionMode /
//     SubmitAnswer / SubmitToolPermission → 一行一个 c.Call(runtime.<name>)
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
	"sync"
	"time"

	"github.com/cago-frame/agents/agent/blocks"
	"github.com/cago-frame/agents/provider"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
)

// remoteSession 一个远端 daemon 上跑的 chat session 在本地的镜像。sessionID
// 是 client/daemon 共享的 int64(daemon 侧不再分配额外的 string sid),所以一个 map 就够。
type remoteSession struct {
	id     int64
	events chan agentruntime.Event
	result *agentruntime.RunResult
	// startSeq 是**开轮那一刻** daemon 通知日志里这条会话的高水位:本轮自己的通知
	// 必然都比它新。它是这一轮在 seq 时间线上的位置 —— 而 runtime.run 这条 RPC 本身
	// 不在那条时间线上(RunAck 不带 seq,日志载荷里也没有轮次身份),少了它就分不出
	// 补齐回放上来的一条终态帧到底是谁的。见 handleRunResultDone 与 turnStartFloor。
	//
	// 0 表示未知(没装重连端口 / 老 daemon / 这一次没读到),此时守卫退化成今天的行为。
	// 发布进 r.sessions 之前赋值,之后不再改写;读方一律在 r.mu 下。
	startSeq int64

	mu     sync.Mutex
	closed bool
}

// Runtime 包装 DaemonClientPort 把 chat session 委托给远端 daemon。生命周期:
//   - New(client) 立即向 client 注册两条 server-push handler
//   - Run() 调 runtime.run 注册 session,后续 runtime.event / runtime.runResultDone
//     按 sessionID 路由
//   - Prefetch(ctx, backendType) 主动拉一次 daemon 的 capability 矩阵缓存到本地,
//     之后 Capabilities() 同步返(chat_svc UI gating 依赖它是同步的)
type Runtime struct {
	client agentruntime.DaemonClientPort

	mu       sync.RWMutex
	sessions map[int64]*remoteSession
	caps     map[agent_backend_entity.BackendType]capability.Capabilities
	// autoSessions 是「自主续轮」(AutonomousTurnSource)的会话级镜像,**独立于**
	// per-Run 的 sessions(后者在 runResultDone 时删除,而自主续轮发生在 Run 收尾
	// *之后*)。按 sessionID 持久(跨 turn / 子进程 evict 复用),conn close 时统一拆。
	// 见 autoturn.go。
	autoSessions map[int64]*autoSession
	// tracked 是 App 启动后按 exec_device_id 找回来、要为之补齐的会话(见 catchup.go)。
	// 它们在本进程内**没有**在飞的一轮 —— 那正是重启后的常态 —— 但断连后仍要重连补齐,
	// 所以必须与 sessions / autoSessions 一起进补齐范围。受 mu 保护。
	tracked map[int64]struct{}

	// ── 断连重连(reconnect.go)──
	reconnect    ReconnectPort
	connObserver ConnStateObserver
	cursorPort   agentruntime.SessionCursorPort
	backoff      []time.Duration
	cursorFlush  time.Duration

	// connMu 只保护「当前这条连接」相关的三个字段:client / daemonFP / 能力探测
	// 结果。它与 mu 分开,是因为重连期间要在不持有会话表锁的前提下换连接。
	connMu     sync.Mutex
	daemonFP   string
	durability durabilityState

	// sessionState 是每条会话的补齐状态(游标 + 连接态 + 补洞串行化)。
	// 与 sessions 分开:sessions 只活在一轮之内,游标要跨轮存活。
	stateMu      sync.Mutex
	sessionState map[int64]*sessionSync

	// 游标落库的防抖攒批,见 markCursorDirty。
	cursorMu    sync.Mutex
	cursorDirty map[int64]int64
	cursorTimer *time.Timer

	stopOnce sync.Once
	stopped  chan struct{}
}

// notifyHandler 是一条 daemon → client 通知的处理函数。
type notifyHandler func(*Runtime, context.Context, json.RawMessage) (any, error)

// New 构造一个 remote.Runtime,并把 runtime.event / runtime.runResultDone
// 两个 server-push handler 注册到 client。调用方负责管理 client 的生命周期(通常
// 是 Pool.Lease)。
//
// 额外起一个 goroutine 监 client.Closed():daemon 进程崩溃 / 网络断 / TLS 失
// 败等情况下,在飞的 run session 永远等不到 runResultDone,events channel 不
// 关 → chat_svc.runTurn 卡在 `for ev := range events`,前端会话一直停在「生
// 成中」。
//
// 装了 WithReconnect 时,断连**不**终结会话:会话转入重连态,退避重连并按游标
// 补齐(见 reconnect.go)。没装重连端口、或对面 daemon 不认补齐族 RPC 时,才
// 回落到给所有 live session 注入 ErrDaemonDisconnected 并 close events,
// chat_svc 走 StreamError 解锁前端。
func New(c agentruntime.DaemonClientPort, opts ...Option) *Runtime {
	r := &Runtime{
		client:       c,
		sessions:     map[int64]*remoteSession{},
		caps:         map[agent_backend_entity.BackendType]capability.Capabilities{},
		autoSessions: map[int64]*autoSession{},
		sessionState: map[int64]*sessionSync{},
		backoff:      defaultReconnectBackoff,
		cursorFlush:  defaultCursorFlushInterval,
		stopped:      make(chan struct{}),
	}
	for _, o := range opts {
		o(r)
	}
	r.registerHandlers(c)
	if closed := c.Closed(); closed != nil {
		go r.watchClose(closed)
	}
	return r
}

// registerHandlers 把五类通知 + MCP 反向隧道挂到一条连接上。重连换连接后要原样再挂
// 一遍 —— 新连接自带一张空的 handler 表。
//
// 五类通知统一经 dispatchNotification 入口,补齐重放走的也是它:实时与补齐因此共用
// 同一套 handler,R5 的等价性是结构性成立的,不只是被测试覆盖。
func (r *Runtime) registerHandlers(c agentruntime.DaemonClientPort) {
	for method, h := range notifyHandlers {
		m, fn := method, h
		c.Handle(m, func(ctx context.Context, raw json.RawMessage) (any, error) {
			return r.dispatchNotification(ctx, m, fn, raw)
		})
	}
	// daemon 上的 CLI 子进程访问内置工具 MCP(org/subagent/group/workflow)时,经此反向
	// 请求隧道回 desktop 执行(真 /mcp/* handler 在 desktop)。见 mcpproxy.go。
	c.Handle(wire.MethodMCPProxy, r.handleMCPProxy)
}

// notifyHandlers 是 daemon → client 五类通知的方法名 → handler 映射。补齐重放按
// method 找回同一个 handler,所以这张表必须是**唯一**的注册来源。
var notifyHandlers = map[string]notifyHandler{
	wire.NotifyEvent:                 (*Runtime).handleEvent,
	wire.NotifyRunResultDone:         (*Runtime).handleRunResultDone,
	wire.NotifyAutonomousTurnStarted: (*Runtime).handleAutonomousTurnStarted,
	wire.NotifyAutonomousTurnEvent:   (*Runtime).handleAutonomousTurnEvent,
	wire.NotifyAutonomousTurnDone:    (*Runtime).handleAutonomousTurnDone,
}

// ErrDaemonDisconnected 当远端 daemon 连接断开(进程崩 / 网络断 / 主动 Close)
// 时,remote.Runtime 注入到在飞 session 的 StopErr。chat_svc 拿到后映射为
// StreamError,前端就能解锁「生成中」并显示一条提示。
var ErrDaemonDisconnected = errors.New("agentruntime/runtimes/remote: daemon connection closed")

// ErrRunInterrupted 这一轮在远端**被打断**了:daemon 重启后按 R10 把非终态会话标成
// 中断态(接管回 ErrNoActiveTurn / ErrSessionNotFound),或那台 daemon 的实例标识对不上
// 导致游标失效(R12 判「按已中断处理」)。连接本身是好的,只是这一轮接不回去了。
//
// 它必须与 ErrDaemonDisconnected 分开:R15 规定中断沿用既有的 error 态、**由消息文案
// 区分其与真实错误**,而上层能拿到的唯一依据就是 StopErr。折成同一个哨兵,「被打断」
// 与「连不上了」就是同一句话,用户分不出发生了什么。
var ErrRunInterrupted = errors.New("agentruntime/runtimes/remote: run interrupted by daemon restart")

// Close 关掉与 daemon 的 client 连接,并停掉重连状态机。
func (r *Runtime) Close() error {
	r.stopOnce.Do(func() { close(r.stopped) })
	r.flushCursors()
	c := r.conn()
	if c == nil {
		return nil
	}
	return c.Close()
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
	if err := r.conn().Call(ctx, wire.MethodCapabilities, wire.CapabilitiesParams{
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
	params, err := buildRunParams(req)
	if err != nil {
		return nil, nil, err
	}
	// 开轮前读一眼日志高水位(顺带完成 R18 的能力探测),把这一轮钉在 seq 时间线上:
	// 不比它新的终态帧都属于已经结束的轮次。见 turnStartFloor。
	floor := r.turnStartFloor(ctx, req.SessionID)
	sess := &remoteSession{
		id:       req.SessionID,
		events:   make(chan agentruntime.Event, 64),
		result:   &agentruntime.RunResult{},
		startSeq: floor,
	}
	r.mu.Lock()
	r.sessions[req.SessionID] = sess
	r.mu.Unlock()

	var ack wire.RunAck
	if err := r.conn().Call(ctx, wire.MethodRun, params, &ack); err != nil {
		r.mu.Lock()
		if r.sessions[req.SessionID] == sess {
			delete(r.sessions, req.SessionID)
		}
		r.mu.Unlock()
		sess.mu.Lock()
		if !sess.closed {
			sess.closed = true
			close(sess.events)
		}
		sess.mu.Unlock()
		logger.Ctx(ctx).Error("remote runtime: Run RPC failed",
			zap.Int64("requestedSid", req.SessionID), zap.Error(err))
		return nil, nil, wire.FromJSONRPCError(err)
	}

	sess.mu.Lock()
	sess.id = ack.SessionID
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
			zap.Int("rawBytes", len(raw)), zap.Error(err))
		return nil, nil
	}
	r.mu.RLock()
	sess := r.sessions[frame.SessionID]
	knownSids := make([]int64, 0, len(r.sessions))
	for k := range r.sessions {
		knownSids = append(knownSids, k)
	}
	r.mu.RUnlock()
	ev, err := agentruntime.UnmarshalEvent(frame.Event)
	if err != nil {
		logger.Ctx(ctx).Warn("remote runtime: UnmarshalEvent failed — dropped",
			zap.Int64("sid", frame.SessionID),
			zap.String("event", string(frame.Event)),
			zap.Error(err))
		return nil, nil
	}
	if sess != nil && frame.Seq > 0 && frame.Seq <= sess.startSeq {
		// 补齐回放上来的、属于**已结束轮次**的事件:它在开轮之前就落库了。放进当前
		// 这一轮的 events,上一轮的 TextDelta 会一字不差地追加到用户刚发出的那条消息
		// 的回答里 —— 用户看到的是一段答非所问的历史。它归补齐轮。
		logger.Ctx(ctx).Debug("remote runtime: event from an ended turn — routed to catch-up",
			zap.Int64("sid", frame.SessionID), zap.Int64("seq", frame.Seq),
			zap.Int64("turnStartSeq", sess.startSeq))
		sess = nil
	}
	if sess == nil {
		// 带 seq 的事件必定来自认得补齐族的 daemon:它要么是重放上来的历史,要么是
		// 一条本进程还没有轮次去接的实时通知(App 刚重启)。两种都是用户没见过的
		// 转录内容,交给补齐轮落成一轮,而不是像老 daemon 那样丢掉。
		if frame.Seq > 0 && r.deliverToCatchUpTurn(ctx, frame.SessionID, ev) {
			return nil, nil
		}
		logger.Ctx(ctx).Warn("remote runtime: event for unknown session — dropped",
			zap.Int64("frameSid", frame.SessionID),
			zap.Int64s("knownSids", knownSids),
			zap.String("event", string(frame.Event)))
		return nil, nil
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if sess.closed {
		logger.Ctx(ctx).Warn("remote runtime: event after session close — dropped",
			zap.Int64("sid", frame.SessionID),
			zap.String("eventType", fmt.Sprintf("%T", ev)))
		return nil, nil
	}
	logger.Ctx(ctx).Debug("remote runtime: event delivered",
		zap.Int64("sid", frame.SessionID),
		zap.String("eventType", fmt.Sprintf("%T", ev)))
	sess.events <- ev
	return nil, nil
}

func (r *Runtime) handleRunResultDone(ctx context.Context, raw json.RawMessage) (any, error) {
	var frame wire.RunResultDoneFrame
	if err := json.Unmarshal(raw, &frame); err != nil {
		logger.Ctx(ctx).Warn("remote runtime: runResultDone unmarshal failed", zap.Error(err))
		return nil, nil
	}
	r.mu.Lock()
	sess, ok := r.sessions[frame.SessionID]
	if ok && frame.Seq > 0 && frame.Seq <= sess.startSeq {
		r.mu.Unlock()
		// 补齐回放上来的、属于**已结束轮次**的终态帧:它在开轮之前就落库了,不可能是
		// 这一轮的结果。放它进去会删掉会话表里当前这一轮、用旧结果覆盖它的 RunResult、
		// 并 close 掉它的 events —— 用户刚发出的消息瞬间「结束」并带着上一轮的答案,
		// 其后的实时帧则全部走下面 handleEvent 的「未知会话」被丢弃。
		//
		// 它与同一轮回放上来的事件走同一个去处:补齐轮。事件在 handleEvent 里按同一条
		// 判据分流,这一帧则是那一轮的收尾 —— 两者合起来,回放的每一轮都完整落成一张
		// 自己的卡片,而不是揉进用户刚发起的这一轮里。
		logger.Ctx(ctx).Warn("remote runtime: runResultDone from an ended turn — routed to catch-up",
			zap.Int64("sid", frame.SessionID),
			zap.Int64("seq", frame.Seq),
			zap.Int64("turnStartSeq", sess.startSeq))
		// 它是**那一轮**的收尾:补齐轮攒的正是那一轮的内容,到此为止。
		r.closeCatchUpTurn(ctx, frame)
		return nil, nil
	}
	if ok {
		delete(r.sessions, frame.SessionID)
	}
	r.mu.Unlock()
	logger.Ctx(ctx).Info("remote runtime: session ended",
		zap.Int64("sid", frame.SessionID),
		zap.Bool("sessionFound", ok),
		zap.String("stopErrMsg", frame.StopErrMsg),
		zap.Int("stopErrCode", frame.StopErrCode),
		zap.String("model", frame.Model))
	if !ok {
		// 本进程没有这一轮(App 重启后补齐回放的整轮就长这样):补齐轮攒到这里为止。
		r.closeCatchUpTurn(ctx, frame)
		return nil, nil
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
	sess.mu.Lock()
	if !sess.closed {
		sess.closed = true
		close(sess.events)
	}
	sess.mu.Unlock()
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
	return r.callSession(ctx, sessionID, wire.MethodSteer, wire.SteerParams{
		SessionID: sessionID, QueuedID: queuedID, Text: text,
	}, &wire.OK{})
}

func (r *Runtime) CancelSteer(ctx context.Context, sessionID int64, queuedID string) ([]string, error) {
	if !r.hasSession(sessionID) {
		return nil, agentruntime.ErrNoActiveTurn
	}
	var res wire.CancelSteerResult
	if err := r.callSession(ctx, sessionID, wire.MethodCancelSteer, wire.CancelSteerParams{
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
	if err := r.callSession(ctx, sessionID, wire.MethodDrainPending, wire.DrainParams{
		SessionID: sessionID,
	}, &res); err != nil {
		return nil
	}
	return res.Steers
}

func (r *Runtime) Abort(ctx context.Context, sessionID int64) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSession(ctx, sessionID, wire.MethodAbort, wire.AbortParams{SessionID: sessionID}, &wire.OK{})
}

func (r *Runtime) StopBackgroundTask(ctx context.Context, sessionID int64, taskID string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSession(ctx, sessionID, wire.MethodStopBackgroundTask, wire.StopBackgroundTaskParams{
		SessionID: sessionID,
		TaskID:    taskID,
	}, &wire.OK{})
}

func (r *Runtime) SetPermissionMode(ctx context.Context, sessionID int64, mode string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSession(ctx, sessionID, wire.MethodSetPermissionMode, wire.SetPermissionModeParams{
		SessionID: sessionID, Mode: mode,
	}, &wire.OK{})
}

func (r *Runtime) SubmitAnswer(ctx context.Context, sessionID int64, requestID string, questions []agentruntime.AskQuestion, answers []agentruntime.AskAnswer, skipped bool) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSession(ctx, sessionID, wire.MethodSubmitAnswer, wire.SubmitAnswerParams{
		SessionID: sessionID, RequestID: requestID,
		Questions: questions, Answers: answers, Skipped: skipped,
	}, &wire.OK{})
}

func (r *Runtime) SubmitToolPermission(ctx context.Context, sessionID int64, requestID string, allow, alwaysAllowSession bool, denyReason string) error {
	if !r.hasSession(sessionID) {
		return agentruntime.ErrNoActiveTurn
	}
	return r.callSession(ctx, sessionID, wire.MethodSubmitToolPermission, wire.SubmitToolPermissionParams{
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

func (r *Runtime) hasSession(sid int64) bool {
	r.mu.RLock()
	_, ok := r.sessions[sid]
	r.mu.RUnlock()
	return ok
}

func (r *Runtime) callSentinel(ctx context.Context, method string, params, result any) error {
	if err := r.conn().Call(ctx, method, params, result); err != nil {
		return wire.FromJSONRPCError(err)
	}
	return nil
}

// callSession 是带会话身份的控制类调用。ErrNoActiveTurn 时**重新接管一次再重试**。
//
// 为什么必须重试:daemon 的 runtime.* 是 per-connection 注册进共享 registry 的,
// 每条新连接都会带着一张空的会话表把它们重新注册一遍,把上一次 runtime.session.attach
// 的接管静默还原。而桌面端同时握着 2-3 条同指纹连接(连接池 / 设备心跳 / 刷新探测),
// 只要其中任意一条重连过,接管就没了 —— 此后提交决策会被 daemon 的幂等折叠(R8)
// 折成 OK,会话永久停在等待输入上。所以接管必须是可反复发起的。
func (r *Runtime) callSession(ctx context.Context, sessionID int64, method string, params, result any) error {
	err := r.callSentinel(ctx, method, params, result)
	// canReconnect 同时管住两件事:没装重连端口的调用方(老接线、单测)一律走今天
	// 的路径,不会凭空多出一次 attach;已被证伪的老 daemon 也不再白试。
	if err == nil || !errors.Is(err, agentruntime.ErrNoActiveTurn) || !r.canReconnect() {
		return err
	}
	if _, aerr := r.attachSession(ctx, sessionID); aerr != nil {
		logger.Ctx(ctx).Warn("remote runtime: re-attach before retry failed",
			zap.Int64("sid", sessionID), zap.String("method", method), zap.Error(aerr))
		return err
	}
	logger.Ctx(ctx).Info("remote runtime: re-attached session, retrying control call",
		zap.Int64("sid", sessionID), zap.String("method", method))
	return r.callSentinel(ctx, method, params, result)
}
