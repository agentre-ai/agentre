package group_svc

import "agentre/internal/model/entity/group_entity"

// groupMessageEvent 是推给前端的消息事件载荷; json 形状必须与 app.GroupMessageItem 一致
// (lowercase 键 + recipientMemberIDs 为已解码的 number 数组), 这样前端 live 事件与 GroupLoad 返回同构。
type groupMessageEvent struct {
	ID                 int64   `json:"id"`
	Seq                int     `json:"seq"`
	SenderKind         string  `json:"senderKind"`
	SenderMemberID     int64   `json:"senderMemberID"`
	RecipientMemberIDs []int64 `json:"recipientMemberIDs"`
	ToUser             bool    `json:"toUser"`
	Content            string  `json:"content"`
	Createtime         int64   `json:"createtime"`
}

func toGroupMessageEvent(m *group_entity.GroupMessage) groupMessageEvent {
	return groupMessageEvent{
		ID:                 m.ID,
		Seq:                m.Seq,
		SenderKind:         m.SenderKind,
		SenderMemberID:     m.SenderMemberID,
		RecipientMemberIDs: m.Recipients(),
		ToUser:             m.ToUser,
		Content:            m.Content,
		Createtime:         m.Createtime,
	}
}
