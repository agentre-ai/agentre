package migrations

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	dbpkg "github.com/cago-frame/cago/database/db"

	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
)

// openTestDB 开一个临时 SQLite，并只跑到 upTo 这条迁移为止（不含之后的），用来构造
// 「本轮迁移之前」的库。迁移自身的测试是「仓储单测一律 sqlmock」的既有例外。
func openTestDB(t *testing.T, upTo string) *gorm.DB {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)

	all := migrationList()
	prefix := make([]*gormigrate.Migration, 0, len(all))
	for _, m := range all {
		prefix = append(prefix, m)
		if m.ID == upTo {
			break
		}
	}
	require.Equal(t, upTo, prefix[len(prefix)-1].ID, "upTo migration not found in migrationList")
	require.NoError(t, gormigrate.New(gormDB, gormigrate.DefaultOptions, prefix).Migrate())
	return gormDB
}

// seedAgent 直接写 agents 行（绕开仓储），模拟迁移前既有库里的数据。
func seedAgent(t *testing.T, gormDB *gorm.DB, name string, backendID int64) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO agents (name, department_id, agent_backend_id, status, createtime, updatetime)
VALUES (?, 1, ?, 1, 0, 0)`, name, backendID).Error)
	var id int64
	require.NoError(t, gormDB.Raw(`SELECT id FROM agents WHERE name = ?`, name).Row().Scan(&id))
	return id
}

// seedPairedAgentredAt 直接写 paired_agentreds 行（绕开仓储），模拟本机已配对的
// agentred；paired_agentreds 的唯一索引在 url 上，「同一台机器换个 url 再配对」是
// 两个主键、同一个指纹的来源。
func seedPairedAgentredAt(t *testing.T, gormDB *gorm.DB, name, fingerprint, url string) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO paired_agentreds
(name, url, daemon_fingerprint, instance_uuid, paired_at, status, createtime, updatetime)
VALUES (?, ?, ?, 'uuid-'||?, 0, 1, 0, 0)`, name, url, fingerprint, name).Error)
	var id int64
	require.NoError(t, gormDB.Raw(`SELECT id FROM paired_agentreds WHERE name = ?`, name).Row().Scan(&id))
	return id
}

// seedProjectLocation 直接写 project_locations 行（绕开仓储），模拟迁移前按旧自然键
// (project_id, device_id) 落的数据。
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

// TestMigration202608080011_BackfillsSingleElementExecTargets 锁住 R15 的回填语义:
// 每个 Agent 现有的 agents.agent_backend_id 转成 agent_exec_targets 里的单元素列表
// (sort_order = 0);agent_backend_id = 0 的 Agent 转成空列表。
func TestMigration202608080011_BackfillsSingleElementExecTargets(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	claudeAgent := seedAgent(t, gormDB, "Claude Agent", 7)
	codexAgent := seedAgent(t, gormDB, "Codex Agent", 9)
	noBackendAgent := seedAgent(t, gormDB, "No Backend Agent", 0)

	require.NoError(t, RunMigrations(gormDB))

	type targetRow struct {
		AgentBackendID int64
		SortOrder      int
	}
	rowsOf := func(agentID int64) []targetRow {
		var out []targetRow
		require.NoError(t, gormDB.Raw(
			`SELECT agent_backend_id, sort_order FROM agent_exec_targets WHERE agent_id = ? ORDER BY sort_order ASC`,
			agentID,
		).Scan(&out).Error)
		return out
	}

	assert.Equal(t, []targetRow{{AgentBackendID: 7, SortOrder: 0}}, rowsOf(claudeAgent))
	assert.Equal(t, []targetRow{{AgentBackendID: 9, SortOrder: 0}}, rowsOf(codexAgent))
	assert.Empty(t, rowsOf(noBackendAgent), "agent_backend_id = 0 的 Agent 必须转成空列表")
}

// TestMigration202608080011_DispatchResolvesSameBackend 锁住等价性:转换之后,派发
// 读路径(chat_svc 的每一处都经由 agent_repo.Agent().Find 拿 AgentBackendID)取到的
// backend 与转换之前逐字节一致;没有执行目标行的 Agent 取到 0。
func TestMigration202608080011_DispatchResolvesSameBackend(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	claudeAgent := seedAgent(t, gormDB, "Claude Agent", 7)
	noBackendAgent := seedAgent(t, gormDB, "No Backend Agent", 0)

	require.NoError(t, RunMigrations(gormDB))
	// 迁移之后把历史列清空:读路径只能从执行目标行派生,不许回落旧列。
	require.NoError(t, gormDB.Exec(`UPDATE agents SET agent_backend_id = 0`).Error)

	ctx := dbpkg.WithContextDB(context.Background(), gormDB)
	repo := agent_repo.NewAgent()

	got, err := repo.Find(ctx, claudeAgent)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(7), got.AgentBackendID)

	empty, err := repo.Find(ctx, noBackendAgent)
	require.NoError(t, err)
	require.NotNil(t, empty)
	assert.Equal(t, int64(0), empty.AgentBackendID)
}

// TestMigration202608080011_ExistingProjectsDefaultToConfigured 锁住 R10 / 决策 21:
// 迁移前已存在的项目行(必然已经有本机路径)迁移后落在「已配置」态 —— 新列的
// 默认值不能把老项目误标成未配置。
func TestMigration202608080011_ExistingProjectsDefaultToConfigured(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	require.NoError(t, gormDB.Exec(`INSERT INTO projects (parent_id, name, path, status, createtime, updatetime)
VALUES (0, 'existing', '/Users/foo/code', 1, 0, 0)`).Error)

	require.NoError(t, RunMigrations(gormDB))

	var missing bool
	require.NoError(t, gormDB.Raw(
		`SELECT local_path_missing FROM projects WHERE name = 'existing'`,
	).Row().Scan(&missing))
	assert.False(t, missing, "已有本机路径的项目迁移后必须仍是已配置态")
}

// TestMigration202608080011_BackfillsFingerprintFromDeviceID 锁住决策 26 的迁移语义:
// 既有行按现有 device_id 反查 paired_agentreds 换算出 daemon_fingerprint,device_id
// 缓存列原样保留。
func TestMigration202608080011_BackfillsFingerprintFromDeviceID(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	deviceID := seedPairedAgentredAt(t, gormDB, "linux-box", "fp-linux-box", "ws://10.0.0.1:9")
	locID := seedProjectLocation(t, gormDB, 1, strconv.FormatInt(deviceID, 10), "/home/me/foo")

	require.NoError(t, RunMigrations(gormDB))

	var fingerprint, deviceIDCol string
	require.NoError(t, gormDB.Raw(
		`SELECT daemon_fingerprint, device_id FROM project_locations WHERE id = ?`, locID,
	).Row().Scan(&fingerprint, &deviceIDCol))
	assert.Equal(t, "fp-linux-box", fingerprint, "既有行的指纹必须从 device_id 反查回填")
	assert.Equal(t, strconv.FormatInt(deviceID, 10), deviceIDCol, "device_id 缓存列原样保留")
}

// TestMigration202608080011_DropsOrphanedDeviceIDRows 锁住迁移对不可能状态的处理:
// device_id 指向一台已经不存在的 paired_agentreds 时反查不到指纹,这类行本项目
// 未发布不必兼容,直接丢弃,不留一个空指纹的行去撞新的部分唯一索引。
func TestMigration202608080011_DropsOrphanedDeviceIDRows(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	locID := seedProjectLocation(t, gormDB, 1, "999", "/orphaned/path")

	require.NoError(t, RunMigrations(gormDB))

	var count int64
	require.NoError(t, gormDB.Raw(`SELECT COUNT(*) FROM project_locations WHERE id = ?`, locID).Row().Scan(&count))
	assert.Equal(t, int64(0), count, "反查不到指纹的孤儿行必须被丢弃")
}

// TestMigration202608080011_DedupesRowsThatCollapseOntoOneFingerprint 锁住迁移必须
// 先去重再建索引:两条 ACTIVE 行的 device_id 不同、但反查出来是同一台 agentred 的
// 同一个指纹时,建部分唯一索引会直接失败。
//
// 这个前置状态完全可达:paired_agentreds 只在 url 上有部分唯一索引,从不对
// daemon_fingerprint 去重;解除配对是软删且**不清理**指向它的 project_locations 行;
// 同一台机器换个 url 再配对就会拿到一个新的 paired_agentreds 主键与同一个指纹。
// 迁移一旦在这里报错,RunMigrations 失败 → bootstrap 起不来 → 桌面端整个打不开,
// 且因为本迁移不在事务里、ID 也没进 ledger,下次启动会死在重复 ADD COLUMN 上。
func TestMigration202608080011_DedupesRowsThatCollapseOntoOneFingerprint(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
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

// TestMigration202608080011_UnresolvedRowsWithDifferentFingerprintsCoexist 锁住「路径
// 记录去重」测试接缝的桌面端那一半: 同一项目下、device_id 都为空(未配对)但指纹不同
// 的多条记录必须能并存,不撞新的部分唯一索引。
func TestMigration202608080011_UnresolvedRowsWithDifferentFingerprintsCoexist(t *testing.T) {
	gormDB := openTestDB(t, "202608080011")

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

// TestMigration202608080011_NewIndexRejectsDuplicateFingerprintPerProject 反向验证新
// 索引真的在管——同一项目下两条 ACTIVE 行撞同一个 daemon_fingerprint 必须被拒绝
// (旧索引按 device_id 去重,已经不再是自然键)。
func TestMigration202608080011_NewIndexRejectsDuplicateFingerprintPerProject(t *testing.T) {
	gormDB := openTestDB(t, "202608080011")

	require.NoError(t, gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '7', 'fp-dup', '/srv/a', 1, 0, 0)`).Error)

	err := gormDB.Exec(`INSERT INTO project_locations
(project_id, device_id, daemon_fingerprint, path, status, createtime, updatetime)
VALUES (1, '8', 'fp-dup', '/srv/b', 1, 0, 0)`).Error
	assert.Error(t, err, "同一项目下两条 ACTIVE 行撞同一指纹必须被新的部分唯一索引拒绝")
}

// TestMigration202608080011_CopiesAgentSkillsIntoExecTargetLosslessly 锁住 R15e:
// agents.skills_json 原样无损地搬进它那唯一一行执行目标。迁移回填产生的行即是那
// 一行——seedAgent 带 backendID 让回填建行,不需要也不能在迁移前手动插 agent_
// exec_targets(表由本迁移创建)。
func TestMigration202608080011_CopiesAgentSkillsIntoExecTargetLosslessly(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	skillsJSON := `[{"id":"superpowers@claude-plugins-official","enabled":true},{"id":"opsctl@opskat","enabled":false}]`
	agentID := seedAgent(t, gormDB, "Skilled Agent", 7)
	require.NoError(t, gormDB.Exec(`UPDATE agents SET skills_json = ? WHERE id = ?`, skillsJSON, agentID).Error)

	require.NoError(t, RunMigrations(gormDB))

	var gotSkillsJSON string
	require.NoError(t, gormDB.Raw(
		`SELECT skills_json FROM agent_exec_targets WHERE agent_id = ?`, agentID,
	).Row().Scan(&gotSkillsJSON))
	assert.Equal(t, skillsJSON, gotSkillsJSON, "agents.skills_json 必须原样无损地搬进它那唯一一行执行目标")

	// agents.skills_json 列保留（本迁移不删列），但迁移之后不再是任何读路径的真源。
	var stillOnAgentRow string
	require.NoError(t, gormDB.Raw(`SELECT skills_json FROM agents WHERE id = ?`, agentID).Row().Scan(&stillOnAgentRow))
	assert.Equal(t, skillsJSON, stillOnAgentRow)
}

// TestMigration202608080011_AgentWithoutExecTargetRowUnaffected 没有执行目标行的
// Agent（未配置后端）没有行可落，迁移不应该为它凭空造一行。
func TestMigration202608080011_AgentWithoutExecTargetRowUnaffected(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	agentID := seedAgent(t, gormDB, "No Backend Agent", 0)
	require.NoError(t, gormDB.Exec(`UPDATE agents SET skills_json = ? WHERE id = ?`, `[{"id":"x","enabled":true}]`, agentID).Error)

	require.NoError(t, RunMigrations(gormDB))

	var count int64
	require.NoError(t, gormDB.Raw(`SELECT COUNT(*) FROM agent_exec_targets WHERE agent_id = ?`, agentID).Row().Scan(&count))
	assert.Zero(t, count)
}

// TestMigration202608080011_ExistingSessionsDefaultToUnpinned 锁住"没值涵盖首轮与
// 全部老会话，因此不需要数据迁移"：迁移前已存在的会话行迁移后新列落在 0（未钉住），
// 派发链路据此回落到按 R15 顺序挑一档并写回。
func TestMigration202608080011_ExistingSessionsDefaultToUnpinned(t *testing.T) {
	gormDB := openTestDB(t, "202608080010")
	require.NoError(t, gormDB.Exec(`INSERT INTO chat_sessions
(agent_id, title, agent_status, status, createtime, updatetime)
VALUES (1, 'existing', 'idle', 1, 0, 0)`).Error)

	require.NoError(t, RunMigrations(gormDB))

	var execAgentBackendID int64
	require.NoError(t, gormDB.Raw(
		`SELECT exec_agent_backend_id FROM chat_sessions WHERE title = 'existing'`,
	).Row().Scan(&execAgentBackendID))
	assert.Equal(t, int64(0), execAgentBackendID, "老会话必须落在未钉住状态,不能凭空回填一档")
}
