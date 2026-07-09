package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607090002_CreatesOrchTasksChecklist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	hasTable := func(table string) bool {
		var n int
		db.Raw(
			"SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = ?",
			table,
		).Scan(&n)
		return n > 0
	}
	hasCol := func(table, col string) bool {
		var n int
		db.Raw(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
			table, col,
		).Scan(&n)
		return n > 0
	}

	// orch_tasks 复用被 202607090001 腾空的表名,重建为待办清单(与派发树零联动)。
	assert.True(t, hasTable("orch_tasks"))
	assert.True(t, hasCol("orch_tasks", "text"))
	assert.True(t, hasCol("orch_tasks", "status"))
	assert.True(t, hasCol("orch_tasks", "assignee_agent_id"))

	// orch_dispatches(202607090001 改名后的执行节点表)仍在,两表并存。
	assert.True(t, hasTable("orch_dispatches"))
}
