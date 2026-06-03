package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606030001 给 chat_sessions 增 group_id 列(群聊成员 backing session 归属),
// 并加覆盖默认列表过滤维度的索引。group_id=0 表示普通单 agent 会话。
func migration202606030001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606030001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN group_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chat_sessions_agent_group_status
ON chat_sessions(agent_id, group_id, status, last_message_at)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_chat_sessions_agent_group_status`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN group_id`).Error
		},
	}
}
