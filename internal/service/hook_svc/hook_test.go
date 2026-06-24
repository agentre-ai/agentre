package hook_svc

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo"
	"github.com/agentre-ai/agentre/internal/repository/hook_repo/mock_hook_repo"
)

func TestCreateHook_RejectsDuplicateName(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)

	mh.EXPECT().FindByName(gomock.Any(), "jira").
		Return(&hook_entity.Hook{ID: 9, Name: "jira"}, nil)

	svc := &hookSvc{now: func() int64 { return 1000 }}
	_, err := svc.CreateHook(context.Background(), &CreateHookRequest{
		Name: "jira", Interpreter: "bash", Command: "echo '{}'",
		ScheduleExpr: "*/5 * * * *",
	})
	if err == nil {
		t.Fatal("expected duplicate-name error")
	}
}

func TestCreateHook_PersistsAndMasksSecrets(t *testing.T) {
	ctrl := gomock.NewController(t)
	mh := mock_hook_repo.NewMockHookRepo(ctrl)
	hook_repo.RegisterHook(mh)

	mh.EXPECT().FindByName(gomock.Any(), "jira").Return(nil, nil)
	mh.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, h *hook_entity.Hook) error { h.ID = 1; return nil })

	svc := &hookSvc{now: func() int64 { return 1000 }}
	item, err := svc.CreateHook(context.Background(), &CreateHookRequest{
		Name: "jira", Interpreter: "bash", Command: "echo '{}'",
		ScheduleExpr: "*/5 * * * *",
		Env:          []EnvVar{{Key: "TOKEN", Value: "supersecret", Secret: true}},
	})
	if err != nil {
		t.Fatalf("CreateHook: %v", err)
	}
	if item.Env[0].Value != maskedSecret {
		t.Fatalf("secret should be masked in projection, got %q", item.Env[0].Value)
	}
}
