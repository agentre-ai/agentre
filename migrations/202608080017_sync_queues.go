package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080017 建两张新表（387-390 行）承载本地同步骨架，物理上落成
// 三张表——出站/入站是「同步队列」按方向拆开的两张（390 行）：
//
//   - sync_lost_changes   「没能同步的改动」(R5)：存被覆盖 / 被丢弃 / 被拒的版本
//     正文与原因，保留 30 天。留住被覆盖那一版的同步标识与基版本，供 R5a 的
//     恢复判断目标是否已被删除。三类失效事件（被覆盖 R5 / 悬空引用超期丢弃
//     R2a / 超窗口重同步时被拒 R6a）共用一张表（决策 12）。
//   - sync_outbound_queue 出站方向（R7）：本地待上行的改动，每条带基版本
//     （R4a、R6a）；本端新建、server 从未见过的行 base_version / entity_sync_id
//     都为空。
//   - sync_inbound_queue  入站方向（R2a）：已收到但因引用目标未到达而暂缓落地
//     的行，同样保留 30 天，超期丢弃并转入 sync_lost_changes。
//
// 三张表都按账号分命名空间（sync_account_id）：换账号后旧账号的记录留在本地，
// 但不参与新账号下的展示 / 上行（R13a）。本迁移只建表结构与索引，不写任何
// 入队 / 出队 / 30 天回收逻辑——真正的上行 / 下行 / 冲突落地是后续任务，这里
// 只给它们留位置（本任务边界：不写任何网络调用）。
func migration202608080017() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080017",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS sync_lost_changes (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sync_account_id BIGINT NOT NULL DEFAULT 0,
	entity_type TEXT NOT NULL,
	entity_sync_id TEXT NOT NULL,
	base_version BIGINT NOT NULL DEFAULT 0,
	reason TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '',
	origin_device TEXT NOT NULL DEFAULT '',
	occurred_at BIGINT NOT NULL DEFAULT 0,
	createtime BIGINT NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_lost_changes_account
ON sync_lost_changes(sync_account_id, createtime)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS sync_outbound_queue (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sync_account_id BIGINT NOT NULL DEFAULT 0,
	entity_type TEXT NOT NULL,
	local_id BIGINT NOT NULL DEFAULT 0,
	entity_sync_id TEXT NOT NULL DEFAULT '',
	op TEXT NOT NULL,
	base_version BIGINT NOT NULL DEFAULT 0,
	queued_at BIGINT NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_outbound_queue_account
ON sync_outbound_queue(sync_account_id, queued_at)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS sync_inbound_queue (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	sync_account_id BIGINT NOT NULL DEFAULT 0,
	entity_type TEXT NOT NULL,
	entity_sync_id TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '',
	missing_sync_id TEXT NOT NULL DEFAULT '',
	received_at BIGINT NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_sync_inbound_queue_account
ON sync_inbound_queue(sync_account_id, received_at)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS sync_inbound_queue`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS sync_outbound_queue`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS sync_lost_changes`).Error
		},
	}
}
