package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080011 建 agent_exec_targets 表：Agent 的**有序执行目标列表**（R15）。
//
// 字段语义：
//   - agent_id         列表属于哪个 Agent
//   - agent_backend_id 这一档指向的 backend（backend 的 device_id 决定它在哪台机器上）
//   - sort_order       列表内顺序，0 起；新建会话按顺序取第一个可用的档
//
// 回填：每个 Agent 现有的 agents.agent_backend_id 转成这张表里的**单元素列表**
// （sort_order = 0），语义与转换前完全一致；agent_backend_id = 0（未配置后端）
// 转成空列表。agents.agent_backend_id 列**保留但不再被读取**，由后续轮次删除——
// 同一轮既改结构又删列会让回滚窗口内的数据无处可去。
//
// 软删 Agent（status = 2）的目标行照样回填：回填按 id 全量搬，读取侧的可见性由
// agents.status 决定（见 agent_repo 的 ListByBackend / CountByBackends）。
func migration202608080011() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080011",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS agent_exec_targets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id INTEGER NOT NULL,
	agent_backend_id INTEGER NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			// 同一个 Agent 的两档不能同序：读取侧靠 sort_order 定「第一个」，并列会让
			// 派发结果随存储顺序漂移。
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_exec_targets_agent_sort
ON agent_exec_targets(agent_id, sort_order)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_exec_targets_agent_backend_id
ON agent_exec_targets(agent_backend_id)`).Error; err != nil {
				return err
			}

			return tx.Exec(`INSERT INTO agent_exec_targets (agent_id, agent_backend_id, sort_order)
SELECT id, agent_backend_id, 0 FROM agents
WHERE agent_backend_id > 0
  AND NOT EXISTS (SELECT 1 FROM agent_exec_targets t WHERE t.agent_id = agents.id)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`DROP TABLE IF EXISTS agent_exec_targets`).Error
		},
	}
}
