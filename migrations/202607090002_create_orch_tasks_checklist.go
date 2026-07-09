package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607090002 待办清单表(与派发树零联动的 TodoWrite 式协作白板)。
// 复用被 202607090001 腾空的 orch_tasks 名字,但列结构全新。
func migration202607090002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607090002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS orch_tasks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				run_id INTEGER NOT NULL DEFAULT 0,
				seq INTEGER NOT NULL DEFAULT 0,
				text TEXT NOT NULL DEFAULT '',
				status TEXT NOT NULL DEFAULT 'pending',
				assignee_agent_id INTEGER NOT NULL DEFAULT 0,
				created_by_agent_id INTEGER NOT NULL DEFAULT 0,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, seq)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS orch_tasks`).Error
		},
	}
}
