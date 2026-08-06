package migrations

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202607240001OpenClawAgentBackend(t *testing.T) {
	t.Run("Given the existing backend table when the migration runs then only non-secret OpenClaw columns are added", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, migration202605220003().Migrate(db))

		require.NoError(t, migration202607240001().Migrate(db))

		for _, column := range []string{
			"openclaw_gateway_url",
			"openclaw_agent_id",
			"openclaw_default_model",
			"openclaw_session_mode",
		} {
			require.True(t, columnExists(t, db, "agent_backends", column), column)
		}

		rows, err := db.Raw(`PRAGMA table_info(agent_backends)`).Rows()
		require.NoError(t, err)
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var cid, notnull, pk int
			var name, ctype string
			var dflt any
			require.NoError(t, rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk))
			lower := strings.ToLower(name)
			require.NotContains(t, lower, "token")
			require.NotContains(t, lower, "secret")
		}
	})
}
