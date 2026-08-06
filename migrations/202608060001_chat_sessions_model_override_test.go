package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202608060001ChatSessionsModelOverride(t *testing.T) {
	t.Run("model_override 追加到 migrationList 末尾且全量迁移可跑", func(t *testing.T) {
		list := migrationList()
		require.NotEmpty(t, list)
		require.Equal(t, "202608060001", list[len(list)-1].ID, "新迁移必须 append 到 migrationList() 末尾")
	})

	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 前置:chat_sessions(006)。
	require.NoError(t, migration202605220006().Migrate(gdb))
	require.NoError(t, migration202608060001().Migrate(gdb))

	// 已有行 model_override 默认空串(跟随供应商默认)。
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions (agent_id, title) VALUES (7, 'plain')`).Error)
	var override string
	require.NoError(t, gdb.Table("chat_sessions").Where("title = 'plain'").Pluck("model_override", &override).Error)
	require.Equal(t, "", override)

	// 可写入会话级覆盖并回读。
	require.NoError(t, gdb.Exec(`INSERT INTO chat_sessions (agent_id, title, model_override) VALUES (7, 'override', 'claude-sonnet-4-5')`).Error)
	require.NoError(t, gdb.Table("chat_sessions").Where("title = 'override'").Pluck("model_override", &override).Error)
	require.Equal(t, "claude-sonnet-4-5", override)

	// Rollback 干净:model_override 列消失。
	require.NoError(t, migration202608060001().Rollback(gdb))
	require.Error(t, gdb.Exec(`SELECT model_override FROM chat_sessions`).Error)
}
