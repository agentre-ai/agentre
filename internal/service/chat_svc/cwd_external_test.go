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

	// R15b / 决策 36：会话钉住某一档之后，一切按会话解析 backend 的路径都必须回到
	// **那一档**，不能回到 Agent 的主档。文件面板的 cwd 走的就是这条链：主档在本机、
	// 钉住的那一档在某台 agentred 时，回到主档会拿本机路径去列远端机器的文件。
	t.Run("会话已钉档 → 用钉住那一档的 backend，不是 Agent 主档", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		t.Cleanup(ctrl.Finish)
		ctx := context.Background()
		sessionMock := registerWorkspaceRepos(t, ctrl)
		agentMock, backendMock := registerCapabilityRepos(t, ctrl)
		sessionMock.EXPECT().Find(ctx, int64(6)).Return(&chat_entity.Session{
			ID: 6, AgentID: 11, ExecAgentBackendID: 13,
		}, nil)
		agentMock.EXPECT().Find(ctx, int64(11)).Return(&agent_entity.Agent{ID: 11, AgentBackendID: 12}, nil)
		// 主档 12(本机)一次都不该被查；钉住的 13 才是这条会话的档。
		backendMock.EXPECT().Find(ctx, int64(13)).Return(&agent_backend_entity.AgentBackend{
			ID: 13, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "4",
		}, nil)

		// deviceID 取自钉住那一档的 backend(device 4)；回到主档 12(本机)会得到 0，
		// 文件面板就会拿本机路径去列远端机器的文件。
		deviceID, _, err := chat_svc.NewChat(chat_svc.NoopEmitter{}).ResolveSessionWorkspace(ctx, 6)
		require.NoError(t, err)
		assert.Equal(t, int64(4), deviceID)
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
