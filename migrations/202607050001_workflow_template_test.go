package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607050001_BackfillsTemplate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// seed 的默认流程带 graph → 回填成占位符模板
	var row struct{ Template string }
	assert.NoError(t, db.Table("workflows").Where("is_default = 1").Scan(&row).Error)
	assert.Equal(t, "{{ DAGPrompt }}", row.Template)
}
