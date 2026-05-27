package chat_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"agentre/internal/model/entity/chat_entity"
)

//go:generate mockgen -source message.go -destination mock_chat_repo/mock_message.go

type MessageRepo interface {
	List(ctx context.Context, sessionID int64) ([]*chat_entity.Message, error)
	Find(ctx context.Context, id int64) (*chat_entity.Message, error)
	NextSeq(ctx context.Context, sessionID int64) (int, error)
	Create(ctx context.Context, m *chat_entity.Message) error
	Update(ctx context.Context, m *chat_entity.Message) error
	// DeleteFromSeq 删除指定 session 下 seq >= fromSeq 的所有消息，返回被删除的行数。
	// 用于「从第 N 条消息开始重新生成」时一次性截断后续记录。
	DeleteFromSeq(ctx context.Context, sessionID int64, fromSeq int) (int64, error)
}

var defaultMessage MessageRepo

func Message() MessageRepo             { return defaultMessage }
func RegisterMessage(impl MessageRepo) { defaultMessage = impl }
func NewMessage() MessageRepo          { return &messageRepo{} }

type messageRepo struct{}

func (r *messageRepo) List(ctx context.Context, sessionID int64) ([]*chat_entity.Message, error) {
	var rows []*chat_entity.Message
	err := db.Ctx(ctx).
		Where("session_id = ?", sessionID).
		Order("seq ASC").
		Find(&rows).Error
	return rows, err
}

func (r *messageRepo) NextSeq(ctx context.Context, sessionID int64) (int, error) {
	var next int
	err := db.Ctx(ctx).
		Table("chat_messages").
		Select("COALESCE(MAX(seq), 0) + 1").
		Where("session_id = ?", sessionID).
		Row().Scan(&next)
	if err != nil {
		return 0, err
	}
	return next, nil
}

func (r *messageRepo) Create(ctx context.Context, m *chat_entity.Message) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *messageRepo) Update(ctx context.Context, m *chat_entity.Message) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *messageRepo) Find(ctx context.Context, id int64) (*chat_entity.Message, error) {
	var m chat_entity.Message
	if err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

func (r *messageRepo) DeleteFromSeq(ctx context.Context, sessionID int64, fromSeq int) (int64, error) {
	res := db.Ctx(ctx).
		Where("session_id = ? AND seq >= ?", sessionID, fromSeq).
		Delete(&chat_entity.Message{})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
