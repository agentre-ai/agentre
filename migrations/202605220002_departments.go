package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220002 建 departments 表。
//
// 字段语义：
//   - parent_id      0 = 顶级部门（与 CEO Agent 同层）
//   - lead_agent_id  0 = 未指定部门长
//   - accent_color   "agent-1".."agent-10" / "neutral" / ""
//   - status         cago consts: ACTIVE / DELETE
func migration202605220002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220002",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS departments (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	icon TEXT NOT NULL DEFAULT '',
	accent_color TEXT NOT NULL DEFAULT '',
	parent_id INTEGER NOT NULL DEFAULT 0,
	lead_agent_id INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_departments_parent_id ON departments(parent_id)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_departments_lead_agent_id ON departments(lead_agent_id)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS departments`).Error
		},
	}
}
