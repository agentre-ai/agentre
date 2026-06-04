// Package group_svc 提供群聊编排的业务逻辑层。
package group_svc

import (
	"context"

	"agentre/internal/pkg/agentruntime/capability"
	"agentre/internal/service/chat_svc"
)

//go:generate mockgen -source gateway.go -destination mock_group_svc/mock_gateway.go

// ChatGateway 是 group_svc 对 chat_svc 的窄依赖(只用这几个 seam, ISP)。
type ChatGateway interface {
	EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
	AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error)
}

// chatSvcGateway 委托给 chat_svc 默认单例。
type chatSvcGateway struct{}

func (chatSvcGateway) EnsureGroupMemberSession(ctx context.Context, agentID, projectID, groupID int64) (int64, error) {
	return chat_svc.Chat().EnsureGroupMemberSession(ctx, agentID, projectID, groupID)
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

func (chatSvcGateway) AgentBackendHasCapability(ctx context.Context, agentID int64, wantCap capability.Capability) (bool, error) {
	return chat_svc.Chat().AgentBackendHasCapability(ctx, agentID, wantCap)
}
