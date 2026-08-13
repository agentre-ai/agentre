package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608130001 落地 R14 的本端执行目标顺序覆盖：agent_exec_target_overrides。
//
// 这是一张**纯本地**表：每台桌面端各自持一份，一个 Agent 至多一行，存该 Agent 执行
// 目标档 backend 的顺序（order_json，JSON 数组）。它不同步、不进同步队列、不上行 ——
// 顺序是机器相关的偏好，账号级只同步 agent_exec_targets 那份默认顺序。本迁移只建表，
// 不回填：没有覆盖 = 用账号默认，是新装与升级两端的同一初始态。
func migration202608130001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608130001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS agent_exec_target_overrides (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id INTEGER NOT NULL,
	order_json TEXT NOT NULL DEFAULT '[]',
	updatetime BIGINT NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_exec_target_overrides_agent
ON agent_exec_target_overrides(agent_id)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS agent_exec_target_overrides`).Error
		},
	}
}
