// Package migrations 汇总并执行 Agentre 桌面端 SQLite 数据库的全部迁移。
//
// 规范：
//   - 文件名前缀 = 时间戳排序键（YYYYMMDDNNNN），调用顺序按时间升序。
//   - 每个迁移返回一个 *gormigrate.Migration，包含 Migrate 与可选的 Rollback。
//   - 一次迁移只做一件事；新增表、加列、加索引各自独立成文件，方便回滚和 git bisect。
//   - DDL 优先使用原生 SQL，避免依赖 GORM AutoMigrate 的隐式行为。
package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// RunMigrations 执行全部迁移。新增迁移时把构造函数追加到 migrationList 末尾。
func RunMigrations(db *gorm.DB) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, migrationList())
	return m.Migrate()
}

// migrationList 按时间升序列出全部迁移构造函数。
func migrationList() []*gormigrate.Migration {
	return []*gormigrate.Migration{
		migration202608080001(), // llm_providers
		migration202608080002(), // departments
		migration202608080003(), // agent_backends
		migration202608080004(), // agents + default agent
		migration202608080005(), // projects + project_agents + project_locations
		migration202608080006(), // chat_sessions + chat_messages
		migration202608080007(), // hooks + hook_events
		migration202608080008(), // app_settings + proxy defaults
		migration202608080009(), // server_state + paired_agentreds
		migration202608080010(), // issues + labels + issue_labels + label defaults
		migration202608080011(), // agent_exec_targets + 单元素回填
		migration202608080012(), // projects.local_path_missing（本机未配置路径状态位）
		migration202608080013(), // project_locations 自然键改为 (project, daemon_fingerprint)
		migration202608080014(), // agent_exec_targets.skills_json（技能授权下沉到执行目标行）
	}
}
