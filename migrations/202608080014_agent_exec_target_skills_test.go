package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedExecTarget 直接写 agent_exec_targets 行（绕开仓储），模拟本轮迁移之前既有库里
// 已经存在的执行目标行（由更早的 migration202608080011 backfill 或之后的应用写入
// 产生，此处不需要真的跑那条历史迁移，只需要还原它跑完之后的结构状态）。
func TestMigration202608080014_CopiesAgentSkillsIntoExecTargetLosslessly(t *testing.T) {
	gormDB := openTestDB(t, "202608080013")
	skillsJSON := `[{"id":"superpowers@claude-plugins-official","enabled":true},{"id":"opsctl@opskat","enabled":false}]`
	agentID := seedAgent(t, gormDB, "Skilled Agent", 7)
	require.NoError(t, gormDB.Exec(`UPDATE agents SET skills_json = ? WHERE id = ?`, skillsJSON, agentID).Error)
	require.NoError(t, gormDB.Exec(
		`INSERT INTO agent_exec_targets (agent_id, agent_backend_id, sort_order) VALUES (?, 7, 0)`,
		agentID,
	).Error)

	require.NoError(t, RunMigrations(gormDB))

	var gotSkillsJSON string
	require.NoError(t, gormDB.Raw(
		`SELECT skills_json FROM agent_exec_targets WHERE agent_id = ?`, agentID,
	).Row().Scan(&gotSkillsJSON))
	assert.Equal(t, skillsJSON, gotSkillsJSON, "agents.skills_json 必须原样无损地搬进它那唯一一行执行目标")

	// agents.skills_json 列保留（本轮不删列），但迁移之后不再是任何读路径的真源。
	var stillOnAgentRow string
	require.NoError(t, gormDB.Raw(`SELECT skills_json FROM agents WHERE id = ?`, agentID).Row().Scan(&stillOnAgentRow))
	assert.Equal(t, skillsJSON, stillOnAgentRow)
}

// TestMigration202608080014_AgentWithoutExecTargetRowUnaffected 没有执行目标行的
// Agent（未配置后端）没有行可落，迁移不应该为它凭空造一行。
func TestMigration202608080014_AgentWithoutExecTargetRowUnaffected(t *testing.T) {
	gormDB := openTestDB(t, "202608080013")
	agentID := seedAgent(t, gormDB, "No Backend Agent", 0)
	require.NoError(t, gormDB.Exec(`UPDATE agents SET skills_json = ? WHERE id = ?`, `[{"id":"x","enabled":true}]`, agentID).Error)

	require.NoError(t, RunMigrations(gormDB))

	var count int64
	require.NoError(t, gormDB.Raw(`SELECT COUNT(*) FROM agent_exec_targets WHERE agent_id = ?`, agentID).Row().Scan(&count))
	assert.Zero(t, count)
}
