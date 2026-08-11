package chat_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/cago-frame/cago/pkg/utils/httputils"
)

// ErrPeerExecutionUnavailable is deliberately narrower than a generic remote
// dial failure: the desktop remains present and its persisted transcript can
// still be read, but the agentred pinned to this session cannot execute a new
// turn.
var ErrPeerExecutionUnavailable = errors.New("desktop history remains available, but the session execution target is unavailable")

// peerMessageSource is source metadata carried by an account peer. It is
// persisted inside the existing text StoredBlock so it survives transcript
// reload without changing the chat_messages schema.
type peerMessageSource struct {
	Device string
	Name   string
}

// PeerSessionSource is the account-authorized caller identity captured by the
// relay connection. The request cannot nominate a different fingerprint.
type PeerSessionSource struct {
	Device string
	Name   string
}

func (s PeerSessionSource) messageSource() peerMessageSource {
	return peerMessageSource{Device: s.Device, Name: s.Name}
}

// PeerSessionControlResult is returned by inbound decision handlers. A second
// winner is a normal, typed outcome rather than a transport failure.
type PeerSessionControlResult struct {
	AlreadyHandled bool `json:"alreadyHandled,omitempty"`
}

// PeerSessionRunResult describes a rejected write while keeping the desktop
// transcript readable. The peer adapter serializes it in typed RPC error data.
type PeerSessionRunResult struct {
	Accepted             bool `json:"accepted"`
	HistoryAvailable     bool `json:"historyAvailable"`
	ExecutionUnavailable bool `json:"executionUnavailable"`
}

// RunPeerSession adapts the existing runtime.run wire request into the
// desktop's session-level Send path. Backend, queue, permission, and MCP
// selection remain entirely owned by Send; only the authenticated source is
// added to the persisted user row.
func (s *chatSvc) RunPeerSession(ctx context.Context, params wire.RunParams, source PeerSessionSource) (*SendResponse, error) {
	if params.SessionID <= 0 || source.Device == "" {
		return nil, fmt.Errorf("invalid peer session run")
	}
	session, err := chat_repo.Session().Find(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return nil, ErrPeerSessionNotFound
	}
	_, backend, _, err := s.resolveAgentBackend(ctx, session, session.AgentID, session.ProjectID)
	if err != nil {
		return nil, err
	}
	if backend.IsRemote() {
		if err := s.preflightPeerRemoteExecution(ctx, backend, session.ID); err != nil {
			return nil, err
		}
	}
	return s.send(ctx, &SendRequest{
		SessionID:             session.ID,
		Text:                  params.UserText,
		PermissionMode:        params.PermissionMode,
		EmitTurnStartedBypass: true,
		peerSource:            source.messageSource(),
	}, sendOptions{})
}

func (s *chatSvc) preflightPeerRemoteExecution(ctx context.Context, backend *agent_backend_entity.AgentBackend, sessionID int64) error {
	_, err := s.selectRunner(ctx, backend, sessionID)
	if err == nil {
		if deviceID, ok := backend.DeviceIDInt(); ok {
			s.releaseRemoteRuntime(deviceID, sessionID)
		}
		return nil
	}
	var httpErr *httputils.Error
	if errors.As(err, &httpErr) && (httpErr.Code == code.RemoteRunnerDialFailed || httpErr.Code == code.AgentBackendInvalidDevice) {
		return fmt.Errorf("%w: %v", ErrPeerExecutionUnavailable, err)
	}
	return err
}

// EnqueuePeerSession uses the existing steer queue; source metadata is carried
// to the normal consumed-steer persistence path rather than creating a second
// queue for remote peers.
func (s *chatSvc) EnqueuePeerSession(ctx context.Context, params wire.SteerParams, source PeerSessionSource) (*EnqueueResponse, error) {
	return s.enqueue(ctx, &EnqueueRequest{SessionID: params.SessionID, Text: params.Text, peerSource: source.messageSource()})
}

func (s *chatSvc) AnswerPeerUserQuestion(ctx context.Context, params wire.SubmitAnswerParams) (PeerSessionControlResult, error) {
	_, err := s.AnswerUserQuestion(ctx, &AnswerUserQuestionRequest{
		SessionID: params.SessionID, RequestID: params.RequestID,
		Answers: chatblocks.AnswersFromRuntime(params.Answers), Skipped: params.Skipped,
	})
	return peerSessionControlResult(err)
}

func (s *chatSvc) AnswerPeerToolPermission(ctx context.Context, params wire.SubmitToolPermissionParams) (PeerSessionControlResult, error) {
	_, err := s.AnswerToolPermission(ctx, &AnswerToolPermissionRequest{
		SessionID: params.SessionID, RequestID: params.RequestID, Allow: params.Allow,
		AlwaysAllowSession: params.AlwaysAllowSession, DenyReason: params.DenyReason,
	})
	return peerSessionControlResult(err)
}

func peerSessionControlResult(err error) (PeerSessionControlResult, error) {
	if errors.Is(err, agentruntime.ErrWaiterNotFound) || errors.Is(err, agentruntime.ErrNoActiveTurn) {
		return PeerSessionControlResult{AlreadyHandled: true}, nil
	}
	return PeerSessionControlResult{}, err
}

// PeerSessionExecutionResult maps the one write-only availability failure to
// the typed RPC payload consumed by the inbound adapter.
func PeerSessionExecutionResult(err error) (PeerSessionRunResult, error) {
	if errors.Is(err, ErrPeerExecutionUnavailable) {
		return PeerSessionRunResult{
			HistoryAvailable: true, ExecutionUnavailable: true,
		}, nil
	}
	return PeerSessionRunResult{}, err
}

func persistPeerMessageSource(message *chat_entity.Message, source peerMessageSource) error {
	if message == nil || source.Device == "" {
		return nil
	}
	var stored []cagoblocks.StoredBlock
	if err := json.Unmarshal([]byte(message.BlocksJSON), &stored); err != nil {
		return fmt.Errorf("decode user message source: %w", err)
	}
	for index := range stored {
		if stored[index].Type != "text" && stored[index].Type != "display_text" {
			continue
		}
		var data map[string]json.RawMessage
		if err := json.Unmarshal(stored[index].Data, &data); err != nil {
			return fmt.Errorf("decode user text source: %w", err)
		}
		device, _ := json.Marshal(source.Device)
		data["sourceDevice"] = device
		if source.Name != "" {
			name, _ := json.Marshal(source.Name)
			data["sourceDeviceName"] = name
		}
		encoded, err := json.Marshal(data)
		if err != nil {
			return fmt.Errorf("encode user text source: %w", err)
		}
		stored[index].Data = encoded
		all, err := json.Marshal(stored)
		if err != nil {
			return fmt.Errorf("encode user message source: %w", err)
		}
		message.BlocksJSON = string(all)
		return nil
	}
	return nil
}

func (s *chatSvc) withPeerSteerSources(steers []agentruntime.ConsumedSteer) []agentruntime.ConsumedSteer {
	for index := range steers {
		if steers[index].SourcePeer != "" || steers[index].QueuedID == "" {
			continue
		}
		value, ok := s.peerSteerSources.LoadAndDelete(steers[index].QueuedID)
		if !ok {
			continue
		}
		source, ok := value.(peerMessageSource)
		if !ok {
			continue
		}
		steers[index].SourcePeer = source.Device
		steers[index].SourceName = source.Name
	}
	return steers
}

func firstTextBlock(blocks []cagoblocks.ContentBlock) string {
	for _, block := range blocks {
		switch text := block.(type) {
		case cagoblocks.TextBlock:
			return text.Text
		case *cagoblocks.TextBlock:
			if text != nil {
				return text.Text
			}
		}
	}
	return ""
}

func peerMessageSourceOf(message *chat_entity.Message) peerMessageSource {
	if message == nil || message.Role != "user" {
		return peerMessageSource{}
	}
	var stored []cagoblocks.StoredBlock
	if json.Unmarshal([]byte(message.BlocksJSON), &stored) != nil {
		return peerMessageSource{}
	}
	for _, block := range stored {
		if block.Type != "text" && block.Type != "display_text" {
			continue
		}
		var data struct {
			Device string `json:"sourceDevice"`
			Name   string `json:"sourceDeviceName"`
		}
		if json.Unmarshal(block.Data, &data) == nil && data.Device != "" {
			return peerMessageSource{Device: data.Device, Name: data.Name}
		}
	}
	return peerMessageSource{}
}
