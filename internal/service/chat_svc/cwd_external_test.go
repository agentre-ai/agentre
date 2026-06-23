package chat_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSessionCwd_Exported(t *testing.T) {
	t.Cleanup(func() { chat_svc.RegisterCwdResolver(nil) })
	chat_svc.RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
		return "/from/resolver", nil
	})
	cwd, err := chat_svc.ResolveSessionCwd(context.Background(),
		&chat_entity.Session{ID: 1, AgentID: 7},
		&agent_backend_entity.AgentBackend{DeviceID: ""},
	)
	require.NoError(t, err)
	assert.Equal(t, "/from/resolver", cwd)
}
