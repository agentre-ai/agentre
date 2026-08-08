package migrations

import (
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
)

// TestMigration202608080006_ChatSchema 钉死基线迁移 202608080006 的 chat_sessions
// 结构（决策 10：未发版直接改基线，不留 model_override 的建→删历史）。
//
//   - chat_sessions 必须含 provider_key 列（会话级供应商归属，空串 = 跟随 agent 绑定）；
//   - chat_sessions 不得再含 model_override 列（#26 会话级模型切换已整体移除）。
//
// 本测试是迁移自测（迁移机制唯一豁免 sqlmock 的例外之一），直接用真 SQLite 执行
// 该条迁移后按表结构断言。
func TestMigration202608080006_ChatSchema(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "chat.db")), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, migration202608080006().Migrate(db))

	require.True(t, db.Migrator().HasTable((&chat_entity.Session{}).TableName()), "基线迁移应建出 chat_sessions 表")
	require.True(t, db.Migrator().HasColumn((&chat_entity.Session{}).TableName(), "provider_key"),
		"基线迁移必须含 provider_key 列（会话级供应商归属）")
	require.False(t, db.Migrator().HasColumn((&chat_entity.Session{}).TableName(), "model_override"),
		"基线迁移不得再含 model_override 列（#26 已整体移除）")
}
