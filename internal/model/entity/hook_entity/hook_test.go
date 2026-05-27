package hook_entity

import (
	"context"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
)

func TestHookSourceCheck(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		source  *HookSource
		wantErr bool
	}{
		{
			name: "valid github source",
			source: &HookSource{
				Kind:             string(SourceKindGitHub),
				Name:             "agentre-bot",
				ConfigJSON:       `{"webhookUrl":"https://agentre.local/hooks/abc","verifySignature":true}`,
				Enabled:          1,
				ConnectionStatus: string(ConnectionConnected),
				Status:           consts.ACTIVE,
			},
		},
		{
			name: "blank name rejected",
			source: &HookSource{
				Kind:             string(SourceKindGitHub),
				Name:             " ",
				ConfigJSON:       `{}`,
				ConnectionStatus: string(ConnectionPending),
				Status:           consts.ACTIVE,
			},
			wantErr: true,
		},
		{
			name: "unknown kind rejected",
			source: &HookSource{
				Kind:             "rss",
				Name:             "feed",
				ConfigJSON:       `{}`,
				ConnectionStatus: string(ConnectionPending),
				Status:           consts.ACTIVE,
			},
			wantErr: true,
		},
		{
			name: "malformed config json rejected",
			source: &HookSource{
				Kind:             string(SourceKindWebhook),
				Name:             "n8n",
				ConfigJSON:       `{`,
				ConnectionStatus: string(ConnectionPending),
				Status:           consts.ACTIVE,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Check(ctx)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestHookRuleCheck(t *testing.T) {
	ctx := context.Background()

	assert.NoError(t, (&HookRule{
		SourceID:      2,
		Name:          "PR opened",
		ConditionExpr: `event_type contains "pr"`,
		TargetAgentID: 1,
		Enabled:       1,
		Status:        consts.ACTIVE,
	}).Check(ctx))

	assert.Error(t, (&HookRule{
		SourceID: 0,
		Name:     "missing source",
		Status:   consts.ACTIVE,
	}).Check(ctx))

	assert.Error(t, (&HookRule{
		SourceID: 1,
		Name:     " ",
		Status:   consts.ACTIVE,
	}).Check(ctx))
}

func TestHookEventCheck(t *testing.T) {
	ctx := context.Background()

	assert.NoError(t, (&HookEvent{
		SourceID:         2,
		Title:            "PR #142",
		EventStatus:      string(EventDispatched),
		PayloadJSON:      `{"action":"opened"}`,
		MatchedRulesJSON: `[{"ruleId":1,"ruleName":"PR opened","matched":true}]`,
		DispatchesJSON:   `[{"agentId":1,"agentName":"CEO 助手","status":"queued"}]`,
		Status:           consts.ACTIVE,
	}).Check(ctx))

	assert.Error(t, (&HookEvent{
		SourceID:    2,
		Title:       "bad status",
		EventStatus: "ignored",
		PayloadJSON: `{}`,
		Status:      consts.ACTIVE,
	}).Check(ctx))

	assert.Error(t, (&HookEvent{
		SourceID:    2,
		Title:       "bad payload",
		EventStatus: string(EventFailed),
		PayloadJSON: `{`,
		Status:      consts.ACTIVE,
	}).Check(ctx))
}
