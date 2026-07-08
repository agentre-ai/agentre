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

	// outline 列已被 202607080002 DROP(步骤/DAG 退场,流程库收窄为纯文本);
	// 这里只验证仍然存活的 durable 贡献 = tags 列。
	require.True(t, gdb.Migrator().HasColumn("workflows", "tags"),
		"expected workflows.tags column")
	require.False(t, gdb.Migrator().HasColumn("workflows", "outline"))

	// 默认 '[]' 可插入并读出。
	require.NoError(t, gdb.Exec(
		`INSERT INTO workflows(name,content,status,createtime,updatetime)
		 VALUES ('w','# w',1,0,0)`).Error)
	var tags string
	require.NoError(t, gdb.Raw(`SELECT tags FROM workflows WHERE name='w'`).
		Row().Scan(&tags))
	require.Equal(t, "[]", tags)
}
