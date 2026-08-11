package chat_svc

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

var (
	// ErrPeerSessionNotFound distinguishes an unknown desktop session from an
	// authorized remote peer that has merely not attached it yet.
	ErrPeerSessionNotFound = errors.New("desktop peer session not found")
	// ErrPeerSessionMetadata means a corrupt local row cannot safely be exposed
	// as a round-A-style unnamed fallback.
	ErrPeerSessionMetadata = errors.New("desktop peer session is missing required metadata")
)

// PeerSessionSubscriber is the remote-notification sink registered by an
// attached account peer. Task 3 only owns its lifecycle; task 4 publishes the
// canonical session events to these subscribers.
type PeerSessionSubscriber interface {
	Notify(method string, params any) error
	Done() <-chan struct{}
}

// ListPeerSessions projects every desktop-owned top-level chat session into
// the existing runtime.session.list wire shape. AgentSyncID refers to the
// account Agent record, where the caller resolves the stored name and avatar.
func (s *chatSvc) ListPeerSessions(ctx context.Context) (*wire.SessionListResult, error) {
	device := remote_device_svc.Default()
	if device == nil {
		return nil, fmt.Errorf("desktop peer fingerprint: remote device service unavailable")
	}
	fingerprint, err := device.DeviceFingerprint()
	if err != nil {
		return nil, fmt.Errorf("desktop peer fingerprint: %w", err)
	}
	if fingerprint == "" {
		return nil, fmt.Errorf("%w: desktop fingerprint", ErrPeerSessionMetadata)
	}

	agents, err := agent_repo.Agent().List(ctx)
	if err != nil {
		return nil, operationFailedWithCause(ctx, err)
	}
	result := &wire.SessionListResult{
		Sessions:                make([]wire.SessionSummary, 0),
		SupportsSessionMetadata: true,
	}
	for _, agent := range agents {
		if agent == nil {
			return nil, fmt.Errorf("%w: nil Agent", ErrPeerSessionMetadata)
		}
		sessions, err := chat_repo.Session().ListByAgentPagedIncludingGroups(ctx, agent.ID, 0, math.MaxInt)
		if err != nil {
			return nil, operationFailedWithCause(ctx, err)
		}
		for _, session := range sessions {
			summary, err := peerSessionSummary(ctx, session, agent, fingerprint)
			if err != nil {
				return nil, err
			}
			result.Sessions = append(result.Sessions, summary)
		}
	}
	return result, nil
}

// AttachPeerSession registers one remote consumer without changing the Wails
// emitter used by the local desktop UI. The registration follows the RPC
// connection's Done signal, so a disconnected peer can never remain present.
func (s *chatSvc) AttachPeerSession(ctx context.Context, params wire.SessionAttachParams, subscriber PeerSessionSubscriber) (wire.SessionAttachResult, error) {
	if params.SessionID <= 0 || subscriber == nil {
		return wire.SessionAttachResult{}, fmt.Errorf("%w: attach parameters", ErrPeerSessionNotFound)
	}
	session, err := chat_repo.Session().Find(ctx, params.SessionID)
	if err != nil {
		return wire.SessionAttachResult{}, operationFailedWithCause(ctx, err)
	}
	if session == nil {
		return wire.SessionAttachResult{}, ErrPeerSessionNotFound
	}
	agent, err := agent_repo.Agent().Find(ctx, session.AgentID)
	if err != nil {
		return wire.SessionAttachResult{}, operationFailedWithCause(ctx, err)
	}
	if agent == nil {
		return wire.SessionAttachResult{}, fmt.Errorf("%w: agent %d", ErrPeerSessionNotFound, session.AgentID)
	}
	backendType, err := peerSessionBackendType(ctx, session, agent)
	if err != nil {
		return wire.SessionAttachResult{}, err
	}
	lifecycle, _, err := peerSessionLifecycle(session)
	if err != nil {
		return wire.SessionAttachResult{}, err
	}

	detach := s.addPeerSubscriber(session.ID, subscriber)
	go func() {
		<-subscriber.Done()
		detach()
	}()
	return wire.SessionAttachResult{
		SessionID:      session.ID,
		BackendType:    backendType,
		LifecycleState: lifecycle,
		LatestSeq:      0,
	}, nil
}

func peerSessionSummary(ctx context.Context, session *chat_entity.Session, agent *agent_entity.Agent, fingerprint string) (wire.SessionSummary, error) {
	if session == nil || agent == nil || strings.TrimSpace(session.Title) == "" || strings.TrimSpace(agent.Name) == "" || agent.SyncID == "" {
		return wire.SessionSummary{}, fmt.Errorf("%w: session %d", ErrPeerSessionMetadata, sessionID(session))
	}
	backendType, err := peerSessionBackendType(ctx, session, agent)
	if err != nil {
		return wire.SessionSummary{}, err
	}
	lifecycle, waiting, err := peerSessionLifecycle(session)
	if err != nil {
		return wire.SessionSummary{}, err
	}
	return wire.SessionSummary{
		SessionID:         session.ID,
		PeerFingerprint:   fingerprint,
		AgentID:           agent.ID,
		Title:             session.Title,
		AgentSyncID:       agent.SyncID,
		ProviderSessionID: session.ProviderSessionID,
		BackendType:       backendType,
		LifecycleState:    lifecycle,
		WaitingForInput:   waiting,
		LatestSeq:         0,
		UpdatedAt:         session.LastMessageAt,
	}, nil
}

func peerSessionBackendType(ctx context.Context, session *chat_entity.Session, agent *agent_entity.Agent) (string, error) {
	backendID := agent.AgentBackendID
	if session.ExecAgentBackendID != 0 {
		backendID = session.ExecAgentBackendID
	}
	if backendID == 0 {
		return "", nil
	}
	backend, err := agent_backend_repo.AgentBackend().Find(ctx, backendID)
	if err != nil {
		return "", operationFailedWithCause(ctx, err)
	}
	if backend == nil {
		return "", nil
	}
	return backend.Type, nil
}

func peerSessionLifecycle(session *chat_entity.Session) (lifecycle string, waiting bool, err error) {
	switch session.AgentStatus {
	case "running":
		return wire.SessionLifecycleRunning, false, nil
	case "waiting":
		return wire.SessionLifecycleRunning, true, nil
	case "idle":
		return wire.SessionLifecycleIdle, false, nil
	case "error":
		return wire.SessionLifecycleInterrupted, false, nil
	default:
		return "", false, fmt.Errorf("%w: session %d status %q", ErrPeerSessionMetadata, sessionID(session), session.AgentStatus)
	}
}

func sessionID(session *chat_entity.Session) int64 {
	if session == nil {
		return 0
	}
	return session.ID
}

func (s *chatSvc) addPeerSubscriber(sessionID int64, subscriber PeerSessionSubscriber) func() {
	s.peerSubscribersMu.Lock()
	if s.peerSubscribers == nil {
		s.peerSubscribers = map[int64]map[uint64]PeerSessionSubscriber{}
	}
	s.nextPeerSubscriberID++
	id := s.nextPeerSubscriberID
	subscribers := s.peerSubscribers[sessionID]
	if subscribers == nil {
		subscribers = map[uint64]PeerSessionSubscriber{}
		s.peerSubscribers[sessionID] = subscribers
	}
	subscribers[id] = subscriber
	s.peerSubscribersMu.Unlock()

	var once bool
	return func() {
		s.peerSubscribersMu.Lock()
		defer s.peerSubscribersMu.Unlock()
		if once {
			return
		}
		once = true
		delete(s.peerSubscribers[sessionID], id)
		if len(s.peerSubscribers[sessionID]) == 0 {
			delete(s.peerSubscribers, sessionID)
		}
	}
}

func (s *chatSvc) peerSubscriberCount(sessionID int64) int {
	s.peerSubscribersMu.Lock()
	defer s.peerSubscribersMu.Unlock()
	return len(s.peerSubscribers[sessionID])
}
