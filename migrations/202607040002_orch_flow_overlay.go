package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607040002 运行时进度 overlay：orch_tasks.node_ref（任务对应的流程节点 label，空=未打标）
// + orchestration_runs.flow_graph（建 Run 时快照的 graph JSON）。
func migration202607040002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607040002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE orch_tasks ADD COLUMN node_ref TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE orchestration_runs ADD COLUMN flow_graph TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE orch_tasks DROP COLUMN node_ref`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE orchestration_runs DROP COLUMN flow_graph`).Error
		},
	}
}
