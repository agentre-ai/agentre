package group_svc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/group_entity"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/group_repo"
	"agentre/internal/repository/group_repo/mock_group_repo"
	"agentre/internal/service/chat_svc"
	"agentre/internal/service/group_svc"
	"agentre/internal/service/group_svc/mock_group_svc"
)

func TestStopGroup_AbortsInflightAndClearsQueue(t *testing.T) {
	Convey("StopGroup 对每个在跑成员调 chat_svc.Stop + 清队列 + run_status=idle", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 2, AgentID: 2, BackingSessionID: 12, Status: group_entity.MemberActive},
		}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		group_svc.MarkInflightForTest(svc, 5, 2)
		gw.EXPECT().Stop(gomock.Any(), &chat_svc.StopRequest{SessionID: 12}).Return(&chat_svc.StopResponse{Stopped: true}, nil)

		So(svc.StopGroup(ctx, 5), ShouldBeNil)
		So(g.RunStatus, ShouldEqual, group_entity.RunIdle)
	})
}

func TestStopGroup_GroupNotFound(t *testing.T) {
	Convey("StopGroup 群不存在 → GroupNotFound 且不调 Stop/Update", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(nil, nil)
		// 无 Update / Stop 的 EXPECT → 被调用即失败。

		svc := group_svc.NewForTest(gw)
		err := svc.StopGroup(ctx, 5)
		So(err, ShouldNotBeNil)
		var httpErr *httputils.Error
		So(errors.As(err, &httpErr), ShouldBeTrue)
		So(httpErr.Code, ShouldEqual, code.GroupNotFound)
	})
}

func TestPauseGroup_SetsRunStatusPaused(t *testing.T) {
	Convey("PauseGroup → run_status=paused, 不中止在跑 turn", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
		// 无 gw.Stop 的 EXPECT → 被调用即失败(Pause 不中止在跑 turn)。

		svc := group_svc.NewForTest(gw)
		So(svc.PauseGroup(ctx, 5), ShouldBeNil)
		So(g.RunStatus, ShouldEqual, group_entity.RunPaused)
	})
}

func TestResumeGroup_SetsRunningAndKicks(t *testing.T) {
	Convey("ResumeGroup → 先发 run_status=running 事件, 再 kick 填槽", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunPaused, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil).AnyTimes()
		// setRunStatus 的 Update + kick 内部 transitionRunStatus 可能再 Update → AnyTimes。
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
		// kick 会 ListByGroup(无 pending → 不起 turn, 随后 quiesce 到 waiting_user)。
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{
			{ID: 1, GroupID: 5, AgentID: 1, BackingSessionID: 11, Status: group_entity.MemberActive},
		}, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		// 经 emitter 同步观察 run_status 转移(不裸读共享指针, 防 -race)。
		// setRunStatus 先发 running; kick 无 work → 再 quiesce 到 waiting_user。
		runStatusCh := make(chan string, 8)
		group_svc.SetEmitterForTest(svc, runStatusEmitter(runStatusCh))
		So(svc.ResumeGroup(ctx, 5), ShouldBeNil)
		So(waitForRunStatus(runStatusCh, group_entity.RunRunning, time.Second), ShouldBeTrue)
	})
}

func TestRenameGroup(t *testing.T) {
	Convey("RenameGroup", t, func() {
		ctx := context.Background()

		Convey("有效标题 → Update 且写入新 Title", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			g := &group_entity.Group{ID: 5, Title: "旧名", Status: consts.ACTIVE}
			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
			groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, gg *group_entity.Group) error {
					So(gg.Title, ShouldEqual, "新名")
					return nil
				})

			svc := group_svc.NewForTest(gw)
			So(svc.RenameGroup(ctx, 5, "新名"), ShouldBeNil)
			So(g.Title, ShouldEqual, "新名")
		})

		Convey("空白标题 → GroupTitleRequired 且不 Update", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(
				&group_entity.Group{ID: 5, Title: "旧名", Status: consts.ACTIVE}, nil)
			// 无 Update 的 EXPECT → 被调用即失败。

			svc := group_svc.NewForTest(gw)
			err := svc.RenameGroup(ctx, 5, "   ")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupTitleRequired)
		})

		Convey("群不存在 → GroupNotFound", func() {
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()
			gw := mock_group_svc.NewMockChatGateway(ctrl)
			groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
			group_repo.RegisterGroup(groupRepo)

			groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(nil, nil)

			svc := group_svc.NewForTest(gw)
			err := svc.RenameGroup(ctx, 5, "新名")
			So(err, ShouldNotBeNil)
			var httpErr *httputils.Error
			So(errors.As(err, &httpErr), ShouldBeTrue)
			So(httpErr.Code, ShouldEqual, code.GroupNotFound)
		})
	})
}

func TestArchiveGroup_StopsAllAndSoftDeletes(t *testing.T) {
	Convey("ArchiveGroup → stopAll + status=DELETE + Update", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		groupRepo := mock_group_repo.NewMockGroupRepo(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterGroup(groupRepo)
		group_repo.RegisterMember(memberRepo)

		g := &group_entity.Group{ID: 5, RunStatus: group_entity.RunRunning, Status: consts.ACTIVE}
		groupRepo.EXPECT().Find(gomock.Any(), int64(5)).Return(g, nil)
		groupRepo.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, gg *group_entity.Group) error {
				So(gg.Status, ShouldEqual, consts.DELETE)
				return nil
			})
		// 无在跑成员 → ListByGroup 返回空, 不调 gw.Stop。
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return(nil, nil).AnyTimes()

		svc := group_svc.NewForTest(gw)
		So(svc.ArchiveGroup(ctx, 5), ShouldBeNil)
		So(g.Status, ShouldEqual, consts.DELETE)
	})
}
