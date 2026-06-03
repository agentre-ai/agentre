package chat_svc_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/repository/chat_repo"
	"agentre/internal/repository/chat_repo/mock_chat_repo"
	"agentre/internal/service/chat_svc"
)

func TestEnsureGroupMemberSession_CreatesWithGroupID(t *testing.T) {
	Convey("给定群内该 agent 无 session, 应新建一个带 group_id 的 session", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()

		// 查既有 (group_id, agent_id) → 无
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)

		// 插入新 session，DoAndReturn 设置返回的 ID
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				s.ID = 7
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3 /*agentID*/, 0 /*projectID*/, 5 /*groupID*/)
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 7)
	})
}

func TestEnsureGroupMemberSession_IdempotentWhenExists(t *testing.T) {
	Convey("给定群内该 agent 已有 active session, 应复用其 id 而不新建", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()

		existing := &chat_entity.Session{ID: 42, AgentID: 3, GroupID: 5}
		// 查既有 (group_id, agent_id) → 找到
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(existing, nil)
		// Create 不应被调用（幂等路径）

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3 /*agentID*/, 0 /*projectID*/, 5 /*groupID*/)
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 42)
	})
}

func TestEnsureGroupMemberSession_InvalidParams(t *testing.T) {
	Convey("参数校验", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ctx := context.Background()

		Convey("agentID=0 → error", func() {
			_, err := svc.EnsureGroupMemberSession(ctx, 0, 0, 5)
			So(err, ShouldNotBeNil)
		})

		Convey("groupID=0 → error", func() {
			_, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 0)
			So(err, ShouldNotBeNil)
		})
	})
}

func TestEnsureGroupMemberSession_RepoFindError(t *testing.T) {
	Convey("FindByGroupAndAgent 返回错误时包装成 i18n 错误而非泄漏原始 DB 错误", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()
		raw := errors.New("sqlite: no such table: chat_sessions")
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, raw)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		_, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5)
		So(err, ShouldNotBeNil)
		// 包装后的 i18n 错误不应原样泄漏底层 DB 错误串给前端。
		So(err.Error(), ShouldNotEqual, raw.Error())
	})
}

func TestEnsureGroupMemberSession_CreateError(t *testing.T) {
	Convey("Create 失败时返回包装错误且 id=0", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()
		raw := errors.New("sqlite: disk I/O error")
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			Return(raw)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5)
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldNotEqual, raw.Error())
		So(id, ShouldEqual, 0)
	})
}
