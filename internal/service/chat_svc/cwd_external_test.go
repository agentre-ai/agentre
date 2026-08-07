package chat_svc_test

import (
	"context"
	"testing"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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

// registerWorkspaceRepos 注册 chat_repo.Session 的 mock,配合
// registerCapabilityRepos 的 agent / backend mock 走通
// session → agent → backend → {deviceID, cwd} 这条解析链(不连 DB)。
func registerWorkspaceRepos(t *testing.T, ctrl *gomock.Controller) *mock_chat_repo.MockSessionRepo {
	t.Helper()
	sessionMock := mock_chat_repo.NewMockSessionRepo(ctrl)
	prev := chat_repo.Session()
	chat_repo.RegisterSession(sessionMock)
	t.Cleanup(func() { chat_repo.RegisterSession(prev) })
	return sessionMock
}

// ResolveSessionWorkspace 是 workspace_fs_svc 的 SessionWorkspaceResolver 实现:
// 它必须同时给出 deviceID 与 cwd —— 前者决定 workspace_fs_svc 走本机还是远端。
func TestResolveSessionWorkspace(t *testing.T) {
	t.Run("本地会话 → deviceID 0 + CwdResolver 给出的 cwd", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		ctx := context.Background()
		sessionMock := registerWorkspaceRepos(t, ctrl)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)
		t.Cleanup(func() { chat_svc.RegisterCwdResolver(nil) })
		chat_svc.RegisterCwdResolver(func(_ context.Context, _ *chat_entity.Session) (string, error) {
			return "/local/project", nil
		})

		sessionMock.EXPECT().Find(ctx, int64(5)).Return(&chat_entity.Session{ID: 5, AgentID: 11}, nil)
		agentMock.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{ID: 11, AgentBackendID: 12}, nil)
		backendMock.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "",
		}, nil)

		deviceID, cwd, err := chat_svc.NewChat(chat_svc.NoopEmitter{}).ResolveSessionWorkspace(ctx, 5)
		require.NoError(t, err)
		assert.Equal(t, int64(0), deviceID)
		assert.Equal(t, "/local/project", cwd)
	})

	t.Run("会话不存在 → 报错", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		ctx := context.Background()
		sessionMock := registerWorkspaceRepos(t, ctrl)
		sessionMock.EXPECT().Find(ctx, int64(404)).Return(nil, nil)

		_, _, err := chat_svc.NewChat(chat_svc.NoopEmitter{}).ResolveSessionWorkspace(ctx, 404)
		assert.Error(t, err)
	})

	t.Run("sessionID 非法 → 不查库", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		registerWorkspaceRepos(t, ctrl) // 没有任何 EXPECT:一旦查库 gomock 判错

		_, _, err := chat_svc.NewChat(chat_svc.NoopEmitter{}).ResolveSessionWorkspace(context.Background(), 0)
		assert.Error(t, err)
	})

	// 远端 backend 但 DeviceID 解析不出整数时,绝不能退化成 deviceID=0 —— 那会让
	// workspace_fs_svc 拿着远端机器的路径去列本机文件系统。
	t.Run("远端 backend 的 DeviceID 无法解析 → 报错而不是回落本机", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		ctx := context.Background()
		sessionMock := registerWorkspaceRepos(t, ctrl)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)

		sessionMock.EXPECT().Find(ctx, int64(5)).Return(&chat_entity.Session{ID: 5, AgentID: 11, ProjectID: 3}, nil)
		agentMock.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{ID: 11, AgentBackendID: 12}, nil)
		backendMock.EXPECT().Find(ctx, int64(12)).Return(&agent_backend_entity.AgentBackend{
			ID: 12, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "not-a-number",
		}, nil)

		deviceID, _, err := chat_svc.NewChat(chat_svc.NoopEmitter{}).ResolveSessionWorkspace(ctx, 5)
		require.Error(t, err)
		assert.Equal(t, int64(0), deviceID)
	})
}
