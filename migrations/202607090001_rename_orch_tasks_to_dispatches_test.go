package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607090001_RenameOrchTasksToDispatches(t *testing.T) {
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

	// orch_tasks 改名为 orch_dispatches, parent_task_id 改名为 parent_dispatch_id。
	assert.True(t, hasTable("orch_dispatches"))
	assert.False(t, hasTable("orch_tasks"))
	assert.True(t, hasCol("orch_dispatches", "parent_dispatch_id"))
	assert.False(t, hasCol("orch_dispatches", "parent_task_id"))
}
