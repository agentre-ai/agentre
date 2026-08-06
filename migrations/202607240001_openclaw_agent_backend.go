package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607240001 adds only non-sensitive OpenClaw Gateway configuration.
// Gateway tokens and device private keys live in the platform keychain and must
// never be represented by a database column.
func migration202607240001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607240001",
		Migrate: func(tx *gorm.DB) error {
			statements := []string{
				`ALTER TABLE agent_backends ADD COLUMN openclaw_gateway_url TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE agent_backends ADD COLUMN openclaw_agent_id TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE agent_backends ADD COLUMN openclaw_default_model TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE agent_backends ADD COLUMN openclaw_session_mode TEXT NOT NULL DEFAULT ''`,
			}
			for _, statement := range statements {
				if err := tx.Exec(statement).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, column := range []string{
				"openclaw_session_mode",
				"openclaw_default_model",
				"openclaw_agent_id",
				"openclaw_gateway_url",
			} {
				if err := tx.Exec(`ALTER TABLE agent_backends DROP COLUMN ` + column).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}
