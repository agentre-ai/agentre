package migrations

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// syncMetaTestTables 镜像 202608080016_sync_metadata.go 里的 syncMetaTables:
// 账号级七张表(366 行),迁移测试对每一张都跑同一份不变量,不抽样。
var syncMetaTestTables = []string{
	"projects",
	"departments",
	"agents",
	"agent_backends",
	"project_agents",
	"project_locations",
	"agent_exec_targets",
}

// TestMigration202608080016_EmptySyncIDsDoNotCollide 锁住部分唯一索引的存在
// 理由(366 行):未登录/迁移前创建的行 sync_id 留空,本仓 DDL 一律 not null
// default ”,若唯一索引不是 WHERE sync_id != ” 的部分索引,第二条空标识行就
// 会撞唯一约束插不进去 —— 这是 R1/R12a 的硬前提。
func TestMigration202608080016_EmptySyncIDsDoNotCollide(t *testing.T) {
	gormDB := openTestDB(t, "202608080015")
	require.NoError(t, RunMigrations(gormDB))

	seeds := map[string]string{
		"projects":           `INSERT INTO projects (parent_id, name, path, status, createtime, updatetime) VALUES (0, ?, '/tmp', 1, 0, 0)`,
		"departments":        `INSERT INTO departments (name, status, createtime, updatetime) VALUES (?, 1, 0, 0)`,
		"agents":             `INSERT INTO agents (name, department_id, status, createtime, updatetime) VALUES (?, 1, 1, 0, 0)`,
		"agent_backends":     `INSERT INTO agent_backends (type, name, status, createtime, updatetime) VALUES ('builtin', ?, 1, 0, 0)`,
		"project_agents":     `INSERT INTO project_agents (project_id, agent_id, joined_at) VALUES (?, ?, 0)`,
		"project_locations":  `INSERT INTO project_locations (project_id, daemon_fingerprint, path, status, createtime, updatetime) VALUES (?, 'fp', '/tmp', 1, 0, 0)`,
		"agent_exec_targets": `INSERT INTO agent_exec_targets (agent_id, agent_backend_id, sort_order) VALUES (?, 1, ?)`,
	}

	for _, table := range syncMetaTestTables {
		t.Run(table, func(t *testing.T) {
			var err1, err2 error
			switch table {
			case "project_agents":
				err1 = gormDB.Exec(seeds[table], 100, 1).Error
				err2 = gormDB.Exec(seeds[table], 100, 2).Error
			case "agent_exec_targets":
				err1 = gormDB.Exec(seeds[table], 200, 0).Error
				err2 = gormDB.Exec(seeds[table], 200, 1).Error
			default:
				err1 = gormDB.Exec(seeds[table], "row-1-"+table).Error
				err2 = gormDB.Exec(seeds[table], "row-2-"+table).Error
			}
			require.NoError(t, err1, "第一条空 sync_id 行必须能插入: %s", table)
			require.NoError(t, err2, "第二条空 sync_id 行不能撞唯一索引(必须是部分索引): %s", table)

			var count int64
			require.NoError(t, gormDB.Raw(fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE sync_id = ''`, table)).Row().Scan(&count))
			assert.GreaterOrEqual(t, count, int64(2), "%s 应该能容纳至少两行空 sync_id", table)
		})
	}
}

// TestMigration202608080016_NonEmptySyncIDsAreUnique 唯一索引仍然对非空标识生效:
// 同一张表里两行相同的非空 sync_id 必须被拒绝——否则同一个同步标识在本地对应
// 两条不同的行,违反 R1「跨机身份稳定」。
func TestMigration202608080016_NonEmptySyncIDsAreUnique(t *testing.T) {
	gormDB := openTestDB(t, "202608080015")
	require.NoError(t, RunMigrations(gormDB))

	require.NoError(t, gormDB.Exec(
		`INSERT INTO departments (name, sync_id, status, createtime, updatetime) VALUES (?, ?, 1, 0, 0)`,
		"dept-a", "dup-sync-id",
	).Error)

	err := gormDB.Exec(
		`INSERT INTO departments (name, sync_id, status, createtime, updatetime) VALUES (?, ?, 1, 0, 0)`,
		"dept-b", "dup-sync-id",
	).Error
	assert.Error(t, err, "重复的非空 sync_id 必须撞唯一索引")
}

// TestMigration202608080016_ExistingRowsDefaultToUnclaimed 迁移前已存在的历史行
// (以及迁移本身不做任何回填)落在 sync_account_id = 0——尚未被任何账号认领,
// 直到后续任务的「首次登录认领」把它们收进当前账号(R12a)。
func TestMigration202608080016_ExistingRowsDefaultToUnclaimed(t *testing.T) {
	gormDB := openTestDB(t, "202608080015")
	require.NoError(t, gormDB.Exec(
		`INSERT INTO departments (name, status, createtime, updatetime) VALUES ('legacy', 1, 0, 0)`,
	).Error)

	require.NoError(t, RunMigrations(gormDB))

	var syncID string
	var accountID, version, updatedAt, deletedAt int64
	var origin string
	require.NoError(t, gormDB.Raw(
		`SELECT sync_id, sync_account_id, sync_version, sync_updated_at, sync_origin, sync_deleted_at FROM departments WHERE name = 'legacy'`,
	).Row().Scan(&syncID, &accountID, &version, &updatedAt, &origin, &deletedAt))

	assert.Equal(t, "", syncID, "迁移不回填历史行的 sync_id——生成时机是行创建/下一次落库时")
	assert.Equal(t, int64(0), accountID)
	assert.Equal(t, int64(0), version)
	assert.Equal(t, int64(0), updatedAt)
	assert.Equal(t, "", origin)
	assert.Equal(t, int64(0), deletedAt)
}
