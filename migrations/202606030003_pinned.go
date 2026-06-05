package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606030003 给 agents / groups 加用户置顶列 pinned。
func migration202606030003() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606030003",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE agents ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE groups ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT 0`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE groups DROP COLUMN pinned`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE agents DROP COLUMN pinned`).Error
		},
	}
}
