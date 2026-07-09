package orch_svc_test

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

type operationFailedLike struct {
	cause error
}

func (e operationFailedLike) Error() string {
	return "Operation failed"
}

func (e operationFailedLike) Unwrap() error {
	return e.cause
}

func TestScheduler_CapsConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	// Reset stale state from any prior test that touched the singleton.
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(2)
	t.Cleanup(func() {
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	// sendCh signals each time SendAndForget is called — deterministic, no sleep.
	sendCh := make(chan struct{}, 8)
	chat.EXPECT().SendAndForget(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, _ int64, _ string) error {
			sendCh <- struct{}{}
			return nil
		}).AnyTimes()

	// ObserveTurn never returns → tasks stay "in flight" until manually settled.
	never := make(chan orch_svc.TurnDone)
	chat.EXPECT().ObserveTurn(gomock.Any()).
		Return((<-chan orch_svc.TurnDone)(never), func() {}).AnyTimes()

	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	Convey("cap=2 时第三个任务排队，settle 后才发射", t, func() {
		for i := 1; i <= 3; i++ {
			orch_svc.Default().EnqueueRunForTest(100,
				&orch_entity.Dispatch{ID: int64(i), RunID: 100, SessionID: int64(600 + i)},
				"go")
		}

		// Exactly 2 SendAndForget calls should fire (blocking reads — deterministic).
		<-sendCh
		<-sendCh

		// 3rd must NOT fire yet — wait a bounded window.
		select {
		case <-sendCh:
			t.Fatal("3rd task launched despite cap=2")
		case <-time.After(100 * time.Millisecond):
			// correct: 3rd is still queued
		}

		// Free one slot → 3rd should now launch.
		orch_svc.Default().OnTaskSettledForTest(100)

		select {
		case <-sendCh:
			// correct: 3rd fired after slot was freed
		case <-time.After(500 * time.Millisecond):
			t.Fatal("3rd task did not launch after OnTaskSettled")
		}
	})
}

func TestScheduler_RetriesTransientSendBusy(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(1)
	restoreBackoff := orch_svc.SetSendRetryBackoffForTest([]time.Duration{0})
	t.Cleanup(func() {
		restoreBackoff()
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	done := make(chan orch_svc.TurnDone)
	close(done)
	chat.EXPECT().ObserveTurn(int64(601)).
		Return((<-chan orch_svc.TurnDone)(done), func() {})

	var attempts atomic.Int32
	sendCh := make(chan struct{}, 2)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(601), "go").
		DoAndReturn(func(_ any, _ int64, _ string) error {
			attempts.Add(1)
			sendCh <- struct{}{}
			return errors.New("database is locked (5) (SQLITE_BUSY)")
		})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(601), "go").
		DoAndReturn(func(_ any, _ int64, _ string) error {
			attempts.Add(1)
			sendCh <- struct{}{}
			return nil
		})

	Convey("SQLite 写锁瞬时冲突时重试发送子任务", t, func() {
		orch_svc.Default().EnqueueRunForTest(100,
			&orch_entity.Dispatch{ID: 1, RunID: 100, SessionID: 601},
			"go")

		select {
		case <-sendCh:
		case <-time.After(time.Second):
			t.Fatal("first SendAndForget was not called")
		}
		select {
		case <-sendCh:
		case <-time.After(time.Second):
			t.Fatal("SendAndForget was not retried")
		}
		So(attempts.Load(), ShouldEqual, 2)
	})
}

func TestScheduler_RetriesWrappedTransientSendBusy(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(1)
	restoreBackoff := orch_svc.SetSendRetryBackoffForTest([]time.Duration{0})
	t.Cleanup(func() {
		restoreBackoff()
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	done := make(chan orch_svc.TurnDone)
	close(done)
	chat.EXPECT().ObserveTurn(int64(601)).
		Return((<-chan orch_svc.TurnDone)(done), func() {})

	var attempts atomic.Int32
	sendCh := make(chan struct{}, 2)
	chat.EXPECT().SendAndForget(gomock.Any(), int64(601), "go").
		DoAndReturn(func(_ any, _ int64, _ string) error {
			attempts.Add(1)
			sendCh <- struct{}{}
			return operationFailedLike{cause: errors.New("database is locked (5) (SQLITE_BUSY)")}
		})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(601), "go").
		DoAndReturn(func(_ any, _ int64, _ string) error {
			attempts.Add(1)
			sendCh <- struct{}{}
			return nil
		})

	Convey("SQLite 写锁被 chat 层包装成业务错误时仍按底层 cause 重试", t, func() {
		orch_svc.Default().EnqueueRunForTest(100,
			&orch_entity.Dispatch{ID: 1, RunID: 100, SessionID: 601},
			"go")

		select {
		case <-sendCh:
		case <-time.After(time.Second):
			t.Fatal("first SendAndForget was not called")
		}
		select {
		case <-sendCh:
		case <-time.After(time.Second):
			t.Fatal("wrapped transient SendAndForget was not retried")
		}
		So(attempts.Load(), ShouldEqual, 2)
	})
}

func TestScheduler_MarksTaskErrorWhenSendFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(chat, nil, nil, tasks, nil, nil)
	orch_svc.Default().ResetSchedulersForTest()
	orch_svc.Default().SetSchedulerCapForTest(1)
	restoreBackoff := orch_svc.SetSendRetryBackoffForTest(nil)
	t.Cleanup(func() {
		restoreBackoff()
		orch_svc.Default().SetSchedulerCapForTest(0)
		orch_svc.Default().ResetSchedulersForTest()
	})

	done := make(chan orch_svc.TurnDone)
	close(done)
	chat.EXPECT().ObserveTurn(int64(601)).
		Return((<-chan orch_svc.TurnDone)(done), func() {})
	chat.EXPECT().SendAndForget(gomock.Any(), int64(601), "go").
		Return(errors.New("send failed"))

	updated := make(chan *orch_entity.Dispatch, 1)
	tasks.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ any, task *orch_entity.Dispatch) error {
			updated <- task
			return nil
		})

	Convey("发送子任务最终失败时任务落 error 而不是永久 running", t, func() {
		orch_svc.Default().EnqueueRunForTest(100,
			&orch_entity.Dispatch{ID: 1, RunID: 100, SessionID: 601},
			"go")

		select {
		case task := <-updated:
			So(task.Status, ShouldEqual, orch_entity.DispatchError)
		case <-time.After(time.Second):
			t.Fatal("task was not marked error")
		}
	})
}
