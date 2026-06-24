package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240002 Hooks 脚本驱动重构：删 source/rule/event 三表，建 hooks + hook_events。
func migration202606240002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240002",
		Migrate: func(tx *gorm.DB) error {
			for _, drop := range []string{"hook_events", "hook_rules", "hook_sources"} {
				if err := tx.Exec("DROP TABLE IF EXISTS " + drop).Error; err != nil {
					return err
				}
			}
			if err := tx.Exec(`CREATE TABLE hooks (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				name TEXT NOT NULL,
				interpreter TEXT NOT NULL DEFAULT 'bash',
				command TEXT NOT NULL DEFAULT '',
				trigger_type TEXT NOT NULL DEFAULT 'schedule',
				schedule_expr TEXT NOT NULL DEFAULT '',
				timezone TEXT NOT NULL DEFAULT 'Asia/Shanghai',
				env_json TEXT NOT NULL DEFAULT '[]',
				state_json TEXT NOT NULL DEFAULT '{}',
				next_run_at INTEGER NOT NULL DEFAULT 0,
				enabled INTEGER NOT NULL DEFAULT 1,
				last_run_at INTEGER NOT NULL DEFAULT 0,
				last_status TEXT NOT NULL DEFAULT '',
				last_error TEXT NOT NULL DEFAULT '',
				last_duration_ms INTEGER NOT NULL DEFAULT 0,
				total_count INTEGER NOT NULL DEFAULT 0,
				status INTEGER NOT NULL DEFAULT 1,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX idx_hooks_due ON hooks(enabled, next_run_at)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE hook_events (
				id INTEGER PRIMARY KEY AUTOINCREMENT,
				hook_id INTEGER NOT NULL,
				title TEXT NOT NULL,
				dedupe_key TEXT NOT NULL DEFAULT '',
				payload_json TEXT NOT NULL DEFAULT '{}',
				received_at INTEGER NOT NULL DEFAULT 0,
				status INTEGER NOT NULL DEFAULT 1,
				createtime INTEGER NOT NULL DEFAULT 0,
				updatetime INTEGER NOT NULL DEFAULT 0
			)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX idx_hook_events_hook ON hook_events(hook_id, received_at)`).Error; err != nil {
				return err
			}
			// 非空 dedupe_key 才去重；空 key 可多条。
			return tx.Exec(`CREATE UNIQUE INDEX ux_hook_events_dedupe ON hook_events(hook_id, dedupe_key) WHERE dedupe_key <> ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS hook_events`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS hooks`).Error
		},
	}
}
