package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202605220010AddsProjectSortOrder(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, migration202605220005().Migrate(gdb))
	require.NoError(t, gdb.Exec(`INSERT INTO projects (id, parent_id, name, path, status) VALUES
		(10, 0, 'Root A', '/tmp/a', 1),
		(20, 0, 'Root B', '/tmp/b', 1),
		(30, 10, 'Child A', '/tmp/a/child', 1)`).Error)

	require.NoError(t, migration202605220010().Migrate(gdb))

	type row struct {
		ID        int64
		SortOrder int
	}
	var roots []row
	require.NoError(t, gdb.Table("projects").
		Select("id, sort_order").
		Where("parent_id = 0").
		Order("sort_order ASC, id ASC").
		Scan(&roots).Error)
	require.Equal(t, []row{{ID: 10, SortOrder: 10}, {ID: 20, SortOrder: 20}}, roots)

	var child row
	require.NoError(t, gdb.Table("projects").
		Select("id, sort_order").
		Where("id = ?", int64(30)).
		Scan(&child).Error)
	require.Equal(t, row{ID: 30, SortOrder: 30}, child)
}
