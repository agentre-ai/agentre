package project_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/project_repo"
)

// ResolveSessionCwd 根据 session 的 project_id 解析实际 cwd。
//
//   - project_id = 0   → AgentCwd(agent_id)（自由会话）
//   - 否则             → project.Path
//
// 项目被软删时回退到 AgentCwd，避免老 session 因项目消失而无法启动。
func (s *projectSvc) ResolveSessionCwd(ctx context.Context, session *chat_entity.Session) (string, error) {
	if session == nil {
		return "", i18n.NewError(ctx, code.ChatSessionNotFound)
	}
	if session.ProjectID == 0 {
		return agentruntime.AgentCwd(session.AgentID)
	}
	p, err := project_repo.Project().Find(ctx, session.ProjectID)
	if err != nil {
		return "", err
	}
	if p == nil {
		return agentruntime.AgentCwd(session.AgentID)
	}
	return p.Path, nil
}
