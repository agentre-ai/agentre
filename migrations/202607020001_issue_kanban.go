package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607020001 issue 看板:新增 stage(4 阶段主生命周期)/position(列内分数排序)/
// assignee_agent_id/session_id(Plan 2 派发预留)。回填 stage 由 state 派生、position 取 createtime。
func migration202607020001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607020001",
		Migrate: func(tx *gorm.DB) error {
			stmts := []string{
				`ALTER TABLE issues ADD COLUMN stage TEXT NOT NULL DEFAULT 'todo'`,
				`ALTER TABLE issues ADD COLUMN position REAL NOT NULL DEFAULT 0`,
				`ALTER TABLE issues ADD COLUMN assignee_agent_id INTEGER NOT NULL DEFAULT 0`,
				`ALTER TABLE issues ADD COLUMN session_id INTEGER NOT NULL DEFAULT 0`,
				`UPDATE issues SET stage = CASE WHEN state = 'closed' THEN 'done' ELSE 'todo' END`,
				`UPDATE issues SET position = createtime`,
				`CREATE INDEX IF NOT EXISTS idx_issues_board ON issues (status, stage, position)`,
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
				`DROP INDEX IF EXISTS idx_issues_board`,
				`ALTER TABLE issues DROP COLUMN session_id`,
				`ALTER TABLE issues DROP COLUMN assignee_agent_id`,
				`ALTER TABLE issues DROP COLUMN position`,
				`ALTER TABLE issues DROP COLUMN stage`,
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
