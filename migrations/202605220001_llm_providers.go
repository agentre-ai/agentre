package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220001 建 llm_providers 表 —— 用户配置的 LLM 供应商。
//
// 字段语义：
//   - type           cago provider 实现：anthropic / openai-chat / openai-response
//   - name           用户可见名称
//   - api_key        明文 API Key（后续单独迭代加密）
//   - base_url       自定义 endpoint，留空走 provider 默认值
//   - model          默认调用的模型 id，可留空
//   - max_output     单次响应最大输出 token 数（0 = 走 cago catalog 默认）
//   - context_window 上下文窗口 token 数（0 = 走 cago catalog 默认）
//   - provider_key   稳定 UUID，跨机器引用用，agent_backends.llm_provider_key 指向它
//   - status         cago consts: ACTIVE / DELETE
func migration202605220001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS llm_providers (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	name TEXT NOT NULL,
	api_key TEXT NOT NULL DEFAULT '',
	base_url TEXT NOT NULL DEFAULT '',
	model TEXT NOT NULL DEFAULT '',
	max_output INTEGER NOT NULL DEFAULT 0,
	context_window INTEGER NOT NULL DEFAULT 0,
	provider_key TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_llm_providers_provider_key ON llm_providers(provider_key)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS llm_providers`).Error
		},
	}
}
