package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606250001(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	require.True(t, gdb.Migrator().HasColumn("hooks", "interpreter_path"),
		"expected hooks.interpreter_path column")

	// 默认空串可插入。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hooks(name,interpreter,command,trigger_type,schedule_expr,timezone,
		 env_json,state_json,next_run_at,enabled,status,createtime,updatetime)
		 VALUES ('j','python','x','schedule','* * * * *','UTC','[]','{}',0,1,1,0,0)`).Error)

	var path string
	require.NoError(t, gdb.Raw(`SELECT interpreter_path FROM hooks WHERE name='j'`).Scan(&path).Error)
	require.Equal(t, "", path)
}
