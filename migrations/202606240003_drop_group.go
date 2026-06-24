package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606240003 删群聊(能力并入编排):DROP 群 4 表 + chat_sessions.group_id
// (先删引用它的 idx_chat_sessions_agent_group_status,否则 SQLite 拒绝 DROP COLUMN)
// + 重置 DEFAULT agent tools_json 为 org + orchestrate(去掉 group_create)。
func migration202606240003() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606240003",
		Migrate: func(tx *gorm.DB) error {
			for _, sql := range []string{
				`DROP TABLE IF EXISTS group_tasks`,
				`DROP TABLE IF EXISTS group_messages`,
				`DROP TABLE IF EXISTS group_members`,
				`DROP TABLE IF EXISTS groups`,
				`DROP INDEX IF EXISTS idx_chat_sessions_agent_group_status`,
				`ALTER TABLE chat_sessions DROP COLUMN group_id`,
				`UPDATE agents SET tools_json='[{"key":"org","enabled":true},{"key":"orchestrate","enabled":true}]' WHERE system_badge='DEFAULT'`,
			} {
				if err := tx.Exec(sql).Error; err != nil {
					return err
				}
			}
			return nil
		},
		// No Rollback: group is hard-deleted, app unreleased, no rollback need.
	}
}
