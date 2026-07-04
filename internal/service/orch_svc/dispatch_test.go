package orch_svc_test

import (
	"context"
	"errors"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestDispatch_SpawnsChildSessionAndTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)

	// 注入 no-op enqueue 钩子，避免 goroutine 与 ctrl.Finish 竞态。
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	// 解析派发者会话 → 找到其 Task（拿 RunID）。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, ProjectID: 42}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, in orch_svc.EnsureOrchSessionInput) (int64, error) {
		So(in.ParentSessionID, ShouldEqual, 500)
		So(in.RunID, ShouldEqual, 100)
		So(in.AgentID, ShouldEqual, 3)
		So(in.Isolate, ShouldBeTrue)
		// 子会话标题 = 派发 brief, 避免侧栏显示「(未命名会话)」。
		So(in.Title, ShouldEqual, "实现登录表单")
		So(in.ProjectID, ShouldEqual, int64(42))
		return 600, nil
	})
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		So(tk.ParentTaskID, ShouldEqual, 9)
		So(tk.SessionID, ShouldEqual, 600)
		So(tk.CallSeq, ShouldEqual, 1)
		So(tk.Status, ShouldEqual, orch_entity.TaskRunning)
		tk.ID = 11
		return nil
	})
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)

	Convey("dispatch 异步起子会话 + Task 并立刻返回 taskID", t, func() {
		id, err := orch_svc.Default().Dispatch(context.Background(), 500, "李", "实现登录表单", true, "")
		So(err, ShouldBeNil)
		So(id, ShouldEqual, 11)
	})
}

// TestDispatch_NilParent: FindBySession returns (nil, nil) → errRunNotActive.
func TestDispatch_NilParent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(501)).Return(nil, nil)

	Convey("nil parent → run not active error", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 501, "李", "brief", false, "")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "run not active")
	})
}

// TestDispatch_NilAgent: FindByName returns (nil, nil) → errAgentNotFound.
func TestDispatch_NilAgent(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(502)).Return(
		&orch_entity.Task{ID: 10, RunID: 200, AgentID: 2, SessionID: 502, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "nobody").Return(nil, nil)

	Convey("nil agent → target agent not found error", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 502, "nobody", "brief", false, "")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "target agent not found")
	})
}

// TestDispatch_CountByRunAgentError: CountByRunAgent returns an error.
func TestDispatch_CountByRunAgentError(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	boomCount := errors.New("boom-count")
	tasks.EXPECT().FindBySession(gomock.Any(), int64(503)).Return(
		&orch_entity.Task{ID: 11, RunID: 300, AgentID: 2, SessionID: 503, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(300), int64(3)).Return(int64(0), boomCount)

	Convey("CountByRunAgent error → propagated", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 503, "李", "brief", false, "")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "boom-count")
	})
}

// TestDispatch_EnsureOrchSessionError: EnsureOrchSession returns an error.
func TestDispatch_EnsureOrchSessionError(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	boomEnsure := errors.New("boom-ensure")
	tasks.EXPECT().FindBySession(gomock.Any(), int64(504)).Return(
		&orch_entity.Task{ID: 12, RunID: 400, AgentID: 2, SessionID: 504, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(400), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(400)).Return(&orch_entity.OrchestrationRun{ID: 400, ProjectID: 0}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(0), boomEnsure)

	Convey("EnsureOrchSession error → propagated", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 504, "李", "brief", false, "")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "boom-ensure")
	})
}

func TestDispatch_StoresNodeRef(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	// 注入 no-op enqueue 钩子，避免真实调度器 goroutine 与 ctrl.Finish 竞态(与既有 dispatch 测试一致)。
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 1, RunID: 100}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "FE工程师").Return(&agent_entity.Agent{ID: 3, Name: "FE工程师"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)

	var savedNodeRef string
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		savedNodeRef = tk.NodeRef
		tk.ID = 7
		return nil
	})
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil) // 父转 awaiting-children

	Convey("dispatch 带 node → 子任务落库 NodeRef", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "FE工程师", "做登录页", false, "FE")
		So(err, ShouldBeNil)
		So(savedNodeRef, ShouldEqual, "FE")
	})
}

func TestDispatch_RejectsAgentOutsideAllowedSet(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, nil)
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(
		&orch_entity.Task{ID: 9, RunID: 100, AgentID: 2, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "外人").Return(&agent_entity.Agent{ID: 9, Name: "外人"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(9)).Return(int64(0), nil)
	// 可参与集 {3,4}、Leader=2；目标 9 不在集内且非 Leader → 拒绝，不建会话/任务。
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(
		&orch_entity.OrchestrationRun{ID: 100, LeaderAgentID: 2, AllowedAgentIDs: "[3,4]"}, nil)

	Convey("dispatch 集外 agent → errAgentNotAllowed", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "外人", "brief", false, "")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "not in allowed set")
	})
}
