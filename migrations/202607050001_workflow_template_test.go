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

	// 旧默认流程(占位符模板)已被 202607080001 取代;这里验证 template 列可用、
	// 且带图流程有非空 template(新内置流程为手写全文)。
	var n int64
	assert.NoError(t, db.Table("workflows").Where("graph != '' AND template != ''").Count(&n).Error)
	assert.GreaterOrEqual(t, n, int64(1))
}
