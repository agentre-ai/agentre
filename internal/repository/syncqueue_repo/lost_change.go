// Package syncqueue_repo 提供本地同步骨架三张表的持久化访问：没能同步的改动
// （LostChangeRepo），以及分出站 / 入站两个方向的同步队列（OutboundQueueRepo /
// InboundQueueRepo）。本包目前只做基础读写；真正的入队 / 出队 / 30 天回收是
// 后续任务，这里只给它们留位置。
package syncqueue_repo

import (
	"context"

	"github.com/cago-frame/cago/database/db"

	"github.com/agentre-ai/agentre/internal/model/entity/syncqueue_entity"
)

//go:generate mockgen -source lost_change.go -destination mock_syncqueue_repo/mock_lost_change.go

// LostChangeRepo 「没能同步的改动」列表的持久化访问（R5）。
type LostChangeRepo interface {
	Create(ctx context.Context, row *syncqueue_entity.LostChange) error
	ListByAccount(ctx context.Context, accountID int64) ([]*syncqueue_entity.LostChange, error)
	Delete(ctx context.Context, id int64) error
}

var defaultLostChange LostChangeRepo

// LostChange 取默认仓储单例。
func LostChange() LostChangeRepo { return defaultLostChange }

// RegisterLostChange 注入仓储实现，由 bootstrap 调用一次。
func RegisterLostChange(impl LostChangeRepo) { defaultLostChange = impl }

// NewLostChange 构造默认 GORM 实现。
func NewLostChange() LostChangeRepo { return &lostChangeRepo{} }

type lostChangeRepo struct{}

func (r *lostChangeRepo) Create(ctx context.Context, row *syncqueue_entity.LostChange) error {
	return db.Ctx(ctx).Create(row).Error
}

func (r *lostChangeRepo) ListByAccount(ctx context.Context, accountID int64) ([]*syncqueue_entity.LostChange, error) {
	var rows []*syncqueue_entity.LostChange
	err := db.Ctx(ctx).
		Where("sync_account_id = ?", accountID).
		Order("occurred_at DESC, id DESC").
		Find(&rows).Error
	return rows, err
}

func (r *lostChangeRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Where("id = ?", id).Delete(&syncqueue_entity.LostChange{}).Error
}
