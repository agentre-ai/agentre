package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606240002(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, RunMigrations(gdb))

	// 新表在。
	for _, tbl := range []string{"hooks", "hook_events"} {
		require.Truef(t, gdb.Migrator().HasTable(tbl), "expected table %s", tbl)
	}
	// 旧表已删。
	for _, tbl := range []string{"hook_sources", "hook_rules"} {
		require.Falsef(t, gdb.Migrator().HasTable(tbl), "expected %s dropped", tbl)
	}

	// 去重部分索引：允许多条空 key。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime)
		 VALUES (1,'a','','{}',0,1,0,0),(1,'b','','{}',0,1,0,0)`).Error,
		"empty dedupe should be allowed multiple times")

	// 非空 key 第一条成功。
	require.NoError(t, gdb.Exec(
		`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime)
		 VALUES (1,'c','K1','{}',0,1,0,0)`).Error)
	// 重复非空 (hook_id,dedupe_key) 应违反唯一索引。
	require.Error(t, gdb.Exec(
		`INSERT INTO hook_events(hook_id,title,dedupe_key,payload_json,received_at,status,createtime,updatetime)
		 VALUES (1,'d','K1','{}',0,1,0,0)`).Error,
		"duplicate (hook_id,dedupe_key) should violate unique index")
}
