package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080018 给 sync_lost_changes 补上路径记录与 backend 的跨机自然键
// 两列（R5a）。
//
// 「没能同步的改动」里的每一条都要能被**恢复成当前值**，而恢复走的是同步适配器的
// 落地路径：project_location 的项目归属与 agentred 指纹、agent_backend 指向的那台
// agentred，都不在载荷正文里——它们是上行项的顶层字段（决策 26 的自然键）。少了
// 这两列，恢复一条路径记录必然解析不出它属于哪个项目（errRefMissing），恢复一条
// 远端 backend 则会把它当成本机的重新上行。
//
// 既有行按空串落地：本轮之前写进去的记录没有这两个值，恢复时与今天表现一致。
func migration202608080018() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080018",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE sync_lost_changes
	ADD COLUMN project_sync_id TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE sync_lost_changes
	ADD COLUMN agentred_fingerprint TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE sync_lost_changes DROP COLUMN agentred_fingerprint`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE sync_lost_changes DROP COLUMN project_sync_id`).Error
		},
	}
}
