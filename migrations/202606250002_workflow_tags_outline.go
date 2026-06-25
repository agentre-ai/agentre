package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606250002 流程库 tags/outline:给人看的展示层(标签+步骤概览),
// JSON 数组存 TEXT,绝不注入 Leader(注入仍只 run.flow_content)。
func migration202606250002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606250002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN tags TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows ADD COLUMN outline TEXT NOT NULL DEFAULT '[]'`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows DROP COLUMN outline`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN tags`).Error
		},
	}
}
