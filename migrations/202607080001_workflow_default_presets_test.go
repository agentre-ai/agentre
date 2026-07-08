package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607080001_SeedsPresetFlows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	type row struct {
		Name, Content, Tags string
	}
	var rows []row
	assert.NoError(t, db.Table("workflows").Select("name,content,tags").Order("updatetime DESC").Scan(&rows).Error)

	// 恰好 4 个内置流程,顺序(updatetime DESC)= Parallel Decompose 第一
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	assert.Equal(t, []string{
		"Parallel Decompose",
		"Sequential Pipeline",
		"Research → Synthesize",
		"Generate → Review → Iterate",
	}, names)

	// is_default 列已 DROP
	var cols []struct{ Name string }
	assert.NoError(t, db.Raw("PRAGMA table_info(workflows)").Scan(&cols).Error)
	for _, c := range cols {
		assert.NotEqual(t, "is_default", c.Name)
	}

	// 每行 content / tags 非空
	for _, r := range rows {
		assert.NotEmpty(t, r.Content, r.Name)
		assert.NotEmpty(t, r.Tags, r.Name)
	}
}
