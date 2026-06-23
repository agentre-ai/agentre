package group_entity

import "strings"

const (
	RoleHost   = "host"
	RoleMember = "member"

	MemberActive = "active"
	MemberLeft   = "left"
)

// GroupMember 群内成员(稳定身份, 绑定一条 backing chat_session)。
type GroupMember struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID          int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	AgentID          int64  `gorm:"column:agent_id;type:bigint;not null;default:0"`
	BackingSessionID int64  `gorm:"column:backing_session_id;type:bigint;not null;default:0"`
	Role             string `gorm:"column:role;type:text;not null;default:'member'"`
	Status           string `gorm:"column:status;type:text;not null;default:'active'"`
	JoinedAt         int64  `gorm:"column:joined_at;type:bigint;not null;default:0"`
	// Nickname 是该成员在本群的备注名(群昵称)。非空时即「群内有效名」——
	// roster 展示 / @mention 匹配 / 注入给 AI 的群名单都用它;为空回落 agent 全局名。
	Nickname string `gorm:"column:nickname;type:text;not null;default:''"`
}

func (*GroupMember) TableName() string { return "group_members" }

func (m *GroupMember) IsHost() bool   { return m != nil && m.Role == RoleHost }
func (m *GroupMember) IsActive() bool { return m != nil && m.Status == MemberActive }

// DisplayName 给出该成员在本群的有效显示名:设了群昵称(非空白)用昵称,否则回落
// 传入的 agent 全局名。空白昵称视同未设(对应「留空 = 用原名」)。
func (m *GroupMember) DisplayName(agentName string) string {
	if m != nil && strings.TrimSpace(m.Nickname) != "" {
		return m.Nickname
	}
	return agentName
}
