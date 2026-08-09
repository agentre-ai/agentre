package migrations

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// seedPairedAgentred 直接写 paired_agentreds 行(绕开仓储),模拟本机已配对的 agentred。
func seedPairedAgentred(t *testing.T, gormDB *gorm.DB, name, fingerprint string) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO paired_agentreds
(name, url, daemon_fingerprint, instance_uuid, paired_at, status, createtime, updatetime)
VALUES (?, 'ws://10.0.0.1:9', ?, 'uuid-'||?, 0, 1, 0, 0)`, name, fingerprint, name).Error)
	var id int64
	require.NoError(t, gormDB.Raw(`SELECT id FROM paired_agentreds WHERE name = ?`, name).Row().Scan(&id))
	return id
}

// seedProjectLocation 直接写 project_locations 行(绕开仓储),模拟本轮迁移之前既有
// 库里按旧自然键 (project_id, device_id) 落的数据。
func seedProjectLocation(t *testing.T, gormDB *gorm.DB, projectID int64, deviceID, path string) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, path, status, createtime, updatetime)
VALUES (?, ?, ?, 1, 0, 0)`, projectID, deviceID, path).Error)
	var id int64
	require.NoError(t, gormDB.Raw(
		`SELECT id FROM project_locations WHERE project_id = ? AND device_id = ? AND path = ?`,
		projectID, deviceID, path,
	).Row().Scan(&id))
	return id
}

// TestMigration202608080013_BackfillsFingerprintFromDeviceID 锁住决策 26 的迁移语义:
// 既有行按现有 device_id 反查 paired_agentreds 换算出 daemon_fingerprint。
func TestMigration202608080013_BackfillsFingerprintFromDeviceID(t *testing.T) {
	gormDB := openTestDB(t, "202608080012")
	deviceID := seedPairedAgentred(t, gormDB, "linux-box", "fp-linux-box")
	locID := seedProjectLocation(t, gormDB, 1, strconv.FormatInt(deviceID, 10), "/home/me/foo")

	require.NoError(t, RunMigrations(gormDB))

	var fingerprint, deviceIDCol string
	require.NoError(t, gormDB.Raw(
		`SELECT daemon_fingerprint, device_id FROM project_locations WHERE id = ?`, locID,
	).Row().Scan(&fingerprint, &deviceIDCol))
	assert.Equal(t, "fp-linux-box", fingerprint, "既有行的指纹必须从 device_id 反查回填")
	assert.Equal(t, strconv.FormatInt(deviceID, 10), deviceIDCol, "device_id 缓存列原样保留")
}

// TestMigration202608080013_DropsOrphanedDeviceIDRows 锁住迁移对不可能状态的处理:
// device_id 指向一台已经不存在的 paired_agentreds 时反查不到指纹,这类行本项目
// 未发布不必兼容,直接丢弃,不留一个空指纹的行去撞新的部分唯一索引。
func TestMigration202608080013_DropsOrphanedDeviceIDRows(t *testing.T) {
	gormDB := openTestDB(t, "202608080012")
	locID := seedProjectLocation(t, gormDB, 1, "999", "/orphaned/path")

	require.NoError(t, RunMigrations(gormDB))

	var count int64
	require.NoError(t, gormDB.Raw(`SELECT COUNT(*) FROM project_locations WHERE id = ?`, locID).Row().Scan(&count))
	assert.Equal(t, int64(0), count, "反查不到指纹的孤儿行必须被丢弃")
}

// TestMigration202608080013_UnresolvedRowsWithDifferentFingerprintsCoexist 锁住「路径记录
// 去重」测试接缝的桌面端那一半: 同一项目下、device_id 都为空(未配对)但指纹不同的
// 多条记录必须能并存,不撞新的部分唯一索引。
func TestMigration202608080013_UnresolvedRowsWithDifferentFingerprintsCoexist(t *testing.T) {
	gormDB := openTestDB(t, "202608080013")

	require.NoError(t, gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '', 'fp-a', '/srv/a', 1, 0, 0)`).Error)
	require.NoError(t, gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '', 'fp-b', '/srv/b', 1, 0, 0)`).Error, "不同指纹、同一项目、都未解析的两行必须能并存")

	var count int64
	require.NoError(t, gormDB.Raw(`SELECT COUNT(*) FROM project_locations WHERE project_id = 1 AND status = 1`).Row().Scan(&count))
	assert.Equal(t, int64(2), count)
}

// TestMigration202608080013_NewIndexRejectsDuplicateFingerprintPerProject 反向验证新索引
// 真的在管——同一项目下两条 ACTIVE 行撞同一个 daemon_fingerprint 必须被拒绝
// (旧索引按 device_id 去重,已经不再是自然键)。
func TestMigration202608080013_NewIndexRejectsDuplicateFingerprintPerProject(t *testing.T) {
	gormDB := openTestDB(t, "202608080013")

	require.NoError(t, gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '7', 'fp-dup', '/srv/a', 1, 0, 0)`).Error)

	err := gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '8', 'fp-dup', '/srv/b', 1, 0, 0)`).Error
	assert.Error(t, err, "同一项目下两条 ACTIVE 行撞同一指纹必须被新的部分唯一索引拒绝")
}
