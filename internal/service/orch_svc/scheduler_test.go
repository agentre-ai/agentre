package orch_svc_test

import (
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestScheduler_CapsConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	chat := mock_orch_svc.NewMockChatGateway(ctrl)
	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
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
				&orch_entity.Task{ID: int64(i), RunID: 100, SessionID: int64(600 + i)},
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
