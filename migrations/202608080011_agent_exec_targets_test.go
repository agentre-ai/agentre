package migrations

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	dbpkg "github.com/cago-frame/cago/database/db"

	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
)

// openTestDB 开一个临时 SQLite,并只跑到 upTo 这条迁移为止(不含之后的),
// 用来构造「本轮迁移之前」的库。迁移自身的测试是「仓储单测一律 sqlmock」的既有例外。
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

// seedAgent 直接写 agents 行(绕开仓储),模拟本轮迁移之前既有库里的数据。
func seedAgent(t *testing.T, gormDB *gorm.DB, name string, backendID int64) int64 {
	t.Helper()
	require.NoError(t, gormDB.Exec(`INSERT INTO agents (name, department_id, agent_backend_id, status, createtime, updatetime)
VALUES (?, 1, ?, 1, 0, 0)`, name, backendID).Error)
	var id int64
	require.NoError(t, gormDB.Raw(`SELECT id FROM agents WHERE name = ?`, name).Row().Scan(&id))
	return id
}

// TestMigration202608080011_BackfillsSingleElementExecTargets 锁住 R15 的迁移语义:
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
