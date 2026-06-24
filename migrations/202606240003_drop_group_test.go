package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606240003_DropGroup(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	for _, tb := range []string{"groups", "group_members", "group_messages", "group_tasks"} {
		require.False(t, tableExists(t, db, tb), tb+" should be dropped")
	}
	require.False(t, columnExists(t, db, "chat_sessions", "group_id"), "chat_sessions.group_id should be dropped")
	require.True(t, tableExists(t, db, "workflows"), "workflows (flow library) must be kept")

	var toolsJSON string
	require.NoError(t, db.Raw(`SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1`).Scan(&toolsJSON).Error)
	require.NotContains(t, toolsJSON, "group_create")
	require.Contains(t, toolsJSON, `"orchestrate"`)
}
