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
	return seedPairedAgentredAt(t, gormDB, name, fingerprint, "ws://10.0.0.1:9")
}

// seedPairedAgentredAt 同上,但可以指定 url——paired_agentreds 的唯一索引在 url 上,
// 同一台机器换个 url 再配对是「两个主键、同一个指纹」的来源。
func seedPairedAgentredAt(t *testing.T, gormDB *gorm.DB, name, fingerprint, url string) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO paired_agentreds
(name, url, daemon_fingerprint, instance_uuid, paired_at, status, createtime, updatetime)
VALUES (?, ?, ?, 'uuid-'||?, 0, 1, 0, 0)`, name, url, fingerprint, name).Error)
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

// TestMigration202608080013_DedupesRowsThatCollapseOntoOneFingerprint 锁住迁移必须
// 先去重再建索引:两条 ACTIVE 行的 device_id 不同、但反查出来是同一台 agentred 的
// 同一个指纹时,建部分唯一索引会直接失败。
//
// 这个前置状态完全可达:paired_agentreds 只在 url 上有部分唯一索引,从不对
// daemon_fingerprint 去重;解除配对是软删且**不清理**指向它的 project_locations 行;
// 同一台机器换个 url 再配对就会拿到一个新的 paired_agentreds 主键与同一个指纹。
// 迁移一旦在这里报错,RunMigrations 失败 → bootstrap 起不来 → 桌面端整个打不开,
// 且因为本迁移不在事务里、ID 也没进 ledger,下次启动会死在 ADD COLUMN 的重复列上。
func TestMigration202608080013_DedupesRowsThatCollapseOntoOneFingerprint(t *testing.T) {
	gormDB := openTestDB(t, "202608080012")
	// 同一台机器换个 url 再配对:两行配对、两个主键、同一个指纹。paired_agentreds
	// 只在 url 上有唯一索引,这个状态本来就允许存在。
	first := seedPairedAgentredAt(t, gormDB, "box-a", "fp-same-box", "ws://10.0.0.1:9")
	second := seedPairedAgentredAt(t, gormDB, "box-b", "fp-same-box", "ws://10.0.0.2:9")
	oldLoc := seedProjectLocation(t, gormDB, 1, strconv.FormatInt(first, 10), "/srv/old")
	newLoc := seedProjectLocation(t, gormDB, 1, strconv.FormatInt(second, 10), "/srv/new")

	require.NoError(t, RunMigrations(gormDB), "迁移必须先把塌缩到同一指纹的行去重,而不是让建索引失败")

	var active int64
	require.NoError(t, gormDB.Raw(
		`SELECT COUNT(*) FROM project_locations WHERE project_id = 1 AND daemon_fingerprint = 'fp-same-box' AND status = 1`,
	).Row().Scan(&active))
	assert.Equal(t, int64(1), active, "同一 (项目, 指纹) 上只允许留一条 ACTIVE 行")

	// 留下的必须是最后写入的那一条:它对应的配对行才是当前有效的那一个。
	var keptID int64
	require.NoError(t, gormDB.Raw(
		`SELECT id FROM project_locations WHERE project_id = 1 AND daemon_fingerprint = 'fp-same-box' AND status = 1`,
	).Row().Scan(&keptID))
	assert.Equal(t, newLoc, keptID, "保留较新的一条")
	assert.NotEqual(t, oldLoc, keptID)
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
