package group_svc

import (
	"agentre/internal/model/entity/group_entity"
)

type CreateGroupRequest struct {
	Title              string
	CoordinatorAgentID int64
	DepartmentID       int64
	ProjectID          int64
	// MemberAgentIDs 建群时一并拉入的初始成员（协调者之外）。每个都经 backendSupportsGroup
	// 门控 + 幂等（ensureMember）。
	MemberAgentIDs []int64
}

type GroupDetail struct {
	Group    *group_entity.Group
	Members  []*group_entity.GroupMember
	Messages []*group_entity.GroupMessage
}

type SendGroupMessageRequest struct {
	GroupID            int64
	Text               string
	RecipientMemberIDs []int64 // 可选: 显式收件人(优先于解析)
	ToUser             bool
}

// InviteResult 是 group_invite 成功拉入的一个成员(id + 显示名),回给协调者 turn。
type InviteResult struct {
	AgentID int64
	Name    string
}

const maxMembers = 8
