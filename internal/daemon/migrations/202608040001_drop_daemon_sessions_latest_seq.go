package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608040001 删掉 daemon_sessions.latest_seq。
//
// 202608010001 建它时的说明是「notification log 的 denormalized 游标,由后续任务的写入
// 路径维护」,而那个写入路径从来没有出现:「某会话最新的 seq」的唯一真相源是通知日志
// 自己的 MAX(seq)(notification_repo.LatestSeq / LatestSeqByPeer,会话清单与显式接管读的
// 就是它)。一列没有写入方、读出来永远是 0 的列不是无害的冗余 —— 照着列名去读它的调用方
// 会让每个客户端每次重连都从游标 0 重拉整段日志。
//
// 补丁迁移而不是改 202608010001:既有迁移一律不改(见包注释与 AGENTS.md)。
func migration202608040001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608040001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE daemon_sessions DROP COLUMN latest_seq`).Error
		},
		// 回滚把列加回来(默认 0,与建表时一致),降级到旧版本 agentred 时那条按列名写入的
		// INSERT 仍然写得进去。
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE daemon_sessions ADD COLUMN latest_seq INTEGER NOT NULL DEFAULT 0`).Error
		},
	}
}
