package chat_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
)

// TestExecDeviceID — pure function unit test (white-box).
func TestExecDeviceID(t *testing.T) {
	assert.Equal(t, "", execDeviceID(nil))

	local := &agent_backend_entity.AgentBackend{} // DeviceID == "" → local
	assert.Equal(t, "", execDeviceID(local))

	remote := &agent_backend_entity.AgentBackend{DeviceID: "dev-9"} // DeviceID != "" → remote
	assert.Equal(t, "dev-9", execDeviceID(remote))
}

// TestResolveSessionExecTarget_LocalUsesResolverCwd — repo-mock integration test.
func TestResolveSessionExecTarget_LocalUsesResolverCwd(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	// Session repo mock + restore
	sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
	prevSess := chat_repo.Session()
	chat_repo.RegisterSession(sessRepo)
	t.Cleanup(func() { chat_repo.RegisterSession(prevSess) })

	// Agent repo mock + restore
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	prevAgent := agent_repo.Agent()
	agent_repo.RegisterAgent(agentRepo)
	t.Cleanup(func() { agent_repo.RegisterAgent(prevAgent) })

	// Agent has no backend (AgentBackendID: 0) → backend Find NOT called, be == nil → local
	sessRepo.EXPECT().Find(gomock.Any(), int64(7)).
		Return(&chat_entity.Session{ID: 7, AgentID: 3}, nil)
	agentRepo.EXPECT().Find(gomock.Any(), int64(3)).
		Return(&agent_entity.Agent{ID: 3, AgentBackendID: 0}, nil)

	// CwdResolver: restore previous impl after test
	prevCwd := resolveCwdFn
	RegisterCwdResolver(func(_ context.Context, sess *chat_entity.Session) (string, error) {
		return "/proj/x", nil
	})
	t.Cleanup(func() { resolveCwdFn = prevCwd })

	svc := NewChat(NoopEmitter{})
	cwd, dev, err := svc.ResolveSessionExecTarget(context.Background(), 7)
	require.NoError(t, err)
	assert.Equal(t, "/proj/x", cwd)
	assert.Equal(t, "", dev)
}

// TestResolveSessionExecTarget_RemoteUsesDeviceID — repo-mock test for remote backend.
func TestResolveSessionExecTarget_RemoteUsesDeviceID(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	// Session repo mock + restore
	sessRepo := mock_chat_repo.NewMockSessionRepo(ctrl)
	prevSess := chat_repo.Session()
	chat_repo.RegisterSession(sessRepo)
	t.Cleanup(func() { chat_repo.RegisterSession(prevSess) })

	// Agent repo mock + restore
	agentRepo := mock_agent_repo.NewMockAgentRepo(ctrl)
	prevAgent := agent_repo.Agent()
	agent_repo.RegisterAgent(agentRepo)
	t.Cleanup(func() { agent_repo.RegisterAgent(prevAgent) })

	// Backend repo mock + restore
	beRepo := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	prevBe := agent_backend_repo.AgentBackend()
	agent_backend_repo.RegisterAgentBackend(beRepo)
	t.Cleanup(func() { agent_backend_repo.RegisterAgentBackend(prevBe) })

	// Session has ProjectID=0 (free session) and agent has backend 42 (remote)
	sessRepo.EXPECT().Find(gomock.Any(), int64(8)).
		Return(&chat_entity.Session{ID: 8, AgentID: 5, ProjectID: 0}, nil)
	agentRepo.EXPECT().Find(gomock.Any(), int64(5)).
		Return(&agent_entity.Agent{ID: 5, AgentBackendID: 42}, nil)
	beRepo.EXPECT().Find(gomock.Any(), int64(42)).
		Return(&agent_backend_entity.AgentBackend{
			ID:       42,
			DeviceID: "dev-9",
			Type:     string(agent_backend_entity.TypeClaudeCode),
		}, nil)

	// CwdResolver: restore previous impl after test
	prevCwd := resolveCwdFn
	RegisterCwdResolver(func(_ context.Context, sess *chat_entity.Session) (string, error) {
		return "/should-not-be-used", nil
	})
	t.Cleanup(func() { resolveCwdFn = prevCwd })

	svc := NewChat(NoopEmitter{})
	// Remote free session → cwd="" (daemon handles it), deviceID="dev-9"
	cwd, dev, err := svc.ResolveSessionExecTarget(context.Background(), 8)
	require.NoError(t, err)
	assert.Equal(t, "", cwd)      // ProjectID=0 remote → empty cwd per resolveSessionCwd
	assert.Equal(t, "dev-9", dev) // remote backend DeviceID propagated
}
