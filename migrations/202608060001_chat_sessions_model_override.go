package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608060001 给 chat_sessions 增列 model_override —— 会话级模型覆盖。
// 空串 = 跟随供应商默认（每轮按 agent_backends.LLMProviderKey → llm_provider.Model 解析）；
// 非空时按 override > provider 模型 > backend 默认 的优先级生效，由 chat_svc.SetSessionModel
// 落库、runTurn 启动时透传给 runtime。默认空串即「不覆盖」，老数据语义与加列前完全一致。
func migration202608060001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608060001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions ADD COLUMN model_override TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN model_override`).Error
		},
	}
}
