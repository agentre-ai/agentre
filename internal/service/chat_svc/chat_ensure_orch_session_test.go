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

// TestEnsureSession_OrchChild_PersistsTitle 锁住编排子会话的落库契约：
// 传入的标题种子(Leader=Goal / 派发=Brief / Ask=问题)必须写进 session.title，
// 否则标题落空 → 侧栏「(未命名会话)」。这是编排会话标题缺失 bug 的 DB 侧回归护栏。
func TestEnsureSession_OrchChild_PersistsTitle(t *testing.T) {
	Convey("Given SessionPurposeOrchChild with a title, When EnsureSession, Then session persists title + run_id + purpose", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForSubagentSession(t, ctrl, int64(7), "acceptEdits")

		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.Title, ShouldEqual, "做个登录页")
				So(s.RunID, ShouldEqual, 100)
				So(s.Purpose, ShouldEqual, chat_entity.SessionPurposeOrchChild)
				s.ID = 501
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		resp, err := svc.EnsureSession(context.Background(), &chat_svc.EnsureSessionRequest{
			Purpose: chat_svc.SessionPurposeOrchChild,
			AgentID: 7,
			RunID:   100,
			Title:   "做个登录页",
		})
		So(err, ShouldBeNil)
		So(resp.SessionID, ShouldEqual, 501)
		So(resp.Created, ShouldBeTrue)
	})
}
