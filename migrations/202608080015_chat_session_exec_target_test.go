package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration202608080015_ExistingSessionsDefaultToUnpinned 锁住"没值涵盖首轮与
// 全部老会话，因此不需要数据迁移"：迁移前已存在的会话行迁移后新列落在 0（未钉住），
// 派发链路据此回落到按 R15 顺序挑一档并写回。
func TestMigration202608080015_ExistingSessionsDefaultToUnpinned(t *testing.T) {
	gormDB := openTestDB(t, "202608080014")
	require.NoError(t, gormDB.Exec(`INSERT INTO chat_sessions
(agent_id, title, agent_status, status, createtime, updatetime)
VALUES (1, 'existing', 'idle', 1, 0, 0)`).Error)

	require.NoError(t, RunMigrations(gormDB))

	var execAgentBackendID int64
	require.NoError(t, gormDB.Raw(
		`SELECT exec_agent_backend_id FROM chat_sessions WHERE title = 'existing'`,
	).Row().Scan(&execAgentBackendID))
	assert.Equal(t, int64(0), execAgentBackendID, "老会话必须落在未钉住状态,不能凭空回填一档")
}

// TestMigration202608080015_ColumnRoundTrips 验证新列能承载"已经钉住某一档"的状态，
// 与既有的 exec_device_id / exec_daemon_fingerprint 并列同写、读写往返不丢信息。
func TestMigration202608080015_ColumnRoundTrips(t *testing.T) {
	gormDB := openTestDB(t, "202608080015")
	require.NoError(t, gormDB.Exec(`INSERT INTO chat_sessions
(agent_id, title, agent_status, exec_device_id, exec_daemon_fingerprint, exec_agent_backend_id, status, createtime, updatetime)
VALUES (1, 'pinned', 'idle', 3, 'sha256:beef', 51, 1, 0, 0)`).Error)

	var deviceID, backendID int64
	var fingerprint string
	require.NoError(t, gormDB.Raw(
		`SELECT exec_device_id, exec_daemon_fingerprint, exec_agent_backend_id FROM chat_sessions WHERE title = 'pinned'`,
	).Row().Scan(&deviceID, &fingerprint, &backendID))
	assert.Equal(t, int64(3), deviceID)
	assert.Equal(t, "sha256:beef", fingerprint)
	assert.Equal(t, int64(51), backendID)
}
