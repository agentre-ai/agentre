package chat_svc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/runtimes/remote/wire"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/repository/syncstate_repo"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/cago-frame/cago/pkg/utils/httputils"
)

// ErrPeerExecutionUnavailable is deliberately narrower than a generic remote
// dial failure: the desktop remains present and its persisted transcript can
// still be read, but the agentred pinned to this session cannot execute a new
// turn.
var ErrPeerExecutionUnavailable = errors.New("desktop history remains available, but the session execution target is unavailable")

// ErrPeerAgentNotFound 是 R17 建会话时对端点的 agentSyncId 在本机找不到（同步还没
// 落地 / 该 Agent 已删除）——不静默落到别的 Agent 上，也不建一条幽灵会话。
var ErrPeerAgentNotFound = errors.New("desktop peer agent not found")

// ErrPeerProjectNotFound 是 R17 建会话时对端点的 cwd 在本机找不到对应项目（项目刚被
// 删/改路径，与上报时不一致）——拒绝而不是静默把会话跑进一个错误的目录。
var ErrPeerProjectNotFound = errors.New("desktop peer project not found")

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
//
// 对端点到的会话在本机不存在时，这是 R17 的「浏览器把新对话派到这台桌面端上」：
// 会话行/标题/转录都在这台机器上新建并跑首轮（见 runFreshPeerSession）。会话存在时
// 原样续轮，行为不变。
func (s *chatSvc) RunPeerSession(ctx context.Context, params wire.RunParams, source PeerSessionSource) (*SendResponse, error) {
	if params.SessionID <= 0 || source.Device == "" {
		return nil, fmt.Errorf("invalid peer session run")
	}
	session, err := chat_repo.Session().Find(ctx, params.SessionID)
	if err != nil {
		return nil, err
	}
	if session == nil {
		return s.runFreshPeerSession(ctx, params, source)
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

// runFreshPeerSession 在桌面端上为远端对端新建一条会话并跑首轮（R17）。对端只携带
// 账号级 agentSyncId 与该项目在本机的 cwd，本机据此解析本地 agent / project 行，然后
// 走与桌面端自己发消息完全相同的 Send 路径（排队、权限模式、转录落库都发生）——
// 会话行、标题与转录因此都住在这台机器上，返回的也是本机的真实会话 id。
func (s *chatSvc) runFreshPeerSession(ctx context.Context, params wire.RunParams, source PeerSessionSource) (*SendResponse, error) {
	if strings.TrimSpace(params.AgentSyncID) == "" {
		return nil, fmt.Errorf("invalid fresh peer session run: agentSyncId is required")
	}
	agentID, err := syncstate_repo.SyncState().FindLocalID(ctx, syncwire.KindAgent, params.AgentSyncID)
	if err != nil {
		return nil, err
	}
	if agentID <= 0 {
		return nil, fmt.Errorf("%w: agent sync id %q", ErrPeerAgentNotFound, params.AgentSyncID)
	}
	projectID, err := resolvePeerProjectID(ctx, strings.TrimSpace(params.Cwd))
	if err != nil {
		return nil, err
	}
	return s.send(ctx, &SendRequest{
		AgentID:               agentID,
		ProjectID:             projectID,
		Text:                  params.UserText,
		PermissionMode:        params.PermissionMode,
		EmitTurnStartedBypass: true,
		peerSource:            source.messageSource(),
	}, sendOptions{})
}

// resolvePeerProjectID 把对端报告的 cwd（该桌面端自己上报过的本机项目路径）翻回本地
// project 行：会话要钉到用户挑的那个项目上，转录与后续轮次才带正确的项目上下文。
// cwd 为空（未挑项目的自由会话）返回 0；路径对不上本机任何已配置项目（项目刚被
// 删/改路径，与上报时不一致）时拒绝——把会话静默跑进一个错误的目录比报错更糟。
func resolvePeerProjectID(ctx context.Context, cwd string) (int64, error) {
	if cwd == "" {
		return 0, nil
	}
	rows, err := project_repo.Project().List(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range rows {
		if p != nil && p.IsActive() && !p.LocalPathMissing && strings.TrimSpace(p.Path) == cwd {
			return p.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: project cwd %q", ErrPeerProjectNotFound, cwd)
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
