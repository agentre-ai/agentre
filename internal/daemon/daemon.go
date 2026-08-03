package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	dbpkg "github.com/cago-frame/cago/database/db"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	daemonmigrations "github.com/agentre-ai/agentre/internal/daemon/migrations"
	"github.com/agentre-ai/agentre/internal/daemon/notifier"
	"github.com/agentre-ai/agentre/internal/daemon/pairing"
	"github.com/agentre-ai/agentre/internal/daemon/remotefs"
	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/daemon/sessions"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/ccoauth"
	"github.com/agentre-ai/agentre/internal/pkg/httpgateway"
	"github.com/agentre-ai/agentre/internal/pkg/pty"
	"github.com/agentre-ai/agentre/internal/pkg/pty/local"
)

// dbFileName 是 agentred 自己的 SQLite 库文件名,位于 Options.DataDir(=
// paths.AgentredDataDir())根目录。与桌面端的 agentre.db 各自独立、互不引用
// (见 internal/daemon/migrations 包注释)。
const dbFileName = "agentred.db"

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

	// db is agentred's own SQLite handle (session durability: daemon_sessions
	// + daemon_notification_logs — see internal/daemon/migrations). Deliberately
	// a per-instance field, never a package-level global set via db.SetDefault:
	// integration_test.go constructs several Daemon values in one test process,
	// and a global would make them silently share one database. Callers reach
	// it through ctx (db.WithContextDB at the Run ctx boundary), never directly.
	db *gorm.DB

	// journal 是通知日志的写入口,Daemon 级一份(会话日志按 (对端, 会话) 分区,不随
	// 连接生灭 —— 断连重连不重置任何序号)。
	journal handlers.JournalPort

	mu  sync.RWMutex
	lan *rpc.LANServer

	// conns 是 daemon 的推送路由表:会话通知按**会话**解析到发起它的那条连接,
	// MCP 反向隧道从同一份状态里解析目标,daemon 上没有第二个「当前连接」的全局。
	conns connRegistry
}

// sessionKey 是 daemon 侧的会话身份(R16):(对端设备指纹, 对端会话 id)。会话 id 是
// 各客户端本地自增的,两个对端各自持有同一个 id 时是两条互不相干的会话。
type sessionKey struct {
	peer string
	sid  int64
}

// connRegistry 记录 daemon 此刻能把通知推给谁,两张互补的表:
//   - live:已认证且还活着的连接。登记只发生在 auth.pair / auth.connect 成功那一刻
//     (bindConn 是 LANServer 的 OnConn 回调,跑在鉴权**之前**),所以完成 WS 升级却
//     从不认证的连接(LAN 扫描器 / 鉴权失败的客户端 / 掉队的重连)根本进不来;
//   - claims:每条会话的推送目标 —— **发起该会话的那条连接**。
//
// 为什么推送目标不能按设备指纹解析:一台桌面端会同时开 2-3 条**同指纹**的已认证连接
// —— 连接池那条承载会话(chat_svc 的 remote.New(lease.Client())),设备监视心跳与刷新
// 探测各占一条。按指纹索引时,后认证的心跳连接会抢走正在跑的会话的通知(daemon 侧推送
// 「成功」,而发起会话的那条一条也收不到,没有错误也没有 seq 跳号),它关闭时还会把正在
// 用的那条一并删掉(此后只落库不推送)。两种表现都是整轮无限期卡住,且不需要任何异常连接。
//
// 接管规则:一条会话的推送目标是最近为它发过 runtime.* 的那条连接,起点是创建它的
// runtime.run(见 trackSessionOwner)。key 里的指纹取自发起调用的那条连接自己的
// rpc.AuthState,所以只有**同指纹**的连接接得走一条会话 —— 指纹是接管的授权,不是路由
// 键。属主连接断开 → 该会话的登记随之撤销 → 通知照常落库、不推送(会话挂起,R2),
// 等同指纹的新连接再发一次 runtime.* 接管(与重连补齐是同一件事)。
//
// 不变量:claims 里的连接必然同时在 live 里(remove 在同一把锁下一起清)。
//
// 本轮规格的非目标仍排除「多客户端同时接入同一台 daemon」:这两张表不做会话共享、
// 不向多个客户端扇出,每条会话在任一时刻只有一个推送目标。
type connRegistry struct {
	mu     sync.Mutex
	seq    uint64
	live   map[*rpc.Conn]liveConn
	claims map[sessionKey]sessionClaim
}

// liveConn 是一条已认证活连接的推送端口 + 它完成鉴权的次序(隧道回落用)。
type liveConn struct {
	n  handlers.NotifierPort
	at uint64
}

// sessionClaim 记住会话此刻的属主连接本身:撤销要按**连接身份**做,不能按指纹 ——
// 同一台设备的另一条连接来去,不得影响正在跑的会话。
type sessionClaim struct {
	conn *rpc.Conn
	at   uint64
}

// peerIdentity 取连接的对端身份(设备指纹)。身份是鉴权成功那一刻才成立的,报了指纹
// 没通过鉴权不算;空指纹不构成可匹配身份(否则一条空指纹连接就能冒领会话的通知),
// 空指纹在 registerMethods 的 auth.* 入参处就已挡下,这里是第二道。
func peerIdentity(c *rpc.Conn) (string, bool) {
	if c == nil {
		return "", false
	}
	auth := c.Auth()
	if !auth.Authenticated || auth.DeviceFingerprint == "" {
		return "", false
	}
	return auth.DeviceFingerprint, true
}

// connClosed 报告连接是否已经关闭。登记前必须查一次:Done 监视 goroutine 与登记是并发
// 的,先关后登记会在表里留下一条指向死连接的陈旧条目,永远等不到清理。
func connClosed(c *rpc.Conn) bool {
	select {
	case <-c.Done():
		return true
	default:
		return false
	}
}

// add 在连接完成 auth.pair / auth.connect 之后登记它。同一条连接改认另一个指纹时,
// 它先前以旧指纹认领的会话一并作废 —— 否则旧对端的会话通知会推给一条已经属于别人的连接。
func (r *connRegistry) add(c *rpc.Conn, n handlers.NotifierPort) {
	if n == nil {
		return
	}
	if _, ok := peerIdentity(c); !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropLocked(c) // 重新鉴权 = 重置这条连接的会话认领(它可以再发 runtime.* 认回来)
	if connClosed(c) {
		return
	}
	if r.live == nil {
		r.live = map[*rpc.Conn]liveConn{}
	}
	r.seq++
	r.live[c] = liveConn{n: n, at: r.seq}
}

// claim 把会话的推送目标记成这条连接(发起 / 接管该会话的那条)。只有已登记的活连接
// 认得了,且认领的必然是它自己指纹下的那条会话 —— 跨对端接管在 key 上就不成立。
func (r *connRegistry) claim(c *rpc.Conn, sid int64) {
	peer, ok := peerIdentity(c)
	if !ok || sid == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, live := r.live[c]; !live {
		return
	}
	if connClosed(c) {
		r.dropLocked(c)
		return
	}
	if r.claims == nil {
		r.claims = map[sessionKey]sessionClaim{}
	}
	r.seq++
	r.claims[sessionKey{peer: peer, sid: sid}] = sessionClaim{conn: c, at: r.seq}
}

// remove 撤销这条连接的一切登记(连接关闭时调用)。按连接身份撤销:同一台设备的其它
// 连接、以及重连后的新连接,都不受这条迟到的清理影响。它认领过的会话就此挂起 ——
// 通知继续落库、不推送,等同指纹的新连接接管。
func (r *connRegistry) remove(c *rpc.Conn) {
	if c == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.dropLocked(c)
}

func (r *connRegistry) dropLocked(c *rpc.Conn) {
	delete(r.live, c)
	for k, cl := range r.claims {
		if cl.conn == c {
			delete(r.claims, k)
		}
	}
}

// ownerOf 返回该会话此刻的推送端口,没有活属主时 nil。
func (r *connRegistry) ownerOf(k sessionKey) handlers.NotifierPort {
	r.mu.Lock()
	defer r.mu.Unlock()
	cl, ok := r.claims[k]
	if !ok {
		return nil
	}
	return r.live[cl.conn].n
}

// routerFor 返回该对端的会话通知出口;该对端此刻一条持有会话的活连接都没有时返回 nil
// —— 调用方(handlers 的 sessionEmitter)据此走「只落库、不推送」的挂起路径。
func (r *connRegistry) routerFor(peer string) handlers.NotifierPort {
	if peer == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.claims {
		if k.peer == peer {
			return sessionRouter{reg: r, peer: peer}
		}
	}
	return nil
}

// tunnelTarget 解析 MCP 反向隧道的目标。隧道请求来自 daemon 本机的 CLI 子进程(是 HTTP
// 不是 RPC),身上只有 desktop 签的不透明 MCP token,没有会话标识,所以这里按「最近被
// 认领的那条会话的属主连接」定目标 —— 跑着会话的那台设备就是这些子进程的发起端。
// 一条会话都还没被认领时(daemon 刚起、或客户端刚重连还没发 runtime.*)回落到最近完成
// 鉴权的活连接:desktop 侧的隧道 handler 是无状态的(把请求重放到它本机 gateway),
// 同一台设备的哪条连接送达都等价。表空 → nil(无目标的应答由 handlers 决定)。
func (r *connRegistry) tunnelTarget() handlers.NotifierPort {
	r.mu.Lock()
	defer r.mu.Unlock()
	var owner sessionClaim
	for _, cl := range r.claims {
		if cl.at > owner.at {
			owner = cl
		}
	}
	if owner.conn != nil {
		return r.live[owner.conn].n
	}
	var newest liveConn
	for _, lc := range r.live {
		if lc.at > newest.at {
			newest = lc
		}
	}
	return newest.n
}

// sessionRouter 是某个对端的会话通知出口:按帧上的 sessionId 把每条通知交给发起该会话
// 的那条连接。sessionId 本来就是通知帧上的会话路由字段(见 wire 包注释),daemon 侧按它
// 解析没有引入任何协议内容。
type sessionRouter struct {
	reg  *connRegistry
	peer string
}

func (s sessionRouter) Notify(method string, params any) error {
	sid, ok := frameSessionID(params)
	if !ok {
		return fmt.Errorf("daemon: cannot route %s: frame %T carries no sessionId", method, params)
	}
	n := s.reg.ownerOf(sessionKey{peer: s.peer, sid: sid})
	if n == nil {
		// 发起该会话的连接已经断开:通知已经落库,等同指纹的新连接接管后补齐。
		return fmt.Errorf("daemon: session %d has no live connection", sid)
	}
	return n.Notify(method, params)
}

func (s sessionRouter) Request(context.Context, string, any, any) error {
	// 反向请求(MCP 隧道)不经这里:它的请求身上没有会话标识,由 tunnelTarget 解析。
	return errors.New("daemon: reverse requests are not routable per session")
}

// frameSessionID 取通知帧上的会话 id。R1 的五类会话通知共用三种帧,每种都以 sessionId
// 做会话路由。新增一类通知帧时必须在这里补一行 —— 漏了会以 error 形式在日志里点名具体
// 类型,而不是静默推给别的连接。
func frameSessionID(params any) (int64, bool) {
	switch f := params.(type) {
	case *wire.EventFrame:
		return f.SessionID, true
	case *wire.RunResultDoneFrame:
		return f.SessionID, true
	case *wire.AutonomousTurnStartedFrame:
		return f.SessionID, true
	}
	return 0, false
}

// notifierForPeer 解析某个对端的推送出口。每次发送时重新解析,绝不静态捕获 —— 断连
// 重连会换一条连接,捕获下来的端口在重连后指向死连接。
func (d *Daemon) notifierForPeer(peer string) handlers.NotifierPort {
	return d.conns.routerFor(peer)
}

// tunnelTarget 解析 MCP 反向隧道的目标(见 connRegistry.tunnelTarget)。
func (d *Daemon) tunnelTarget() handlers.NotifierPort {
	return d.conns.tunnelTarget()
}

// New constructs a Daemon from Options. It loads persistent state, creates
// sub-systems, and registers all static (non-per-conn) RPC methods.
func New(opts Options) (*Daemon, error) {
	st, err := state.Load(opts.DataDir)
	if err != nil {
		return nil, err
	}
	gormDB, err := openDB(opts.DataDir)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	if err := daemonmigrations.RunMigrations(gormDB); err != nil {
		closeDB(gormDB)
		return nil, fmt.Errorf("run migrations: %w", err)
	}
	// 注入仓储默认实现,让 notification_repo.Notification() 拿到 GORM 版。New 是
	// agentred 的组装根,位置对应桌面端 internal/bootstrap/cago.go 里 RunMigrations
	// 之后的那批 RegisterXxx。实现本身无状态(句柄经 ctx 传),同进程多个 Daemon
	// 注册同一个实现互不干扰。
	notification_repo.RegisterNotification(notification_repo.NewNotification())
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
		opts: opts, state: st, db: gormDB,
		journal:  notificationJournal{db: gormDB},
		sessions: sessions.NewRegistry(), pairing: pm, ratelim: rl,
		registry: reg, auth: auth,
	}
	d.gateway = httpgateway.New("127.0.0.1", 0, NewProviderLookup(st))
	// 内置工具 MCP(org/subagent/group/workflow)隧道:daemon 上 CLI 子进程把请求打到
	// 本机 gateway 的 /mcp/*(URL 已由 runtime.Run 改写成 daemon base),这里捕获后反向
	// 请求回 desktop 执行(真 handler 在 desktop)。仅一条 catch-all,serveMCP 最长前缀
	// 匹配下命中所有 /mcp/* 路径。
	d.gateway.RegisterMCP(httpgateway.RouteMCPPrefix, handlers.NewMCPTunnelHandler(d.tunnelTarget))
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
		// 空指纹不是可匹配身份:rpc/auth.go 的 HandlePair 不拒绝它,配对下来会在
		// PairedPeers 里留一条空键的对端,之后任何 auth.connect 都能顶着空指纹认证成功。
		if pp.DeviceFingerprint == "" {
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
			d.conns.add(c, notifier.New(c))
		}
		return res, nil
	})
	d.registry.Register("auth.connect", func(ctx context.Context, p json.RawMessage) (any, error) {
		var cp rpc.ConnectParams
		if err := jsonUnmarshal(p, &cp); err != nil {
			return nil, rpc.ErrInvalidParams
		}
		if cp.DeviceFingerprint == "" { // 同 auth.pair:空指纹不是可匹配身份
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
			d.conns.add(c, notifier.New(c))
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
	// Inject this Daemon's own db handle at the ctx boundary (not db.SetDefault
	// — see the db field's doc comment). Everything downstream derives its ctx
	// from this one (gateway requests, IPC, per-connection RPC handlers), so a
	// repository calling db.Ctx(ctx) anywhere below this point resolves to this
	// instance's database, never another Daemon's.
	ctx = dbpkg.WithContextDB(ctx, d.db)
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
// 挂载 runtime.* 13 个 RPC 到共享 registry。RuntimeHandlers 持有 per-session backend
// type cache,所以是 per-conn 构造的;会话通知**不**推回「这条」连接 —— 它在发送那一刻
// 按会话解析属主连接(见 notifierForPeer / connRegistry),因为 fanout goroutine 会活过
// 这条连接。
//
// bindConn 跑在鉴权**之前**(它是 OnConn 回调,auth.pair / auth.connect 是之后才到的
// RPC),所以这里**不**把连接登记成推送目标 —— 登记只发生在鉴权成功那一刻,会话认领
// 更要等到它真为某条会话发 runtime.*,否则一条从不认证的连接就能顶掉正主。
func (d *Daemon) bindConn(c *rpc.Conn) {
	n := notifier.New(c)
	rh := handlers.NewRuntimeHandlers(handlers.RuntimeDeps{
		// 会话通知的推送目标在发送那一刻按会话解析,不捕获 n:RuntimeHandlers 是
		// per-conn 的,而它起的 fanout goroutine 会活过这条连接。
		NotifyFor: d.notifierForPeer,
		Journal:   d.journal,
		Gateway:   d.gateway,
		Lookup:    NewProviderLookup(d.state),
	})
	// runtime.* 全族都过 trackSessionOwner:哪条连接为某会话发了 runtime.*,该会话的
	// 通知此后就推给它(见 connRegistry 的接管规则)。
	regRuntime := func(method string, h rpc.HandlerFunc) {
		d.registry.Register(method, d.trackSessionOwner(h))
	}
	regRuntime(wire.MethodCapabilities, wrapGuarded(rh.Capabilities))
	regRuntime(wire.MethodRun, wrapGuarded(rh.Run))
	regRuntime(wire.MethodSteer, wrapGuardedSentinel(rh.Steer))
	regRuntime(wire.MethodCancelSteer, wrapGuardedSentinel(rh.CancelSteer))
	regRuntime(wire.MethodDrainPending, wrapGuarded(rh.DrainPending))
	regRuntime(wire.MethodAbort, wrapGuardedSentinel(rh.Abort))
	regRuntime(wire.MethodStopBackgroundTask, wrapGuardedSentinel(rh.StopBackgroundTask))
	regRuntime(wire.MethodSetPermissionMode, wrapGuardedSentinel(rh.SetPermissionMode))
	regRuntime(wire.MethodSubmitAnswer, wrapGuardedSentinel(rh.SubmitAnswer))
	regRuntime(wire.MethodSubmitToolPermission, wrapGuardedSentinel(rh.SubmitToolPermission))
	regRuntime(wire.MethodGetGoal, wrapGuardedSentinel(rh.GetGoal))
	regRuntime(wire.MethodSetGoal, wrapGuardedSentinel(rh.SetGoal))
	regRuntime(wire.MethodClearGoal, wrapGuardedSentinel(rh.ClearGoal))

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
		// 连接断开 → 撤销它的登记与会话认领(避免对死连接推通知 / 发反向请求)。
		// 按连接身份撤销:同一台设备的其它连接、以及重连后的新连接都不受影响。
		d.conns.remove(c)
	}()
}

// trackSessionOwner 包住 runtime.* 的注册:调用方是哪条连接,该会话此后的通知就推给
// 哪条(接管规则见 connRegistry)。会话 id 直接读参数上的 sessionId —— 13 个 runtime.*
// 参数里除 capabilities 外都带它,没带的自然不认领。
//
// 必须在 handler **之前**认领:runtime.run 一返回,fanout goroutine 就开始推事件,
// 记晚了首批事件会被当成「没有活属主」只落库,而客户端此刻还没有补齐能力。
func (d *Daemon) trackSessionOwner(next rpc.HandlerFunc) rpc.HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (any, error) {
		var p struct {
			SessionID int64 `json:"sessionId"`
		}
		if err := jsonUnmarshal(raw, &p); err == nil {
			d.conns.claim(rpc.ConnFromContext(ctx), p.SessionID)
		}
		return next(ctx, raw)
	}
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

// openDB opens agentred's own SQLite handle at <dataDir>/agentred.db (state.Load
// already created dataDir with 0700 before this is called). Deliberately a
// plain gorm.Open — not a cago db.Database() component — because cago's
// database/db package only exports a package-level Default()/SetDefault();
// there is no way to obtain a handle from it without writing that global,
// which this package must not do (see the db field's doc comment).
func openDB(dataDir string) (*gorm.DB, error) {
	dbPath := filepath.Join(dataDir, dbFileName)
	// busy_timeout mirrors internal/bootstrap/cago.go's sqliteDSN: concurrent
	// writers otherwise hit SQLITE_BUSY near-instantly instead of waiting.
	dsn := dbPath + "?_pragma=busy_timeout(5000)"
	return gorm.Open(sqlite.Open(dsn), &gorm.Config{})
}

// notificationJournal 是 handlers.JournalPort 的 daemon 级实现:把「本该发出的通知」
// 写进本实例的 daemon_notification_logs,seq 由仓储在同一条语句里分配。
//
// 它自己往 ctx 上注入本 Daemon 的 db 句柄:通知的生产者是脱离请求 ctx 的 fanout
// goroutine(它可能拿到的只是一个裸 ctx),而 daemon 故意不写 db.SetDefault(同进程
// 多个 Daemon 会互相串库,见 Daemon.db 注释),所以句柄只能从这里给。
type notificationJournal struct{ db *gorm.DB }

func (j notificationJournal) Append(ctx context.Context, peerFingerprint, peerSessionID, method string, payload json.RawMessage) (int64, error) {
	row := &notification_repo.NotificationLog{
		PeerFingerprint: peerFingerprint,
		PeerSessionID:   peerSessionID,
		Method:          method,
		Payload:         string(payload),
	}
	if err := notification_repo.Notification().Append(dbpkg.WithContextDB(ctx, j.db), row); err != nil {
		return 0, err
	}
	return row.Seq, nil
}

// closeDB 关闭 openDB 拿到的句柄。只在 New 的失败路径上用:Daemon 构造失败时若不关,
// 这个 sql.DB 与它的文件句柄会一直挂着(进程内重试构造会每次泄一份)。构造成功后的
// 句柄跟随进程存活,没有单独的关闭时机。
func closeDB(gormDB *gorm.DB) {
	sqlDB, err := gormDB.DB()
	if err != nil {
		return
	}
	_ = sqlDB.Close()
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
