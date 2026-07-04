package orch_svc_test

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestDispatch_EmitsRunUpdated(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, runs, tasks, nil, emit)

	// no-op enqueue 钩子:emit(orch:run:updated)在 Dispatch 内同步发出,不依赖调度器 goroutine。
	// 注入空 enqueue 避免该 goroutine 泄漏到后续测试——否则它的 ObserveTurn/SendAndForget 会在
	// 本测试结束后命中其他测试的 Default().chat mock,造成整包偶发 FAIL(与 dispatch_test 同因)。
	orch_svc.Default().SetEnqueueForTest(func(int64, *orch_entity.Task, string) {})
	t.Cleanup(func() { orch_svc.Default().SetEnqueueForTest(nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).Return(&orch_entity.OrchestrationRun{ID: 100, ProjectID: 0}, nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	// emit 在 Dispatch 内同步发出(no-op enqueue 后无异步路径),DoAndReturn 不能调用 So
	// (无 Convey 上下文),故用普通变量捕获 runId,在 Convey 块里断言。
	var capturedRunID int64
	emit.EXPECT().Emit(gomock.Any(), "orch:run:updated", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, payload any) {
		m := payload.(map[string]any)
		if id, ok := m["runId"].(int64); ok {
			capturedRunID = id
		}
	}).MinTimes(1)

	Convey("dispatch 触发 orch:run:updated 并携带正确 runId", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "李", "做X", false, "")
		So(err, ShouldBeNil)
		So(capturedRunID, ShouldEqual, int64(100))
	})
}
