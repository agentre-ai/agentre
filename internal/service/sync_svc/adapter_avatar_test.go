package sync_svc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo/mock_syncstate_repo"
)

// stubAvatarTransport 是 avatarTransport 的替身：记下每一次 Put / Get，按脚本回应。
type stubAvatarTransport struct {
	putCalls []avatarPutCall

	getCalled      int
	getLastHash    string
	getContent     string
	getContentType string
	getErr         error
}

func (s *stubAvatarTransport) PutAvatar(_ context.Context, contentHash, contentType, content string) error {
	s.putCalls = append(s.putCalls, avatarPutCall{Hash: contentHash, ContentType: contentType, Content: content})
	return nil
}

func (s *stubAvatarTransport) GetAvatar(_ context.Context, contentHash string) (string, string, error) {
	s.getCalled++
	s.getLastHash = contentHash
	if s.getErr != nil {
		return "", "", s.getErr
	}
	return s.getContent, s.getContentType, nil
}

const testAvatarDataURL = "data:image/png;base64,QUFBQQ=="

// TestAgentAdapter_LoadEmitsAvatarHashNotContent R16a 的守卫断言：同步载荷里的
// Agent 只带 AvatarDataURL 的内容哈希，不带正文；哈希参与内容比较所以换头像
// 照常触发同步（这里先验证 load 侧只上行哈希）。
func TestAgentAdapter_LoadEmitsAvatarHashNotContent(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.Agent)
			*row = agent_entity.Agent{
				ID: 5, Name: "Ava", AvatarDataURL: testAvatarDataURL,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1", SyncVersion: 3},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	avatar := &stubAvatarTransport{}
	out, err := agentAdapter{avatar: avatar}.load(context.Background(), "agent-1")
	require.NoError(t, err)
	require.NotNil(t, out)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, avatarHash(testAvatarDataURL), payload["avatar_hash"])
	assert.NotContains(t, payload, "avatar_data_url", "正文一律不进同步载荷")
	assert.NoError(t, syncwire.GuardPayload(out.Payload), "守卫也认可这份载荷不带头像正文")

	require.Len(t, avatar.putCalls, 1, "本机持有正文时应该单独推一次给对端")
	assert.Equal(t, avatarHash(testAvatarDataURL), avatar.putCalls[0].Hash)
	assert.Equal(t, testAvatarDataURL, avatar.putCalls[0].Content)
	assert.Equal(t, "image/png", avatar.putCalls[0].ContentType)
}

// TestAgentAdapter_LoadWithoutAvatar_SkipsUpload 没有自定义头像时载荷里的
// avatar_hash 为空，也不触发任何一次头像上传。
func TestAgentAdapter_LoadWithoutAvatar_SkipsUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.Agent)
			*row = agent_entity.Agent{ID: 5, Name: "Ava", SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"}}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	avatar := &stubAvatarTransport{}
	out, err := agentAdapter{avatar: avatar}.load(context.Background(), "agent-1")
	require.NoError(t, err)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.NotContains(t, payload, "avatar_hash")
	assert.Empty(t, avatar.putCalls)
}

// TestAgentAdapter_LoadAvatarUploadFailure_StillReturnsPayload 上传失败不阻塞
// Agent 本身照常同步（R16a）：load 仍然返回一份合法载荷。
func TestAgentAdapter_LoadAvatarUploadFailure_StillReturnsPayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.Agent)
			*row = agent_entity.Agent{
				ID: 5, Name: "Ava", AvatarDataURL: testAvatarDataURL,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	avatar := &failingPutAvatarTransport{err: errors.New("upload failed")}
	out, err := agentAdapter{avatar: avatar}.load(context.Background(), "agent-1")
	require.NoError(t, err, "头像上传失败不该让整个 Agent 上行失败")
	require.NotNil(t, out)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, avatarHash(testAvatarDataURL), payload["avatar_hash"])
}

type failingPutAvatarTransport struct{ err error }

func (f *failingPutAvatarTransport) PutAvatar(context.Context, string, string, string) error {
	return f.err
}
func (f *failingPutAvatarTransport) GetAvatar(context.Context, string) (string, string, error) {
	return "", "", f.err
}

// TestAgentAdapter_ApplyFetchesAvatarWhenNotHeld 接收方尚未持有该哈希时才单独
// 取一次正文：新建的 Agent 没有任何既有内容，遇到非空 avatar_hash 必须去取。
func TestAgentAdapter_ApplyFetchesAvatarWhenNotHeld(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	var created *agent_entity.Agent
	agents.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, a *agent_entity.Agent) error { created = a; return nil })
	agent_repo.RegisterAgent(agents)

	hash := avatarHash(testAvatarDataURL)
	avatar := &stubAvatarTransport{getContent: testAvatarDataURL, getContentType: "image/png"}

	err := agentAdapter{avatar: avatar}.apply(context.Background(), &inbound{
		Kind: syncwire.KindAgent, SyncID: "agent-1", Version: 1,
		Payload: []byte(`{"name":"Landed","avatar_hash":"` + hash + `"}`),
	}, map[string]int64{})

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, testAvatarDataURL, created.AvatarDataURL)
	assert.Equal(t, 1, avatar.getCalled, "尚未持有这份哈希，必须取一次")
	assert.Equal(t, hash, avatar.getLastHash)
}

// TestAgentAdapter_ApplySkipsFetchWhenAlreadyHoldingSameHash 已经持有同一份哈希
// 内容时不重新取（R16a）。
func TestAgentAdapter_ApplySkipsFetchWhenAlreadyHoldingSameHash(t *testing.T) {
	ctrl := gomock.NewController(t)
	hash := avatarHash(testAvatarDataURL)

	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.Agent)
			*row = agent_entity.Agent{
				ID: 5, AvatarDataURL: testAvatarDataURL,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	var updated *agent_entity.Agent
	agents.EXPECT().UpdateRow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, a *agent_entity.Agent) error { updated = a; return nil })
	agent_repo.RegisterAgent(agents)

	avatar := &stubAvatarTransport{}
	err := agentAdapter{avatar: avatar}.apply(context.Background(), &inbound{
		Kind: syncwire.KindAgent, SyncID: "agent-1", Version: 2,
		Payload: []byte(`{"name":"Renamed","avatar_hash":"` + hash + `"}`),
	}, map[string]int64{})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, testAvatarDataURL, updated.AvatarDataURL)
	assert.Zero(t, avatar.getCalled, "已经持有同一份哈希，不该再取一次")
}

// TestAgentAdapter_ApplyFallsBackToInitialsWhenFetchFails 取正文失败或超时时
// 降级为占位字母头像（既有 AgentAvatar 的 initials 分支），不阻塞这一行落地。
func TestAgentAdapter_ApplyFallsBackToInitialsWhenFetchFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	var created *agent_entity.Agent
	agents.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, a *agent_entity.Agent) error { created = a; return nil })
	agent_repo.RegisterAgent(agents)

	hash := avatarHash(testAvatarDataURL)
	avatar := &stubAvatarTransport{getErr: errors.New("timeout")}

	err := agentAdapter{avatar: avatar}.apply(context.Background(), &inbound{
		Kind: syncwire.KindAgent, SyncID: "agent-1", Version: 1,
		Payload: []byte(`{"name":"Landed","avatar_hash":"` + hash + `"}`),
	}, map[string]int64{})

	require.NoError(t, err, "取头像失败不该阻塞这个 Agent 落地")
	require.NotNil(t, created)
	assert.Empty(t, created.AvatarDataURL, "降级为占位字母头像：AvatarDataURL 留空")
	assert.Equal(t, consts.ACTIVE, created.Status)
}

// TestAgentAdapter_ApplyClearsAvatarWhenHashEmpty 对端删掉了自定义头像（载荷里
// avatar_hash 为空）时，本机也跟着清空，而不是继续留着旧内容。
func TestAgentAdapter_ApplyClearsAvatarWhenHashEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).DoAndReturn(
		func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.Agent)
			*row = agent_entity.Agent{
				ID: 5, AvatarDataURL: testAvatarDataURL,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	var updated *agent_entity.Agent
	agents.EXPECT().UpdateRow(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, a *agent_entity.Agent) error { updated = a; return nil })
	agent_repo.RegisterAgent(agents)

	avatar := &stubAvatarTransport{}
	err := agentAdapter{avatar: avatar}.apply(context.Background(), &inbound{
		Kind: syncwire.KindAgent, SyncID: "agent-1", Version: 2,
		Payload: []byte(`{"name":"Renamed"}`),
	}, map[string]int64{})

	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Empty(t, updated.AvatarDataURL)
	assert.Zero(t, avatar.getCalled)
}

// TestAgentAdapter_ApplyGivenNilAvatarTransport_FallsBackGracefully 单机模式
// （New(nil)）或直接构造 agentAdapter{} 的场景：没有网络出入口时不 panic，直接
// 降级为占位字母头像。
func TestAgentAdapter_ApplyGivenNilAvatarTransport_FallsBackGracefully(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgent, "agent-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	var created *agent_entity.Agent
	agents.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, a *agent_entity.Agent) error { created = a; return nil })
	agent_repo.RegisterAgent(agents)

	hash := avatarHash(testAvatarDataURL)
	err := agentAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindAgent, SyncID: "agent-1", Version: 1,
		Payload: []byte(`{"name":"Landed","avatar_hash":"` + hash + `"}`),
	}, map[string]int64{})

	require.NoError(t, err)
	assert.Empty(t, created.AvatarDataURL)
}
