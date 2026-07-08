package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040002_AddsFlowOverlayColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// node_ref / flow_graph 两列都已被 202607080002 DROP(运行时进度 overlay 随步骤/DAG
	// 退场)。202607040002 已无 durable 贡献留存,这里只验证迁移链能干净跑通、列确已不在。
	assert.False(t, db.Migrator().HasColumn("orch_tasks", "node_ref"))
	assert.False(t, db.Migrator().HasColumn("orchestration_runs", "flow_graph"))
}
