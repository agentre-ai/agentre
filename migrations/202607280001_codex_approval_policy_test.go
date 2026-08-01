package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202607280001CodexApprovalPolicy(t *testing.T) {
	t.Run("Given a legacy Codex backend uses on-failure when the migration runs then it is normalized without changing other backends", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, migration202605220003().Migrate(db))
		require.NoError(t, db.Exec(`
			INSERT INTO agent_backends (id, type, name, approval, env_json, status)
			VALUES
				(1, 'codex', 'legacy codex', 'on-failure', '{}', 1),
				(2, 'codex', 'current codex', 'never', '{}', 1),
				(3, 'claudecode', 'other backend', 'on-failure', '{}', 1)
		`).Error)

		require.NoError(t, migration202607280001().Migrate(db))

		var rows []struct {
			ID       int64
			Approval string
		}
		require.NoError(t, db.Raw(`SELECT id, approval FROM agent_backends ORDER BY id`).Scan(&rows).Error)
		require.Equal(t, []struct {
			ID       int64
			Approval string
		}{
			{ID: 1, Approval: "on-request"},
			{ID: 2, Approval: "never"},
			{ID: 3, Approval: "on-failure"},
		}, rows)
	})
}
