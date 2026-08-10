package syncqueue_entity

// 出站队列的操作类型（R7）。
const (
	OpCreate = "create"
	OpUpdate = "update"
	OpDelete = "delete"
)

// OutboundQueueItem 出站队列的一行（R7）：本地待上行的改动，每条带基版本
// （R4a、R6a）。本端新建、server 从未见过的行 BaseVersion 与 EntitySyncID 都
// 为空。
type OutboundQueueItem struct {
	ID int64 `gorm:"column:id;primaryKey;autoIncrement"`
	// SyncAccountID 这条待上行的改动属于哪个账号（R13a）。
	SyncAccountID int64 `gorm:"column:sync_account_id;type:bigint;not null;default:0"`
	// EntityType 指向账号级七张表中的哪一张。
	EntityType string `gorm:"column:entity_type;type:text;not null"`
	// LocalID 变更行的本地自增主键，供后续任务重新读取该行当前内容。
	LocalID int64 `gorm:"column:local_id;type:bigint;not null;default:0"`
	// EntitySyncID 这一行自己的同步标识；本端新建、server 从未见过时为空。
	EntitySyncID string `gorm:"column:entity_sync_id;type:text;not null;default:''"`
	// Op 三选一：OpCreate / OpUpdate / OpDelete。
	Op string `gorm:"column:op;type:text;not null"`
	// BaseVersion 上行时携带的「本端最后一次见到的同步版本号」（R4a）；新建行为空（0）。
	BaseVersion int64 `gorm:"column:base_version;type:bigint;not null;default:0"`
	// QueuedAt 入队时间（毫秒 epoch）。
	QueuedAt int64 `gorm:"column:queued_at;type:bigint;not null;default:0"`
}

func (*OutboundQueueItem) TableName() string { return "sync_outbound_queue" }
