package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606250001 hooks 加 interpreter_path:覆盖解释器二进制路径(空=LookPath 自动解析)。
func migration202606250001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606250001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hooks ADD COLUMN interpreter_path TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hooks DROP COLUMN interpreter_path`).Error
		},
	}
}
