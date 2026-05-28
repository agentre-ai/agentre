package app

import (
	"errors"
	"runtime"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestValidateOpenPath(t *testing.T) {
	Convey("Given various path inputs", t, func() {
		Convey("when path is empty, then error", func() {
			_, err := validateOpenPath("")
			So(err, ShouldNotBeNil)
		})
		Convey("when path is relative, then error", func() {
			_, err := validateOpenPath("foo/bar.go")
			So(err, ShouldNotBeNil)
		})
		Convey("when path contains '..', then error", func() {
			_, err := validateOpenPath("/foo/../bar.go")
			So(err, ShouldNotBeNil)
		})
		Convey("when path has '..:line' suffix (potential bypass), then error", func() {
			_, err := validateOpenPath("/foo/..:42")
			So(err, ShouldNotBeNil)
		})
		Convey("when filename contains '..' but is not a '..' segment, then accept", func() {
			got, err := validateOpenPath("/Users/x/File..Go")
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "/Users/x/File..Go")
		})
		Convey("when POSIX absolute path with :line:col, then return without suffix", func() {
			got, err := validateOpenPath("/Users/x/foo.go:42:7")
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "/Users/x/foo.go")
		})
		Convey("when POSIX absolute path without suffix, then return as-is", func() {
			got, err := validateOpenPath("/Users/x/foo.go")
			So(err, ShouldBeNil)
			So(got, ShouldEqual, "/Users/x/foo.go")
		})
		Convey("when Windows absolute path with line suffix, then strip suffix", func() {
			got, err := validateOpenPath(`C:\Users\x\foo.go:10`)
			So(err, ShouldBeNil)
			So(got, ShouldEqual, `C:\Users\x\foo.go`)
		})
	})
}

func TestOpenPath_dispatchesPlatformCommand(t *testing.T) {
	Convey("Given a stubbed exec runner", t, func() {
		var gotName string
		var gotArgs []string
		origRun := runOpenCmd
		runOpenCmd = func(name string, args ...string) error {
			gotName = name
			gotArgs = args
			return nil
		}
		defer func() { runOpenCmd = origRun }()

		Convey("when OpenPath is called with a valid absolute path", func() {
			a := &App{}
			err := a.OpenPath("/tmp/file.go:42")
			So(err, ShouldBeNil)

			switch runtime.GOOS {
			case "darwin":
				So(gotName, ShouldEqual, "open")
				So(gotArgs, ShouldResemble, []string{"/tmp/file.go"})
			case "windows":
				So(gotName, ShouldEqual, "cmd")
				So(gotArgs, ShouldResemble, []string{"/c", "start", "", "/tmp/file.go"})
			default:
				So(gotName, ShouldEqual, "xdg-open")
				So(gotArgs, ShouldResemble, []string{"/tmp/file.go"})
			}
		})

		Convey("when exec returns error, then OpenPath propagates", func() {
			runOpenCmd = func(name string, args ...string) error {
				return errors.New("boom")
			}
			a := &App{}
			err := a.OpenPath("/tmp/file.go")
			So(err, ShouldNotBeNil)
		})

		Convey("when path is invalid, then exec is not called", func() {
			called := false
			runOpenCmd = func(name string, args ...string) error {
				called = true
				return nil
			}
			a := &App{}
			err := a.OpenPath("relative/path.go")
			So(err, ShouldNotBeNil)
			So(called, ShouldBeFalse)
		})
	})
}
