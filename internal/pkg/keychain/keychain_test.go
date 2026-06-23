package keychain_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/pkg/keychain"
)

func TestMemory(t *testing.T) {
	Convey("memory keychain round-trips a secret", t, func() {
		k := keychain.NewMemory()
		So(k.Set("acc", "secret"), ShouldBeNil)
		v, err := k.Get("acc")
		So(err, ShouldBeNil)
		So(v, ShouldEqual, "secret")
	})

	Convey("Get on missing account returns ErrNotFound", t, func() {
		_, err := keychain.NewMemory().Get("missing")
		So(err, ShouldEqual, keychain.ErrNotFound)
	})

	Convey("Delete removes the secret; second Get returns ErrNotFound", t, func() {
		k := keychain.NewMemory()
		So(k.Set("acc", "s"), ShouldBeNil)
		So(k.Delete("acc"), ShouldBeNil)
		_, err := k.Get("acc")
		So(err, ShouldEqual, keychain.ErrNotFound)
	})

	Convey("SetDefault / Default round-trip", t, func() {
		k := keychain.NewMemory()
		keychain.SetDefault(k)
		So(keychain.Default(), ShouldEqual, k)
	})
}

func TestFile(t *testing.T) {
	Convey("file keychain round-trips a secret with 0600 perms", t, func() {
		dir := t.TempDir()
		k := keychain.NewFile(dir)
		So(k.Set("acc", "secret"), ShouldBeNil)

		info, err := os.Stat(filepath.Join(dir, "acc"))
		So(err, ShouldBeNil)
		So(info.Mode().Perm(), ShouldEqual, os.FileMode(0o600))

		v, err := k.Get("acc")
		So(err, ShouldBeNil)
		So(v, ShouldEqual, "secret")
	})

	Convey("Get on a missing account returns ErrNotFound", t, func() {
		dir := t.TempDir()
		_, err := keychain.NewFile(dir).Get("missing")
		So(err, ShouldEqual, keychain.ErrNotFound)
	})

	Convey("Delete removes the file; double Delete returns ErrNotFound", t, func() {
		dir := t.TempDir()
		k := keychain.NewFile(dir)
		So(k.Set("acc", "s"), ShouldBeNil)
		So(k.Delete("acc"), ShouldBeNil)
		So(k.Delete("acc"), ShouldEqual, keychain.ErrNotFound)
	})
}
