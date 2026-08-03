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

	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
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
