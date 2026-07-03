package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607030001 编排可参与范围：orchestration_runs.allowed_agent_ids（JSON []int64，空=全部）。
func migration202607030001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607030001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orchestration_runs ADD COLUMN allowed_agent_ids TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orchestration_runs DROP COLUMN allowed_agent_ids`).Error
		},
	}
}
