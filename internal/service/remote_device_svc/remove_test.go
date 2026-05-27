// internal/service/remote_device_svc/remove_test.go
package remote_device_svc_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"
)

func TestRemove(t *testing.T) {
	Convey("repo.Delete failure surfaces", t, func() {
		repo, _, _, _, svc := setupSvc(t)
		repo.EXPECT().Delete(gomock.Any(), int64(1)).Return(errors.New("db"))
		err := svc.Remove(context.Background(), 1)
		So(err, ShouldNotBeNil)
	})
	Convey("happy path deletes row + token", t, func() {
		repo, _, kc, w, svc := setupSvc(t)
		repo.EXPECT().Delete(gomock.Any(), int64(1)).Return(nil)
		kc.EXPECT().Delete("agentre-daemon-token-1").Return(nil)
		w.EXPECT().Stop(int64(1))
		err := svc.Remove(context.Background(), 1)
		So(err, ShouldBeNil)
	})
	Convey("keychain delete failure does not error (logged)", t, func() {
		repo, _, kc, w, svc := setupSvc(t)
		repo.EXPECT().Delete(gomock.Any(), int64(1)).Return(nil)
		kc.EXPECT().Delete("agentre-daemon-token-1").Return(errors.New("kc down"))
		w.EXPECT().Stop(int64(1))
		err := svc.Remove(context.Background(), 1)
		So(err, ShouldBeNil)
	})
}
