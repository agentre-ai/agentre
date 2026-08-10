package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608100001 为 chat_messages 的 pi 转录替换「恢复标记」查询补索引。
//
// FindReplacementRecoveryForSession 按 (role, device_id) 查恢复标记（role =
// "__agentre_pi_recovery__"）。该查询在每次 pi 会话发送消息时都会经
// acquireTurnGate → reconcileTranscriptReplacement 同步执行；此前 chat_messages
// 只有 (session_id, seq) 一个索引，这条件退化成对整张表（行内含大块 blocks_json）
// 的全表扫描 —— 实测每条 pi 消息发送被拖慢 0.6~0.85s。补 (role, device_id) 索引后
// 变为索引点查，毫秒级。
//
// 纯结构迁移（CREATE INDEX IF NOT EXISTS），不回填、不重写任何行。
func migration202608100001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608100001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`CREATE INDEX IF NOT EXISTS idx_chat_messages_recovery
ON chat_messages(role, device_id)`).Error
		},
	}
}
