package migrations

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// 编排能力于 202607140001 整体移除后,原属本文件的
// TestMigration202606240001_Orchestration 已删除;此文件仅保留
// tableExists / columnExists 两个迁移测试共享助手(drop_group 等仍在用)。

func tableExists(t *testing.T, db *gorm.DB, name string) bool {
	t.Helper()
	var n int
	require.NoError(t, db.Raw(
		`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n).Error)
	return n == 1
}

func columnExists(t *testing.T, db *gorm.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Raw(`PRAGMA table_info(` + table + `)`).Rows()
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull, pk int
		var dflt any
		require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
		if name == col {
			return true
		}
	}
	return false
}
