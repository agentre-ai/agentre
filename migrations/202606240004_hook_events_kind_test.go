package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606240004(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	// kind 列在 hook_events 上。
	require.True(t, gdb.Migrator().HasColumn(hookEventsProbe{}, "kind"), "expected hook_events.kind column")

	// 未显式给 kind 的旧式插入应回落默认 'output'。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime)
		 VALUES (1,'legacy','','{}',0,1,0,0)`).Error)
	var kind string
	require.NoError(t, gdb.Raw(`SELECT kind FROM hook_events WHERE title = 'legacy'`).Scan(&kind).Error)
	require.Equal(t, "output", kind, "missing kind should default to output")

	// failure 行可显式写入。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hook_events(hook_id,kind,title,dedupe_key,payload_json,received_at,status,createtime,updatetime)
		 VALUES (1,'failure','boom','','{"exitCode":1}',0,1,0,0)`).Error)
	require.NoError(t, gdb.Raw(`SELECT kind FROM hook_events WHERE title = 'boom'`).Scan(&kind).Error)
	require.Equal(t, "failure", kind)
}

type hookEventsProbe struct{}

func (hookEventsProbe) TableName() string { return "hook_events" }
