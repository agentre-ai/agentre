package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240001 编排能力基座：Run/Task 表 + chat_sessions.run_id + orchestrate 工具种子。
func migration202606240001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orchestration_runs (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				goal TEXT NOT NULL DEFAULT '',
				leader_agent_id INTEGER NOT NULL DEFAULT 0,
				flow_id INTEGER NOT NULL DEFAULT 0,
				flow_content TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending',
				root_task_id INTEGER NOT NULL DEFAULT 0,
				project_id INTEGER NOT NULL DEFAULT 0,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orchestration_runs_status ON orchestration_runs(status, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orch_tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				run_id INTEGER NOT NULL DEFAULT 0,
				agent_id INTEGER NOT NULL DEFAULT 0,
				session_id INTEGER NOT NULL DEFAULT 0,
				parent_task_id INTEGER NOT NULL DEFAULT 0,
				kind TEXT NOT NULL DEFAULT 'dispatch',
				status TEXT NOT NULL DEFAULT 'pending',
				brief TEXT NOT NULL DEFAULT '',
				result TEXT NOT NULL DEFAULT '',
				call_seq INTEGER NOT NULL DEFAULT 0,
				refs TEXT NOT NULL DEFAULT '',
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, status)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_session ON orch_tasks(session_id)`).Error; err != nil {
				return err
			}
			// 隐藏编排子会话用：chat_sessions.run_id（0=非编排会话）。
			if err := tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN run_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			// DEFAULT agent 默认启用 orchestrate（可当 Leader / 子派）。
			return tx.Exec(`UPDATE agents
				SET tools_json = json_insert(
					CASE WHEN json_valid(tools_json) THEN tools_json ELSE '[]' END,
					'$[#]', json('{"key":"orchestrate","enabled":true}'))
				WHERE system_badge = 'DEFAULT'
				  AND (tools_json IS NULL OR instr(tools_json, '"orchestrate"') = 0)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN run_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS orch_tasks`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS orchestration_runs`).Error
		},
	}
}
