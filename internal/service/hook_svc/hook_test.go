package hook_svc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/emersion/go-imap/v2"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"agentre/internal/model/entity/agent_entity"
	"agentre/internal/model/entity/hook_entity"
	"agentre/internal/repository/agent_repo"
	"agentre/internal/repository/agent_repo/mock_agent_repo"
	"agentre/internal/repository/hook_repo"
	"agentre/internal/repository/hook_repo/mock_hook_repo"
)

type fakeMailFetcher struct {
	messages    []EmailMessage
	err         error
	cfg         SourceConfig
	limit       int
	uidValidity uint32
	cursorReset bool
}

func (f *fakeMailFetcher) FetchUnread(_ context.Context, cfg SourceConfig, limit int) (*MailFetchResult, error) {
	f.cfg = cfg
	f.limit = limit
	if f.err != nil {
		return nil, f.err
	}
	return &MailFetchResult{
		Messages:    f.messages,
		UIDValidity: f.uidValidity,
		CursorReset: f.cursorReset,
	}, nil
}

func setupHookSvc(t *testing.T) (
	context.Context,
	*mock_hook_repo.MockHookSourceRepo,
	*mock_hook_repo.MockHookRuleRepo,
	*mock_hook_repo.MockHookEventRepo,
	*mock_agent_repo.MockAgentRepo,
	*hookSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	sourceRepo := mock_hook_repo.NewMockHookSourceRepo(ctrl)
	ruleRepo := mock_hook_repo.NewMockHookRuleRepo(ctrl)
	eventRepo := mock_hook_repo.NewMockHookEventRepo(ctrl)
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	hook_repo.RegisterHookSource(sourceRepo)
	hook_repo.RegisterHookRule(ruleRepo)
	hook_repo.RegisterHookEvent(eventRepo)
	agent_repo.RegisterAgent(agentRepo)
	return context.Background(), sourceRepo, ruleRepo, eventRepo, agentRepo, &hookSvc{now: func() int64 { return 1700000000 }}
}

func ceoAgent() *agent_entity.Agent {
	return &agent_entity.Agent{
		ID:           1,
		Name:         "CEO 助手",
		AvatarColor:  "agent-1",
		SystemBadge:  "DEFAULT",
		Status:       consts.ACTIVE,
		PromptJSON:   "[]",
		SkillsJSON:   "[]",
		DepartmentID: 0,
	}
}

func TestCreateSourceCreatesFallbackRule(t *testing.T) {
	ctx, sourceRepo, ruleRepo, _, agentRepo, svc := setupHookSvc(t)

	sourceRepo.EXPECT().FindByName(gomock.Any(), "agentre-bot").Return(nil, nil)
	sourceRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, source *hook_entity.HookSource) error {
			assert.Equal(t, "github", source.Kind)
			assert.Equal(t, "agentre-bot", source.Name)
			assert.Equal(t, 1, source.Enabled)
			assert.Equal(t, string(hook_entity.ConnectionPending), source.ConnectionStatus)
			assert.Contains(t, source.ConfigJSON, "webhookUrl")
			source.ID = 42
			return nil
		})
	ruleRepo.EXPECT().ListBySource(gomock.Any(), int64(42)).Return(nil, nil)
	agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{ceoAgent()}, nil)
	ruleRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, rule *hook_entity.HookRule) error {
			assert.Equal(t, int64(42), rule.SourceID)
			assert.Equal(t, "兜底规则", rule.Name)
			assert.Equal(t, int64(1), rule.TargetAgentID)
			assert.Equal(t, 1, rule.IsFallback)
			return nil
		})

	resp, err := svc.CreateSource(ctx, &CreateHookSourceRequest{
		Kind:       "github",
		Name:       " agentre-bot ",
		Identifier: "agentre-frame",
		Enabled:    true,
		Config: SourceConfig{
			WebhookURL:      "https://agentre.local/hooks/abc",
			VerifySignature: true,
			Events:          []string{"pull_request"},
		},
	})

	assert.NoError(t, err)
	assert.Equal(t, int64(42), resp.Item.ID)
	assert.True(t, resp.Item.Enabled)
}

func TestCreateSourceRejectsDuplicateName(t *testing.T) {
	ctx, sourceRepo, _, _, _, svc := setupHookSvc(t)

	sourceRepo.EXPECT().FindByName(gomock.Any(), "agentre-bot").
		Return(&hook_entity.HookSource{ID: 7, Name: "agentre-bot", Status: consts.ACTIVE}, nil)

	_, err := svc.CreateSource(ctx, &CreateHookSourceRequest{
		Kind:    "github",
		Name:    "agentre-bot",
		Enabled: true,
		Config:  SourceConfig{},
	})

	assert.Error(t, err)
}

func TestSourceToItemNormalizesMissingEventList(t *testing.T) {
	item := sourceToItem(&hook_entity.HookSource{
		ID:               1,
		Kind:             "email",
		Name:             "工作邮箱",
		ConfigJSON:       `{"imapServer":"imap.gmail.com"}`,
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionConnected),
		Status:           consts.ACTIVE,
	})

	assert.NotNil(t, item.Config.Events)
	assert.Empty(t, item.Config.Events)
}

func TestSourceToItemMasksAppPassword(t *testing.T) {
	source := &hook_entity.HookSource{
		ID:               1,
		Kind:             "email",
		Name:             "工作邮箱",
		ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret"}),
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionConnected),
		Status:           consts.ACTIVE,
	}

	item := sourceToItem(source)

	assert.Equal(t, maskedSecret, item.Config.AppPassword)
	assert.Equal(t, "secret", parseSourceConfig(source.ConfigJSON).AppPassword)
}

func TestUpdateSourcePreservesAppPasswordWhenBlankOrMasked(t *testing.T) {
	cases := []struct {
		name     string
		password string
	}{
		{name: "blank", password: ""},
		{name: "masked", password: maskedSecret},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, sourceRepo, _, _, _, svc := setupHookSvc(t)
			source := &hook_entity.HookSource{
				ID:               2,
				Kind:             string(hook_entity.SourceKindEmail),
				Name:             "工作邮箱",
				Identifier:       "ops@example.com",
				ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret", LastUID: 41, UIDValidity: 9}),
				Enabled:          1,
				ConnectionStatus: string(hook_entity.ConnectionConnected),
				Status:           consts.ACTIVE,
			}

			sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
			sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
				DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
					cfg := parseSourceConfig(got.ConfigJSON)
					assert.Equal(t, "imap2.example.com", cfg.IMAPServer)
					assert.Equal(t, "secret", cfg.AppPassword)
					assert.Equal(t, uint32(41), cfg.LastUID)
					assert.Equal(t, uint32(9), cfg.UIDValidity)
					return nil
				})

			resp, err := svc.UpdateSource(ctx, &UpdateHookSourceRequest{
				ID:         2,
				Kind:       string(hook_entity.SourceKindEmail),
				Name:       "工作邮箱",
				Identifier: "ops@example.com",
				Enabled:    true,
				Config: SourceConfig{
					IMAPServer:      "imap2.example.com",
					EmailAddress:    "ops@example.com",
					AppPassword:     tc.password,
					LastUID:         41,
					UIDValidity:     9,
					PollingInterval: "5m",
				},
			})

			assert.NoError(t, err)
			assert.Equal(t, maskedSecret, resp.Item.Config.AppPassword)
		})
	}
}

func TestDeleteFallbackRuleRejected(t *testing.T) {
	ctx, _, ruleRepo, _, _, svc := setupHookSvc(t)

	ruleRepo.EXPECT().Find(gomock.Any(), int64(4)).
		Return(&hook_entity.HookRule{ID: 4, SourceID: 2, Name: "兜底规则", IsFallback: 1, Status: consts.ACTIVE}, nil)

	_, err := svc.DeleteRule(ctx, &DeleteHookRuleRequest{ID: 4})

	assert.Error(t, err)
}

func TestTestSourceCreatesLocalEventWithoutAgentRuntime(t *testing.T) {
	ctx, sourceRepo, ruleRepo, eventRepo, agentRepo, svc := setupHookSvc(t)
	source := &hook_entity.HookSource{
		ID:               2,
		Kind:             "github",
		Name:             "agentre-bot",
		Identifier:       "agentre-frame",
		ConfigJSON:       "{}",
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionPending),
		TotalCount:       1284,
		Status:           consts.ACTIVE,
	}
	fallback := &hook_entity.HookRule{
		ID:            4,
		SourceID:      2,
		Name:          "兜底规则",
		ConditionExpr: "未命中任何规则",
		TargetAgentID: 1,
		Enabled:       1,
		IsFallback:    1,
		Status:        consts.ACTIVE,
	}

	sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
	sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
			assert.Equal(t, string(hook_entity.ConnectionConnected), got.ConnectionStatus)
			assert.Equal(t, int64(1285), got.TotalCount)
			return nil
		})
	agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{ceoAgent()}, nil)
	ruleRepo.EXPECT().ListBySource(gomock.Any(), int64(2)).Return([]*hook_entity.HookRule{fallback}, nil)
	eventRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *hook_entity.HookEvent) error {
			assert.Equal(t, "connection_test", event.EventType)
			assert.Equal(t, string(hook_entity.EventDispatched), event.EventStatus)
			dispatches := []HookDispatchItem{}
			assert.NoError(t, json.Unmarshal([]byte(event.DispatchesJSON), &dispatches))
			assert.Len(t, dispatches, 1)
			assert.Equal(t, "queued", dispatches[0].Status)
			event.ID = 88
			return nil
		})

	resp, err := svc.TestSource(ctx, &TestHookSourceRequest{ID: 2})

	assert.NoError(t, err)
	assert.Equal(t, int64(88), resp.Event.ID)
	assert.Equal(t, "queued", resp.Event.Dispatches[0].Status)
	assert.Contains(t, resp.Event.Dispatches[0].Message, "not enabled")
}

func TestSyncEmailSourceCreatesEventAndUpdatesCursor(t *testing.T) {
	ctx, sourceRepo, ruleRepo, eventRepo, agentRepo, svc := setupHookSvc(t)
	fetcher := &fakeMailFetcher{
		messages: []EmailMessage{
			{
				UID:       42,
				MessageID: "message-42@example.com",
				Subject:   "Invoice approved",
				From:      "Alice <alice@example.com>",
				To:        []string{"ops@example.com"},
				Date:      time.Unix(1699999900, 0),
				Text:      "The invoice has been approved.",
			},
		},
	}
	svc.mailFetcher = fetcher
	source := &hook_entity.HookSource{
		ID:               2,
		Kind:             string(hook_entity.SourceKindEmail),
		Name:             "工作邮箱",
		Identifier:       "ops@example.com",
		ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret", LastUID: 41}),
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionPending),
		TotalCount:       9,
		Status:           consts.ACTIVE,
	}
	rule := &hook_entity.HookRule{
		ID:            10,
		SourceID:      2,
		Name:          "Invoice",
		ConditionExpr: `subject contains "invoice"`,
		TargetAgentID: 1,
		Enabled:       1,
		Status:        consts.ACTIVE,
	}

	sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
	eventRepo.EXPECT().FindBySourceRef(gomock.Any(), int64(2), "message-42@example.com").Return(nil, nil)
	agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{ceoAgent()}, nil)
	ruleRepo.EXPECT().ListBySource(gomock.Any(), int64(2)).Return([]*hook_entity.HookRule{rule}, nil)
	eventRepo.EXPECT().Create(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, event *hook_entity.HookEvent) error {
			assert.Equal(t, "Invoice approved", event.Title)
			assert.Equal(t, "message-42@example.com", event.SourceRef)
			assert.Equal(t, "Alice <alice@example.com>", event.Sender)
			assert.Equal(t, "email.received", event.EventType)
			assert.Equal(t, string(hook_entity.EventDispatched), event.EventStatus)
			payload := map[string]any{}
			assert.NoError(t, json.Unmarshal([]byte(event.PayloadJSON), &payload))
			assert.Equal(t, "Invoice approved", payload["subject"])
			assert.Equal(t, false, payload["attachmentsDownloaded"])
			event.ID = 300
			return nil
		})
	sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
			assert.Equal(t, string(hook_entity.ConnectionConnected), got.ConnectionStatus)
			assert.Equal(t, int64(1700000000), got.LastSyncTime)
			assert.Equal(t, int64(10), got.TotalCount)
			cfg := parseSourceConfig(got.ConfigJSON)
			assert.Equal(t, uint32(42), cfg.LastUID)
			return nil
		})

	resp, err := svc.SyncEmailSource(ctx, &SyncEmailSourceRequest{ID: 2, Limit: 5})

	assert.NoError(t, err)
	assert.Equal(t, uint32(41), fetcher.cfg.LastUID)
	assert.Equal(t, 5, fetcher.limit)
	assert.Equal(t, 1, resp.Created)
	assert.Equal(t, 0, resp.Skipped)
	assert.Len(t, resp.Events, 1)
	assert.Equal(t, int64(300), resp.Events[0].ID)
	assert.Contains(t, resp.Events[0].MatchedRuleNames, "Invoice")
	assert.Equal(t, maskedSecret, resp.Item.Config.AppPassword)
}

func TestSyncEmailSourceResetsCursorWhenUIDValidityChanges(t *testing.T) {
	ctx, sourceRepo, ruleRepo, _, agentRepo, svc := setupHookSvc(t)
	svc.mailFetcher = &fakeMailFetcher{
		uidValidity: 20,
		cursorReset: true,
	}
	source := &hook_entity.HookSource{
		ID:               2,
		Kind:             string(hook_entity.SourceKindEmail),
		Name:             "工作邮箱",
		ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret", LastUID: 99, UIDValidity: 10}),
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionConnected),
		Status:           consts.ACTIVE,
	}

	sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
	agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{ceoAgent()}, nil)
	ruleRepo.EXPECT().ListBySource(gomock.Any(), int64(2)).Return(nil, nil)
	sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
			cfg := parseSourceConfig(got.ConfigJSON)
			assert.Equal(t, uint32(0), cfg.LastUID)
			assert.Equal(t, uint32(20), cfg.UIDValidity)
			return nil
		})

	resp, err := svc.SyncEmailSource(ctx, &SyncEmailSourceRequest{ID: 2})

	assert.NoError(t, err)
	assert.Equal(t, 0, resp.Created)
	assert.Equal(t, 0, resp.Skipped)
}

func TestSyncEmailSourceSkipsDuplicateMessage(t *testing.T) {
	ctx, sourceRepo, ruleRepo, eventRepo, agentRepo, svc := setupHookSvc(t)
	svc.mailFetcher = &fakeMailFetcher{
		messages: []EmailMessage{{UID: 42, MessageID: "message-42@example.com", Subject: "Duplicate", From: "alice@example.com"}},
	}
	source := &hook_entity.HookSource{
		ID:               2,
		Kind:             string(hook_entity.SourceKindEmail),
		Name:             "工作邮箱",
		ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret", LastUID: 41}),
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionPending),
		Status:           consts.ACTIVE,
	}

	sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
	eventRepo.EXPECT().FindBySourceRef(gomock.Any(), int64(2), "message-42@example.com").
		Return(&hook_entity.HookEvent{ID: 99, SourceID: 2, SourceRef: "message-42@example.com", Status: consts.ACTIVE}, nil)
	agentRepo.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{ceoAgent()}, nil)
	ruleRepo.EXPECT().ListBySource(gomock.Any(), int64(2)).Return(nil, nil)
	sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
			assert.Equal(t, int64(0), got.TotalCount)
			assert.Equal(t, uint32(42), parseSourceConfig(got.ConfigJSON).LastUID)
			return nil
		})

	resp, err := svc.SyncEmailSource(ctx, &SyncEmailSourceRequest{ID: 2})

	assert.NoError(t, err)
	assert.Equal(t, 0, resp.Created)
	assert.Equal(t, 1, resp.Skipped)
	assert.Empty(t, resp.Events)
}

func TestSyncEmailSourceMarksSourceErrorWhenFetchFails(t *testing.T) {
	ctx, sourceRepo, _, _, _, svc := setupHookSvc(t)
	svc.mailFetcher = &fakeMailFetcher{err: errors.New("login rejected")}
	source := &hook_entity.HookSource{
		ID:               2,
		Kind:             string(hook_entity.SourceKindEmail),
		Name:             "工作邮箱",
		ConfigJSON:       serializeConfig(SourceConfig{IMAPServer: "imap.example.com", EmailAddress: "ops@example.com", AppPassword: "secret"}),
		Enabled:          1,
		ConnectionStatus: string(hook_entity.ConnectionPending),
		Status:           consts.ACTIVE,
	}

	sourceRepo.EXPECT().Find(gomock.Any(), int64(2)).Return(source, nil)
	sourceRepo.EXPECT().Update(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, got *hook_entity.HookSource) error {
			assert.Equal(t, string(hook_entity.ConnectionError), got.ConnectionStatus)
			return nil
		})

	_, err := svc.SyncEmailSource(ctx, &SyncEmailSourceRequest{ID: 2})

	assert.Error(t, err)
}

func TestSelectEmailUIDBatchKeepsOldestUnread(t *testing.T) {
	uids := []imap.UID{45, 43, 44, 42}

	got := selectEmailUIDBatch(uids, 2)

	assert.Equal(t, []imap.UID{42, 43}, got)
	assert.Equal(t, []imap.UID{45, 43, 44, 42}, uids)
}
