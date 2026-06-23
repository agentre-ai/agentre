package group_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/utils/httputils"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/service/group_svc"
	"github.com/agentre-ai/agentre/internal/service/group_svc/mock_group_svc"
)

func assertCode(t *testing.T, err error, want int) {
	t.Helper()
	So(err, ShouldNotBeNil)
	var httpErr *httputils.Error
	So(errors.As(err, &httpErr), ShouldBeTrue)
	So(httpErr.Code, ShouldEqual, want)
}

func TestSetMemberNickname_PersistsTrimmed(t *testing.T) {
	Convey("设群昵称: 去空白后定向落库, 不与他人有效名冲突即通过", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		target := &group_entity.GroupMember{ID: 7, GroupID: 5, AgentID: 70, Status: group_entity.MemberActive}
		other := &group_entity.GroupMember{ID: 8, GroupID: 5, AgentID: 80, Status: group_entity.MemberActive}
		memberRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(target, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{target, other}, nil)
		memberRepo.EXPECT().SetNickname(gomock.Any(), int64(7), "后端工程师").Return(nil)

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{70: "Codex", 80: "Claude Code"})
		So(svc.SetMemberNickname(ctx, 7, "  后端工程师  "), ShouldBeNil)
	})
}

func TestSetMemberNickname_RejectsDuplicate(t *testing.T) {
	Convey("设群昵称: 与同群其他成员有效名(含其昵称)冲突 → GroupNicknameTaken, 不落库", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		target := &group_entity.GroupMember{ID: 7, GroupID: 5, AgentID: 70, Status: group_entity.MemberActive}
		other := &group_entity.GroupMember{ID: 8, GroupID: 5, AgentID: 80, Status: group_entity.MemberActive, Nickname: "前端工程师"}
		memberRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(target, nil)
		memberRepo.EXPECT().ListByGroup(gomock.Any(), int64(5)).Return([]*group_entity.GroupMember{target, other}, nil)
		// 无 SetNickname EXPECT → 一旦落库即失败。

		svc := group_svc.NewForTestWithNames(gw, map[int64]string{70: "Codex", 80: "Claude Code"})
		assertCode(t, svc.SetMemberNickname(ctx, 7, "前端工程师"), code.GroupNicknameTaken)
	})
}

func TestSetMemberNickname_MemberNotFound(t *testing.T) {
	Convey("设群昵称: 成员不存在 → GroupMemberNotFound, 不查名单/不落库", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		memberRepo.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)

		svc := group_svc.NewForTest(gw)
		assertCode(t, svc.SetMemberNickname(ctx, 99, "x"), code.GroupMemberNotFound)
	})
}

func TestSetMemberNickname_ClearsWhenBlank(t *testing.T) {
	Convey("设群昵称: 空白=清除, 写空串且跳过唯一性校验(不查名单)", t, func() {
		ctx := context.Background()
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		gw := mock_group_svc.NewMockChatGateway(ctrl)
		memberRepo := mock_group_repo.NewMockGroupMemberRepo(ctrl)
		group_repo.RegisterMember(memberRepo)

		target := &group_entity.GroupMember{ID: 7, GroupID: 5, AgentID: 70, Status: group_entity.MemberActive, Nickname: "后端工程师"}
		memberRepo.EXPECT().Find(gomock.Any(), int64(7)).Return(target, nil)
		memberRepo.EXPECT().SetNickname(gomock.Any(), int64(7), "").Return(nil)
		// 无 ListByGroup EXPECT → 清除不应触发唯一性查询。

		svc := group_svc.NewForTest(gw)
		So(svc.SetMemberNickname(ctx, 7, "   "), ShouldBeNil)
	})
}
