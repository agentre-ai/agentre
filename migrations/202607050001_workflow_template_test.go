package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607050001_AddsTemplateColumn(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// 旧默认流程(占位符模板)已被 202607080001 取代;template 列本身又被 202607080002
	// DROP({{ DAGPrompt }} 模板机制随步骤/DAG 退场)。202607050001 已无 durable 贡献
	// 留存,这里只验证迁移链能干净跑通、列确已不在。
	assert.False(t, db.Migrator().HasColumn("workflows", "template"))
}
