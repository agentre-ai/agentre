package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestMigration202608010001ChatSessionsRemoteCursor 钉死远端会话三列的语义:
// 迁移前就存在的老行必须默认「本机执行、无游标」(与今天行为一致),远端会话能把
// 执行位置 / daemon 实例标识 / 事件游标写进去再原样读回,回滚后三列干净消失。
func TestMigration202608010001ChatSessionsRemoteCursor(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
	// 先跑到本迁移之前,种一行「老数据」——它经历的是 ALTER TABLE 加列,而不是带默认值的 INSERT。
	require.NoError(t, m.MigrateTo("202607140001"))
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions (id, agent_id, title) VALUES (11, 7, 'legacy')`).Error)

	require.NoError(t, m.Migrate())

	// 老行 = 本机执行:配对 daemon 为 0、实例标识为空串、游标为 0。
	var legacy struct {
		ExecDeviceID          int64  `gorm:"column:exec_device_id"`
		ExecDaemonFingerprint string `gorm:"column:exec_daemon_fingerprint"`
		EventCursor           int64  `gorm:"column:event_cursor"`
	}
	require.NoError(t, gdb.Raw(
		`SELECT exec_device_id, exec_daemon_fingerprint, event_cursor FROM chat_sessions WHERE id = 11`,
	).Scan(&legacy).Error)
	require.Equal(t, int64(0), legacy.ExecDeviceID, "老会话必须仍表示本机执行")
	require.Equal(t, "", legacy.ExecDaemonFingerprint)
	require.Equal(t, int64(0), legacy.EventCursor)

	// 远端会话:执行位置 + daemon 实例标识 + 游标写得进、读得回。
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions
		(id, agent_id, title, exec_device_id, exec_daemon_fingerprint, event_cursor)
		VALUES (12, 7, 'remote', 3, 'sha256:beef', 42)`).Error)
	var remote struct {
		ExecDeviceID          int64  `gorm:"column:exec_device_id"`
		ExecDaemonFingerprint string `gorm:"column:exec_daemon_fingerprint"`
		EventCursor           int64  `gorm:"column:event_cursor"`
	}
	require.NoError(t, gdb.Raw(
		`SELECT exec_device_id, exec_daemon_fingerprint, event_cursor FROM chat_sessions WHERE id = 12`,
	).Scan(&remote).Error)
	require.Equal(t, int64(3), remote.ExecDeviceID)
	require.Equal(t, "sha256:beef", remote.ExecDaemonFingerprint)
	require.Equal(t, int64(42), remote.EventCursor)

	// 回滚干净:三列全部消失。
	require.NoError(t, m.RollbackLast())
	require.Error(t, gdb.Exec(`SELECT exec_device_id FROM chat_sessions`).Error)
	require.Error(t, gdb.Exec(`SELECT exec_daemon_fingerprint FROM chat_sessions`).Error)
	require.Error(t, gdb.Exec(`SELECT event_cursor FROM chat_sessions`).Error)
}
