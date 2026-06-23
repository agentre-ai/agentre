package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606160002ChatSessionsPurpose(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 前置:chat_sessions(006)。
	require.NoError(t, migration202605220006().Migrate(gdb))

	require.NoError(t, migration202606160002().Migrate(gdb))

	// 已有行 purpose 默认空串(普通顶层会话)。
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions (agent_id, title) VALUES (7, 'plain')`).Error)
	var purpose string
	require.NoError(t, gdb.Table("chat_sessions").Where("title = 'plain'").Pluck("purpose", &purpose).Error)
	require.Equal(t, "", purpose)

	// 子 agent 委派会话可写入标记。
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions (agent_id, title, purpose) VALUES (7, 'sub', 'subagent_call')`).Error)
	require.NoError(t, gdb.Table("chat_sessions").Where("title = 'sub'").Pluck("purpose", &purpose).Error)
	require.Equal(t, "subagent_call", purpose)

	// Rollback 干净:purpose 列消失。
	require.NoError(t, migration202606160002().Rollback(gdb))
	require.Error(t, gdb.Exec(`SELECT purpose FROM chat_sessions`).Error)
}
