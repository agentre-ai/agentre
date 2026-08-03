package notification_repo_test

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
)

// appendSQLPattern 是 Append 必须发出的那条 SQL:分配 seq 与写入在同一条语句里
// 完成。分成「先 SELECT MAX(seq)+1 再 INSERT」两条语句的实现会漏掉这个模式而失败
// ——那种实现下两个并发写者会读到同一个 MAX(seq)、其中一条通知被静默丢弃
// (见 daemon_test.go 的并发用例)。
const appendSQLPattern = "INSERT INTO daemon_notification_logs " +
	"\\(peer_fingerprint, peer_session_id, seq, method, payload, created_at\\) " +
	"SELECT \\?, \\?, COALESCE\\(MAX\\(seq\\), 0\\) \\+ 1, \\?, \\?, \\? " +
	"FROM daemon_notification_logs WHERE peer_fingerprint = \\? AND peer_session_id = \\? " +
	"RETURNING seq"

// TestNotificationRepo_Append_AllocatesNextSeqInOneStatement 覆盖任务目标的
// 「一条通知能以下一个 seq 落库」:Append 只发一条语句,库分配的 seq 经 RETURNING
// 回填到入参上供调用方构造推送帧。
func TestNotificationRepo_Append_AllocatesNextSeqInOneStatement(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	mock.ExpectQuery(appendSQLPattern).
		WithArgs("peerA", "s1", "runtime.event", "{}", sqlmock.AnyArg(), "peerA", "s1").
		WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(7))

	n := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Method: "runtime.event", Payload: "{}",
	}
	require.NoError(t, repo.Append(ctx, n))
	assert.Equal(t, int64(7), n.Seq, "库分配的 seq 必须回填到入参")
	assert.NotZero(t, n.CreatedAt, "落库时间必须被填上")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNotificationRepo_Append_PropagatesError 覆盖错误路径:落库失败必须冒泡给
// 调用方(R3 靠它判断「不推进 seq、不推送」),且失败时不得回填 Seq。
func TestNotificationRepo_Append_PropagatesError(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	mock.ExpectQuery(appendSQLPattern).WillReturnError(errors.New("disk I/O error"))

	n := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Method: "runtime.event", Payload: "{}",
	}
	err := repo.Append(ctx, n)
	require.Error(t, err)
	assert.Zero(t, n.Seq, "落库失败不得回填 seq")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNotificationRepo_Create_DuplicateSeqIsIdempotent 覆盖「同 (会话, seq) 重复写入
// 幂等」:对已经落过的 (peerFingerprint, peerSessionID, seq) 再次 Create 必须成功而不
// 报错(供调用方在未确认写入是否成功时安全重试),不能是唯一约束冲突的裸错误冒泡。
func TestNotificationRepo_Create_DuplicateSeqIsIdempotent(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	n := &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: 1, Method: "runtime.event", Payload: `{"a":1}`,
	}

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `daemon_notification_logs`.*ON DUPLICATE KEY UPDATE").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Create(ctx, n))

	// 重复写入同一个 (peer, session, seq):第二次影响行数为 0(冲突被吞掉),但调用方
	// 视角必须是成功,不是错误。
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `daemon_notification_logs`.*ON DUPLICATE KEY UPDATE").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()
	require.NoError(t, repo.Create(ctx, n), "duplicate (peer, session, seq) write must be idempotent, not error")

	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNotificationRepo_LatestSeq_ReadsMaxSeqFromTheLog 覆盖「某会话最新的 seq」:
// 它的唯一真相源是通知日志自己的 MAX(seq)(daemon_sessions.latest_seq 无写入方,读它
// 会永远报 0,客户端每次重连都重拉整段日志)。会话一条通知都没有时报 0。
func TestNotificationRepo_LatestSeq_ReadsMaxSeqFromTheLog(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\) FROM daemon_notification_logs WHERE peer_fingerprint = \\? AND peer_session_id = \\?").
		WithArgs("peerA", "s1").
		WillReturnRows(sqlmock.NewRows([]string{"seq"}).AddRow(42))

	got, err := repo.LatestSeq(ctx, "peerA", "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(42), got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNotificationRepo_LatestSeqByPeer_GroupsPerSession 覆盖会话清单要的那份「每条
// 会话的最新 seq」:一条 GROUP BY 查询把该对端全部会话的 MAX(seq) 一次取回,而不是按
// 会话数发 N 条查询。没有通知的会话不出现在结果里,调用方按 0 处理。
func TestNotificationRepo_LatestSeqByPeer_GroupsPerSession(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	mock.ExpectQuery("SELECT peer_session_id, MAX\\(seq\\) AS seq FROM daemon_notification_logs WHERE peer_fingerprint = \\? GROUP BY peer_session_id").
		WithArgs("peerA").
		WillReturnRows(sqlmock.NewRows([]string{"peer_session_id", "seq"}).
			AddRow("s1", 42).
			AddRow("s2", 7))

	got, err := repo.LatestSeqByPeer(ctx, "peerA")
	require.NoError(t, err)
	assert.Equal(t, map[string]int64{"s1": 42, "s2": 7}, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestNotificationRepo_ListSince_CursorBoundaries 覆盖测试接缝表要求的「增量拉取边界」:
// 起始游标为 0、起始游标大于最新 seq、以及翻页 hasMore 标志。
func TestNotificationRepo_ListSince_CursorBoundaries(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	t.Run("cursor=0 返回从 seq=1 开始的全部通知", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"peer_fingerprint", "peer_session_id", "seq", "method", "payload", "created_at"}).
			AddRow("peerA", "s1", 1, "runtime.event", "{}", 100).
			AddRow("peerA", "s1", 2, "runtime.event", "{}", 200)
		mock.ExpectQuery("SELECT \\* FROM `daemon_notification_logs` WHERE peer_fingerprint = \\? AND peer_session_id = \\? AND seq > \\? ORDER BY seq ASC LIMIT \\?").
			WithArgs("peerA", "s1", int64(0), 11).
			WillReturnRows(rows)

		got, hasMore, err := repo.ListSince(ctx, "peerA", "s1", 0, 10)
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, int64(1), got[0].Seq)
		assert.Equal(t, int64(2), got[1].Seq)
		assert.False(t, hasMore)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("cursor 大于最新 seq 返回空且 hasMore=false", func(t *testing.T) {
		mock.ExpectQuery("SELECT \\* FROM `daemon_notification_logs` WHERE peer_fingerprint = \\? AND peer_session_id = \\? AND seq > \\? ORDER BY seq ASC LIMIT \\?").
			WithArgs("peerA", "s1", int64(999), 11).
			WillReturnRows(sqlmock.NewRows([]string{"peer_fingerprint", "peer_session_id", "seq", "method", "payload", "created_at"}))

		got, hasMore, err := repo.ListSince(ctx, "peerA", "s1", 999, 10)
		require.NoError(t, err)
		assert.Empty(t, got)
		assert.False(t, hasMore)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("超过 limit 的剩余行触发 hasMore=true 且只返回 limit 条", func(t *testing.T) {
		rows := sqlmock.NewRows([]string{"peer_fingerprint", "peer_session_id", "seq", "method", "payload", "created_at"}).
			AddRow("peerA", "s1", 1, "runtime.event", "{}", 100).
			AddRow("peerA", "s1", 2, "runtime.event", "{}", 200).
			AddRow("peerA", "s1", 3, "runtime.event", "{}", 300)
		mock.ExpectQuery("SELECT \\* FROM `daemon_notification_logs` WHERE peer_fingerprint = \\? AND peer_session_id = \\? AND seq > \\? ORDER BY seq ASC LIMIT \\?").
			WithArgs("peerA", "s1", int64(0), 3).
			WillReturnRows(rows)

		got, hasMore, err := repo.ListSince(ctx, "peerA", "s1", 0, 2)
		require.NoError(t, err)
		require.Len(t, got, 2, "page must be capped at limit even though the mock returned limit+1 rows")
		assert.Equal(t, int64(1), got[0].Seq)
		assert.Equal(t, int64(2), got[1].Seq)
		assert.True(t, hasMore)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
