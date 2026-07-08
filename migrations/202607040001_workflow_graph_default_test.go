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
	// 这里只验证 202607040001 durable 贡献 = graph 列可用、且存在带 task 节点的流程图。
	var n int64
	assert.NoError(t, db.Table("workflows").Where("graph LIKE ?", `%"kind":"task"%`).Count(&n).Error)
	assert.GreaterOrEqual(t, n, int64(1))
}
