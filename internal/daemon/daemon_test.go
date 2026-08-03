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
	"sync"
	"testing"
	"time"

	dbpkg "github.com/cago-frame/cago/database/db"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/daemon/notifier"
	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
	"github.com/agentre-ai/agentre/internal/daemon/rpc"
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
