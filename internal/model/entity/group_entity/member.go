package group_entity

const (
	RoleCoordinator = "coordinator"
	RoleMember      = "member"

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
}

func (*GroupMember) TableName() string { return "group_members" }

func (m *GroupMember) IsCoordinator() bool { return m != nil && m.Role == RoleCoordinator }
func (m *GroupMember) IsActive() bool      { return m != nil && m.Status == MemberActive }
