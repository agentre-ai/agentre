package chat_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

func TestEnsureSession_SubagentCall(t *testing.T) {
	Convey("Given SessionPurposeSubagentCall, When EnsureSession is called, Then it creates a fresh session every time (non-idempotent)", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForGroupSession(t, ctrl, int64(7), "acceptEdits")

		ctx := context.Background()

		// First call: expects Create, returns id=101
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.AgentID, ShouldEqual, 7)
				So(s.GroupID, ShouldEqual, 0)
				So(s.AgentStatus, ShouldEqual, "idle")
				So(s.PermissionMode, ShouldEqual, "acceptEdits")
				So(s.PermissionModeAtLaunch, ShouldEqual, "acceptEdits")
				// 子 agent 会话必须落 purpose 标记, repo 层据此从所有会话列表/计数隐藏它。
				So(s.Purpose, ShouldEqual, chat_entity.SessionPurposeSubagent)
				s.ID = 101
				return nil
			})

		// Second call: expects another Create, returns id=202
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				s.ID = 202
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})

		resp1, err := svc.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
			Purpose:   chat_svc.SessionPurposeSubagentCall,
			AgentID:   7,
			ProjectID: 0,
			Title:     "subagent task",
		})
		So(err, ShouldBeNil)
		So(resp1.SessionID, ShouldEqual, 101)
		So(resp1.Created, ShouldBeTrue)

		resp2, err := svc.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
			Purpose:   chat_svc.SessionPurposeSubagentCall,
			AgentID:   7,
			ProjectID: 0,
			Title:     "subagent task",
		})
		So(err, ShouldBeNil)
		So(resp2.SessionID, ShouldEqual, 202)
		So(resp2.Created, ShouldBeTrue)

		// Two successive calls must produce distinct session IDs (non-idempotent)
		So(resp1.SessionID, ShouldNotEqual, resp2.SessionID)
	})

	Convey("Given SessionPurposeSubagentCall with agentID=0, When EnsureSession is called, Then it returns InvalidParameter error", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ctx := context.Background()

		_, err := svc.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
			Purpose: chat_svc.SessionPurposeSubagentCall,
			AgentID: 0,
		})
		So(err, ShouldNotBeNil)
	})
}

func TestSessionProjectID(t *testing.T) {
	Convey("Given a session with a project, When SessionProjectID is called, Then it returns that project id", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		sessRepo.EXPECT().Find(gomock.Any(), int64(55)).
			Return(&chat_entity.Session{ID: 55, ProjectID: 42}, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		pid, err := svc.SessionProjectID(context.Background(), 55)
		So(err, ShouldBeNil)
		So(pid, ShouldEqual, 42)
	})

	Convey("Given sessionID<=0, When SessionProjectID is called, Then it returns 0 without hitting the repo", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		pid, err := svc.SessionProjectID(context.Background(), 0)
		So(err, ShouldBeNil)
		So(pid, ShouldEqual, 0)
	})
}
