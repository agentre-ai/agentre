package chat_svc

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc/mock_remote_device_svc"
)

type peerSessionTestDeps struct {
	agent   *mock_agent_repo.MockAgentRepo
	backend *mock_agent_backend_repo.MockAgentBackendRepo
	session *mock_chat_repo.MockSessionRepo
	message *mock_chat_repo.MockMessageRepo
	device  *mock_remote_device_svc.MockRemoteDeviceSvc
	svc     *chatSvc
}

func setupPeerSessionTest(t *testing.T) *peerSessionTestDeps {
	t.Helper()
	ctrl := gomock.NewController(t)
	deps := &peerSessionTestDeps{
		agent:   mock_agent_repo.NewMockAgentRepo(ctrl),
		backend: mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl),
		session: mock_chat_repo.NewMockSessionRepo(ctrl),
		message: mock_chat_repo.NewMockMessageRepo(ctrl),
		device:  mock_remote_device_svc.NewMockRemoteDeviceSvc(ctrl),
		svc:     NewChat(NoopEmitter{}).(*chatSvc),
	}
	prevAgent, prevBackend, prevSession, prevMessage, prevDevice := agent_repo.Agent(), agent_backend_repo.AgentBackend(), chat_repo.Session(), chat_repo.Message(), remote_device_svc.Default()
	agent_repo.RegisterAgent(deps.agent)
	agent_backend_repo.RegisterAgentBackend(deps.backend)
	chat_repo.RegisterSession(deps.session)
	chat_repo.RegisterMessage(deps.message)
	remote_device_svc.SetDefault(deps.device)
	t.Cleanup(func() {
		agent_repo.RegisterAgent(prevAgent)
		agent_backend_repo.RegisterAgentBackend(prevBackend)
		chat_repo.RegisterSession(prevSession)
		chat_repo.RegisterMessage(prevMessage)
		remote_device_svc.SetDefault(prevDevice)
		ctrl.Finish()
	})
	return deps
}

// Given desktop-owned chat rows, when an account peer asks for the session list,
// then every row keeps its actual title, Agent identity, live status, and last activity.
func TestListPeerSessions_GivenDesktopSessions_ThenReturnsCompleteNonDegradedSummaries(t *testing.T) {
	deps := setupPeerSessionTest(t)
	ctx := context.Background()
	agent := &agent_entity.Agent{
		ID:             7,
		Name:           "Release captain",
		SyncMeta:       syncmeta_entity.SyncMeta{SyncID: "01HXAGENTIDENTITY0000000000"},
		AgentBackendID: 11,
		Status:         consts.ACTIVE,
	}
	deps.device.EXPECT().DeviceFingerprint().Return("sha256:desktop", nil)
	deps.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{agent}, nil)
	deps.session.EXPECT().ListByAgentPagedIncludingGroups(ctx, int64(7), 0, math.MaxInt).
		Return([]*chat_entity.Session{
			{ID: 41, AgentID: 7, Title: "Ship the release", AgentStatus: "waiting", LastMessageAt: 1710000000000, ProviderSessionID: "provider-41", Status: consts.ACTIVE},
			{ID: 42, AgentID: 7, Title: "Investigate timeout", AgentStatus: "error", LastMessageAt: 1710000001000, Status: consts.ACTIVE},
			{ID: 43, AgentID: 7, Title: "Document the release", AgentStatus: "idle", LastMessageAt: 1710000002000, Status: consts.ACTIVE},
		}, nil)
	deps.backend.EXPECT().Find(ctx, int64(11)).Return(&agent_backend_entity.AgentBackend{ID: 11, Type: string(agent_backend_entity.TypeClaudeCode)}, nil).AnyTimes()

	got, err := deps.svc.ListPeerSessions(ctx)
	require.NoError(t, err)
	require.Len(t, got.Sessions, 3)

	assert.Equal(t, wire.SessionSummary{
		SessionID:         41,
		PeerFingerprint:   "sha256:desktop",
		AgentID:           7,
		Title:             "Ship the release",
		AgentSyncID:       "01HXAGENTIDENTITY0000000000",
		ProviderSessionID: "provider-41",
		BackendType:       string(agent_backend_entity.TypeClaudeCode),
		LifecycleState:    wire.SessionLifecycleRunning,
		WaitingForInput:   true,
		UpdatedAt:         1710000000000,
	}, got.Sessions[0])
	assert.Equal(t, wire.SessionLifecycleInterrupted, got.Sessions[1].LifecycleState)
	assert.False(t, got.Sessions[1].WaitingForInput)
	assert.Equal(t, int64(1710000001000), got.Sessions[1].UpdatedAt)
	assert.Equal(t, wire.SessionLifecycleIdle, got.Sessions[2].LifecycleState)
	assert.False(t, got.Sessions[2].WaitingForInput)
	assert.Equal(t, int64(1710000002000), got.Sessions[2].UpdatedAt)

	// Guard R5: desktop rows must never become the round-A unnamed fallback.
	assert.NotEmpty(t, got.Sessions[0].Title, "title must be the stored desktop title, not a placeholder")
	assert.NotEmpty(t, got.Sessions[0].AgentSyncID, "AgentSyncID lets the peer resolve the stored name and avatar")
	assert.NotEqual(t, "Unnamed", got.Sessions[0].Title)
}

// Given a corrupt desktop row missing first-class title or Agent identity, when
// it is listed, then the adapter rejects it instead of fabricating a degraded group.
func TestListPeerSessions_GivenMissingTitleOrAgentIdentity_ThenRejectsRatherThanDegrade(t *testing.T) {
	for name, tc := range map[string]struct {
		title     string
		agentSync string
	}{
		"blank title":            {title: "", agentSync: "01HXAGENTIDENTITY0000000000"},
		"missing Agent identity": {title: "Ship the release", agentSync: ""},
	} {
		t.Run(name, func(t *testing.T) {
			deps := setupPeerSessionTest(t)
			ctx := context.Background()
			deps.device.EXPECT().DeviceFingerprint().Return("sha256:desktop", nil)
			deps.agent.EXPECT().List(ctx).Return([]*agent_entity.Agent{{
				ID: 7, Name: "Release captain", Status: consts.ACTIVE,
				SyncMeta: syncmeta_entity.SyncMeta{SyncID: tc.agentSync},
			}}, nil)
			deps.session.EXPECT().ListByAgentPagedIncludingGroups(ctx, int64(7), 0, math.MaxInt).
				Return([]*chat_entity.Session{{ID: 41, AgentID: 7, Title: tc.title, AgentStatus: "idle", Status: consts.ACTIVE}}, nil)

			got, err := deps.svc.ListPeerSessions(ctx)
			require.Error(t, err)
			assert.Nil(t, got, "never emit a blank or guessed fallback summary")
		})
	}
}

type recordingPeerSubscriber struct {
	done chan struct{}
}

func (*recordingPeerSubscriber) Notify(string, any) error { return nil }
func (s *recordingPeerSubscriber) Done() <-chan struct{}  { return s.done }

// Given the desktop UI already owns a session, when a remote peer attaches,
// then it is added as an additional subscriber and is removed when its channel closes.
func TestAttachPeerSession_GivenLiveDesktopSession_ThenAddsAndCleansUpRemoteSubscriber(t *testing.T) {
	deps := setupPeerSessionTest(t)
	ctx := context.Background()
	deps.session.EXPECT().Find(ctx, int64(41)).Return(&chat_entity.Session{
		ID: 41, AgentID: 7, AgentStatus: "waiting", Status: consts.ACTIVE,
	}, nil)
	deps.agent.EXPECT().Find(ctx, int64(7)).Return(&agent_entity.Agent{ID: 7, AgentBackendID: 11, Status: consts.ACTIVE}, nil)
	deps.backend.EXPECT().Find(ctx, int64(11)).Return(&agent_backend_entity.AgentBackend{ID: 11, Type: string(agent_backend_entity.TypeClaudeCode)}, nil)
	deps.message.EXPECT().List(ctx, int64(41)).Return(nil, nil)

	subscriber := &recordingPeerSubscriber{done: make(chan struct{})}
	got, err := deps.svc.AttachPeerSession(ctx, wire.SessionAttachParams{SessionID: 41}, subscriber)
	require.NoError(t, err)
	assert.Equal(t, wire.SessionAttachResult{
		SessionID: 41, BackendType: string(agent_backend_entity.TypeClaudeCode), LifecycleState: wire.SessionLifecycleRunning,
	}, got)
	assert.Equal(t, 1, deps.svc.peerSubscriberCount(41), "remote attaches alongside the desktop UI; it must not replace a local subscriber")

	close(subscriber.done)
	require.Eventually(t, func() bool {
		return deps.svc.peerSubscriberCount(41) == 0
	}, time.Second, time.Millisecond, "closing the peer channel must remove its remote presence")
}

func TestAttachPeerSession_GivenInvalidOrMissingSession_ThenRejects(t *testing.T) {
	deps := setupPeerSessionTest(t)
	subscriber := &recordingPeerSubscriber{done: make(chan struct{})}
	_, err := deps.svc.AttachPeerSession(context.Background(), wire.SessionAttachParams{}, subscriber)
	require.Error(t, err)

	deps.session.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
	_, err = deps.svc.AttachPeerSession(context.Background(), wire.SessionAttachParams{SessionID: 99}, subscriber)
	assert.True(t, errors.Is(err, ErrPeerSessionNotFound))
}
