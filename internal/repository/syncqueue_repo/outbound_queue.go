package syncqueue_repo

import (
	"context"

	"github.com/cago-frame/cago/database/db"

	"github.com/agentre-ai/agentre/internal/model/entity/syncqueue_entity"
)

//go:generate mockgen -source outbound_queue.go -destination mock_syncqueue_repo/mock_outbound_queue.go

// OutboundQueueRepo 出站方向同步队列的持久化访问（R7）：本地待上行的改动。
type OutboundQueueRepo interface {
	Create(ctx context.Context, row *syncqueue_entity.OutboundQueueItem) error
	ListByAccount(ctx context.Context, accountID int64) ([]*syncqueue_entity.OutboundQueueItem, error)
	Delete(ctx context.Context, id int64) error
}

var defaultOutboundQueue OutboundQueueRepo

// OutboundQueue 取默认仓储单例。
func OutboundQueue() OutboundQueueRepo { return defaultOutboundQueue }

// RegisterOutboundQueue 注入仓储实现，由 bootstrap 调用一次。
func RegisterOutboundQueue(impl OutboundQueueRepo) { defaultOutboundQueue = impl }

// NewOutboundQueue 构造默认 GORM 实现。
func NewOutboundQueue() OutboundQueueRepo { return &outboundQueueRepo{} }

type outboundQueueRepo struct{}

func (r *outboundQueueRepo) Create(ctx context.Context, row *syncqueue_entity.OutboundQueueItem) error {
	return db.Ctx(ctx).Create(row).Error
}

func (r *outboundQueueRepo) ListByAccount(ctx context.Context, accountID int64) ([]*syncqueue_entity.OutboundQueueItem, error) {
	var rows []*syncqueue_entity.OutboundQueueItem
	err := db.Ctx(ctx).
		Where("sync_account_id = ?", accountID).
		Order("queued_at ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *outboundQueueRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Where("id = ?", id).Delete(&syncqueue_entity.OutboundQueueItem{}).Error
}
