package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080012 给 projects 加「本机未配置路径」的显式状态位（R10、决策 21）。
//
// local_path_missing 为 true 表示该项目行本机还没有可用的 cwd；path 此时恒为
// 空串，但空串不是判据本身 —— 任何读取 path 的分支都必须先看这一列，而不是
// 靠 path == "" 推断（避免"path 为空却被当成已配置项目"的隐式坑）。
//
// 既有行全部已经有本机路径，新列默认值 0（已配置）使它们迁移后保持不变；本
// 迁移不回填任何"未配置"行 —— 那类行由后续轮次的同步落地产生。
func migration202608080012() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080012",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE projects
	ADD COLUMN local_path_missing BOOLEAN NOT NULL DEFAULT 0`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE projects DROP COLUMN local_path_missing`).Error
		},
	}
}
