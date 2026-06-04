package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/go-gormigrate/gormigrate/v2"
	. "github.com/smartystreets/goconvey/convey"
	"gorm.io/gorm"
)

func TestMigration202606030002Group(t *testing.T) {
	Convey("迁移后 groups/group_members/group_messages 三表存在", t, func() {
		gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		So(err, ShouldBeNil)
		m := gormigrate.New(gdb, gormigrate.DefaultOptions, migrationList())
		So(m.Migrate(), ShouldBeNil)
		So(gdb.Migrator().HasTable("groups"), ShouldBeTrue)
		So(gdb.Migrator().HasTable("group_members"), ShouldBeTrue)
		So(gdb.Migrator().HasTable("group_messages"), ShouldBeTrue)
	})
}
