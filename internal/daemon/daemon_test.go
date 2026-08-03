package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbpkg "github.com/cago-frame/cago/database/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/notifier"
	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
)

// TestDaemon_OpensOwnDatabaseAndRunsMigrations 覆盖任务目标的第一句:agentred
// 启动时在 DataDir(=AgentredDataDir())下开库并跑自己的迁移。
func TestDaemon_OpensOwnDatabaseAndRunsMigrations(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{DataDir: dir})
	require.NoError(t, err)

	dbPath := filepath.Join(dir, "agentred.db")
	if _, statErr := os.Stat(dbPath); statErr != nil {
		t.Fatalf("sqlite file %s missing: %v", dbPath, statErr)
	}
	require.NotNil(t, d.db)
	assert.True(t, d.db.Migrator().HasTable("daemon_sessions"))
	assert.True(t, d.db.Migrator().HasTable("daemon_notification_logs"))
}

// TestDaemon_NewRegistersNotificationRepo 回归:New 是 agentred 的组装根,必须像
// internal/bootstrap/cago.go 在 RunMigrations 之后注入仓储默认实现那样,把
// notification_repo 的 GORM 实现注册进去。不注册的话 notification_repo.Notification()
// 永远是 nil,后续任务的推送路径一调就 nil panic。
func TestDaemon_NewRegistersNotificationRepo(t *testing.T) {
	prev := notification_repo.Notification()
	t.Cleanup(func() { notification_repo.RegisterNotification(prev) })
	notification_repo.RegisterNotification(nil)

	_, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)

	assert.NotNil(t, notification_repo.Notification(), "New must register the notification repo implementation")
}

// TestDaemon_NewFailsWhenDatabaseUnusable 错误路径:库文件存在但不是合法 SQLite 时,
// New 必须带上下文报错返回,而不是揣着一个跑不了迁移的句柄继续启动。
func TestDaemon_NewFailsWhenDatabaseUnusable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agentred.db"), []byte("not a sqlite file"), 0o600))

	d, err := New(Options{DataDir: dir})
	require.Error(t, err)
	assert.Nil(t, d)
}

// TestDaemon_NotificationJournal_ConcurrentAppendsAreLosslessAndGapFree 覆盖任务目标的
// 「以下一个 seq 落库、seq 单调无洞」在并发写下也成立:同一会话的通知生产者不止一个
// (handlers/runtime.go 的 fanout 与 startAutonomousFanout 是两个各自独立的 goroutine,
// 同一 sid 上可同时推送),先读 MAX(seq) 再写入的两步实现会让两个写者拿到同一个 seq,
// 后写的那条被幂等写静默吞掉——通知永久丢失而调用方以为落库成功。
func TestDaemon_NotificationJournal_ConcurrentAppendsAreLosslessAndGapFree(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()

	const writers = 24
	var wg sync.WaitGroup
	errs := make([]error, writers)
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = repo.Append(ctx, &notification_repo.NotificationLog{
				PeerFingerprint: "peerA", PeerSessionID: "s1",
				Method:  "runtime.event",
				Payload: fmt.Sprintf(`{"n":%d}`, i),
			})
		}()
	}
	wg.Wait()
	for i, appendErr := range errs {
		require.NoError(t, appendErr, "writer %d", i)
	}

	rows, hasMore, err := repo.ListSince(ctx, "peerA", "s1", 0, writers)
	require.NoError(t, err)
	assert.False(t, hasMore)
	require.Len(t, rows, writers, "every appended notification must be in the log — none silently dropped")
	seen := map[string]bool{}
	for i, row := range rows {
		assert.Equal(t, int64(i+1), row.Seq, "seq 必须从 1 起连续无洞、按序读回")
		assert.False(t, seen[row.Payload], "payload %s written twice", row.Payload)
		seen[row.Payload] = true
	}

	// R16:另一个对端持有同名会话 id 时是另一条会话,seq 空间从 1 重新开始。
	other := &notification_repo.NotificationLog{
		PeerFingerprint: "peerB", PeerSessionID: "s1", Method: "runtime.event", Payload: "{}",
	}
	require.NoError(t, repo.Append(ctx, other))
	assert.Equal(t, int64(1), other.Seq, "another peer's same-named session must own a separate seq space")
}

// TestDaemon_NotificationJournal_DuplicateSeqWriteIsIdempotent 覆盖任务目标的
// 「同 (会话, seq) 重复写入幂等」在真库(SQLite,生产方言)上成立:重复写不报错、
// 也不产生第二行,且不覆盖已落库的载荷。
func TestDaemon_NotificationJournal_DuplicateSeqWriteIsIdempotent(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()

	first := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: 1, Method: "runtime.event", Payload: `{"first":true}`,
	}
	require.NoError(t, repo.Create(ctx, first))
	require.NoError(t, repo.Create(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: 1, Method: "runtime.event", Payload: `{"second":true}`,
	}), "重复写同一 (peer, session, seq) 必须成功返回,不是唯一约束报错")

	rows, _, err := repo.ListSince(ctx, "peerA", "s1", 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1, "duplicate write must not create a second row")
	assert.Equal(t, `{"first":true}`, rows[0].Payload, "已落库的事实不被重复写覆盖")
}

// TestDaemon_DatabaseHandlesAreIsolatedPerInstance 回归:两个不同 DataDir 的
// Daemon 各自开的是物理上独立的 SQLite 文件——用两个 Daemon 各自写一条通知,确认
// 经由各自 db.WithContextDB(ctx, d.db) 注入的 ctx 互不可见对方的数据(会捕捉「实际
// 打开的库路径没跟着 DataDir 走」这类 bug,例如两者都落到同一个硬编码/共享路径)。
func TestDaemon_DatabaseHandlesAreIsolatedPerInstance(t *testing.T) {
	d1, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	d2, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)

	require.NotNil(t, d1.db)
	require.NotNil(t, d2.db)
	assert.NotSame(t, d1.db, d2.db, "each Daemon must own its own *gorm.DB handle")

	ctx1 := dbpkg.WithContextDB(context.Background(), d1.db)
	require.NoError(t, notification_repo.NewNotification().Create(ctx1, &notification_repo.NotificationLog{
		PeerFingerprint: "peer", PeerSessionID: "s1", Seq: 1, Method: "m", Payload: "{}",
	}))

	ctx2 := dbpkg.WithContextDB(context.Background(), d2.db)
	rows, _, err := notification_repo.NewNotification().ListSince(ctx2, "peer", "s1", 0, 10)
	require.NoError(t, err)
	assert.Empty(t, rows, "writing through d1's handle must not be visible through d2's handle")
}

// TestDaemon_NewDoesNotLeakIntoGlobalDefaultDB 回归:New 绝不能调 db.SetDefault
// ——那是 cago database/db 包级全局,会让 internal/daemon/integration_test.go
// 同进程构造的多个 Daemon 静默共享同一个库(参见 db 字段注释)。写入经 d 自己的
// db.WithContextDB(ctx, d.db) 落库,再用完全没有注入过 db 句柄的裸 ctx
// (context.Background())去读同一个键:SetDefault 会让裸 ctx 落回全局、读到这行;
// 正确实现下 db.Ctx 无全局可回落,该调用必须不返回这行数据(cago db.Ctx 在
// defaultDB 未设置时对裸 ctx 的调用会 panic,recover 视为该断言的通过路径)。
func TestDaemon_NewDoesNotLeakIntoGlobalDefaultDB(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)

	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	require.NoError(t, notification_repo.NewNotification().Create(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "leak-guard-peer", PeerSessionID: "leak-guard-session", Seq: 1, Method: "m", Payload: "{}",
	}))

	func() {
		defer func() {
			// A panic here is the expected outcome when db.Ctx has no global
			// default to fall back to (defaultDB was never set) — that proves
			// New did not call db.SetDefault.
			_ = recover()
		}()
		rows, _, err := notification_repo.NewNotification().ListSince(context.Background(), "leak-guard-peer", "leak-guard-session", 0, 10)
		if err == nil {
			assert.Empty(t, rows, "a bare ctx (never wrapped with db.WithContextDB) must not resolve to this Daemon's data — that would mean New() called db.SetDefault")
		}
	}()
}

// authedConn 造一条「已完成 auth.pair / auth.connect」的连接。ws 传 nil:活连接表只读
// AuthState,不碰底层 socket。
func authedConn(fingerprint string) *rpc.Conn {
	c := rpc.NewConn(nil, rpc.NewRegistry())
	c.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: fingerprint})
	return c
}

// TestConnRegistry_ResolvesEachPeerToItsOwnConn 覆盖 R16 的解析侧:会话的通知只推给
// 它自己那台设备。两台设备同时在线时,按指纹解析必须各归各的;没在线的指纹解析为 nil
// (通知已落库,等它重连后按游标补齐),而不是回落到「随便哪条活连接」。
func TestConnRegistry_ResolvesEachPeerToItsOwnConn(t *testing.T) {
	var r connRegistry
	connA, connB := authedConn("fp-a"), authedConn("fp-b")
	nA, nB := notifier.New(connA), notifier.New(connB)
	r.add(connA, nA)
	r.add(connB, nB)

	assert.Same(t, nA, r.lookup("fp-a"))
	assert.Same(t, nB, r.lookup("fp-b"))
	assert.Nil(t, r.lookup("fp-never-seen"), "不在线的对端解析为 nil,不回落到别人的连接")
}

// TestConnRegistry_TunnelTargetIsMostRecentlyAuthenticatedConn 覆盖 MCP 反向隧道的解析:
// 隧道请求来自 daemon 本机的 CLI 子进程、身上没有对端标识,所以从同一张活连接表里取最近
// 完成鉴权的那条(沿用改造前「后连接覆盖」的语义,只是野连接不再算数);它离线后回落到
// 仍在线的连接,表空才是 nil。
func TestConnRegistry_TunnelTargetIsMostRecentlyAuthenticatedConn(t *testing.T) {
	var r connRegistry
	assert.Nil(t, r.latest(), "一条连接都没有时没有隧道目标")

	connA, connB := authedConn("fp-a"), authedConn("fp-b")
	nA, nB := notifier.New(connA), notifier.New(connB)
	r.add(connA, nA)
	assert.Same(t, nA, r.latest())
	r.add(connB, nB)
	assert.Same(t, nB, r.latest(), "隧道目标取最近完成鉴权的那条")

	r.remove(connB)
	assert.Same(t, nA, r.latest(), "它离线后回落到仍在线的连接,而不是变 nil")
}

// TestConnRegistry_UnauthenticatedConnNeverEnters 钉死本任务的症结:登记的前提是
// **已认证**。一条完成 WS 升级却从不认证的连接(以及一条声称已认证却没有指纹的连接
// —— rpc/auth.go 的 HandlePair 不拒绝空 deviceFingerprint)都不进表,更顶不掉正主;
// 空指纹也不构成可匹配身份,否则任何野连接都能冒领所有会话的通知。
func TestConnRegistry_UnauthenticatedConnNeverEnters(t *testing.T) {
	var r connRegistry
	desktop := authedConn("fp-desktop")
	nDesktop := notifier.New(desktop)
	r.add(desktop, nDesktop)

	stray := rpc.NewConn(nil, rpc.NewRegistry()) // 从不认证
	r.add(stray, notifier.New(stray))
	blank := rpc.NewConn(nil, rpc.NewRegistry())
	blank.SetAuth(rpc.AuthState{Authenticated: true}) // 认证了但没有指纹
	r.add(blank, notifier.New(blank))
	claimed := rpc.NewConn(nil, rpc.NewRegistry())
	// 报了指纹却没通过鉴权:身份是鉴权成功那一刻才成立的,光报不算。
	claimed.SetAuth(rpc.AuthState{DeviceFingerprint: "fp-desktop"})
	r.add(claimed, notifier.New(claimed))

	assert.Nil(t, r.lookup(""), "空指纹不是可匹配身份")
	assert.Same(t, nDesktop, r.lookup("fp-desktop"), "野连接不得顶掉已认证的正主")
	assert.Same(t, nDesktop, r.latest(), "MCP 反向隧道的目标同样只在已认证连接里取")
}

// TestConnRegistry_ReconnectBySameFingerprintResumesPushing 覆盖断连重连:同一台设备
// 换一条连接回来后,解析立刻指向新连接;而旧连接的关闭清理**迟到**时不得误删新登记
// (清理按连接身份而不是按指纹),否则重连之后一条也推不出去。
func TestConnRegistry_ReconnectBySameFingerprintResumesPushing(t *testing.T) {
	var r connRegistry
	old := authedConn("fp-desktop")
	r.add(old, notifier.New(old))

	fresh := authedConn("fp-desktop")
	nFresh := notifier.New(fresh)
	r.add(fresh, nFresh)
	assert.Same(t, nFresh, r.lookup("fp-desktop"), "重连后解析指向新连接")

	r.remove(old) // 旧连接的 Done 清理迟到
	assert.Same(t, nFresh, r.lookup("fp-desktop"), "迟到的旧连接清理不得撤掉新连接的登记")

	r.remove(fresh)
	assert.Nil(t, r.lookup("fp-desktop"), "设备真正离线后解析为 nil")
}

// TestDaemon_BindConnDoesNotRegisterUnauthenticatedConn 钉死接线:bindConn 跑在鉴权
// **之前**(它是 LANServer 的 OnConn 回调,auth.pair / auth.connect 是之后才到的 RPC),
// 所以它绝不能把连接登记成推送目标 —— 那正是野连接顶掉正主的原因。会话通知与 MCP
// 反向隧道两条解析都必须仍然落在已认证的那台设备上。
func TestDaemon_BindConnDoesNotRegisterUnauthenticatedConn(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)

	desktop := authedConn("fp-desktop")
	nDesktop := notifier.New(desktop)
	d.conns.add(desktop, nDesktop)

	d.bindConn(rpc.NewConn(nil, rpc.NewRegistry())) // 野连接:完成升级,从不认证

	assert.Same(t, nDesktop, d.notifierForPeer("fp-desktop"),
		"野连接接入后,已配对设备的会话通知仍必须解析到它自己那条连接")
	assert.Same(t, nDesktop, d.tunnelTarget(),
		"MCP 反向隧道目标同样不得被野连接顶掉")
	assert.Nil(t, d.notifierForPeer(""), "空指纹不是可匹配身份")
}

func TestDaemon_BootShutdown(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		lan := d.lan
		d.mu.RUnlock()
		return lan != nil && lan.Addr() != ""
	}, 2*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestDaemon_IPCStatus(t *testing.T) {
	dir := t.TempDir()
	d, err := New(Options{
		DataDir: dir,
		LANHost: "127.0.0.1",
		LANPort: 0,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	require.Eventually(t, func() bool {
		_, err := os.Stat(d.SocketPath())
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)

	tr := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", d.SocketPath())
	}}
	c := &http.Client{Transport: tr}

	resp, err := c.Get("http://daemon/local/status")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var v map[string]any
	require.NoError(t, json.Unmarshal(body, &v))
	assert.NotEmpty(t, v["daemonUUID"])
	assert.NotContains(t, v, "keyStorage")

	resp2, err := c.Get("http://daemon/local/pair")
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	body2, _ := io.ReadAll(resp2.Body)
	var pp map[string]any
	require.NoError(t, json.Unmarshal(body2, &pp))
	code, _ := pp["code"].(string)
	assert.Len(t, code, 6)
}

// TestRecoverHandlerPanic 验证 RPC handler panic 被吃掉,翻成
// rpc.Error{ErrInternal} 让 daemon 进程不挂、客户端收到结构化错误,而不是
// 看到 SIGSEGV 整个 agentred 进程死。回归 claudecode runtime nil deref 把整
// 个 daemon 打挂 / 前端无任何提示 / 会话永远卡在「生成中」的旧 bug。
//
// 直接走 recoverHandlerPanic 而不是 wrapGuarded 是因为后者会先撞 requireAuth
// (需要真 *rpc.Conn 注入),与本测想覆盖的 panic-recovery 边界正交。
func TestRecoverHandlerPanic(t *testing.T) {
	t.Run("panic 翻成 daemon handler panic 错误", func(t *testing.T) {
		var err error
		func() {
			defer recoverHandlerPanic(&err)
			panic("boom")
		}()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daemon handler panic")
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("nil pointer deref panic 同样被回收(回归原始 SIGSEGV 场景)", func(t *testing.T) {
		var err error
		func() {
			defer recoverHandlerPanic(&err)
			var p *int
			_ = *p
		}()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daemon handler panic")
	})

	t.Run("无 panic 时 err 保持 nil", func(t *testing.T) {
		var err error
		func() { defer recoverHandlerPanic(&err) }()
		assert.NoError(t, err)
	})
}
