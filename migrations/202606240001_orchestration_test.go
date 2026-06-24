package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n).Error)
	return n == 1
}

func columnExists(t *testing.T, db *gorm.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Raw(`PRAGMA table_info(` + table + `)`).Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == col {
			return true
		}
	}
	return false
}

func TestMigration202606240001_Orchestration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(db))

	require.True(t, tableExists(t, db, "orchestration_runs"))
	require.True(t, tableExists(t, db, "orch_tasks"))
	require.True(t, columnExists(t, db, "chat_sessions", "run_id"))

	// DEFAULT agent 应被种上 orchestrate 工具。
	var toolsJSON string
	require.NoError(t, db.Raw(
		`SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1`).Scan(&toolsJSON).Error)
	require.Contains(t, toolsJSON, `"orchestrate"`)
}
