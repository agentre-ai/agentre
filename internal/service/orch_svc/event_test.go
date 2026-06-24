package orch_svc_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

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
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	emit := mock_orch_svc.NewMockEmitter(ctrl)
	orch_svc.Default().RegisterDeps(chat, agents, nil, tasks, nil, emit)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, Status: orch_entity.TaskRunning}, nil)
	agents.EXPECT().FindByName(gomock.Any(), "李").Return(&agent_entity.Agent{ID: 3, Name: "李"}, nil)
	tasks.EXPECT().CountByRunAgent(gomock.Any(), int64(100), int64(3)).Return(int64(0), nil)
	chat.EXPECT().EnsureOrchSession(gomock.Any(), gomock.Any()).Return(int64(600), nil)
	tasks.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	chat.EXPECT().SendAndForget(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	chat.EXPECT().ObserveTurn(gomock.Any()).Return(make(<-chan orch_svc.TurnDone), func() {}).AnyTimes()

	// emit 从调度器 goroutine 中发出,不能在 DoAndReturn 里调用 So（无 Convey 上下文）。
	// 用原子变量捕获 runId，等 emit 到达后再在 Convey 块里断言。
	// DoAndReturn 同时关闭 emitDone，确保 ctrl.Finish 在 emit 之后调用（避免竞态）。
	var capturedRunID atomic.Int64
	emitDone := make(chan struct{})
	emit.EXPECT().Emit(gomock.Any(), "orch:run:updated", gomock.Any()).DoAndReturn(func(_ context.Context, _ string, payload any) {
		m := payload.(map[string]any)
		if id, ok := m["runId"].(int64); ok {
			capturedRunID.Store(id)
		}
		select {
		case <-emitDone:
		default:
			close(emitDone)
		}
	}).MinTimes(1)

	Convey("dispatch 触发 orch:run:updated 并携带正确 runId", t, func() {
		_, err := orch_svc.Default().Dispatch(context.Background(), 500, "李", "做X", false)
		So(err, ShouldBeNil)
		// 等待 emit 到达（最多 1s），确保断言有效且 ctrl.Finish 安全。
		select {
		case <-emitDone:
		case <-time.After(time.Second):
			t.Fatal("orch:run:updated 未在 1s 内发出")
		}
		So(capturedRunID.Load(), ShouldEqual, int64(100))
	})
	// ctrl.Finish 在 Convey 块（包含同步等待）之后调用，避免 goroutine 在 Finish 后回调 mock。
	ctrl.Finish()
}
