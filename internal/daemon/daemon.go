package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/notifier"
	"github.com/agentre-ai/agentre/internal/daemon/pairing"
	"github.com/agentre-ai/agentre/internal/daemon/remotefs"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/daemon/sessions"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/ccoauth"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"
)

// Options configures the Daemon at construction time.
type Options struct {
	DataDir     string
	LANHost     string
	LANPort     int
	TLSCertFile string
	TLSKeyFile  string

	// CCUsageFetcher 注入 claudecode.usage handler 用的 OAuth 拉取函数。
	// 留空 → 走 ccoauth.NewLocalFetcher()(从当前机器环境读 token + 调真实 endpoint);
	// 集成测试传入 stub 屏蔽真实网络 / 真实 keychain。
	CCUsageFetcher handlers.CCUsageFetcher
}

// Daemon assembles and runs all agentred sub-systems.
type Daemon struct {
	opts     Options
	state    *state.State
	gateway  *httpgateway.Gateway
	sessions *sessions.Registry
	pairing  *pairing.Manager
	ratelim  *pairing.RateLimiter
	registry *rpc.Registry
	auth     *rpc.AuthHandlers

	mu  sync.RWMutex
	lan *rpc.LANServer

	// mcpNotifier 是当前活跃连接的反向请求端口,供 daemon 本机 gateway 的 /mcp/ 隧道
	// 把 CLI 子进程的内置工具 MCP 调用反向请求回 desktop。daemon 单客户端 MVP(见
	// bindConn 注释):同一时刻一条连接,后连接覆盖、断开清空。
	mcpNotifierMu sync.Mutex
	mcpNotifier   handlers.NotifierPort
}

// setActiveNotifier / activeNotifier 维护当前连接的反向请求端口(MCP 隧道用)。
func (d *Daemon) setActiveNotifier(n handlers.NotifierPort) {
	d.mcpNotifierMu.Lock()
	d.mcpNotifier = n
	d.mcpNotifierMu.Unlock()
}

func (d *Daemon) clearActiveNotifier(n handlers.NotifierPort) {
	d.mcpNotifierMu.Lock()
	if d.mcpNotifier == n { // 仅在仍是自己时清,避免误清后到的新连接
		d.mcpNotifier = nil
	}
	d.mcpNotifierMu.Unlock()
}

func (d *Daemon) activeNotifier() handlers.NotifierPort {
	d.mcpNotifierMu.Lock()
	defer d.mcpNotifierMu.Unlock()
	return d.mcpNotifier
}

// New constructs a Daemon from Options. It loads persistent state, creates
// sub-systems, and registers all static (non-per-conn) RPC methods.
func New(opts Options) (*Daemon, error) {
	st, err := state.Load(opts.DataDir)
	if err != nil {
		return nil, err
	}
	reg := rpc.NewRegistry()

	pmOpts := pairing.ManagerOpts{TTL: 5 * time.Minute}
	if st.Preferences.PairingCodeTTLSeconds > 0 {
		pmOpts.TTL = time.Duration(st.Preferences.PairingCodeTTLSeconds) * time.Second
	}
	pm := pairing.NewManager(pmOpts)
	rlOpts := pairing.RateLimitOpts{MaxAttempts: 3, Window: 60 * time.Second}
	if st.Preferences.PairingRateLimit.MaxAttemptsPerIP > 0 {
		rlOpts.MaxAttempts = st.Preferences.PairingRateLimit.MaxAttemptsPerIP
	}
	if st.Preferences.PairingRateLimit.WindowSeconds > 0 {
		rlOpts.Window = time.Duration(st.Preferences.PairingRateLimit.WindowSeconds) * time.Second
	}
	rl := pairing.NewRateLimiter(rlOpts)
	auth := rpc.NewAuthHandlers(st, pm, rl)

	d := &Daemon{
		opts: opts, state: st,
		sessions: sessions.NewRegistry(), pairing: pm, ratelim: rl,
		registry: reg, auth: auth,
	}
	d.gateway = httpgateway.New("127.0.0.1", 0, NewProviderLookup(st))
	// 内置工具 MCP(org/subagent/group/workflow)隧道:daemon 上 CLI 子进程把请求打到
	// 本机 gateway 的 /mcp/*(URL 已由 runtime.Run 改写成 daemon base),这里捕获后反向
	// 请求回 desktop 执行(真 handler 在 desktop)。仅一条 catch-all,serveMCP 最长前缀
	// 匹配下命中所有 /mcp/* 路径。
	d.gateway.RegisterMCP(httpgateway.RouteMCPPrefix, handlers.NewMCPTunnelHandler(d.activeNotifier))
	d.registerMethods()
	return d, nil
}

// requireAuth returns ErrUnauthorized when the calling connection has not
// completed auth.pair / auth.connect. Called by every non-auth handler.
func requireAuth(ctx context.Context) error {
	c := rpc.ConnFromContext(ctx)
	if c == nil || !c.Auth().Authenticated {
		return rpc.ErrUnauthorized
	}
	return nil
}

// registerMethods installs all static (non-per-connection) RPC handlers.
func (d *Daemon) registerMethods() {
	d.registry.Register("auth.pair", func(ctx context.Context, p json.RawMessage) (any, error) {
		var pp rpc.PairParams
		if err := jsonUnmarshal(p, &pp); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		ip := ipFromContext(ctx)
		res, err := d.auth.HandlePair(ctx, ip, pp)
		if err != nil {
			return nil, err
		}
		if c := rpc.ConnFromContext(ctx); c != nil {
			c.SetAuth(rpc.AuthState{
				Authenticated:     true,
				DeviceFingerprint: pp.DeviceFingerprint,
				DeviceName:        pp.DeviceName,
			})
		}
		return res, nil
	})
	d.registry.Register("auth.connect", func(ctx context.Context, p json.RawMessage) (any, error) {
		var cp rpc.ConnectParams
		if err := jsonUnmarshal(p, &cp); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		res, err := d.auth.HandleConnect(ctx, cp)
		if err != nil {
			return nil, err
		}
		if c := rpc.ConnFromContext(ctx); c != nil {
			c.SetAuth(rpc.AuthState{
				Authenticated:     true,
				DeviceFingerprint: cp.DeviceFingerprint,
			})
		}
		return res, nil
	})
	d.registry.Register("auth.revoke", wrapGuarded(func(ctx context.Context, params struct {
		DeviceFingerprint string `json:"deviceFingerprint"`
	}) (handlers.OK, error) {
		if err := d.auth.HandleRevoke(ctx, params.DeviceFingerprint); err != nil {
			return handlers.OK{}, err
		}
		return handlers.OK{OK: true}, nil
	}))

	llmH := handlers.NewLLMHandlers(d.state)
	d.registry.Register("llm.upsert", wrapGuarded(llmH.Upsert))
	d.registry.Register("llm.delete", wrapGuarded(llmH.Delete))
	d.registry.Register("llm.list", wrapGuardedNoParams(llmH.List))

	sessH := handlers.NewSessionHandlers(d.sessions)
	d.registry.Register("session.list", wrapGuardedNoParams(sessH.List))
	d.registry.Register("session.get", wrapGuarded(sessH.Get))

	cliH := handlers.NewCLIHandlers(d.gateway, NewProviderLookup(d.state))
	d.registry.Register("cli.resolvePath", wrapGuarded(cliH.ResolvePath))
	d.registry.Register("cli.probe", wrapGuarded(cliH.Probe))

	healthH := handlers.NewHealthHandlers(d.state.InstanceUUID(), d.state)
	d.registry.Register("health.ping", wrapGuardedNoParams(healthH.Ping))

	// claudecode.usage:agentred 在它自己所在机器上读 Claude Code 的 OAuth 凭证
	// 并调 api.anthropic.com/api/oauth/usage,返回 5h/7d 配额给桌面 HUD。每台
	// device 的配额是该机器登录账号的,所以必须就地读不能由桌面代理。
	ccFetcher := d.opts.CCUsageFetcher
	if ccFetcher == nil {
		ccFetcher = ccoauth.NewLocalFetcher()
	}
	ccUsageH := handlers.NewCCUsageHandlers(ccFetcher)
	d.registry.Register("claudecode.usage", wrapGuardedNoParams(ccUsageH.Get))

	// skills.list:在 daemon 本机枚举已装技能包(= `claude plugin list --json`),供
	// desktop 给远端 agent 配 per-agent 技能时展 daemon 真实可用集(而非 desktop 的)。
	skillsH := handlers.NewSkillsHandlers()
	d.registry.Register("skills.list", wrapGuarded(skillsH.List))

	// runtime.* RPC 族 1:1 镜像 agentruntime.Runtime + 7 个可选子接口,
	// 把远端 agentre 当成「本地」backend 跑。Handler 在 bindConn
	// 里按连接挂载（要 NotifierPort）。MVP 单客户端假设下 registry 是全局,
	// 多客户端时切 per-Conn registry。

	// remotefs.Register 接受已构造好的 rpc.HandlerFunc,泛型 wrapGuarded[Req,Res] 的
	// 签名约束与其不匹配,改用 WrapFunc 闭包注入 requireAuth。
	remotefs.Register(d.registry, remotefs.NewHandlers(remotefs.Options{}),
		func(fn rpc.HandlerFunc) rpc.HandlerFunc {
			return func(ctx context.Context, raw json.RawMessage) (any, error) {
				if err := requireAuth(ctx); err != nil {
					return nil, err
				}
				return fn(ctx, raw)
			}
		})
}

// Run starts the HTTP gateway, IPC unix socket, and LAN WebSocket server,
// blocking until ctx is canceled or a fatal error occurs.
func (d *Daemon) Run(ctx context.Context) error {
	if err := d.gateway.Start(ctx); err != nil {
		return err
	}
	if d.gateway.URL() == "" {
		// Gateway bind failed; keep going — CLI-login backends can still
		// operate without a gateway token. Structured logging is not wired
		// at daemon level yet (T18 MVP); stderr is loud enough for ops.
		fmt.Fprintln(os.Stderr, "agentred: gateway not running; providers without token will attempt CLI login")
	}
	if _, err := d.startIPC(ctx); err != nil {
		return fmt.Errorf("ipc: %w", err)
	}
	lan := rpc.NewLANServer(rpc.LANOpts{
		Host:        d.opts.LANHost,
		Port:        d.opts.LANPort,
		TLSCertFile: d.opts.TLSCertFile,
		TLSKeyFile:  d.opts.TLSKeyFile,
		Registry:    d.registry,
		OnConn:      d.bindConn,
	})
	d.mu.Lock()
	d.lan = lan
	d.mu.Unlock()
	return lan.Run(ctx)
}

// bindConn is called by LANServer once per accepted WebSocket connection.
// 挂载 runtime.* 9 个 RPC（capabilities / run / steer / cancelSteer /
// drainPending / abort / setPermissionMode / submitAnswer /
// submitToolPermission）到共享 registry。RuntimeHandlers 自己持有 NotifierPort
// 把 events / runResultDone 推回到这条连接,以及 per-session backend type
// cache,所以是 per-conn 构造的。
func (d *Daemon) bindConn(c *rpc.Conn) {
	n := notifier.New(c)
	// 把这条连接的反向请求端口登记为当前活跃端口,供 /mcp/ 隧道用(单客户端 MVP)。
	d.setActiveNotifier(n)
	rh := handlers.NewRuntimeHandlers(handlers.RuntimeDeps{
		Notify:  n,
		Gateway: d.gateway,
		Lookup:  NewProviderLookup(d.state),
	})
	d.registry.Register(wire.MethodCapabilities, wrapGuarded(rh.Capabilities))
	d.registry.Register(wire.MethodRun, wrapGuarded(rh.Run))
	d.registry.Register(wire.MethodSteer, wrapGuardedSentinel(rh.Steer))
	d.registry.Register(wire.MethodCancelSteer, wrapGuardedSentinel(rh.CancelSteer))
	d.registry.Register(wire.MethodDrainPending, wrapGuarded(rh.DrainPending))
	d.registry.Register(wire.MethodAbort, wrapGuardedSentinel(rh.Abort))
	d.registry.Register(wire.MethodSetPermissionMode, wrapGuardedSentinel(rh.SetPermissionMode))
	d.registry.Register(wire.MethodSubmitAnswer, wrapGuardedSentinel(rh.SubmitAnswer))
	d.registry.Register(wire.MethodSubmitToolPermission, wrapGuardedSentinel(rh.SubmitToolPermission))
	d.registry.Register(wire.MethodGetGoal, wrapGuardedSentinel(rh.GetGoal))
	d.registry.Register(wire.MethodSetGoal, wrapGuardedSentinel(rh.SetGoal))
	d.registry.Register(wire.MethodClearGoal, wrapGuardedSentinel(rh.ClearGoal))

	// Terminal: local PTY backend; per-conn emitter pushes terminal.data /
	// terminal.exit events back over this ws connection (same per-conn rationale
	// as runtime.* above — events are scoped to whoever opened the terminal).
	termBackend := localPTYBackendAdapter{be: local.NewBackend()}
	termEmitter := handlers.EmitterFunc(func(_ context.Context, name string, payload any) {
		_ = n.Notify(name, payload)
	})
	termH := handlers.NewTerminalHandlers(termBackend, termEmitter)
	d.registry.Register("terminal.open", wrapGuarded(termH.Open))
	d.registry.Register("terminal.write", wrapGuarded(termH.Write))
	d.registry.Register("terminal.resize", wrapGuarded(termH.Resize))
	d.registry.Register("terminal.close", wrapGuarded(termH.Close))
	// When this connection drops, kill the PTYs it opened — otherwise the
	// remote shells (and whatever they run) leak until daemon shutdown.
	go func() {
		<-c.Done()
		termH.CloseAll()
		d.clearActiveNotifier(n) // 连接断开 → 撤销 MCP 隧道端口(避免对死连接发反向请求)
	}()
}

// wrapGuarded is wrap + requireAuth check. Use for any method except auth.*.
//
// Handler panics 被 recoverHandlerPanic 收住,翻成 ErrInternal 让 daemon 进
// 程继续活着、客户端 chat_svc 得到一条可读错误(走 wire.FromJSONRPCError
// → 触发 StreamError 让前端 UI 解锁"生成中"状态)。历史教训:claudecode
// runtime 在 daemon 进程 nil panic 直接 SIGSEGV 整个 agentred,前端 UI 收不
// 到任何提示,会话永远卡在 generating。
func wrapGuarded[Req any, Res any](fn func(context.Context, Req) (Res, error)) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (res any, err error) {
		defer recoverHandlerPanic(&err)
		if err := requireAuth(ctx); err != nil {
			return nil, err
		}
		var req Req
		if err := jsonUnmarshal(raw, &req); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		return fn(ctx, req)
	}
}

// wrapGuardedSentinel 同 wrapGuarded,但额外把 handler 返回的 agentruntime
// sentinel(ErrNoActiveTurn / ErrSteerNotFound / ErrUnsupported / ErrAborted)
// 翻成稳定 JSON-RPC error code,客户端 wire.FromJSONRPCError 反向 rehydrate
// 让 errors.Is(err, agentruntime.ErrXxx) 跨进程继续工作。
func wrapGuardedSentinel[Req any, Res any](fn func(context.Context, Req) (Res, error)) rpc.HandlerFunc {
	wrapped := wrapGuarded(fn)
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		res, err := wrapped(ctx, raw)
		if err != nil {
			if mapped := wire.ToJSONRPCError(err); mapped != nil {
				return nil, mapped
			}
		}
		return res, err
	}
}

// wrapGuardedNoParams is wrapNoParams + requireAuth check.
func wrapGuardedNoParams[Res any](fn func(context.Context) (Res, error)) rpc.HandlerFunc {
	return func(ctx context.Context, _ json.RawMessage) (res any, err error) {
		defer recoverHandlerPanic(&err)
		if err := requireAuth(ctx); err != nil {
			return nil, err
		}
		return fn(ctx)
	}
}

// recoverHandlerPanic 是 RPC handler 的最后一道防线:把 panic 翻成 ErrInternal
// 让 daemon 进程不挂、客户端收到结构化错误。stack trace 进日志方便事后定位。
// 命名 err 返回值由调用方提供(`err *error`),defer 写回最终返回。
func recoverHandlerPanic(errOut *error) {
	if r := recover(); r != nil {
		stack := debug.Stack()
		log.Printf("daemon rpc handler panic: %v\n%s", r, stack)
		*errOut = &rpc.Error{
			Code:    rpc.ErrInternal.Code,
			Message: fmt.Sprintf("daemon handler panic: %v", r),
		}
	}
}

func jsonUnmarshal(b json.RawMessage, v any) error {
	if len(b) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

type remoteAddrKey struct{}

func ipFromContext(ctx context.Context) string {
	v := ctx.Value(remoteAddrKey{})
	if s, ok := v.(string); ok {
		host, _, err := net.SplitHostPort(s)
		if err == nil {
			return host
		}
		return s
	}
	return ""
}

// localPTYBackendAdapter bridges *local.Backend (returns pty.Handle) to
// handlers.PTYBackend (returns handlers.PTYHandle). The returned pty.Handle
// structurally satisfies handlers.PTYHandle (identical method set).
type localPTYBackendAdapter struct {
	be *local.Backend
}

func (a localPTYBackendAdapter) Open(ctx context.Context, spec pty.Spec) (handlers.PTYHandle, error) {
	return a.be.Open(ctx, spec)
}

var _ handlers.PTYBackend = localPTYBackendAdapter{}
