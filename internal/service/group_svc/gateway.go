// Package group_svc 提供群聊编排的业务逻辑层。
package group_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/pkg/agentruntime/capability"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

//go:generate mockgen -source gateway.go -destination mock_group_svc/mock_gateway.go

// ChatGateway 是 group_svc 对 chat_svc 的窄依赖(只用这几个 seam, ISP)。
type ChatGateway interface {
	EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
	// DeleteSession 软删某 backing session(成员离群/群归档时清理, 避免遗留 group_id>0
	// 的 ACTIVE 会话经 ListAgents 的 IncludingGroups 变体残留在 agent 侧栏)。
	DeleteSession(ctx context.Context, sessionID int64) error
	AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error)
	// BeginToolApproval / FinishToolApproval 把 group_create 的审批卡路由到发起 agent 的
	// 单聊流(走 chat_svc 通用工具审批管线,ToolKey=group_create,无新 UI 形态);Begin 返回
	// 等待 channel,waiter 与前端应答路由由 chat_svc 统一持有。
	BeginToolApproval(ctx context.Context, sessionID int64, blk *chatblocks.ToolApprovalBlock) (<-chan bool, error)
	FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}

// chatSvcGateway 委托给 chat_svc 默认单例。
type chatSvcGateway struct{}

func (chatSvcGateway) EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error) {
	return chat_svc.Chat().EnsureSession(ctx, req)
}

func (chatSvcGateway) Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error) {
	return chat_svc.Chat().Send(ctx, req)
}

func (chatSvcGateway) ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func()) {
	return chat_svc.Chat().ObserveTurn(sessionID)
}

func (chatSvcGateway) Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error) {
	return chat_svc.Chat().Stop(ctx, req)
}

func (chatSvcGateway) DeleteSession(ctx context.Context, sessionID int64) error {
	_, err := chat_svc.Chat().Delete(ctx, &chat_svc.DeleteRequest{SessionID: sessionID})
	return err
}

func (chatSvcGateway) AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error) {
	return chat_svc.Chat().AgentBackendHasCapability(ctx, agentID, wantCap)
}

func (chatSvcGateway) BeginToolApproval(ctx context.Context, sessionID int64, blk *chatblocks.ToolApprovalBlock) (<-chan bool, error) {
	return chat_svc.Chat().BeginToolApproval(ctx, sessionID, blk)
}

func (chatSvcGateway) FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error {
	return chat_svc.Chat().FinishToolApproval(ctx, sessionID, requestID, status, result)
}
