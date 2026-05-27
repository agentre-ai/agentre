package project_location_entity

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
)

func TestProjectLocation_Check(t *testing.T) {
	ctx := context.Background()

	Convey("Check", t, func() {
		Convey("nil receiver → error", func() {
			var p *ProjectLocation
			So(p.Check(ctx), ShouldNotBeNil)
		})
		Convey("invalid project_id → error", func() {
			p := &ProjectLocation{ProjectID: 0, Path: "/a"}
			So(p.Check(ctx), ShouldNotBeNil)
		})
		Convey("empty path → error", func() {
			p := &ProjectLocation{ProjectID: 1, Path: ""}
			So(p.Check(ctx), ShouldNotBeNil)
		})
		Convey("relative path on remote device → error", func() {
			p := &ProjectLocation{ProjectID: 1, DeviceID: "7", Path: "foo/bar"}
			So(p.Check(ctx), ShouldNotBeNil)
		})
		Convey("valid local absolute → ok", func() {
			p := &ProjectLocation{ProjectID: 1, DeviceID: "", Path: "/Users/me/foo"}
			So(p.Check(ctx), ShouldBeNil)
		})
		Convey("valid remote absolute → ok", func() {
			p := &ProjectLocation{ProjectID: 1, DeviceID: "7", Path: "/home/me/foo"}
			So(p.Check(ctx), ShouldBeNil)
		})
	})

	Convey("IsActive / IsLocal", t, func() {
		p := &ProjectLocation{Status: consts.ACTIVE, DeviceID: ""}
		So(p.IsActive(), ShouldBeTrue)
		So(p.IsLocal(), ShouldBeTrue)

		q := &ProjectLocation{Status: consts.DELETE, DeviceID: "7"}
		So(q.IsActive(), ShouldBeFalse)
		So(q.IsLocal(), ShouldBeFalse)
	})
}
