package server_state_entity

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestServerState(t *testing.T) {
	Convey("IsLoggedIn requires user_id, device_id, keychain_account all populated", t, func() {
		s := &ServerState{ServerUserID: 1, DeviceID: 1, KeychainAccount: "x"}
		So(s.IsLoggedIn(), ShouldBeTrue)
	})
	Convey("partial state is not logged in", t, func() {
		So((&ServerState{ServerUserID: 1, DeviceID: 1}).IsLoggedIn(), ShouldBeFalse)
		So((&ServerState{ServerUserID: 1, KeychainAccount: "x"}).IsLoggedIn(), ShouldBeFalse)
		So((&ServerState{DeviceID: 1, KeychainAccount: "x"}).IsLoggedIn(), ShouldBeFalse)
		So((&ServerState{}).IsLoggedIn(), ShouldBeFalse)
	})
	Convey("Touch updates Updatetime to a positive recent value", t, func() {
		s := &ServerState{}
		s.Touch()
		So(s.Updatetime, ShouldBeGreaterThan, int64(0))
	})
}
