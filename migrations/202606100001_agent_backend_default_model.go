package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606100001 adds agent_backends.default_model — the claudecode
// backend's --model value, used to pick a custom model (e.g. claude-fable-5)
// when running on the CLI's own login state (no bound LLM provider).
func migration202606100001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606100001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE agent_backends ADD COLUMN default_model TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE agent_backends DROP COLUMN default_model`).Error
		},
	}
}
