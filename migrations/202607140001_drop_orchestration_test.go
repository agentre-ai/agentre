package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607140001_DropsOrchestration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

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

	// 编排 + 流程库表全部被删。
	assert.False(t, hasTable("orchestration_runs"))
	assert.False(t, hasTable("orch_dispatches"))
	assert.False(t, hasTable("orch_tasks"))
	assert.False(t, hasTable("workflows"))
	// chat_sessions.run_id 列被删。
	assert.False(t, hasCol("chat_sessions", "run_id"))

	// DEFAULT agent 的 tools_json 不再含 orchestrate/workflow(org 等保留)。
	var tools string
	db.Raw("SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1").Scan(&tools)
	assert.NotContains(t, tools, `"orchestrate"`)
	assert.NotContains(t, tools, `"workflow"`)
	assert.Contains(t, tools, `"org"`)
}
