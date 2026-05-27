package app

import (
	"agentre/internal/service/agent_svc"
)

// CreateAgent 新建 Agent。
func (a *App) CreateAgent(req *agent_svc.CreateAgentRequest) (*agent_svc.CreateAgentResponse, error) {
	return agent_svc.Agent().Create(a.ctx, req)
}

// UpdateAgent 更新 Agent。
func (a *App) UpdateAgent(req *agent_svc.UpdateAgentRequest) (*agent_svc.UpdateAgentResponse, error) {
	return agent_svc.Agent().Update(a.ctx, req)
}

// MoveAgent 换部门 + 同级排序。
func (a *App) MoveAgent(req *agent_svc.MoveAgentRequest) (*agent_svc.MoveAgentResponse, error) {
	return agent_svc.Agent().Move(a.ctx, req)
}

// DeleteAgent 软删 Agent。CEO 拒绝。
func (a *App) DeleteAgent(req *agent_svc.DeleteAgentRequest) (*agent_svc.DeleteAgentResponse, error) {
	return agent_svc.Agent().Delete(a.ctx, req)
}

// UploadAgentAvatar 写入 Agent 头像（base64 data URL，≤ 2MB，PNG/JPEG/WEBP）。
func (a *App) UploadAgentAvatar(req *agent_svc.UploadAvatarRequest) (*agent_svc.UploadAvatarResponse, error) {
	return agent_svc.Agent().UploadAvatar(a.ctx, req)
}

// DeleteAgentAvatar 清空 Agent 头像，回退到首字母派生。
func (a *App) DeleteAgentAvatar(req *agent_svc.DeleteAvatarRequest) (*agent_svc.DeleteAvatarResponse, error) {
	return agent_svc.Agent().DeleteAvatar(a.ctx, req)
}
