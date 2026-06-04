package group_svc

import (
	"agentre/internal/model/entity/group_entity"
)

type CreateGroupRequest struct {
	Title              string
	CoordinatorAgentID int64
	DepartmentID       int64
	ProjectID          int64
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

const maxMembers = 8
