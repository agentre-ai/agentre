package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202605220011CreatesIssueTablesAndSeedsLabels(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, migration202605220011().Migrate(gdb))

	require.NoError(t, gdb.Exec(`INSERT INTO issues (id, title) VALUES (1, 'demo')`).Error)
	require.NoError(t, gdb.Exec(`INSERT INTO issue_labels (issue_id, label_id) VALUES (1, 1)`).Error)

	var count int64
	require.NoError(t, gdb.Table("labels").Where("status = 1").Count(&count).Error)
	require.Equal(t, int64(10), count)

	var tone string
	require.NoError(t, gdb.Table("labels").Select("tone").Where("name = ?", "bug").Scan(&tone).Error)
	require.Equal(t, "bug", tone)

	// 幂等：重跑不重复 seed
	require.NoError(t, migration202605220011().Migrate(gdb))
	require.NoError(t, gdb.Table("labels").Where("status = 1").Count(&count).Error)
	require.Equal(t, int64(10), count)
}
