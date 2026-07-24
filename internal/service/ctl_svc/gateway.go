package ctl_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
	"github.com/agentre-ai/agentre/internal/service/project_svc"
)

//go:generate mockgen -source gateway.go -destination mock_ctl_svc/mock_gateway.go

// AgentGateway 控制 API 对 agent 数据的窄依赖(ISP)。agent_repo.Agent() 直接满足。
type AgentGateway interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
}

// ProjectInfo 是控制 API 回传的扁平项目信息(与 project_svc 的树形态解耦)。
type ProjectInfo struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

// ProjectGateway 控制 API 对项目列表的窄依赖：拿到扁平化的可派发项目。
type ProjectGateway interface {
	List(ctx context.Context) ([]ProjectInfo, error)
}

// ChatGateway 控制 API 对 chat_svc 的窄依赖：建会话 → 起轮 →(可选)读最终文本。
type ChatGateway interface {
	EnsureSession(ctx context.Context, req *chat_svc.EnsureSessionRequest) (*chat_svc.EnsureSessionResponse, error)
	Send(ctx context.Context, req *chat_svc.SendRequest) (*chat_svc.SendResponse, error)
	ObserveTurn(sessionID int64) (<-chan chat_svc.TurnResult, func())
	FinalAssistantText(ctx context.Context, messageID int64) (string, error)
	Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error)
}

// ---- 生产实现(供 bootstrap 接线) ----

// chatSvcGateway 委托给 chat_svc 默认单例(懒解析 chat_svc.Chat(), 兼容 bootstrap 接线早于
// RegisterChat 的时序)。
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
func (chatSvcGateway) FinalAssistantText(ctx context.Context, messageID int64) (string, error) {
	return chat_svc.Chat().FinalAssistantText(ctx, messageID)
}
func (chatSvcGateway) Stop(ctx context.Context, req *chat_svc.StopRequest) (*chat_svc.StopResponse, error) {
	return chat_svc.Chat().Stop(ctx, req)
}

// ChatSvcGateway 生产用 chat_svc 网关(供 bootstrap 接线)。
func ChatSvcGateway() ChatGateway { return chatSvcGateway{} }

// projectSvcGateway 把 project_svc 的项目树扁平化为 ProjectInfo 列表。
type projectSvcGateway struct{}

func (projectSvcGateway) List(ctx context.Context) ([]ProjectInfo, error) {
	tree, err := project_svc.Default().ListTree(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectInfo, 0, len(tree))
	var walk func(nodes []*project_svc.ProjectNode)
	walk = func(nodes []*project_svc.ProjectNode) {
		for _, n := range nodes {
			if n == nil || n.Project == nil {
				continue
			}
			out = append(out, ProjectInfo{ID: n.Project.ID, Name: n.Project.Name, Path: n.Project.Path})
			walk(n.Children)
		}
	}
	walk(tree)
	return out, nil
}

// ProjectSvcGateway 生产用项目网关(供 bootstrap 接线)。
func ProjectSvcGateway() ProjectGateway { return projectSvcGateway{} }
