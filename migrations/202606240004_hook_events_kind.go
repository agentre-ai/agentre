package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240004 给 hook_events 加 kind 列:区分脚本产出(output)与运行失败留痕(failure),
// 让失败也能与成功产出并列进「运行日志」,不再只活在 hooks.last_error 单槽里。
func migration202606240004() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240004",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hook_events ADD COLUMN kind TEXT NOT NULL DEFAULT 'output'`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE hook_events DROP COLUMN kind`).Error
		},
	}
}
