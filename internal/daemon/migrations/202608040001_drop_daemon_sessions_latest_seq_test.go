package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMigration202608040001_DropsLatestSeqAndKeepsTheRest 迁移后 daemon_sessions 上
// 不再有 latest_seq 这一列,而会话行本身(身份 / 元数据 / 生命周期)一列不少。
//
// 会漏掉它的实现:把列留着「以后可能用得上」。留着的后果不是多占几个字节 —— 「某会话
// 最新的 seq」的真相源是通知日志的 MAX(seq),而这一列没有任何写入方,读它永远得到 0。
// 一个照着列名去读的调用方会让客户端每次重连都从 0 重拉整段日志。
func TestMigration202608040001_DropsLatestSeqAndKeepsTheRest(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	assert.False(t, hasColumn(db, "daemon_sessions", "latest_seq"),
		"latest_seq 没有写入方,读它只会永远报 0 —— 列必须真的没了")
	for _, col := range []string{
		"peer_fingerprint", "peer_session_id", "agent_id", "cwd",
		"backend_type", "lifecycle_state", "created_at", "updated_at",
	} {
		assert.True(t, hasColumn(db, "daemon_sessions", col), "daemon_sessions missing column %s", col)
	}
}

// TestMigration202608040001_PreservesExistingSessionRows 已有会话行不能被这次删列
// 抹掉:daemon 重启后靠 daemon_sessions 判断哪些会话要标成中断(R10),丢行等于把
// 一批远端会话的身份连同它们的日志一起变成孤儿。
func TestMigration202608040001_PreservesExistingSessionRows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	// 先只跑到删列之前那一版,写一行进去,再跑完剩下的迁移。
	require.NoError(t, migration202608010001().Migrate(db))
	require.NoError(t, db.Exec(
		`INSERT INTO daemon_sessions
		 (peer_fingerprint, peer_session_id, agent_id, cwd, backend_type, lifecycle_state, latest_seq, created_at, updated_at)
		 VALUES ('peerA', 's1', 7, '/tmp/w', 'claudecode', 'running', 42, 100, 200)`).Error)

	require.NoError(t, RunMigrations(db))

	var row struct {
		PeerFingerprint string `gorm:"column:peer_fingerprint"`
		PeerSessionID   string `gorm:"column:peer_session_id"`
		AgentID         int64  `gorm:"column:agent_id"`
		Cwd             string `gorm:"column:cwd"`
		BackendType     string `gorm:"column:backend_type"`
		LifecycleState  string `gorm:"column:lifecycle_state"`
	}
	require.NoError(t, db.Raw(`SELECT peer_fingerprint, peer_session_id, agent_id, cwd, backend_type, lifecycle_state
		FROM daemon_sessions`).Scan(&row).Error)
	assert.Equal(t, "peerA", row.PeerFingerprint)
	assert.Equal(t, "s1", row.PeerSessionID)
	assert.Equal(t, int64(7), row.AgentID)
	assert.Equal(t, "/tmp/w", row.Cwd)
	assert.Equal(t, "claudecode", row.BackendType)
	assert.Equal(t, "running", row.LifecycleState)
}

// TestMigration202608040001_Rollback 回滚把列加回来(带默认值 0),让降级到旧版本
// agentred 时那条 INSERT 仍然能写。
func TestMigration202608040001_Rollback(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	require.NoError(t, migration202608040001().Rollback(db))

	assert.True(t, hasColumn(db, "daemon_sessions", "latest_seq"))
}

// hasColumn 报告某表上有没有这一列。
func hasColumn(db *gorm.DB, table, col string) bool {
	var n int
	db.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col).Scan(&n)
	return n > 0
}
