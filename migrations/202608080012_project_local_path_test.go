package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration202608080012_ExistingProjectsDefaultToConfigured 锁住 R10 / 决策 21:
// 迁移前已存在的项目行(必然已经有本机路径)迁移后落在「已配置」态 —— 新列的
// 默认值不能把老项目误标成未配置。
func TestMigration202608080012_ExistingProjectsDefaultToConfigured(t *testing.T) {
	gormDB := openTestDB(t, "202608080011")
	require.NoError(t, gormDB.Exec(`INSERT INTO projects (parent_id, name, path, status, createtime, updatetime)
VALUES (0, 'existing', '/Users/foo/code', 1, 0, 0)`).Error)

	require.NoError(t, RunMigrations(gormDB))

	var missing bool
	require.NoError(t, gormDB.Raw(
		`SELECT local_path_missing FROM projects WHERE name = 'existing'`,
	).Row().Scan(&missing))
	assert.False(t, missing, "已有本机路径的项目迁移后必须仍是已配置态")
}

// TestMigration202608080012_UnconfiguredRowRoundTrips 验证新列能承载「未配置」态:
// path 恒为空串、local_path_missing 为 true,读写往返不丢信息(为后续同步落地的
// 项目行铺路,本迁移本身不产生这类行)。
func TestMigration202608080012_UnconfiguredRowRoundTrips(t *testing.T) {
	gormDB := openTestDB(t, "202608080012")
	require.NoError(t, gormDB.Exec(`INSERT INTO projects
(parent_id, name, path, local_path_missing, status, createtime, updatetime)
VALUES (0, 'synced', '', 1, 1, 0, 0)`).Error)

	var path string
	var missing bool
	require.NoError(t, gormDB.Raw(
		`SELECT path, local_path_missing FROM projects WHERE name = 'synced'`,
	).Row().Scan(&path, &missing))
	assert.Equal(t, "", path)
	assert.True(t, missing)
}
