package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607080002 删除步骤/DAG 相关列:流程库退化为纯文本提示词库。
//   - workflows: graph / template / outline
//   - orchestration_runs: flow_graph
//   - orch_tasks: node_ref
//
// content 已是渲染后正文,无需数据迁移;SQLite DROP COLUMN(≥3.35)已被 202607080001 用过。
//
// 注:任务表实际表名为 orch_tasks(见 202607040002),非计划文档中笔误的 tasks。
func migration202607080002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607080002",
		Migrate: func(tx *gorm.DB) error {
			stmts := []string{
				`ALTER TABLE workflows DROP COLUMN graph`,
				`ALTER TABLE workflows DROP COLUMN template`,
				`ALTER TABLE workflows DROP COLUMN outline`,
				`ALTER TABLE orchestration_runs DROP COLUMN flow_graph`,
				`ALTER TABLE orch_tasks DROP COLUMN node_ref`,
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
				`ALTER TABLE workflows ADD COLUMN graph text NOT NULL DEFAULT ''`,
				`ALTER TABLE workflows ADD COLUMN template text NOT NULL DEFAULT ''`,
				`ALTER TABLE workflows ADD COLUMN outline text NOT NULL DEFAULT '[]'`,
				`ALTER TABLE orchestration_runs ADD COLUMN flow_graph text NOT NULL DEFAULT ''`,
				`ALTER TABLE orch_tasks ADD COLUMN node_ref text NOT NULL DEFAULT ''`,
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
