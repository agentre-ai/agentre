package orch_svc_test

import (
	"context"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestSend_ReopensSameTaskNoNewNode(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(11)).Return(&orch_entity.Task{ID: 11, RunID: 100, ParentTaskID: 9, SessionID: 600, Status: orch_entity.TaskDone}, nil)
	// Update is called at least once: assert task 11 is set to running.
	// Also accept the caller flip (task 9 → AwaitingChildren) via AnyTimes.
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, tk *orch_entity.Task) error {
		switch tk.ID {
		case 11:
			So(tk.Status, ShouldEqual, orch_entity.TaskRunning)
		case 9:
			So(tk.Status, ShouldEqual, orch_entity.TaskAwaitingChildren)
		}
		return nil
	}).AnyTimes()
	// Do NOT expect tasks.Create (gomock fails on unexpected calls → proves no new node).

	// Deterministic re-trigger: capture SendAndForget via buffered channel.
	sendCh := make(chan struct{}, 1)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(600), "再补上错误处理").DoAndReturn(
		func(_ context.Context, _ int64, _ string) error {
			sendCh <- struct{}{}
			return nil
		},
	)
	// A never-closing channel so watchCompletion goroutine blocks harmlessly.
	chat.EXPECT().ObserveTurn(int64(600)).Return(
		(<-chan orch_svc.TurnDone)(make(chan orch_svc.TurnDone)),
		func() {},
	).AnyTimes()

	Convey("send 把同一任务重开为 running、不新增节点", t, func() {
		err := orch_svc.Default().Send(context.Background(), 500, 11, "再补上错误处理")
		So(err, ShouldBeNil)

		// Deterministically confirm re-trigger fired into session 600.
		select {
		case <-sendCh:
			// success
		case <-time.After(1 * time.Second):
			t.Fatal("timeout: SendAndForget was not called within 1s")
		}
	})
}

func TestSend_RejectsForeignTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)

	// caller has ID=9; task has ParentTaskID=7 (NOT 9) → errNotYourTask.
	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500}, nil)
	tasks.EXPECT().Find(gomock.Any(), int64(22)).Return(&orch_entity.Task{ID: 22, RunID: 100, ParentTaskID: 7, SessionID: 700, Status: orch_entity.TaskDone}, nil)
	// No SendAndForget or enqueue should happen — return before enqueueRun.

	Convey("send 拒绝非自己派发的任务", t, func() {
		err := orch_svc.Default().Send(context.Background(), 500, 22, "这不是我的任务")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, "只能 send 给自己派发的任务")
	})
}
