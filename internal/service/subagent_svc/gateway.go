package subagent_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

//go:generate mockgen -source gateway.go -destination mock_subagent_svc/mock_gateway.go

// AgentGateway 子 agent 工具对 agent 数据的窄依赖(ISP)。agent_repo.Agent() 直接满足。
type AgentGateway interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
}

// ChatGateway 子 agent 工具对 chat_svc 的窄依赖:读调用方会话的项目 → 建一次性会话 → 起轮 → 读最终文本。
type ChatGateway interface {
	SessionProjectID(ctx context.Context, sessionID int64) (int64, error)
	EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
	FinalAssistantText(ctx context.Context, messageID int64) (string, error)
}

// chatSvcGateway 委托给 chat_svc 默认单例(生产实现;测试注 mock)。
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
func (chatSvcGateway) FinalAssistantText(ctx context.Context, messageID int64) (string, error) {
	return chat_svc.Chat().FinalAssistantText(ctx, messageID)
}
func (chatSvcGateway) SessionProjectID(ctx context.Context, sessionID int64) (int64, error) {
	return chat_svc.Chat().SessionProjectID(ctx, sessionID)
}

// ChatSvcGateway 生产用 chat_svc 网关(供 bootstrap 接线)。
func ChatSvcGateway() ChatGateway { return chatSvcGateway{} }
