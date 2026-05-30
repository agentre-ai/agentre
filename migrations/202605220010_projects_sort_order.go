package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220010 adds persisted sibling order for project trees.
func migration202605220010() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220010",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE projects ADD COLUMN sort_order INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE projects SET sort_order = id WHERE sort_order = 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_projects_parent_sort
	ON projects(parent_id, status, sort_order, id)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP INDEX IF EXISTS idx_projects_parent_sort`).Error
		},
	}
}
