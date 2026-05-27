package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220007 建 hook_sources / hook_rules / hook_events 三张表 ——
// 外部信号源（邮件 / GitHub / Slack / cron / webhook / 系统通知）→ 路由规则 → 事件日志。
//
// 不带 demo seed —— 首次启动时三张表为空，用户从设置页手动加源/规则。
func migration202605220007() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220007",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS hook_sources (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	kind TEXT NOT NULL,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	identifier TEXT NOT NULL DEFAULT '',
	config_json TEXT NOT NULL DEFAULT '{}',
	enabled INTEGER NOT NULL DEFAULT 1,
	connection_status TEXT NOT NULL DEFAULT 'pending',
	last_sync_time INTEGER NOT NULL DEFAULT 0,
	total_count INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_hook_sources_name_active ON hook_sources(name) WHERE status = 1`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_hook_sources_kind ON hook_sources(kind)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS hook_rules (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id INTEGER NOT NULL,
	name TEXT NOT NULL,
	condition_expr TEXT NOT NULL DEFAULT '',
	target_agent_id INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	is_fallback INTEGER NOT NULL DEFAULT 0,
	sort_order INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_hook_rules_source_id ON hook_rules(source_id)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_hook_rules_target_agent_id ON hook_rules(target_agent_id)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS hook_events (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	source_id INTEGER NOT NULL,
	title TEXT NOT NULL,
	source_ref TEXT NOT NULL DEFAULT '',
	sender TEXT NOT NULL DEFAULT '',
	event_type TEXT NOT NULL DEFAULT '',
	event_status TEXT NOT NULL,
	payload_json TEXT NOT NULL DEFAULT '{}',
	matched_rules_json TEXT NOT NULL DEFAULT '[]',
	dispatches_json TEXT NOT NULL DEFAULT '[]',
	received_at INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_hook_events_source_id ON hook_events(source_id)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_hook_events_received_at ON hook_events(received_at)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS hook_events`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS hook_rules`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS hook_sources`).Error
		},
	}
}
