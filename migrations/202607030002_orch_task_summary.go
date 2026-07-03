package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607030002 回报分层：orch_tasks.summary（显式小结，空=无主动汇报）。
func migration202607030002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607030002",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orch_tasks ADD COLUMN summary TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE orch_tasks DROP COLUMN summary`).Error
		},
	}
}
