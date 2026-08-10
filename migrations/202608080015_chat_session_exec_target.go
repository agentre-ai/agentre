package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080015 给 chat_sessions 加「这条会话钉住的执行目标档」一列
// （R15b / 决策 36）：exec_agent_backend_id 是 agent_exec_targets.agent_backend_id
// 的引用，回答"哪一档"；与既有的 exec_device_id / exec_daemon_fingerprint（回答
// "哪台机器 / 哪个 daemon 实例"）语义正交、并列，三列此后由 chat_repo 的
// UpdateExecDaemon 同一条专用单列更新一并写入。
//
// 新列默认值 0（尚未钉住），覆盖首轮与全部老会话 —— 派发链路读到 0 时回落到按
// R15 顺序挑一档并写回，因此本迁移不需要回填任何既有行。
func migration202608080015() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080015",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions
	ADD COLUMN exec_agent_backend_id BIGINT NOT NULL DEFAULT 0`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN exec_agent_backend_id`).Error
		},
	}
}
