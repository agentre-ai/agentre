package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMigration202608010001_CreatesTables 建表:daemon_sessions 与
// daemon_notification_logs 两张表都存在,且带上 R16 要求的复合主键列
// (peer_fingerprint, peer_session_id[, seq])。
func TestMigration202608010001_CreatesTables(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	hasTable := func(table string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		return n > 0
	}
	hasCol := func(table, col string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col).Scan(&n)
		return n > 0
	}

	assert.True(t, hasTable("daemon_sessions"))
	for _, col := range []string{
		"peer_fingerprint", "peer_session_id", "agent_id", "cwd",
		"backend_type", "lifecycle_state", "latest_seq", "created_at", "updated_at",
	} {
		assert.True(t, hasCol("daemon_sessions", col), "daemon_sessions missing column %s", col)
	}

	assert.True(t, hasTable("daemon_notification_logs"))
	for _, col := range []string{
		"peer_fingerprint", "peer_session_id", "seq", "method", "payload", "created_at",
	} {
		assert.True(t, hasCol("daemon_notification_logs", col), "daemon_notification_logs missing column %s", col)
	}
}

// TestMigration202608010001_CompositePrimaryKeysEnforceUniqueness 索引覆盖:
// 两张表的复合主键必须真正拒绝重号——不同对端各自持有同一个本地会话 id 时互不冲突
// (R16),但同一 (对端, 会话[, seq]) 重复插入必须撞唯一约束。
func TestMigration202608010001_CompositePrimaryKeysEnforceUniqueness(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	require.NoError(t, db.Exec(
		`INSERT INTO daemon_sessions (peer_fingerprint, peer_session_id, latest_seq, created_at, updated_at)
		 VALUES ('peerA', 's1', 0, 0, 0)`).Error)
	// 不同对端持有同一个本地会话 id 'con't collide (R16)。
	require.NoError(t, db.Exec(
		`INSERT INTO daemon_sessions (peer_fingerprint, peer_session_id, latest_seq, created_at, updated_at)
		 VALUES ('peerB', 's1', 0, 0, 0)`).Error)
	// 同一 (对端, 会话) 重复插入必须撞主键。
	assert.Error(t, db.Exec(
		`INSERT INTO daemon_sessions (peer_fingerprint, peer_session_id, latest_seq, created_at, updated_at)
		 VALUES ('peerA', 's1', 0, 0, 0)`).Error, "duplicate (peer_fingerprint, peer_session_id) should violate primary key")

	require.NoError(t, db.Exec(
		`INSERT INTO daemon_notification_logs (peer_fingerprint, peer_session_id, seq, method, payload, created_at)
		 VALUES ('peerA', 's1', 1, 'm', '{}', 0)`).Error)
	assert.Error(t, db.Exec(
		`INSERT INTO daemon_notification_logs (peer_fingerprint, peer_session_id, seq, method, payload, created_at)
		 VALUES ('peerA', 's1', 1, 'm2', '{}', 0)`).Error, "duplicate (peer, session, seq) should violate primary key")
}

// TestMigration202608010001_Rollback 回滚:两张表都被删除。
func TestMigration202608010001_Rollback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	require.NoError(t, migration202608010001().Rollback(db))

	var n int
	db.Raw(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name IN ('daemon_sessions','daemon_notification_logs')`).Scan(&n)
	assert.Equal(t, 0, n)
}
