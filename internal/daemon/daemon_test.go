package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	dbpkg "github.com/cago-frame/cago/database/db"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/client"
	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/notifier"
	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
	"github.com/agentre-ai/agentre/internal/daemon/state"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
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

// TestDaemon_DatabaseUsesWALSoCatchUpReadsDoNotStallTheStreamingWriter 钉死开库方式的
// 可观察后果:通知日志的写是**每个流式事件一条**同步事务,而 session.pull 的补齐读是一段
// 持续着的读事务。回滚日志模式下读事务持 SHARED 锁,写事务提交要 EXCLUSIVE —— 写者只能
// 在 5s busy timeout 上干等,等不到就报 database is locked,那条通知既不落库也不推送(R3)。
// WAL 下读写各走一份快照,互不阻塞。
func TestDaemon_DatabaseUsesWALSoCatchUpReadsDoNotStallTheStreamingWriter(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })

	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()
	require.NoError(t, repo.Append(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Method: "runtime.event", Payload: "{}",
	}))

	// 补齐侧:一次翻页拉取是一个开着的读事务(整段期间都持有读锁)。
	reader := d.db.Begin()
	require.NoError(t, reader.Error)
	t.Cleanup(func() { _ = reader.Rollback() })
	var rows []*notification_repo.NotificationLog
	require.NoError(t, reader.Where("peer_fingerprint = ?", "peerA").Find(&rows).Error)
	require.Len(t, rows, 1)

	// 流式侧:同一时刻的下一条通知必须照常落库。
	done := make(chan error, 1)
	go func() {
		done <- repo.Append(ctx, &notification_repo.NotificationLog{
			PeerFingerprint: "peerA", PeerSessionID: "s1", Method: "runtime.event", Payload: `{"delta":"x"}`,
		})
	}()
	select {
	case appendErr := <-done:
		require.NoError(t, appendErr, "补齐读在飞时,流式通知仍必须落得进库")
	case <-time.After(2 * time.Second):
		t.Fatal("streaming append is stuck behind an open catch-up read — the daemon database must be opened in WAL mode")
	}
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

// TestDaemon_NotificationJournal_AppendNeverReusesASeq 在真库(SQLite,生产方言)上钉死
// 「seq 只由库分配」:Append 是唯一写路径,入参上填了 Seq 也不作数,两次写入拿到的是两个
// 相邻的 seq,两条通知都在日志里。
//
// 会漏掉它的实现:再开一条「按调用方给的 seq 写入」的路径。那种实现下两个写者会撞同一个
// 主键——要么后到的那条被冲突处理静默吞掉(通知永久丢失,而调用方以为落库成功),要么把
// 裸的唯一约束错误抛回通知热路径。
func TestDaemon_NotificationJournal_AppendNeverReusesASeq(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()

	first := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: 1, Method: "runtime.event", Payload: `{"first":true}`,
	}
	require.NoError(t, repo.Append(ctx, first))
	second := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: 1, Method: "runtime.event", Payload: `{"second":true}`,
	}
	require.NoError(t, repo.Append(ctx, second), "入参里的 seq 撞车不该让写入失败")
	assert.Equal(t, int64(1), first.Seq)
	assert.Equal(t, int64(2), second.Seq, "第二条必须拿到下一个 seq,而不是复用入参里那个")

	rows, _, err := repo.ListSince(ctx, "peerA", "s1", 0, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2, "两条通知都必须在日志里,一条也不能被冲突处理吞掉")
	assert.Equal(t, `{"first":true}`, rows[0].Payload)
	assert.Equal(t, `{"second":true}`, rows[1].Payload)
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
	require.NoError(t, notification_repo.NewNotification().Append(ctx1, &notification_repo.NotificationLog{
		PeerFingerprint: "peer", PeerSessionID: "s1", Method: "m", Payload: "{}",
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
	require.NoError(t, notification_repo.NewNotification().Append(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "leak-guard-peer", PeerSessionID: "leak-guard-session", Method: "m", Payload: "{}",
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

// authedConn 造一条「已完成 auth.pair / auth.connect」的连接。ws 传 nil:登记表只读
// AuthState 与 Done(),不碰底层 socket。
func authedConn(fingerprint string) *rpc.Conn {
	c := rpc.NewConn(nil, rpc.NewRegistry())
	c.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: fingerprint})
	return c
}

// openWSConn 造一条**真** ws 的服务端视角连接(rpc.Conn 的 Close / Done 都要底层 socket,
// nil ws 的连接永远关不掉 —— 挂在它 Done() 上的 goroutine 会一直漏着)。
func openWSConn(t *testing.T) *rpc.Conn {
	t.Helper()
	upgrader := websocket.Upgrader{Subprotocols: []string{rpc.Subprotocol}}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		<-r.Context().Done()
		_ = ws.Close()
	}))
	t.Cleanup(s.Close)
	dialer := *websocket.DefaultDialer
	dialer.Subprotocols = []string{rpc.Subprotocol}
	ws, resp, err := dialer.Dial("ws"+s.URL[len("http"):]+"/", nil)
	require.NoError(t, err)
	if resp != nil {
		_ = resp.Body.Close()
	}
	return rpc.NewConn(ws, rpc.NewRegistry())
}

// closedAuthedConn 造一条**已认证且已经关闭**的连接:模拟「登记 / 认领与 Done 监视赛跑,
// 连接先关一步」的时序。
func closedAuthedConn(t *testing.T, fingerprint string) *rpc.Conn {
	t.Helper()
	c := openWSConn(t)
	c.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: fingerprint})
	require.NoError(t, c.Close())
	<-c.Done()
	return c
}

// assertTarget 断言解析出来的推送端口就是期望的那条连接的(先确认解析到了东西:
// assert.Same 对 nil 只会报「Both arguments must be pointers」,看不出是没解析到)。
func assertTarget(t *testing.T, want, got handlers.NotifierPort, msg string) {
	t.Helper()
	require.NotNil(t, got, msg)
	assert.Same(t, want, got, msg)
}

// registerAuthed 造一条已认证连接并按「鉴权成功那一刻」登记它,返回连接与它的推送端口。
func registerAuthed(r *connRegistry, fingerprint string) (*rpc.Conn, handlers.NotifierPort) {
	c := authedConn(fingerprint)
	n := notifier.New(c)
	r.add(c, n)
	return c, n
}

// TestConnRegistry_SessionTargetIsTheConnThatStartedIt 钉死本任务的核心决定:一条会话的
// 推送目标是**发起它的那条连接**,不是「该设备的某条连接」。一台桌面端同时开 2-3 条同
// 指纹的已认证连接(连接池 / 设备监视心跳 / 刷新探测),按指纹索引时后认证的那条会把
// 正在跑的会话整个抢走。
func TestConnRegistry_SessionTargetIsTheConnThatStartedIt(t *testing.T) {
	var r connRegistry
	pool, nPool := registerAuthed(&r, "fp-desktop")
	r.claim(pool, 7) // runtime.run 起会话 7

	_, nHeartbeat := registerAuthed(&r, "fp-desktop") // 心跳连接:同指纹,后认证,不发 runtime.*

	target := r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7})
	assertTarget(t, nPool, target, "会话的通知只推给发起它的那条连接")
	assert.NotSame(t, nHeartbeat, target, "同指纹的后来者不得抢走正在跑的会话")
}

// TestConnRegistry_SameDeviceConnClosingLeavesTheSessionAlone 覆盖撤销侧:同指纹的另一条
// 连接**关闭**时,清理必须按连接身份做 —— 按指纹删会把正在用的那条一并抹掉,此后会话
// 只落库、推不出去。
func TestConnRegistry_SameDeviceConnClosingLeavesTheSessionAlone(t *testing.T) {
	var r connRegistry
	pool, nPool := registerAuthed(&r, "fp-desktop")
	r.claim(pool, 7)
	heartbeat, _ := registerAuthed(&r, "fp-desktop")

	r.remove(heartbeat)

	assertTarget(t, nPool, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"一条与会话无关的同设备连接来去,不得改变会话的推送目标")
}

// TestConnRegistry_TakeoverIsPerSessionAndSameFingerprintOnly 覆盖接管规则与它的授权:
// 同指纹的连接为某会话发过 runtime.* 之后接管**那一条**会话(其它会话不受影响);
// 另一台设备的连接怎么发都接管不走 —— 指纹是接管的授权,不是路由键。
func TestConnRegistry_TakeoverIsPerSessionAndSameFingerprintOnly(t *testing.T) {
	var r connRegistry
	first, nFirst := registerAuthed(&r, "fp-desktop")
	r.claim(first, 7)
	r.claim(first, 8)

	second, nSecond := registerAuthed(&r, "fp-desktop")
	r.claim(second, 7) // 同指纹的新连接为会话 7 发了一次 runtime.*

	assertTarget(t, nSecond, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}), "接管的是这一条会话")
	assertTarget(t, nFirst, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 8}), "同设备的另一条会话不受牵连")

	other, nOther := registerAuthed(&r, "fp-other")
	r.claim(other, 7)
	assertTarget(t, nSecond, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"另一台设备报同一个会话 id 也接管不走 —— 那在 daemon 上是另一条会话(R16)")
	assertTarget(t, nOther, r.ownerOf(sessionKey{peer: "fp-other", sid: 7}), "它自己那条会话归它")
}

// TestConnRegistry_UndoClaimRestoresThePreviousOwner 覆盖被拒调用的还原(接管的凭据是
// daemon **受理**了那条 runtime.*):认领必须跑在 handler 之前,所以 handler 拒了这一条时
// 要把属主还原回认领前的那条连接 —— 还原成「无属主」同样是错的,那会让正在跑的会话平白
// 挂起。已经被更晚的认领接走时不得回卷,前主此刻已经不在线时也不得把它写回去(那是一条
// 指向死连接的陈旧条目)。
func TestConnRegistry_UndoClaimRestoresThePreviousOwner(t *testing.T) {
	var r connRegistry
	owner, nOwner := registerAuthed(&r, "fp-desktop")
	r.claim(owner, 7)

	intruder, _ := registerAuthed(&r, "fp-desktop")
	r.undoClaim(r.claim(intruder, 7))
	assertTarget(t, nOwner, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"被拒的 runtime.* 不接管:属主还原成认领前的那条连接")

	r.undoClaim(r.claim(intruder, 9))
	assert.Nil(t, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 9}),
		"认领前没有属主的会话,还原之后仍然没有属主")

	rolled := r.claim(intruder, 7)
	later, nLater := registerAuthed(&r, "fp-desktop")
	r.claim(later, 7) // 处理期间另一条连接接管了同一条会话
	r.undoClaim(rolled)
	assertTarget(t, nLater, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"迟到的还原不得回卷更晚落定的接管")

	stale := r.claim(intruder, 7)
	r.remove(later) // 前主在 handler 处理期间掉线
	r.undoClaim(stale)
	assert.Nil(t, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"前主已经不在线时,还原不得留下一条指向死连接的条目")
}

// TestConnRegistry_OwnerDeathSuspendsSessionUntilTakeover 覆盖挂起与恢复(R2):属主连接
// 断开 → 该会话解析为「没有出口」(通知照常落库、不推送),同指纹的新连接**认证还不够**,
// 要为它发一次 runtime.* 才接管回来。
func TestConnRegistry_OwnerDeathSuspendsSessionUntilTakeover(t *testing.T) {
	var r connRegistry
	owner, _ := registerAuthed(&r, "fp-desktop")
	r.claim(owner, 7)

	r.remove(owner)
	assert.Nil(t, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}), "属主断开 → 会话挂起")
	assert.Nil(t, r.routerFor("fp-desktop"), "该对端此刻没有持有会话的活连接 → 只落库不推送")

	fresh, nFresh := registerAuthed(&r, "fp-desktop")
	assert.Nil(t, r.routerFor("fp-desktop"), "光重连不接管:它还没为任何会话发过 runtime.*")

	r.claim(fresh, 7)
	assertTarget(t, nFresh, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}), "重连后发 runtime.* 即接管")
	assert.NotNil(t, r.routerFor("fp-desktop"))
}

// TestConnRegistry_UnauthenticatedConnNeverEnters 覆盖登记的前提是**已认证**:完成 WS
// 升级却从不认证的连接、认证了却没有指纹的连接、报了指纹却没通过鉴权的连接,都既不进
// 活连接表也认领不了会话;空指纹不构成可匹配身份。
func TestConnRegistry_UnauthenticatedConnNeverEnters(t *testing.T) {
	var r connRegistry
	desktop, nDesktop := registerAuthed(&r, "fp-desktop")
	r.claim(desktop, 7)

	stray := rpc.NewConn(nil, rpc.NewRegistry()) // 从不认证
	r.add(stray, notifier.New(stray))
	r.claim(stray, 7)
	blank := rpc.NewConn(nil, rpc.NewRegistry())
	blank.SetAuth(rpc.AuthState{Authenticated: true}) // 认证了但没有指纹
	r.add(blank, notifier.New(blank))
	r.claim(blank, 7)
	spoof := rpc.NewConn(nil, rpc.NewRegistry())
	// 报了指纹却没通过鉴权:身份是鉴权成功那一刻才成立的,光报不算。
	spoof.SetAuth(rpc.AuthState{DeviceFingerprint: "fp-desktop"})
	r.add(spoof, notifier.New(spoof))
	r.claim(spoof, 7)

	assert.Nil(t, r.routerFor(""), "空指纹不是可匹配身份")
	assert.Nil(t, r.ownerOf(sessionKey{peer: "", sid: 7}))
	assertTarget(t, nDesktop, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}), "野连接顶不掉正主")
	assertTarget(t, nDesktop, r.tunnelTarget(), "MCP 反向隧道的目标同样只在已认证连接里取")
}

// TestConnRegistry_ReauthDropsClaimsOfThePreviousFingerprint 回归:一条连接先认证 fp-a
// 认领了会话、又改认 fp-b 时,旧指纹下的认领必须一并作废 —— 否则 fp-a 那条会话的通知会
// 推给一条已经属于 fp-b 的连接(跨对端误推)。
func TestConnRegistry_ReauthDropsClaimsOfThePreviousFingerprint(t *testing.T) {
	var r connRegistry
	c, _ := registerAuthed(&r, "fp-a")
	r.claim(c, 7)
	require.NotNil(t, r.ownerOf(sessionKey{peer: "fp-a", sid: 7}))

	c.SetAuth(rpc.AuthState{Authenticated: true, DeviceFingerprint: "fp-b"})
	r.add(c, notifier.New(c))

	assert.Nil(t, r.ownerOf(sessionKey{peer: "fp-a", sid: 7}),
		"改认指纹之后,旧对端的会话不得再解析到这条连接")
	assert.Nil(t, r.routerFor("fp-a"))
}

// TestConnRegistry_ClosedConnLeavesNoStaleEntry 覆盖登记与 Done 监视的竞态:连接先关、
// 登记(或认领)后到时,表里会留下一条指向死连接的陈旧条目 —— 它的清理早就跑过了,
// 之后再没人来收,会话的通知从此推给一条死连接。
func TestConnRegistry_ClosedConnLeavesNoStaleEntry(t *testing.T) {
	var r connRegistry
	dead := closedAuthedConn(t, "fp-desktop")

	r.add(dead, notifier.New(dead))
	r.claim(dead, 7)

	assert.Nil(t, r.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}), "已关闭的连接不得成为会话目标")
	assert.Nil(t, r.routerFor("fp-desktop"))
	assert.Nil(t, r.tunnelTarget(), "MCP 反向隧道同样不得指向一条已关闭的连接")
}

// recordingNotifier 记下推给它的通知,用来观察一条通知**实际落到了哪条连接**。
type recordingNotifier struct{ got []string }

func (n *recordingNotifier) Notify(method string, _ any) error {
	n.got = append(n.got, method)
	return nil
}

func (n *recordingNotifier) Request(context.Context, string, any, any) error { return nil }

// TestSessionRouter_RoutesByFrameSessionID 覆盖出口本身的契约:同一个对端出口上,每条
// 通知按帧上的 sessionId 交给发起那条会话的连接(两条会话各归各的);帧上没有 sessionId、
// 或该会话已经没有活属主时,报错而不是推给别的连接 —— 静默推错比推不出去更糟,通知已经
// 落库,推不出去等接管后补齐即可。
func TestSessionRouter_RoutesByFrameSessionID(t *testing.T) {
	var r connRegistry
	connA, connB := authedConn("fp-desktop"), authedConn("fp-desktop")
	nA, nB := &recordingNotifier{}, &recordingNotifier{}
	r.add(connA, nA)
	r.add(connB, nB)
	r.claim(connA, 7)
	r.claim(connB, 8)

	out := r.routerFor("fp-desktop")
	require.NotNil(t, out)
	require.NoError(t, out.Notify(wire.NotifyEvent, &wire.EventFrame{SessionID: 7}))
	require.NoError(t, out.Notify(wire.NotifyRunResultDone, &wire.RunResultDoneFrame{SessionID: 8}))
	assert.Equal(t, []string{wire.NotifyEvent}, nA.got, "会话 7 的通知只落在发起它的那条连接")
	assert.Equal(t, []string{wire.NotifyRunResultDone}, nB.got, "会话 8 的通知只落在发起它的那条连接")

	assert.Error(t, out.Notify(wire.NotifyEvent, map[string]any{"sessionId": 7}),
		"帧上取不到 sessionId 时必须报错点名类型,而不是猜一条连接推过去")
	r.remove(connA)
	assert.Error(t, out.Notify(wire.NotifyEvent, &wire.EventFrame{SessionID: 7}),
		"属主断开后这条会话没有出口(通知已落库,等接管后补齐)")
	assert.Equal(t, []string{wire.NotifyRunResultDone}, nB.got, "更不能改推给同设备的另一条连接")
}

// TestConnRegistry_TunnelTargetFollowsTheSessionOwner 覆盖 MCP 反向隧道的解析:隧道请求
// 来自 daemon 本机的 CLI 子进程(HTTP,身上没有会话标识),所以取最近被认领的那条会话的
// 属主连接 —— 跑着会话的那台设备就是这些子进程的发起端。一条会话都没被认领时回落到最近
// 完成鉴权的活连接(desktop 侧隧道 handler 无状态,同设备哪条连接送达都等价);表空 → nil。
func TestConnRegistry_TunnelTargetFollowsTheSessionOwner(t *testing.T) {
	var r connRegistry
	assert.Nil(t, r.tunnelTarget(), "一条连接都没有时没有隧道目标")

	runner, nRunner := registerAuthed(&r, "fp-desktop")
	assertTarget(t, nRunner, r.tunnelTarget(), "还没有会话时回落到最近完成鉴权的活连接")

	r.claim(runner, 7)
	_, nLater := registerAuthed(&r, "fp-other") // 另一台设备后认证,但没跑会话
	assertTarget(t, nRunner, r.tunnelTarget(), "隧道目标跟着会话属主走,不被后认证的设备顶掉")

	r.remove(runner)
	assertTarget(t, nLater, r.tunnelTarget(), "属主离线后回落到仍在线的连接")
}

// TestDaemon_BindConnDoesNotMakeUnauthenticatedConnATarget 钉死接线:bindConn 跑在鉴权
// **之前**(它是 LANServer 的 OnConn 回调),所以它绝不能把连接登记成推送目标 —— 那正是
// 野连接顶掉正主的原因。会话通知与 MCP 反向隧道两条解析都必须仍落在正主那条连接上。
func TestDaemon_BindConnDoesNotMakeUnauthenticatedConnATarget(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })

	desktop, nDesktop := registerAuthed(&d.conns, "fp-desktop")
	d.conns.claim(desktop, 7)

	// 野连接:完成升级,从不认证。用真 ws 连接以便它能真的关闭 —— bindConn 会挂一个
	// 等 Done 的 goroutine,连接永不关闭的话它就永远退不出去(测试进程里的泄漏)。
	stray := openWSConn(t)
	d.bindConn(stray)
	t.Cleanup(func() { _ = stray.Close() })

	router := d.notifierForPeer("fp-desktop")
	require.NotNil(t, router, "野连接接入后,正主的会话仍必须有推送出口")
	assertTarget(t, nDesktop, d.conns.ownerOf(sessionKey{peer: "fp-desktop", sid: 7}),
		"野连接接入后,会话仍解析到发起它的那条连接")
	assertTarget(t, nDesktop, d.tunnelTarget(), "MCP 反向隧道目标同样不得被野连接顶掉")
	assert.Nil(t, d.notifierForPeer(""), "空指纹不是可匹配身份")
}

// TestDaemon_AuthRejectsEmptyDeviceFingerprint 回归:rpc/auth.go 的 HandlePair 不拒绝空
// deviceFingerprint,配对下来会在 PairedPeers 里留一条空键的对端,之后任何连接都能顶着
// 空指纹 auth.connect 成功。daemon 在入参处挡掉。
func TestDaemon_AuthRejectsEmptyDeviceFingerprint(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir()})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })

	code, err := d.pairing.Generate()
	require.NoError(t, err)

	_, err = d.registry.Dispatch(context.Background(), "auth.pair",
		json.RawMessage(`{"code":"`+code+`","deviceName":"nameless","deviceFingerprint":""}`))
	require.Error(t, err, "auth.pair must reject an empty deviceFingerprint")
	assert.NotContains(t, d.state.Snapshot().PairedPeers, "", "空指纹绝不能被配对进 PairedPeers")

	_, err = d.registry.Dispatch(context.Background(), "auth.connect",
		json.RawMessage(`{"deviceFingerprint":"","deviceToken":"whatever"}`))
	require.Error(t, err, "auth.connect must reject an empty deviceFingerprint")
}

// TestDaemon_GivenClaimedAndUnavailableRelay_WhenRunning_ThenLANKeepsServing
// covers R14's degradation boundary: a relay failure must stay in the outbound
// background loop rather than preventing the daemon's direct LAN server from starting.
func TestDaemon_GivenClaimedAndUnavailableRelay_WhenRunning_ThenLANKeepsServing(t *testing.T) {
	var requests atomic.Int32
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer device-access-token", r.Header.Get("Authorization"))
		requests.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(relay.Close)

	dir, err := os.MkdirTemp("", "agentred-hub-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	st, err := state.Load(dir)
	require.NoError(t, err)
	st.Claim("account-42", "cached-public-key", state.AccountCredential{AccessToken: "device-access-token"})
	require.NoError(t, st.Save())

	d, err := New(Options{
		DataDir:      dir,
		LANHost:      "127.0.0.1",
		LANPort:      0,
		HubServerURL: relay.URL,
	})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case runErr := <-errCh:
			require.NoError(t, runErr)
		case <-time.After(3 * time.Second):
			t.Error("Run did not return after cancel")
		}
	})

	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready && requests.Load() > 0
	}, 2*time.Second, 10*time.Millisecond, "the LAN server must run while the claimed daemon retries the relay")
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

func TestDaemon_TwoConnectionsKeepTerminalHandlersIsolated(t *testing.T) {
	dataDir, err := os.MkdirTemp("", "agentred-isolation-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dataDir) })
	d, err := New(Options{DataDir: dataDir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	daemonCtx, stopDaemon := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- d.Run(daemonCtx) }()
	defer func() {
		stopDaemon()
		select {
		case runErr := <-errCh:
			assert.NoError(t, runErr)
		case <-time.After(3 * time.Second):
			t.Error("daemon did not stop within 3s")
		}
	}()
	require.Eventually(t, func() bool {
		d.mu.RLock()
		ready := d.lan != nil && d.lan.Addr() != ""
		d.mu.RUnlock()
		return ready
	}, 2*time.Second, 10*time.Millisecond)

	pairBody := readLocalPair(t, d)
	pairCode, _ := pairBody["code"].(string)
	require.Len(t, pairCode, 6)

	d.mu.RLock()
	lanURL := d.lan.URL()
	d.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientA, err := client.Dial(ctx, client.Options{URL: lanURL})
	require.NoError(t, err)
	defer func() { _ = clientA.Close() }()
	var pairResp rpc.PairResult
	require.NoError(t, clientA.Call(ctx, "auth.pair", rpc.PairParams{
		Code:              pairCode,
		DeviceName:        "connection-a",
		DeviceFingerprint: "sha256:shared-device",
	}, &pairResp))

	clientB, err := client.Dial(ctx, client.Options{URL: lanURL})
	require.NoError(t, err)
	defer func() { _ = clientB.Close() }()
	require.NoError(t, clientB.Call(ctx, "auth.connect", rpc.ConnectParams{
		DeviceFingerprint:         "sha256:shared-device",
		DeviceToken:               pairResp.DeviceToken,
		ExpectedDaemonFingerprint: pairResp.DaemonFingerprint,
	}, nil))

	require.NoError(t, clientA.Call(ctx, "health.ping", nil, nil))
	require.NoError(t, clientB.Call(ctx, "health.ping", nil, nil))

	// Filling A's bounded pending-close tombstones exercises A's
	// TerminalHandlers without opening a real platform PTY. B must retain a
	// fresh handler set even after A reaches its own capacity and disconnects.
	var capacityErr error
	for i := 0; i < 1024 && capacityErr == nil; i++ {
		capacityErr = clientA.Call(ctx, "terminal.close", map[string]any{
			"terminalId":        fmt.Sprintf("connection-a-%d", i),
			"cancelPendingOpen": true,
		}, nil)
	}
	require.Error(t, capacityErr)
	require.Contains(t, capacityErr.Error(), "capacity")
	require.NoError(t, clientA.Close())
	require.NoError(t, clientB.Call(ctx, "terminal.close", map[string]any{
		"terminalId":        "connection-b-after-a-close",
		"cancelPendingOpen": true,
	}, nil))

	_, err = d.registry.Dispatch(context.Background(), "terminal.close", nil)
	require.ErrorIs(t, err, rpc.ErrMethodNotFound, "bindConn must not mutate the bootstrap registry")
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

// TestDaemon_IPCStatus_ReportsDatabasePathAndSize 覆盖规格「安全、隐私…/磁盘增长」的
// 那一句:库文件路径与体量必须在 daemon 状态查询里看得见,用户才能自行判断何时清理。
//
// 断言的是可观察事实而不是「有这两个键」:路径要真的指向这个 DataDir 下的库文件,
// 体量要跟着库一起长 —— 写进去一批通知之后报出来的字节数必须变大,不能是一个常量、
// 也不能只报主库文件而漏掉 WAL 旁文件(WAL 模式下新写入先落在 -wal 上)。
func TestDaemon_IPCStatus_ReportsDatabasePathAndSize(t *testing.T) {
	// 不用 t.TempDir():它以测试名建目录,而 unix socket 的路径在 macOS 上只有 104 字节,
	// 长测试名会让 IPC 直接绑不上。
	dir, err := os.MkdirTemp("", "agentred-status")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{DataDir: dir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(d.SocketPath())
		return statErr == nil
	}, 2*time.Second, 10*time.Millisecond)

	tr := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", d.SocketPath())
	}}
	client := &http.Client{Transport: tr}
	status := func() map[string]any {
		t.Helper()
		resp, getErr := client.Get("http://daemon/local/status")
		require.NoError(t, getErr)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		var v map[string]any
		require.NoError(t, json.Unmarshal(body, &v))
		return v
	}

	before := status()
	assert.Equal(t, filepath.Join(dir, dbFileName), before["dbPath"],
		"状态查询必须交出这台 daemon 真实的库文件路径")
	sizeBefore, ok := before["dbSizeBytes"].(float64)
	require.True(t, ok, "状态查询必须交出库文件体量")
	assert.Positive(t, sizeBefore)

	dbCtx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()
	payload := `{"delta":"` + strings.Repeat("x", 4096) + `"}`
	for range 200 {
		require.NoError(t, repo.Append(dbCtx, &notification_repo.NotificationLog{
			PeerFingerprint: "peerA", PeerSessionID: "s1", Method: wire.NotifyEvent, Payload: payload,
		}))
	}

	sizeAfter, ok := status()["dbSizeBytes"].(float64)
	require.True(t, ok)
	assert.Greater(t, sizeAfter, sizeBefore, "体量必须跟着库一起长,而不是一个常量")
}

// TestDaemon_IPCStatus_CountsSessionsRunningRightNow 钉死状态查询里的「活跃会话数」:
// 它必须来自 daemon 自己记着的生命周期(一轮起手 running、轮末 idle、重启标 interrupted),
// 而不是一张没有任何写入方的内存表 —— 那样的话有轮次正在跑时 `agentred status` 照样
// 印 Active sessions: 0,读的人据此以为自己的会话没了。
func TestDaemon_IPCStatus_CountsSessionsRunningRightNow(t *testing.T) {
	// 不用 t.TempDir():它以测试名建目录,而 unix socket 的路径在 macOS 上只有 104 字节。
	dir, err := os.MkdirTemp("", "agentred-active")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{DataDir: dir, LANHost: "127.0.0.1", LANPort: 0})
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()
	require.Eventually(t, func() bool {
		_, statErr := os.Stat(d.SocketPath())
		return statErr == nil
	}, 2*time.Second, 10*time.Millisecond)

	tr := &http.Transport{DialContext: func(_ context.Context, _, _ string) (net.Conn, error) {
		return net.Dial("unix", d.SocketPath())
	}}
	client := &http.Client{Transport: tr}
	activeSessions := func() float64 {
		t.Helper()
		resp, getErr := client.Get("http://daemon/local/status")
		require.NoError(t, getErr)
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		var v map[string]any
		require.NoError(t, json.Unmarshal(body, &v))
		n, ok := v["activeSessions"].(float64)
		require.True(t, ok, "状态查询必须交出活跃会话数")
		return n
	}

	assert.Zero(t, activeSessions(), "一轮都没跑过时是 0")

	dbCtx := dbpkg.WithContextDB(context.Background(), d.db)
	seedSession(t, dbCtx, d.sessionStore, "peerA", "1", wire.SessionLifecycleRunning)
	seedSession(t, dbCtx, d.sessionStore, "peerA", "2", wire.SessionLifecycleIdle)
	seedSession(t, dbCtx, d.sessionStore, "peerB", "1", wire.SessionLifecycleRunning)

	assert.Equal(t, float64(2), activeSessions(),
		"数的是此刻真的在跑的那些:空闲会话不算,别的对端在跑的算")
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

// seedJournal 给某会话灌 n 条日志(Append 依次分配 seq 1..n),全部盖上同一个落库时间。
func seedJournal(t *testing.T, ctx context.Context, peer, sid string, n int, createdAt int64) {
	t.Helper()
	repo := notification_repo.NewNotification()
	for i := 1; i <= n; i++ {
		row := &notification_repo.NotificationLog{
			PeerFingerprint: peer, PeerSessionID: sid,
			Method: wire.NotifyEvent, Payload: fmt.Sprintf(`{"seq":%d}`, i), CreatedAt: createdAt,
		}
		require.NoError(t, repo.Append(ctx, row))
		require.Equal(t, int64(i), row.Seq)
	}
}

// seedSession 给某会话建一条生命周期行。
func seedSession(t *testing.T, ctx context.Context, store daemonSessionStore, peer, sid, lifecycle string) {
	t.Helper()
	require.NoError(t, store.Start(ctx, handlers.SessionRecord{
		PeerFingerprint: peer, PeerSessionID: sid, BackendType: "claudecode", LifecycleState: lifecycle,
	}))
}

// journalSeqs 读回某会话此刻还剩哪些 seq。
func journalSeqs(t *testing.T, ctx context.Context, peer, sid string) []int64 {
	t.Helper()
	rows, _, err := notification_repo.NewNotification().ListSince(ctx, peer, sid, 0, 1000)
	require.NoError(t, err)
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Seq)
	}
	return out
}

// TestDaemon_CollectJournal_NeverReclaimsARangeThatCouldStillBeCaughtUp 钉死留存策略
// 的判据。回收只碰**整个留存窗口内一条新通知都没有**的会话,而且永远保留它的高水位那一行:
//
//   - 还在产出的会话一行都不动 —— 哪怕它最老的那批日志早过了窗口。那段老前缀正是一个
//     久未上线的客户端重连后要补齐的区间(R5:补齐序列与不断连逐条相等)。
//   - 非终态(running)的会话一行都不动:它随时可能被接管接着跑。
//   - 库里没有会话行的日志一行都不动:身份不明时一律保守。
func TestDaemon_CollectJournal_NeverReclaimsARangeThatCouldStillBeCaughtUp(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir(), JournalRetention: 24 * time.Hour})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })
	ctx := dbpkg.WithContextDB(context.Background(), d.db)

	old := time.Now().Add(-90 * 24 * time.Hour).UnixMilli()
	fresh := time.Now().UnixMilli()

	// 安静了整个窗口的空闲会话:可回收。
	seedSession(t, ctx, d.sessionStore, "peerA", "1", wire.SessionLifecycleIdle)
	seedJournal(t, ctx, "peerA", "1", 5, old)
	// 老前缀 + 刚刚还在产出:一行都不能动。
	seedSession(t, ctx, d.sessionStore, "peerA", "2", wire.SessionLifecycleIdle)
	seedJournal(t, ctx, "peerA", "2", 3, old)
	require.NoError(t, notification_repo.NewNotification().Append(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "2", Method: wire.NotifyEvent, Payload: "{}", CreatedAt: fresh,
	}))
	// 还在跑的会话:安静再久也不动。
	seedSession(t, ctx, d.sessionStore, "peerA", "3", wire.SessionLifecycleRunning)
	seedJournal(t, ctx, "peerA", "3", 4, old)
	// 没有会话行的孤儿日志:不动。
	seedJournal(t, ctx, "peerB", "9", 4, old)

	collected, err := d.collectJournal(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(4), collected, "只该回收那条安静会话高水位以下的 4 行")

	assert.Equal(t, []int64{5}, journalSeqs(t, ctx, "peerA", "1"),
		"安静会话只留高水位那一行")
	assert.Equal(t, []int64{1, 2, 3, 4}, journalSeqs(t, ctx, "peerA", "2"),
		"还在产出的会话,连它窗口之外的老前缀也必须原封不动 —— 那正是久未上线的客户端要补齐的区间")
	assert.Equal(t, []int64{1, 2, 3, 4}, journalSeqs(t, ctx, "peerA", "3"),
		"非终态会话随时可能被接管接着跑,一行都不回收")
	assert.Equal(t, []int64{1, 2, 3, 4}, journalSeqs(t, ctx, "peerB", "9"),
		"库里没有会话行时身份不明,保守起见不回收")
}

// TestDaemon_CollectJournal_KeepsTheSeqTimelineIntact 覆盖回收之后这条会话**接得回去**:
// 高水位不退、下一条通知接着往上排、落在高水位前一格的客户端仍拉得到那一条。
//
// 少了这条约束,回收就是在制造 8496c291 修掉的那种静默冻结:MAX(seq) 被抹掉后 Append
// 从 1 重新分配,而客户端游标还停在旧高水位上,此后每一条实时通知都被当成重复丢弃 ——
// 没有跳号、没有错误,会话就是再也不出字。
func TestDaemon_CollectJournal_KeepsTheSeqTimelineIntact(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir(), JournalRetention: 24 * time.Hour})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })
	ctx := dbpkg.WithContextDB(context.Background(), d.db)
	repo := notification_repo.NewNotification()

	seedSession(t, ctx, d.sessionStore, "peerA", "1", wire.SessionLifecycleIdle)
	seedJournal(t, ctx, "peerA", "1", 5, time.Now().Add(-90*24*time.Hour).UnixMilli())

	_, err = d.collectJournal(context.Background())
	require.NoError(t, err)

	latest, err := repo.LatestSeq(ctx, "peerA", "1")
	require.NoError(t, err)
	assert.Equal(t, int64(5), latest, "高水位不因回收而后退")

	next := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "1", Method: wire.NotifyEvent, Payload: "{}",
	}
	require.NoError(t, repo.Append(ctx, next))
	assert.Equal(t, int64(6), next.Seq, "回收之后 seq 接着往上排,绝不从 1 重来")

	rows, _, err := repo.ListSince(ctx, "peerA", "1", 4, 10)
	require.NoError(t, err)
	require.Len(t, rows, 2, "落在高水位前一格的客户端仍能按游标拉平")
	assert.Equal(t, int64(5), rows[0].Seq)
	assert.Equal(t, int64(6), rows[1].Seq)
}

// TestDaemon_CollectJournal_RetentionCanBeTurnedOff 覆盖留存窗口的开关:规格把
// 「永久保留」写成了默认承诺,负数窗口因此必须让回收整个不发生(一行都不删)。
func TestDaemon_CollectJournal_RetentionCanBeTurnedOff(t *testing.T) {
	d, err := New(Options{DataDir: t.TempDir(), JournalRetention: -1})
	require.NoError(t, err)
	t.Cleanup(func() { closeDB(d.db) })
	ctx := dbpkg.WithContextDB(context.Background(), d.db)

	seedSession(t, ctx, d.sessionStore, "peerA", "1", wire.SessionLifecycleIdle)
	seedJournal(t, ctx, "peerA", "1", 5, time.Now().Add(-90*24*time.Hour).UnixMilli())

	collected, err := d.collectJournal(context.Background())
	require.NoError(t, err)
	assert.Zero(t, collected)
	assert.Len(t, journalSeqs(t, ctx, "peerA", "1"), 5, "关掉留存窗口时一行都不回收")
}

// TestDaemon_RunCollectsTheJournal 钉死接线:回收必须由 daemon 自己跑起来,而不是等
// 谁来调 —— 没有调用方的回收路径等于没有回收路径,日志照旧无限增长。
func TestDaemon_RunCollectsTheJournal(t *testing.T) {
	dir, err := os.MkdirTemp("", "agentred-collect")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	d, err := New(Options{DataDir: dir, LANHost: "127.0.0.1", LANPort: 0, JournalRetention: time.Hour})
	require.NoError(t, err)
	dbCtx := dbpkg.WithContextDB(context.Background(), d.db)
	seedSession(t, dbCtx, d.sessionStore, "peerA", "1", wire.SessionLifecycleIdle)
	seedJournal(t, dbCtx, "peerA", "1", 5, time.Now().Add(-90*24*time.Hour).UnixMilli())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = d.Run(ctx) }()

	require.Eventually(t, func() bool {
		return len(journalSeqs(t, dbCtx, "peerA", "1")) == 1
	}, 3*time.Second, 20*time.Millisecond, "daemon 跑起来之后必须自己回收掉安静会话的日志前缀")
}
