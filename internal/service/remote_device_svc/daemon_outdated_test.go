package remote_device_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
)

// Given 桌面端在某台配对设备的 daemon 上探到「不认识补齐族 RPC」(R18),
// When 配对设备面板拉这台设备,Then 该设备的视图上要带着「版本过旧」这条说明。
//
// 面板只读 DeviceView,所以探测结论必须落在视图上;落在别处用户就只能从日志里知道
// 「为什么这台设备一断连整轮就没了」。
func TestRecordDaemonOutdated_SurfacesOnDeviceViews(t *testing.T) {
	Convey("探到老 daemon 后,清单与单查都说明该设备版本过旧", t, func() {
		repo, _, _, _, svc := setupSvc(t)
		row := &paired_agentred_entity.PairedAgentred{ID: 7, Name: "old", URL: "ws://old/rpc"}
		repo.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{row}, nil).AnyTimes()
		repo.EXPECT().Get(gomock.Any(), int64(7)).Return(row, nil).AnyTimes()

		svc.RecordDaemonOutdated(7, true)

		list, err := svc.List(context.Background())
		So(err, ShouldBeNil)
		So(list, ShouldHaveLength, 1)
		So(list[0].DaemonOutdated, ShouldBeTrue)

		one, err := svc.Get(context.Background(), 7)
		So(err, ShouldBeNil)
		So(one.DaemonOutdated, ShouldBeTrue)
	})

	Convey("没探过的设备不背这条说明", t, func() {
		repo, _, _, _, svc := setupSvc(t)
		repo.EXPECT().Get(gomock.Any(), int64(9)).
			Return(&paired_agentred_entity.PairedAgentred{ID: 9, Name: "fresh"}, nil)

		one, err := svc.Get(context.Background(), 9)
		So(err, ShouldBeNil)
		So(one.DaemonOutdated, ShouldBeFalse)
	})

	Convey("升级过的 daemon 再探一次就把说明撤下来", t, func() {
		repo, _, _, _, svc := setupSvc(t)
		repo.EXPECT().Get(gomock.Any(), int64(7)).
			Return(&paired_agentred_entity.PairedAgentred{ID: 7, Name: "upgraded"}, nil).AnyTimes()

		svc.RecordDaemonOutdated(7, true)
		svc.RecordDaemonOutdated(7, false)

		one, err := svc.Get(context.Background(), 7)
		So(err, ShouldBeNil)
		So(one.DaemonOutdated, ShouldBeFalse)
	})
}
