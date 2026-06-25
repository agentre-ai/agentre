package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606250002(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	require.True(t, gdb.Migrator().HasColumn("workflows", "tags"),
		"expected workflows.tags column")
	require.True(t, gdb.Migrator().HasColumn("workflows", "outline"),
		"expected workflows.outline column")

	// 默认 '[]' 可插入并读出。
	require.NoError(t, gdb.Exec(
		`INSERT INTO workflows(name,content,status,createtime,updatetime)
		 VALUES ('w','# w',1,0,0)`).Error)
	var tags, outline string
	require.NoError(t, gdb.Raw(`SELECT tags, outline FROM workflows WHERE name='w'`).
		Row().Scan(&tags, &outline))
	require.Equal(t, "[]", tags)
	require.Equal(t, "[]", outline)
}
