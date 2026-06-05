package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606030003Pinned(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
	require.NoError(t, m.Migrate())

	require.True(t, gdb.Migrator().HasColumn("agents", "pinned"), "agents.pinned column missing")
	require.True(t, gdb.Migrator().HasColumn("groups", "pinned"), "groups.pinned column missing")
}
