package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607140001_DropsOrchestration(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	hasTable := func(table string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n)
		return n > 0
	}
	hasCol := func(table, col string) bool {
		var n int
		db.Raw("SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?", table, col).Scan(&n)
		return n > 0
	}

	// 编排 + 流程库表全部被删。
	assert.False(t, hasTable("orchestration_runs"))
	assert.False(t, hasTable("orch_dispatches"))
	assert.False(t, hasTable("orch_tasks"))
	assert.False(t, hasTable("workflows"))
	// chat_sessions.run_id 列被删。
	assert.False(t, hasCol("chat_sessions", "run_id"))

	// DEFAULT agent 的 tools_json 不再含 orchestrate/workflow(org 等保留)。
	var tools string
	db.Raw("SELECT tools_json FROM agents WHERE system_badge='DEFAULT' LIMIT 1").Scan(&tools)
	assert.NotContains(t, tools, `"orchestrate"`)
	assert.NotContains(t, tools, `"workflow"`)
	assert.Contains(t, tools, `"org"`)
}

// 种真实数据验证孤儿清理:编排子会话(purpose='orch_child' 命中 / run_id>0 命中)及其消息
// 被删,而普通会话及其消息保留——守住「WHERE 过宽误删用户会话」这个最危险的回归。
func TestMigration202607140001_DeletesOrphanOrchSessionsKeepsNormal(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)

	m := gormigrate.New(db, gormigrate.DefaultOptions, migrationList())
	// 跑到 drop 迁移之前:此时 orch 表 + chat_sessions.run_id/purpose 列都在,可种子。
	assert.NoError(t, m.MigrateTo("202607090003"))

	// 101=编排子会话(purpose 命中);102=编排子会话(run_id 命中);103=普通用户会话。
	assert.NoError(t, db.Exec(`INSERT INTO chat_sessions (id, agent_id, run_id, purpose) VALUES
		(101, 1, 0, 'orch_child'),
		(102, 1, 7, ''),
		(103, 1, 0, 'user')`).Error)
	assert.NoError(t, db.Exec(`INSERT INTO chat_messages (id, session_id, role) VALUES
		(201, 101, 'assistant'),
		(202, 102, 'assistant'),
		(203, 103, 'user')`).Error)

	// 跑 drop 迁移(孤儿删除 → DROP COLUMN run_id → DROP TABLE → tools_json 重写)。
	assert.NoError(t, m.MigrateTo("202607140001"))

	count := func(sql string) int {
		var n int
		db.Raw(sql).Scan(&n)
		return n
	}
	// 两条编排子会话被删,普通会话保留。
	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM chat_sessions WHERE id IN (101, 102)`), "编排子会话应被删")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM chat_sessions WHERE id = 103`), "普通会话必须保留")
	// 对应消息同步清理,普通会话消息保留。
	assert.Equal(t, 0, count(`SELECT COUNT(*) FROM chat_messages WHERE id IN (201, 202)`), "编排子会话消息应被删")
	assert.Equal(t, 1, count(`SELECT COUNT(*) FROM chat_messages WHERE id = 203`), "普通会话消息必须保留")
}
