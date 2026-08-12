package sync_svc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/project_location_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo/mock_project_location_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo/mock_project_repo"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo/mock_remote_device_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo/mock_syncstate_repo"
)

// TestProjectAdapter_LoadTranslatesParentToSyncIDAndOmitsLocalPath R2：项目的父项目
// 在载荷里是父项目的**同步标识**，不是本地自增 ID；本机路径（决策 6）与状态位一律
// 不进载荷。
func TestProjectAdapter_LoadTranslatesParentToSyncIDAndOmitsLocalPath(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProject, "proj-child", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*project_entity.Project)
			*row = project_entity.Project{
				ID: 5, ParentID: 4, Name: "Child", Path: "/Users/me/secret-repo",
				SortOrder: 2, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "proj-child", SyncVersion: 7},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	projects := mock_project_repo.NewMockProjectRepo(ctrl)
	projects.EXPECT().Find(gomock.Any(), int64(4)).Return(&project_entity.Project{
		ID: 4, Name: "Parent", SyncMeta: syncmeta_entity.SyncMeta{SyncID: "proj-parent"},
	}, nil)
	project_repo.RegisterProject(projects)

	out, err := projectAdapter{}.load(context.Background(), "proj-child")
	require.NoError(t, err)
	require.NotNil(t, out)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, "proj-parent", payload["parent_sync_id"])
	assert.NotContains(t, payload, "path", "本机路径只上报、不同步（决策 6）")
	assert.NotContains(t, payload, "parent_id", "载荷里不出现任何本地自增 ID")
	assert.NoError(t, syncwire.GuardPayload(out.Payload))
}

// TestProjectAdapter_ApplyLandsAsLocalPathMissing R10：同步进来的项目不带源端本机
// 路径，落成「本机未配置路径」状态。
func TestProjectAdapter_ApplyLandsAsLocalPathMissing(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProject, "proj-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	projects := mock_project_repo.NewMockProjectRepo(ctrl)
	var created *project_entity.Project
	projects.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, p *project_entity.Project) error { created = p; return nil })
	project_repo.RegisterProject(projects)

	err := projectAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindProject, SyncID: "proj-1", Version: 3,
		Payload: []byte(`{"name":"Landed","sort_order":1}`),
	}, map[string]int64{})

	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "proj-1", created.SyncID, "沿用源端的同步标识（R1）")
	assert.Empty(t, created.Path)
	assert.True(t, created.LocalPathMissing)
	assert.Equal(t, consts.ACTIVE, created.Status)
}

// TestAgentBackendAdapter_LoadUsesFingerprintAndProviderKeyOnly R2：backend 指向的
// agentred 在载荷外层用**指纹**表达，provider 只有 provider_key 这个字符串键，
// llm_providers 的任何正文（含 APIKey）都不出本机。
func TestAgentBackendAdapter_LoadUsesFingerprintAndProviderKeyOnly(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentBackend, "be-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_backend_entity.AgentBackend)
			*row = agent_backend_entity.AgentBackend{
				ID: 9, Type: "claudecode", Name: "构建机 Claude", LLMProviderKey: "anthropic-main",
				DeviceID: "fp-builder", CLIPath: "/opt/claude", EnvJSON: `{"K":"V"}`,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "be-1", SyncVersion: 2},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	out, err := agentBackendAdapter{}.load(context.Background(), "be-1")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "fp-builder", out.AgentredFingerprint, "指向哪台机器用指纹表达（决策 20）")

	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, "anthropic-main", payload["provider_key"])
	assert.NotContains(t, payload, "api_key")
	assert.NotContains(t, payload, "device_id")
	assert.NoError(t, syncwire.GuardPayload(out.Payload))
}

// TestAgentBackendAdapter_GivenLegacyNumericDeviceID_UploadsCanonicalFingerprint
// 前端创建 agentred backend 时仍会提交本机 paired_agentreds 的数值行 ID（历史路径），
// 但 wire 层的 AgentredFingerprint 字段语义是「全局指纹」。数值行 ID 跨机毫无意义——
// 对端按同号解析会落到它自己那张表里另一台机器上（错机派发）。load 必须把它翻译成
// 指向那台 daemon 的指纹再上行。
func TestAgentBackendAdapter_GivenLegacyNumericDeviceID_UploadsCanonicalFingerprint(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentBackend, "be-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_backend_entity.AgentBackend)
			*row = agent_backend_entity.AgentBackend{
				ID: 9, Type: "claudecode", Name: "构建机 Claude",
				DeviceID: "3", SyncMeta: syncmeta_entity.SyncMeta{SyncID: "be-1", SyncVersion: 2},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	paired := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	paired.EXPECT().Get(gomock.Any(), int64(3)).Return(&paired_agentred_entity.PairedAgentred{
		ID: 3, DaemonFingerprint: "fp-builder",
	}, nil)
	remote_device_repo.RegisterPairedAgentred(paired)

	out, err := agentBackendAdapter{}.load(context.Background(), "be-1")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "fp-builder", out.AgentredFingerprint,
		"a numeric paired-row ID must never cross the wire as a fingerprint")
}

// TestAgentBackendAdapter_GivenLegacyNumericDeviceIDOfUnpairedDaemon_SkipsUpload
// 数值行 ID 指向的 daemon 已解除配对时翻不出指纹。宁可这一条不上行（旧行为），
// 也不能让数值行 ID 带着错机语义过机。
func TestAgentBackendAdapter_GivenLegacyNumericDeviceIDOfUnpairedDaemon_SkipsUpload(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentBackend, "be-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_backend_entity.AgentBackend)
			*row = agent_backend_entity.AgentBackend{
				ID: 9, Type: "claudecode", Name: "构建机 Claude",
				DeviceID: "3", SyncMeta: syncmeta_entity.SyncMeta{SyncID: "be-1", SyncVersion: 2},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	paired := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	paired.EXPECT().Get(gomock.Any(), int64(3)).Return(nil, nil)
	remote_device_repo.RegisterPairedAgentred(paired)

	out, err := agentBackendAdapter{}.load(context.Background(), "be-1")
	require.NoError(t, err)
	require.Nil(t, out, "an unexpressable numeric target must not be uploaded")
}

func TestAgentBackendAdapter_GivenCanonicalFingerprint_PreservesItWithoutPairLookup(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentBackend, "be-desktop", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_backend_entity.AgentBackend)
			*row = agent_backend_entity.AgentBackend{
				ID: 8, Type: "claudecode", Name: "Desktop Claude", DeviceID: "sha256:desktop-a",
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "be-desktop"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	// A backend's persisted device ID is already the canonical fingerprint. The
	// sync adapter must not treat it as a local paired-row ID.
	remote_device_repo.RegisterPairedAgentred(
		mock_remote_device_repo.NewMockPairedAgentredRepo(gomock.NewController(t)))

	out, err := agentBackendAdapter{}.load(context.Background(), "be-desktop")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "sha256:desktop-a", out.AgentredFingerprint)
}

// TestAgentBackendAdapter_GivenUnpairedFingerprint_PersistsNamedTargetDirectly
// R12: pairing affects dispatch availability, never the synced target identity.
func TestAgentBackendAdapter_GivenUnpairedFingerprint_PersistsNamedTargetDirectly(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentBackend, "be-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)
	backends := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	var created *agent_backend_entity.AgentBackend
	backends.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, row *agent_backend_entity.AgentBackend) error { created = row; return nil })
	agent_backend_repo.RegisterAgentBackend(backends)

	in := &inbound{Kind: syncwire.KindAgentBackend, SyncID: "be-1", AgentredFingerprint: "fp-unknown",
		Payload: []byte(`{"type":"claudecode","name":"构建机"}`)}
	assert.Empty(t, agentBackendAdapter{}.refs(in))
	require.NoError(t, agentBackendAdapter{}.apply(context.Background(), in, nil))
	require.NotNil(t, created)
	assert.Equal(t, "fp-unknown", created.DeviceID)
}

// TestAgentExecTargetAdapter_TranslatesBothEndsToSyncIDs R2：执行目标的两端（它所属
// 的 Agent 与它指向的 backend）都以同步标识过机，落地时翻回接收端的两个本地 ID。
func TestAgentExecTargetAdapter_TranslatesBothEndsToSyncIDs(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindAgentExecTarget, "tgt-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*agent_entity.AgentExecTarget)
			*row = agent_entity.AgentExecTarget{
				ID: 11, AgentID: 2, AgentBackendID: 9, SortOrder: 1, SkillsJSON: `[{"id":"a@b","enabled":true}]`,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "tgt-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)

	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{
		ID: 2, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"},
	}, nil)
	agent_repo.RegisterAgent(agents)
	backends := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	backends.EXPECT().Find(gomock.Any(), int64(9)).Return(&agent_backend_entity.AgentBackend{
		ID: 9, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "be-1"},
	}, nil)
	agent_backend_repo.RegisterAgentBackend(backends)

	out, err := agentExecTargetAdapter{}.load(context.Background(), "tgt-1")
	require.NoError(t, err)
	require.NotNil(t, out)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, "agent-1", payload["agent_sync_id"])
	assert.Equal(t, "be-1", payload["backend_sync_id"])
	assert.NoError(t, syncwire.GuardPayload(out.Payload))

	// 落地：两个引用都翻回接收端的本地 ID。
	targets := mock_agent_repo.NewMockAgentExecTargetRepo(ctrl)
	var upserted *agent_entity.AgentExecTarget
	targets.EXPECT().UpsertFromSync(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, row *agent_entity.AgentExecTarget) error { upserted = row; return nil })
	agent_repo.RegisterAgentExecTarget(targets)

	in := &inbound{Kind: syncwire.KindAgentExecTarget, SyncID: "tgt-1", Payload: out.Payload}
	require.NoError(t, agentExecTargetAdapter{}.apply(context.Background(), in, map[string]int64{
		"agent:agent-1": 77, "agent_backend:be-1": 88,
	}))
	require.NotNil(t, upserted)
	assert.Equal(t, int64(77), upserted.AgentID)
	assert.Equal(t, int64(88), upserted.AgentBackendID)
	assert.Equal(t, "tgt-1", upserted.SyncID, "跨机身份终身不变（R1）")
}

// TestAgentExecTargetAdapter_GivenMissingAgent_Defers R2a：引用目标没到就暂缓落地，
// 绝不写悬空引用。
func TestAgentExecTargetAdapter_GivenMissingAgent_Defers(t *testing.T) {
	in := &inbound{
		Kind:    syncwire.KindAgentExecTarget,
		SyncID:  "tgt-1",
		Payload: []byte(`{"agent_sync_id":"agent-1","backend_sync_id":"be-1"}`),
	}
	assert.Equal(t, []ref{
		{Kind: syncwire.KindAgent, SyncID: "agent-1"},
		{Kind: syncwire.KindAgentBackend, SyncID: "be-1"},
	}, agentExecTargetAdapter{}.refs(in))

	err := agentExecTargetAdapter{}.apply(context.Background(), in, map[string]int64{"agent:agent-1": 77})
	assert.ErrorIs(t, err, errRefMissing, "backend 还没到，这一行暂缓")
}

// TestProjectAgentAdapter_TranslatesBothEnds R2：成员关系的项目与 Agent 双方都用
// 同步标识表达。
func TestProjectAgentAdapter_TranslatesBothEnds(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectAgent, "pa-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*project_entity.ProjectAgent)
			*row = project_entity.ProjectAgent{
				ProjectID: 4, AgentID: 2, JoinedAt: 999,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "pa-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)
	projects := mock_project_repo.NewMockProjectRepo(ctrl)
	projects.EXPECT().Find(gomock.Any(), int64(4)).Return(&project_entity.Project{
		ID: 4, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "proj-1"},
	}, nil)
	project_repo.RegisterProject(projects)
	agents := mock_agent_repo.NewMockAgentRepo(ctrl)
	agents.EXPECT().Find(gomock.Any(), int64(2)).Return(&agent_entity.Agent{
		ID: 2, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "agent-1"},
	}, nil)
	agent_repo.RegisterAgent(agents)

	out, err := projectAgentAdapter{}.load(context.Background(), "pa-1")
	require.NoError(t, err)
	require.NotNil(t, out)
	var payload map[string]any
	require.NoError(t, json.Unmarshal(out.Payload, &payload))
	assert.Equal(t, "proj-1", payload["project_sync_id"])
	assert.Equal(t, "agent-1", payload["agent_sync_id"])
	assert.NoError(t, syncwire.GuardPayload(out.Payload))
}

// TestProjectLocationAdapter_CarriesNaturalKeyOutsidePayload R4b/决策 26：路径记录的
// 账号内自然键（项目同步标识, agentred 指纹）走上行项的顶层字段，server 据此合并。
func TestProjectLocationAdapter_CarriesNaturalKeyOutsidePayload(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectLocation, "loc-1", gomock.Any()).
		DoAndReturn(func(_ context.Context, _, _ string, dest any) (bool, error) {
			row := dest.(*project_location_entity.ProjectLocation)
			*row = project_location_entity.ProjectLocation{
				ID: 6, ProjectID: 4, DaemonFingerprint: "fp-builder", Path: "/srv/repo",
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: "loc-1"},
			}
			return true, nil
		})
	syncstate_repo.RegisterSyncState(state)
	projects := mock_project_repo.NewMockProjectRepo(ctrl)
	projects.EXPECT().Find(gomock.Any(), int64(4)).Return(&project_entity.Project{
		ID: 4, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "proj-1"},
	}, nil)
	project_repo.RegisterProject(projects)

	out, err := projectLocationAdapter{}.load(context.Background(), "loc-1")
	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "proj-1", out.ProjectSyncID)
	assert.Equal(t, "fp-builder", out.AgentredFingerprint)
	assert.JSONEq(t, `{"path":"/srv/repo"}`, string(out.Payload))
	assert.NoError(t, syncwire.GuardPayload(out.Payload))
}

// TestProjectLocationAdapter_ApplyResolvesDeviceIDWhenPaired 决策 26/34：device_id 是
// 「由指纹解析出的本地缓存」，本机已配对那台 agentred 时**落地当场就该解析出来**，
// 而不是等用户点开项目设置的「位置」页签才回填——决策 34 的「本机没路径就落到配了
// 路径的那一档」与两个 cwd 解析点都按 device_id 查行（FindByProjectAndDevice），
// 缓存空着它们就一律判成「这台机器上没配这个项目的路径」。
func TestProjectLocationAdapter_ApplyResolvesDeviceIDWhenPaired(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectLocation, "loc-1", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	paired := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	paired.EXPECT().List(gomock.Any()).Return([]*paired_agentred_entity.PairedAgentred{
		{ID: 9, DaemonFingerprint: "fp-builder"},
	}, nil).AnyTimes()
	remote_device_repo.RegisterPairedAgentred(paired)

	locations := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	locations.EXPECT().FindByProjectAndFingerprint(gomock.Any(), int64(4), "fp-builder").Return(nil, nil)
	var created *project_location_entity.ProjectLocation
	locations.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, row *project_location_entity.ProjectLocation) error {
			created = row
			return nil
		})
	project_location_repo.RegisterProjectLocation(locations)

	err := projectLocationAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindProjectLocation, SyncID: "loc-1",
		ProjectSyncID: "proj-1", AgentredFingerprint: "fp-builder",
		Payload: []byte(`{"path":"/srv/repo"}`),
	}, map[string]int64{ref{Kind: syncwire.KindProject, SyncID: "proj-1"}.key(): 4})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Equal(t, "9", created.DeviceID, "已配对：落地当场解析出本地缓存")
	assert.Equal(t, "fp-builder", created.DaemonFingerprint, "身份仍然是指纹")
}

// TestProjectLocationAdapter_ApplyLeavesDeviceIDEmptyWhenUnpaired R2b：本机没配对
// 那台 agentred 时缓存留空——行本身照常留着，只是不参与解析、不呈现。
func TestProjectLocationAdapter_ApplyLeavesDeviceIDEmptyWhenUnpaired(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectLocation, "loc-2", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	paired := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	paired.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()
	remote_device_repo.RegisterPairedAgentred(paired)

	locations := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	locations.EXPECT().FindByProjectAndFingerprint(gomock.Any(), int64(4), "fp-unknown").Return(nil, nil)
	var created *project_location_entity.ProjectLocation
	locations.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, row *project_location_entity.ProjectLocation) error {
			created = row
			return nil
		})
	project_location_repo.RegisterProjectLocation(locations)

	err := projectLocationAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindProjectLocation, SyncID: "loc-2",
		ProjectSyncID: "proj-1", AgentredFingerprint: "fp-unknown",
		Payload: []byte(`{"path":"/srv/repo"}`),
	}, map[string]int64{ref{Kind: syncwire.KindProject, SyncID: "proj-1"}.key(): 4})
	require.NoError(t, err)
	require.NotNil(t, created)
	assert.Empty(t, created.DeviceID)
}

// TestProjectLocationAdapter_GivenNaturalKeyHeldLocally_TakesOverThatRow 两端各自为
// 同一个（项目, agentred 指纹）建了一行，带着不同的同步标识落在同一个自然键上
// （R4b）。本机那一行还占着 uniq_project_locations_proj_fingerprint，硬插会撞唯一
// 索引，抛出一个原始的 SQLite 错误——而这正好发生在「合并落败方」这一类行上。
//
// 自然键就是身份：接管本机那一行（改写正文并让它跟随账号里胜出的那个同步标识），
// 不为同一件事再插一行。兄弟适配器 projectAgentAdapter 早就这么处理同类冲突了。
func TestProjectLocationAdapter_GivenNaturalKeyHeldLocally_TakesOverThatRow(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectLocation, "loc-remote", gomock.Any()).
		Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	paired := mock_remote_device_repo.NewMockPairedAgentredRepo(ctrl)
	paired.EXPECT().List(gomock.Any()).Return(nil, nil).AnyTimes()
	remote_device_repo.RegisterPairedAgentred(paired)

	locations := mock_project_location_repo.NewMockProjectLocationRepo(ctrl)
	locations.EXPECT().FindByProjectAndFingerprint(gomock.Any(), int64(4), "fp-builder").Return(
		&project_location_entity.ProjectLocation{
			ID: 11, ProjectID: 4, Path: "/old", DaemonFingerprint: "fp-builder",
			SyncMeta: syncmeta_entity.SyncMeta{SyncID: "loc-local"},
		}, nil)
	var saved *project_location_entity.ProjectLocation
	locations.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, row *project_location_entity.ProjectLocation) error {
			saved = row
			return nil
		})
	// Create 没有 EXPECT：再插一行就会撞唯一索引。
	project_location_repo.RegisterProjectLocation(locations)

	err := projectLocationAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindProjectLocation, SyncID: "loc-remote",
		ProjectSyncID: "proj-1", AgentredFingerprint: "fp-builder",
		Payload: []byte(`{"path":"/srv/repo"}`),
	}, map[string]int64{ref{Kind: syncwire.KindProject, SyncID: "proj-1"}.key(): 4})

	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, int64(11), saved.ID, "接管本机那一行，不新插一行")
	assert.Equal(t, "/srv/repo", saved.Path)
	assert.Equal(t, "loc-remote", saved.SyncID, "自然键上的行跟随账号里胜出的同步标识")
}

// TestProjectAgentAdapter_GivenSamePairAlreadyLocal_KeepsLocalRow 两端各自把同一个
// Agent 加进同一个项目时，两行带着不同的同步标识落在同一个联合主键上：保留本机
// 那一行，不硬插一条撞主键（撞了会把整轮下行带崩）。
func TestProjectAgentAdapter_GivenSamePairAlreadyLocal_KeepsLocalRow(t *testing.T) {
	ctrl := gomock.NewController(t)
	state := mock_syncstate_repo.NewMockSyncStateRepo(ctrl)
	state.EXPECT().FindRow(gomock.Any(), syncwire.KindProjectAgent, "pa-remote", gomock.Any()).Return(false, nil)
	syncstate_repo.RegisterSyncState(state)

	members := mock_project_repo.NewMockProjectAgentRepo(ctrl)
	members.EXPECT().ListByProject(gomock.Any(), int64(4)).Return([]*project_entity.ProjectAgent{
		{ProjectID: 4, AgentID: 2, SyncMeta: syncmeta_entity.SyncMeta{SyncID: "pa-local"}},
	}, nil)
	// CreateFromSync 一次都不该被调用：没有设期望，调到就直接失败。
	project_repo.RegisterProjectAgent(members)

	err := projectAgentAdapter{}.apply(context.Background(), &inbound{
		Kind: syncwire.KindProjectAgent, SyncID: "pa-remote",
		Payload: []byte(`{"project_sync_id":"proj-1","agent_sync_id":"agent-1","joined_at":9}`),
	}, map[string]int64{"project:proj-1": 4, "agent:agent-1": 2})

	require.NoError(t, err)
}
