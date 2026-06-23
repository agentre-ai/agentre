package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606030001 建群聊基线 schema。群聊未发布前的中间迁移合并到此处。
func migration202606030001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606030001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN group_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chat_sessions_agent_group_status
ON chat_sessions(agent_id, group_id, status, last_message_at)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE agents ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS groups (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	title TEXT NOT NULL DEFAULT '',
	host_agent_id INTEGER NOT NULL DEFAULT 0,
	department_id INTEGER NOT NULL DEFAULT 0,
	project_id INTEGER NOT NULL DEFAULT 0,
	run_status TEXT NOT NULL DEFAULT 'idle',
	round_count INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	pinned BOOLEAN NOT NULL DEFAULT 0,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_groups_status ON groups(status, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_members (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	agent_id INTEGER NOT NULL DEFAULT 0,
	backing_session_id INTEGER NOT NULL DEFAULT 0,
	role TEXT NOT NULL DEFAULT 'member',
	status TEXT NOT NULL DEFAULT 'active',
	joined_at INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_members_group ON group_members(group_id, status)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_group_members_group_agent ON group_members(group_id, agent_id)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_messages (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	seq INTEGER NOT NULL DEFAULT 0,
	sender_kind TEXT NOT NULL DEFAULT 'agent',
	sender_member_id INTEGER NOT NULL DEFAULT 0,
	recipient_member_ids TEXT NOT NULL DEFAULT '[]',
	to_user INTEGER NOT NULL DEFAULT 0,
	content TEXT NOT NULL DEFAULT '',
	source_message_id INTEGER NOT NULL DEFAULT 0,
	createtime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_messages_group_seq ON group_messages(group_id, seq)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_group_messages_group_seq`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_group_members_group_agent`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_group_members_group`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_groups_status`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS group_messages`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS group_members`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS groups`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE agents DROP COLUMN pinned`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_chat_sessions_agent_group_status`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN group_id`).Error
		},
	}
}
