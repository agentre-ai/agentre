package hook_svc

import (
	"context"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/pkg/hookexec"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo/mock_hook_repo"
)

type fakeRunner struct {
	res *hookexec.RunResult
	err error
}

func (f fakeRunner) Run(_ context.Context, _ hookexec.RunSpec) (*hookexec.RunResult, error) {
	return f.res, f.err
}

func TestRunHook_DryRunParsesButDoesNotPersist(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)
	// dry-run 不应触碰 event repo（没有 EXPECT 即代表「不可被调用」）。
	hook_repo.RegisterHookEvent(mock_hook_repo.NewMockHookEventRepo(ctrl))

	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
	}, nil)

	svc := &hookSvc{
		now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{
			ExitCode: 0, Duration: 10 * time.Millisecond,
			Stdout: []byte(`{"events":[{"title":"t","dedupeKey":"K1","payload":{"a":1}}],"state":{"c":2}}`),
		}},
	}
	out, err := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: true})
	if err != nil {
		t.Fatalf("RunHook: %v", err)
	}
	if out.Persisted || len(out.Events) != 1 || out.NewCount != 1 {
		t.Fatalf("dry-run unexpected: %+v", out)
	}
}

func TestRunHook_RealPersistsDedupAndState(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	me := mock_hook_repo.NewMockHookEventRepo(ctrl)
	hook_repo.RegisterHook(mh)
	hook_repo.RegisterHookEvent(me)

	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
		ScheduleExpr: "*/5 * * * *",
	}, nil)
	// 第一条新事件 → 查重未命中 → 落库；hook 状态回写。
	me.EXPECT().FindByDedupeKey(gomock.Any(), int64(1), "K1").Return(nil, nil)
	me.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	mh.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error {
			if h.LastStatus != "ok" || h.TotalCount != 1 {
				t.Errorf("hook not updated correctly: %+v", h)
			}
			return nil
		})

	svc := &hookSvc{
		now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{ExitCode: 0,
			Stdout: []byte(`{"events":[{"title":"t","dedupeKey":"K1"}],"state":{"c":2}}`)}},
	}
	out, err := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: false})
	if err != nil || !out.Persisted || out.NewCount != 1 {
		t.Fatalf("real run unexpected: out=%+v err=%v", out, err)
	}
}

func TestRunHook_NonZeroExitMarksFailed(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)
	hook_repo.RegisterHookEvent(mock_hook_repo.NewMockHookEventRepo(ctrl))
	mh.EXPECT().Find(gomock.Any(), int64(1)).Return(&hook_entity.Hook{
		ID: 1, Name: "j", Interpreter: "bash", Command: "x", EnvJSON: "[]", StateJSON: "{}",
		ScheduleExpr: "*/5 * * * *"}, nil)
	mh.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error {
			if h.LastStatus != "failed" {
				t.Errorf("expected failed, got %q", h.LastStatus)
			}
			return nil
		})
	svc := &hookSvc{now: func() int64 { return 1000 },
		runner: fakeRunner{res: &hookexec.RunResult{ExitCode: 1, Stderr: []byte("boom")}}}
	out, _ := svc.RunHook(context.Background(), &RunHookRequest{ID: 1, DryRun: false})
	if out.ExitCode != 1 {
		t.Fatalf("expected exit 1, got %+v", out)
	}
}
