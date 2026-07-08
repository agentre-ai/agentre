package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607080002_DropsFlowDagColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	hasCol := func(table, col string) bool {
		var n int
		db.Raw(
			"SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?",
			table, col,
		).Scan(&n)
		return n > 0
	}

	// DAG/步骤列已被删除
	assert.False(t, hasCol("workflows", "graph"))
	assert.False(t, hasCol("workflows", "template"))
	assert.False(t, hasCol("workflows", "outline"))
	assert.False(t, hasCol("orchestration_runs", "flow_graph"))
	// 表名实际为 orch_tasks(见 202607040002),非计划文档笔误的 tasks。
	assert.False(t, hasCol("orch_tasks", "node_ref"))

	// 保留列仍在,且 4 内置流程正文存活
	assert.True(t, hasCol("workflows", "content"))
	var cnt int
	db.Raw("SELECT COUNT(*) FROM workflows WHERE content <> ''").Scan(&cnt)
	assert.GreaterOrEqual(t, cnt, 4)
}
