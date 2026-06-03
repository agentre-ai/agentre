package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/gorm"
)

func TestMigration202606030001GroupID(t *testing.T) {
	Convey("给定全量迁移跑过, chat_sessions 应含 group_id 列且默认 0", t, func() {
		gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		So(err, ShouldBeNil)
		m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
		So(m.Migrate(), ShouldBeNil)

		So(gdb.Migrator().HasColumn("chat_sessions", "group_id"), ShouldBeTrue)

		// 既有列默认值 0：插一行不指定 group_id
		So(gdb.Exec(`INSERT INTO chat_sessions (agent_id, status) VALUES (1, 1)`).Error, ShouldBeNil)
		var gid int64
		So(gdb.Raw(`SELECT group_id FROM chat_sessions LIMIT 1`).Scan(&gid).Error, ShouldBeNil)
		So(gid, ShouldEqual, 0)
	})
}
