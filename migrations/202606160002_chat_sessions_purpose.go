package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606160002 给 chat_sessions 增列 purpose —— 标识会话的内部用途。
// 普通顶层会话为空串；子 agent 委派会话(agent_call)落 'subagent_call'，repo 层据此在
// 所有会话列表/计数里无条件隐藏它(见 chat_repo.nonSubagentScope)。与按 group_id 区分的
// 群成员 backing session 不同：子 agent 会话 group_id=0，只能靠 purpose 过滤。
func migration202606160002() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606160002",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN purpose TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN purpose`).Error
		},
	}
}
