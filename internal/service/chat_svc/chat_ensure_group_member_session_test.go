package chat_svc_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// registerAgentBackendForGroupSession 注册 agent(agentID→backendID 12)+ claudecode 后端,
// 让 ensureGroupMemberSession 的创建分支能解析出 backing session 的默认权限模式。
// 用 AnyTimes 容忍创建路径调用一次、复用/早退路径零次。
func registerAgentBackendForGroupSession(t *testing.T, ctrl *gomock.Controller, agentID int64, defaultMode string) {
	t.Helper()
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	prevA := agent_repo.Agent()
	agent_repo.RegisterAgent(agentRepo)
	t.Cleanup(func() { agent_repo.RegisterAgent(prevA) })
	agentRepo.EXPECT().Find(gomock.Any(), agentID).
		Return(&agent_entity.Agent{ID: agentID, AgentBackendID: 12}, nil).AnyTimes()

	beRepo := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	prevB := agent_backend_repo.AgentBackend()
	agent_backend_repo.RegisterAgentBackend(beRepo)
	t.Cleanup(func() { agent_backend_repo.RegisterAgentBackend(prevB) })
	beRepo.EXPECT().Find(gomock.Any(), int64(12)).
		Return(&agent_backend_entity.AgentBackend{
			ID:                    12,
			Type:                  string(agent_backend_entity.TypeClaudeCode),
			DefaultPermissionMode: defaultMode,
			Status:                consts.ACTIVE,
		}, nil).AnyTimes()
}

func TestEnsureGroupMemberSession_CreatesWithGroupID(t *testing.T) {
	Convey("给定群内该 agent 无 session, 应新建一个带 group_id 的 session", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForGroupSession(t, ctrl, int64(3), "")

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
		id, err := svc.EnsureGroupMemberSession(ctx, 3 /*agentID*/, 0 /*projectID*/, 5 /*groupID*/, "支付小队 / 后端")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 7)
	})
}

func TestEnsureGroupMemberSession_PersistsLaunchPermissionMode(t *testing.T) {
	Convey("Given 群成员 backing session 新建, 应按 agent 后端默认权限模式同步落 PermissionMode + PermissionModeAtLaunch", t, func() {
		// 对齐普通新建会话(send 新建分支同步落 mode + at_launch):否则前端中途打开刚建好的
		// backing session, 在 runtime 异步回填 at_launch 之前 LoadSession 读到空串会把 bypass pill 错灰。
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		registerAgentBackendForGroupSession(t, ctrl, int64(3), "acceptEdits")

		ctx := context.Background()
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.PermissionMode, ShouldEqual, "acceptEdits")
				So(s.PermissionModeAtLaunch, ShouldEqual, "acceptEdits")
				s.ID = 88
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 88)
	})
}

func TestEnsureSession_GroupMemberPurpose(t *testing.T) {
	Convey("Given a group member session purpose, When no active session exists, Then EnsureSession creates a reusable group backing session", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForGroupSession(t, ctrl, int64(3), "")

		ctx := context.Background()
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.AgentID, ShouldEqual, 3)
				So(s.ProjectID, ShouldEqual, 9)
				So(s.GroupID, ShouldEqual, 5)
				So(s.Title, ShouldEqual, "支付小队 / 后端")
				So(s.AgentStatus, ShouldEqual, "idle")
				s.ID = 77
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		resp, err := svc.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
			Purpose:   chat_svc.SessionPurposeGroupMember,
			AgentID:   3,
			ProjectID: 9,
			GroupID:   5,
			Title:     "支付小队 / 后端",
		})
		So(err, ShouldBeNil)
		So(resp.SessionID, ShouldEqual, 77)
		So(resp.Created, ShouldBeTrue)
	})

	Convey("Given a group member session already exists, When EnsureSession is called, Then it reuses the existing session", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(&chat_entity.Session{ID: 42, AgentID: 3, GroupID: 5, Title: "支付小队 / 后端"}, nil)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		resp, err := svc.EnsureSession(ctx, &chat_svc.EnsureSessionRequest{
			Purpose: chat_svc.SessionPurposeGroupMember,
			AgentID: 3,
			GroupID: 5,
			Title:   "ignored on reuse",
		})
		So(err, ShouldBeNil)
		So(resp.SessionID, ShouldEqual, 42)
		So(resp.Created, ShouldBeFalse)
	})
}

func TestEnsureSession_GroupMemberPurposeSerializesConcurrentCreate(t *testing.T) {
	Convey("Given a group member session is being created, When the same session is ensured concurrently, Then only one backing session is created", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForGroupSession(t, ctrl, int64(3), "")

		ctx := context.Background()
		var created atomic.Bool
		createStarted := make(chan struct{})
		allowCreate := make(chan struct{})
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			DoAndReturn(func(context.Context, int64, int64) (*chat_entity.Session, error) {
				if created.Load() {
					return &chat_entity.Session{ID: 77, AgentID: 3, GroupID: 5, Title: "支付小队 / 后端"}, nil
				}
				return nil, nil
			}).AnyTimes()
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				close(createStarted)
				<-allowCreate
				s.ID = 77
				created.Store(true)
				return nil
			}).Times(1)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		req := &chat_svc.EnsureSessionRequest{
			Purpose:   chat_svc.SessionPurposeGroupMember,
			AgentID:   3,
			ProjectID: 9,
			GroupID:   5,
			Title:     "支付小队 / 后端",
		}
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		ids := make(chan int64, 2)
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.EnsureSession(ctx, req)
			if err != nil {
				errs <- err
				return
			}
			ids <- resp.SessionID
		}()
		select {
		case <-createStarted:
		case <-time.After(2 * time.Second):
			t.Fatal("first EnsureSession did not enter Create")
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := svc.EnsureSession(ctx, req)
			if err != nil {
				errs <- err
				return
			}
			ids <- resp.SessionID
		}()
		time.Sleep(50 * time.Millisecond)
		close(allowCreate)
		wg.Wait()
		close(errs)
		close(ids)
		So(len(errs), ShouldEqual, 0)
		So(len(ids), ShouldEqual, 2)
		for id := range ids {
			So(id, ShouldEqual, 77)
		}
	})
}

func TestEnsureGroupMemberSession_CreatesWithGroupTitle(t *testing.T) {
	Convey("Given a group member session is created, When a title is supplied, Then the backing session stores the group title instead of staying unnamed", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })
		registerAgentBackendForGroupSession(t, ctrl, int64(3), "")

		ctx := context.Background()
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.Title, ShouldEqual, "支付小队 / 后端")
				So(s.GroupID, ShouldEqual, 5)
				s.ID = 7
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
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

		existing := &chat_entity.Session{ID: 42, AgentID: 3, GroupID: 5, Title: "支付小队 / 后端"}
		// 查既有 (group_id, agent_id) → 找到
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(existing, nil)
		// Create 不应被调用（幂等路径）

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3 /*agentID*/, 0 /*projectID*/, 5 /*groupID*/, "支付小队 / 后端")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 42)
	})
}

func TestEnsureGroupMemberSession_RepairsExistingUntitledSession(t *testing.T) {
	Convey("Given a group backing session already exists without a title, When ensuring it with a title, Then it reuses the session and repairs the title", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()
		existing := &chat_entity.Session{ID: 42, AgentID: 3, GroupID: 5, Title: "  "}
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(existing, nil)
		sessRepo.EXPECT().
			Update(gomock.Any(), gomock.Any()).
			DoAndReturn(func(_ context.Context, s *chat_entity.Session) error {
				So(s.ID, ShouldEqual, 42)
				So(s.Title, ShouldEqual, "支付小队 / 后端")
				return nil
			})

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 42)
	})

	Convey("Given a group backing session already has a title, When ensuring it with another title, Then it does not overwrite the existing title", t, func() {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)

		sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
		prev := chat_repo.Session()
		chat_repo.RegisterSession(sessRepo)
		t.Cleanup(func() { chat_repo.RegisterSession(prev) })

		ctx := context.Background()
		existing := &chat_entity.Session{ID: 42, AgentID: 3, GroupID: 5, Title: "旧标题"}
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(existing, nil)
		// Update 不应被调用，避免覆盖已有非空标题。

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 42)
	})
}

func TestEnsureGroupMemberSession_InvalidParams(t *testing.T) {
	Convey("参数校验", t, func() {
		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		ctx := context.Background()

		Convey("agentID=0 → error", func() {
			_, err := svc.EnsureGroupMemberSession(ctx, 0, 0, 5, "支付小队 / 后端")
			So(err, ShouldNotBeNil)
		})

		Convey("groupID=0 → error", func() {
			_, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 0, "支付小队 / 后端")
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
		_, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
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
		registerAgentBackendForGroupSession(t, ctrl, int64(3), "")

		ctx := context.Background()
		raw := errors.New("sqlite: disk I/O error")
		sessRepo.EXPECT().
			FindByGroupAndAgent(gomock.Any(), int64(5), int64(3)).
			Return(nil, nil)
		sessRepo.EXPECT().
			Create(gomock.Any(), gomock.Any()).
			Return(raw)

		svc := chat_svc.NewChat(chat_svc.NoopEmitter{})
		id, err := svc.EnsureGroupMemberSession(ctx, 3, 0, 5, "支付小队 / 后端")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldNotEqual, raw.Error())
		So(id, ShouldEqual, 0)
	})
}
