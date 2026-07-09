package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607090001 执行节点改名:orch_tasks → orch_dispatches。
// 语义 = 一次派发(brief 派给某 agent,绑一条子会话)。腾出 orch_tasks 名字给待办清单(202607090002)。
// SQLite 无 ALTER INDEX RENAME,故索引 DROP+CREATE 换名,避免与新 orch_tasks 索引撞名。
func migration202607090001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607090001",
		Migrate: func(tx *gorm.DB) error {
			stmts := []string{
				`ALTER TABLE orch_tasks RENAME TO orch_dispatches`,
				`ALTER TABLE orch_dispatches RENAME COLUMN parent_task_id TO parent_dispatch_id`,
				`DROP INDEX IF EXISTS idx_orch_tasks_run`,
				`DROP INDEX IF EXISTS idx_orch_tasks_session`,
				`CREATE INDEX IF NOT EXISTS idx_orch_dispatches_run ON orch_dispatches(run_id, status)`,
				`CREATE INDEX IF NOT EXISTS idx_orch_dispatches_session ON orch_dispatches(session_id)`,
			}
			for _, s := range stmts {
				if err := tx.Exec(s).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			stmts := []string{
				`DROP INDEX IF EXISTS idx_orch_dispatches_run`,
				`DROP INDEX IF EXISTS idx_orch_dispatches_session`,
				`ALTER TABLE orch_dispatches RENAME COLUMN parent_dispatch_id TO parent_task_id`,
				`ALTER TABLE orch_dispatches RENAME TO orch_tasks`,
				`CREATE INDEX IF NOT EXISTS idx_orch_tasks_run ON orch_tasks(run_id, status)`,
				`CREATE INDEX IF NOT EXISTS idx_orch_tasks_session ON orch_tasks(session_id)`,
			}
			for _, s := range stmts {
				if err := tx.Exec(s).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}
