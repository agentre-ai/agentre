package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040001_AddsGraphColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// 旧内置「Default Orchestration Flow」已被 202607080001 取代并删除;
	// graph 列本身又被 202607080002 DROP(步骤/DAG 退场)。202607040001 已无 durable
	// 贡献留存,这里只验证迁移链能干净跑通、列确已不在。
	assert.False(t, db.Migrator().HasColumn("workflows", "graph"))
}
