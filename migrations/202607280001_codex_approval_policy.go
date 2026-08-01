package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607280001 normalizes the approval value removed by Codex CLI.
// codex-cli 0.145.0 accepts only untrusted, on-request, and never.
func migration202607280001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607280001_codex_approval_policy",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`
				UPDATE agent_backends
				SET approval = 'on-request'
				WHERE type = 'codex' AND approval = 'on-failure'
			`).Error
		},
	}
}
