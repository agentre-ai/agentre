package notification_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/daemon/repository/notification_repo"
)

// TestNotificationRepo_NextSeqAndCreate_MonotonicNoGaps 覆盖测试接缝表要求的
// 「seq 单调无洞」:对同一 (peerFingerprint, peerSessionID),连续
// NextSeq→Create 配对必须产出连续递增、无跳号的 seq 序列。
func TestNotificationRepo_NextSeqAndCreate_MonotonicNoGaps(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := notification_repo.NewNotification()

	// 第一条:日志表还没有该会话的行,MAX(seq) 为 NULL,COALESCE 落到 0+1=1。
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\) \\+ 1 FROM `daemon_notification_logs` WHERE peer_fingerprint = \\? AND peer_session_id = \\?").
		WithArgs("peerA", "s1").
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(1))
	seq1, err := repo.NextSeq(ctx, "peerA", "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), seq1)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `daemon_notification_logs`.*ON DUPLICATE KEY UPDATE").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Create(ctx, &notification_repo.NotificationLog{
		PeerFingerprint: "peerA", PeerSessionID: "s1", Seq: seq1, Method: "runtime.event", Payload: "{}",
	}))

	// 第二条:上一条已落库,MAX(seq)=1,下一个是 2 —— 不是 3、不是仍然 1(无洞)。
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\) \\+ 1 FROM `daemon_notification_logs` WHERE peer_fingerprint = \\? AND peer_session_id = \\?").
		WithArgs("peerA", "s1").
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(2))
	seq2, err := repo.NextSeq(ctx, "peerA", "s1")
	require.NoError(t, err)
	assert.Equal(t, int64(2), seq2, "second seq must immediately follow the first, no gap")

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
