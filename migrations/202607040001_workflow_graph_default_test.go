package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040001_SeedsDefaultFlow(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	var count int64
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Count(&count).Error)
	assert.Equal(t, int64(1), count)

	var row struct {
		Name    string
		Content string
		Graph   string
	}
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Scan(&row).Error)
	assert.Equal(t, "Default Orchestration Flow", row.Name)
	assert.Contains(t, row.Content, "finish with a summary @user")
	assert.Contains(t, row.Graph, "\"kind\":\"task\"")
}
