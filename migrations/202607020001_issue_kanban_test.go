package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202607020001(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// Run all migrations up to (and including) the previous tail so that the
	// issues table exists before we insert pre-migration test rows.
	m := gormigrate.New(db, gormigrate.DefaultOptions, migrationList())
	require.NoError(t, m.MigrateTo("202606250002"))

	// Insert one closed and one open issue before the kanban migration runs.
	require.NoError(t, db.Exec(
		`INSERT INTO issues (id, title, state, status, createtime) VALUES (1,'a','closed',1,1000),(2,'b','open',1,2000)`).Error)

	// Now run the kanban migration.
	require.NoError(t, m.MigrateTo("202607020001"))

	// Verify new columns exist.
	require.True(t, columnExists(t, db, "issues", "stage"), "issues.stage column must exist")
	require.True(t, columnExists(t, db, "issues", "position"), "issues.position column must exist")
	require.True(t, columnExists(t, db, "issues", "assignee_agent_id"), "issues.assignee_agent_id column must exist")
	require.True(t, columnExists(t, db, "issues", "session_id"), "issues.session_id column must exist")

	// Verify backfill: closed→done, open→todo; position=createtime.
	type row struct {
		Stage    string
		Position float64
	}
	var r1, r2 row
	require.NoError(t, db.Raw(`SELECT stage, position FROM issues WHERE id = 1`).Scan(&r1).Error)
	require.NoError(t, db.Raw(`SELECT stage, position FROM issues WHERE id = 2`).Scan(&r2).Error)

	assert.Equal(t, "done", r1.Stage)
	assert.Equal(t, float64(1000), r1.Position)
	assert.Equal(t, "todo", r2.Stage)
	assert.Equal(t, float64(2000), r2.Position)
}
