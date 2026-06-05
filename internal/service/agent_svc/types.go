// Package agent_svc 暴露 Agent 的应用服务接口与请求/响应类型。
package agent_svc

import "agentre/internal/service/department_svc"

// 复用 department_svc 里的 AgentItem 作为 service 返回 — 避免重复定义。
type AgentItem = department_svc.AgentItem

// CreateAgentRequest 新建 Agent。SystemBadge 由 service 强制忽略（CEO 仅 seed 写入）。
type CreateAgentRequest struct {
	Name           string                         `json:"name" binding:"required"`
	Description    string                         `json:"description"`
	AvatarColor    string                         `json:"avatarColor"`
	AvatarIcon     string                         `json:"avatarIcon"`
	DepartmentID   int64                          `json:"departmentId"`
	ParentAgentID  int64                          `json:"parentAgentId"`
	AgentBackendID int64                          `json:"agentBackendId" binding:"required"`
	Prompt         []string                       `json:"prompt"`
	Skills         []department_svc.AgentSkillDTO `json:"skills"`
}

type CreateAgentResponse struct {
	Item *AgentItem `json:"item"`
}

// UpdateAgentRequest 更新 Agent；禁止改 system_badge / department_id。
type UpdateAgentRequest struct {
	ID             int64                          `json:"id" binding:"required"`
	Name           string                         `json:"name" binding:"required"`
	Description    string                         `json:"description"`
	AvatarColor    string                         `json:"avatarColor"`
	AvatarIcon     string                         `json:"avatarIcon"`
	AgentBackendID int64                          `json:"agentBackendId"`
	Prompt         []string                       `json:"prompt"`
	Skills         []department_svc.AgentSkillDTO `json:"skills"`
}

type UpdateAgentResponse struct {
	Item *AgentItem `json:"item"`
}

// MoveAgentRequest 换挂载位置 + 同级排序。CEO 拒绝。
type MoveAgentRequest struct {
	ID               int64 `json:"id" binding:"required"`
	NewDepartmentID  int64 `json:"newDepartmentId"`
	NewParentAgentID int64 `json:"newParentAgentId"`
	NewSortOrder     int   `json:"newSortOrder"`
}

type MoveAgentResponse struct {
	Item *AgentItem `json:"item"`
}

// DeleteAgentRequest 软删 Agent。CEO 拒绝。
type DeleteAgentRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteAgentResponse struct{}

// UploadAvatarRequest 写入 Agent 头像（base64 data URL）。
// DataURL 形如 "data:image/png;base64,..." —— service 会校验前缀与字节数。
type UploadAvatarRequest struct {
	ID      int64  `json:"id" binding:"required"`
	DataURL string `json:"dataUrl" binding:"required"`
}

type UploadAvatarResponse struct {
	Item *AgentItem `json:"item"`
}

// DeleteAvatarRequest 清空 Agent 头像，回退到首字母。
type DeleteAvatarRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteAvatarResponse struct {
	Item *AgentItem `json:"item"`
}

// SetPinnedRequest 切换 Agent 用户置顶（侧栏混排列表浮顶）。
type SetPinnedRequest struct {
	ID     int64 `json:"id" binding:"required"`
	Pinned bool  `json:"pinned"`
}

type SetPinnedResponse struct {
	ID     int64 `json:"id"`
	Pinned bool  `json:"pinned"`
}
