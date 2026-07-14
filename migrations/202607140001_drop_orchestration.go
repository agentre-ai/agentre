package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607140001 删除编排子系统:DROP 编排/流程库 4 表 + chat_sessions.run_id 列,
// 清掉孤儿编排子会话(purpose='orch_child')及其消息,并从 agents.tools_json 去掉
// orchestrate/workflow 工具种子(保留 org/subagent/hook)。
// 编排能力被整体移除,应用未发布,硬删除,无 Rollback。
func migration202607140001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607140001",
		Migrate: func(tx *gorm.DB) error {
			for _, sql := range []string{
				// 先删孤儿编排子会话的消息,再删会话(此时 run_id 列还在,可用于过滤)。
				`DELETE FROM chat_messages WHERE session_id IN (
					SELECT id FROM chat_sessions WHERE purpose = 'orch_child' OR run_id > 0)`,
				`DELETE FROM chat_sessions WHERE purpose = 'orch_child' OR run_id > 0`,
				// 去掉 chat_sessions.run_id 列(SQLite >= 3.35 原生支持 DROP COLUMN;
				// 202606240001 建列时未加索引,可直接 DROP)。
				`ALTER TABLE chat_sessions DROP COLUMN run_id`,
				// DROP 编排/流程库表(各自索引随表一并删除)。
				`DROP TABLE IF EXISTS orch_dispatches`,
				`DROP TABLE IF EXISTS orch_tasks`,
				`DROP TABLE IF EXISTS orchestration_runs`,
				`DROP TABLE IF EXISTS workflows`,
				// 从 tools_json 里剔除 orchestrate/workflow,保留其余工具(org/subagent/hook)。
				`UPDATE agents SET tools_json = (
					SELECT COALESCE(json_group_array(json(value)), '[]')
					FROM json_each(CASE WHEN json_valid(tools_json) THEN tools_json ELSE '[]' END)
					WHERE json_extract(value, '$.key') NOT IN ('orchestrate', 'workflow'))
				WHERE json_valid(tools_json)
				  AND (instr(tools_json, '"orchestrate"') > 0 OR instr(tools_json, '"workflow"') > 0)`,
			} {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
			}
			return nil
		},
		// No Rollback: orchestration hard-deleted, app unreleased.
	}
}
