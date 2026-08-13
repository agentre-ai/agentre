package migrations

import (
	"encoding/json"
	"strings"

	"github.com/go-gormigrate/gormigrate/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// migration202608110001 Provider 1 → N 稳定模型（spec 2026-08-11-llm-provider-models
// 「Domain model and lifecycle / Migration and compatibility」）。
//
// 变更内容（全程在同一个事务里，任一步失败整体回滚，不留半批）：
//
//  1. 新建 llm_provider_models 表：稳定 model_key（不可变，唯一索引）、可编辑
//     model_id、显示名、token 元数据、独立 enabled 与软删除 status。
//     model_id 按 (provider_id, model_id) 精确、大小写敏感去重 —— SQLite TEXT
//     默认 BINARY 比较即逐字节比较，'A' ≠ 'a'，天然满足「精确、大小写敏感」。
//  2. 搬迁旧 llm_providers 单模型数据：
//     - Model 非空 → 生成一条稳定子模型（model_key 用随机 UUID），搬 token 元数据，
//     设为默认，Provider 保持 enabled=1；
//     - Model 为空 → 不伪造模型，Provider 保留为可见但 enabled=0，default_model_key 为空。
//  3. 重建 llm_providers 表：删掉旧 model / max_output / context_window 三列，
//     新增 enabled + default_model_key，然后重建 uniq_llm_providers_provider_key。
//  4. agent_backends / chat_sessions 各加 model_key 列（默认 ”）。旧 Backend 与
//     Session 只钉 ProviderKey、配空 ModelKey，解释为 provider-default（#39 语义）。
//  5. 旧 Claude Route 字符串 {"OPUS":"<provider-key>"} 一次性转换为结构化 target
//     {"OPUS":{"providerKey":"...","modelKey":""}}（modelKey 空 = provider-default；
//     alias 缺省 = inherit-main，运行时不保留旧 parser）。
func migration202608110001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608110001",
		Migrate: func(tx *gorm.DB) error {
			return tx.Transaction(func(tx *gorm.DB) error {
				// 1. llm_provider_models 表 + 索引
				if err := tx.Exec(`CREATE TABLE IF NOT EXISTS llm_provider_models (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	provider_id INTEGER NOT NULL,
	model_key TEXT NOT NULL DEFAULT '',
	model_id TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	context_window INTEGER NOT NULL DEFAULT 0,
	max_output INTEGER NOT NULL DEFAULT 0,
	enabled INTEGER NOT NULL DEFAULT 1,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
					return err
				}
				// model_key 全局稳定唯一，承担 Backend/Session/Route 跨实体引用。
				if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_llm_provider_models_model_key
ON llm_provider_models(model_key)`).Error; err != nil {
					return err
				}
				// 同一 Provider 内 model_id 精确、大小写敏感去重（SQLite BINARY 比较）。
				if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_llm_provider_models_provider_model_id
ON llm_provider_models(provider_id, model_id)`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_llm_provider_models_provider
ON llm_provider_models(provider_id, status)`).Error; err != nil {
					return err
				}

				// 2. 搬迁旧 Provider 单模型 → 稳定子模型，并计算 enabled / default_model_key
				type legacyProvider struct {
					ID            int64
					ProviderKey   string
					Type          string
					Name          string
					APIKey        string
					BaseURL       string
					Model         string
					MaxOutput     int
					ContextWindow int
					Status        int
					Createtime    int64
					Updatetime    int64
				}
				var providers []legacyProvider
				if err := tx.Raw(`SELECT id, provider_key, type, name, api_key, base_url,
	model, max_output, context_window, status, createtime, updatetime
	FROM llm_providers`).Scan(&providers).Error; err != nil {
					return err
				}

				type rebuilt struct {
					Enabled         int
					DefaultModelKey string
				}
				rebuildByID := make(map[int64]rebuilt, len(providers))
				for _, p := range providers {
					r := rebuilt{Enabled: 1}
					if strings.TrimSpace(p.Model) != "" {
						key := uuid.NewString()
						if err := tx.Exec(`INSERT INTO llm_provider_models
	(provider_id, model_key, model_id, name, context_window, max_output, enabled, status, createtime, updatetime)
	VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?)`,
							p.ID, key, p.Model, p.Model, p.ContextWindow, p.MaxOutput,
							p.Status, p.Createtime, p.Updatetime).Error; err != nil {
							return err
						}
						r.DefaultModelKey = key
					} else {
						// 空 Model 不伪造子模型：Provider 保留可见但 disabled。
						r.Enabled = 0
					}
					rebuildByID[p.ID] = r
				}

				// 3. 重建 llm_providers：删旧单模型列，加 enabled + default_model_key
				if err := tx.Exec(`CREATE TABLE llm_providers_new (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	type TEXT NOT NULL,
	name TEXT NOT NULL,
	api_key TEXT NOT NULL DEFAULT '',
	base_url TEXT NOT NULL DEFAULT '',
	provider_key TEXT NOT NULL DEFAULT '',
	enabled INTEGER NOT NULL DEFAULT 1,
	default_model_key TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
					return err
				}
				for _, p := range providers {
					r := rebuildByID[p.ID]
					if err := tx.Exec(`INSERT INTO llm_providers_new
	(id, type, name, api_key, base_url, provider_key, enabled, default_model_key, status, createtime, updatetime)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						p.ID, p.Type, p.Name, p.APIKey, p.BaseURL, p.ProviderKey,
						r.Enabled, r.DefaultModelKey, p.Status, p.Createtime, p.Updatetime).Error; err != nil {
						return err
					}
				}
				if err := tx.Exec(`DROP TABLE llm_providers`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`ALTER TABLE llm_providers_new RENAME TO llm_providers`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_llm_providers_provider_key
ON llm_providers(provider_key)`).Error; err != nil {
					return err
				}

				// 4. Backend / Session 的稳定 ModelKey 列（默认 '' = provider-default）
				if err := tx.Exec(`ALTER TABLE agent_backends
	ADD COLUMN model_key TEXT NOT NULL DEFAULT ''`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`ALTER TABLE chat_sessions
	ADD COLUMN model_key TEXT NOT NULL DEFAULT ''`).Error; err != nil {
					return err
				}

				// 5. 旧 Claude Route 字符串 → 结构化 target
				if err := migrateLegacyModelRoutes(tx); err != nil {
					return err
				}
				return nil
			})
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Transaction(func(tx *gorm.DB) error {
				// 结构化 Route target → 旧字符串格式（保留 providerKey，丢弃 modelKey）
				if err := rollbackModelRoutes(tx); err != nil {
					return err
				}
				if err := tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN model_key`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`ALTER TABLE agent_backends DROP COLUMN model_key`).Error; err != nil {
					return err
				}
				// 恢复旧 Provider 单模型列：model 取默认子模型的 model_id，token 元数据取默认子模型。
				if err := tx.Exec(`CREATE TABLE llm_providers_old (
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
				if err := tx.Exec(`INSERT INTO llm_providers_old
	(id, type, name, api_key, base_url, model, max_output, context_window, provider_key, status, createtime, updatetime)
SELECT p.id, p.type, p.name, p.api_key, p.base_url,
	COALESCE(m.model_id, ''),
	COALESCE(m.max_output, 0),
	COALESCE(m.context_window, 0),
	p.provider_key, p.status, p.createtime, p.updatetime
FROM llm_providers p
LEFT JOIN llm_provider_models m ON m.model_key = p.default_model_key AND m.status = 1`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`DROP TABLE llm_providers`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`ALTER TABLE llm_providers_old RENAME TO llm_providers`).Error; err != nil {
					return err
				}
				if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_llm_providers_provider_key
ON llm_providers(provider_key)`).Error; err != nil {
					return err
				}
				return tx.Exec(`DROP TABLE IF EXISTS llm_provider_models`).Error
			})
		},
	}
}

// routeTarget 是迁移后 Claude Route 的结构化 target：ModelKey 空 = provider-default；
// alias 缺失 = inherit-main（不写进 JSON）。
type routeTarget struct {
	ProviderKey string `json:"providerKey"`
	ModelKey    string `json:"modelKey"`
}

// migrateLegacyModelRoutes 把 claudecode 旧格式 {"OPUS":"<provider-key>"} 一次性转成
// 结构化 {"OPUS":{"providerKey":"...","modelKey":""}}。其它类型/空路由原样保留。
func migrateLegacyModelRoutes(tx *gorm.DB) error {
	type legacyBackend struct {
		ID          int64
		Type        string
		ModelRoutes string
	}
	var backends []legacyBackend
	if err := tx.Raw(`SELECT id, type, model_routes FROM agent_backends`).Scan(&backends).Error; err != nil {
		return err
	}
	for _, b := range backends {
		old := strings.TrimSpace(b.ModelRoutes)
		if b.Type != "claudecode" || old == "" || old == "{}" {
			continue
		}
		var legacy map[string]string
		if err := json.Unmarshal([]byte(old), &legacy); err != nil {
			// 无法解析的旧路由按原样保留，不阻断迁移（无消费者会读它）。
			continue
		}
		targets := make(map[string]routeTarget, len(legacy))
		for alias, providerKey := range legacy {
			providerKey = strings.TrimSpace(providerKey)
			if providerKey == "" {
				continue
			}
			targets[strings.ToUpper(strings.TrimSpace(alias))] = routeTarget{ProviderKey: providerKey}
		}
		if len(targets) == 0 {
			continue
		}
		out, err := json.Marshal(targets)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE agent_backends SET model_routes = ? WHERE id = ?`, string(out), b.ID).Error; err != nil {
			return err
		}
	}
	return nil
}

// rollbackModelRoutes 把结构化 target 转回旧字符串格式（只保留 providerKey）。
func rollbackModelRoutes(tx *gorm.DB) error {
	type legacyBackend struct {
		ID          int64
		Type        string
		ModelRoutes string
	}
	var backends []legacyBackend
	if err := tx.Raw(`SELECT id, type, model_routes FROM agent_backends`).Scan(&backends).Error; err != nil {
		return err
	}
	for _, b := range backends {
		old := strings.TrimSpace(b.ModelRoutes)
		if b.Type != "claudecode" || old == "" || old == "{}" {
			continue
		}
		var targets map[string]routeTarget
		if err := json.Unmarshal([]byte(old), &targets); err != nil {
			continue
		}
		legacy := make(map[string]string, len(targets))
		for alias, t := range targets {
			if t.ProviderKey == "" {
				continue
			}
			legacy[strings.ToUpper(strings.TrimSpace(alias))] = t.ProviderKey
		}
		if len(legacy) == 0 {
			continue
		}
		out, err := json.Marshal(legacy)
		if err != nil {
			return err
		}
		if err := tx.Exec(`UPDATE agent_backends SET model_routes = ? WHERE id = ?`, string(out), b.ID).Error; err != nil {
			return err
		}
	}
	return nil
}
