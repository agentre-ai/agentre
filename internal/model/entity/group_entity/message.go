package group_entity

import (
	"encoding/json"
	"strings"
)

const (
	SenderKindUser   = "user"
	SenderKindAgent  = "agent"
	SenderKindSystem = "system" // 系统行(X 加入 / 工具审批冒泡)
)

// GroupMessage 群内一条消息(始终存原文)。
type GroupMessage struct {
	ID                 int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID            int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	Seq                int    `gorm:"column:seq;type:int;not null;default:0"`
	SenderKind         string `gorm:"column:sender_kind;type:text;not null;default:'agent'"`
	SenderMemberID     int64  `gorm:"column:sender_member_id;type:bigint;not null;default:0"`
	RecipientMemberIDs string `gorm:"column:recipient_member_ids;type:text;not null;default:'[]'"`
	ToUser             bool   `gorm:"column:to_user;type:integer;not null;default:false"`
	Content            string `gorm:"column:content;type:text;not null;default:''"`
	SourceMessageID    int64  `gorm:"column:source_message_id;type:bigint;not null;default:0"`
	Createtime         int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
}

func (*GroupMessage) TableName() string { return "group_messages" }

// Recipients 反序列化收件成员 id 列表(空/坏数据返回空切片)。
func (m *GroupMessage) Recipients() []int64 {
	if m == nil || strings.TrimSpace(m.RecipientMemberIDs) == "" {
		return []int64{}
	}
	var ids []int64
	if err := json.Unmarshal([]byte(m.RecipientMemberIDs), &ids); err != nil {
		return []int64{}
	}
	return ids
}

// SetRecipients 序列化收件成员 id 列表。
func (m *GroupMessage) SetRecipients(ids []int64) {
	if ids == nil {
		ids = []int64{}
	}
	// []int64 恒可 marshal(无 channel/cycle/自定义 marshaler), 故有意忽略 err。
	b, _ := json.Marshal(ids)
	m.RecipientMemberIDs = string(b)
}
