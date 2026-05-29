package project_svc

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/pkg/agentruntime"
	"agentre/internal/pkg/code"
	"agentre/internal/repository/project_location_repo"
	"agentre/internal/repository/project_repo"
)

// ResolveProjectCwd 解析「某 device 下某项目」的终端工作目录。
//
//	deviceID == ""  → project.Path（本地）；项目不存在/软删 → ProjectNotFound
//	deviceID != ""  → project_location.FindByProjectAndDevice；未配置 → ProjectLocationMissing
func (s *projectSvc) ResolveProjectCwd(ctx context.Context, projectID int64, deviceID string) (string, error) {
	if deviceID == "" {
		p, err := project_repo.Project().Find(ctx, projectID)
		if err != nil {
			return "", err
		}
		if p == nil || !p.IsActive() {
			return "", i18n.NewError(ctx, code.ProjectNotFound)
		}
		return p.Path, nil
	}
	loc, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", i18n.NewError(ctx, code.ProjectLocationMissing)
		}
		return "", err
	}
	return loc.Path, nil
}

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
