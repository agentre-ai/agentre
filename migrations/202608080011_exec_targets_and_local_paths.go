package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080011 合并落地工作区同步前的「执行目标 + 本机路径」数据地基：
//
//  1. agent_exec_targets（R15）——Agent 的有序执行目标列表，并把每个 Agent 现有的
//     agents.agent_backend_id 回填成单元素列表（sort_order=0，语义与转换前一致；
//     agent_backend_id=0 的 Agent 转成空列表）。
//  2. agent_exec_targets.skills_json（R15e）——技能授权下沉到执行目标行，把每个
//     Agent 现有的 agents.skills_json 原样搬进它那唯一一行。
//     agents.agent_backend_id / agents.skills_json 两列保留但不再被读取，由后续
//     轮次删除——同一轮既改结构又删列会让回滚窗口内的数据无处可去。
//  3. chat_sessions.exec_agent_backend_id（R15b）——会话钉住的执行目标档，默认 0
//     （未钉住）；老会话派发时回落到按 R15 顺序挑一档并写回，无需回填。
//  4. projects.local_path_missing（R10/决策 21）——「本机未配置路径」显式状态位；
//     既有行都有本机路径，新列默认 0（已配置），不回填未配置行。
//  5. project_locations 账号内自然键 (project, device_id) → (project,
//     daemon_fingerprint)（决策 26/R2b）：device_id 降级为本地缓存，既有行按
//     device_id 反查 paired_agentreds 换算指纹，反查不到的行丢弃；换键会把多行
//     塌缩成一行，落败行先软删再建部分唯一索引。
//
// 本迁移纯结构 + 一次性回填，不发起任何网络调用。
func migration202608080011() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080011",
		Migrate: func(tx *gorm.DB) error {
			// 1. agent_exec_targets 表 + 索引
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS agent_exec_targets (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	agent_id INTEGER NOT NULL,
	agent_backend_id INTEGER NOT NULL,
	sort_order INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			// 同一个 Agent 的两档不能同序：读取侧靠 sort_order 定「第一个」，并列
			// 会让派发结果随存储顺序漂移。
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_agent_exec_targets_agent_sort
ON agent_exec_targets(agent_id, sort_order)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_agent_exec_targets_agent_backend_id
ON agent_exec_targets(agent_backend_id)`).Error; err != nil {
				return err
			}
			// 2. 回填：agents.agent_backend_id → 单元素列表。软删 Agent（status=2）
			//    的目标行照样回填：回填按 id 全量搬，读取侧可见性由 agents.status 决定。
			if err := tx.Exec(`INSERT INTO agent_exec_targets (agent_id, agent_backend_id, sort_order)
SELECT id, agent_backend_id, 0 FROM agents
WHERE agent_backend_id > 0
  AND NOT EXISTS (SELECT 1 FROM agent_exec_targets t WHERE t.agent_id = agents.id)`).Error; err != nil {
				return err
			}
			// 3. 技能授权下沉：agents.skills_json → 每行执行目标
			if err := tx.Exec(`ALTER TABLE agent_exec_targets
	ADD COLUMN skills_json TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE agent_exec_targets
	SET skills_json = COALESCE(
		(SELECT a.skills_json FROM agents a WHERE a.id = agent_exec_targets.agent_id),
		'[]'
	)`).Error; err != nil {
				return err
			}
			// 4. 会话钉档列（默认 0 = 未钉住）
			if err := tx.Exec(`ALTER TABLE chat_sessions
	ADD COLUMN exec_agent_backend_id BIGINT NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			// 5. 项目「本机未配置路径」状态位（默认 0 = 已配置）
			if err := tx.Exec(`ALTER TABLE projects
	ADD COLUMN local_path_missing BOOLEAN NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			// 6. project_locations 自然键换指纹
			if err := tx.Exec(`ALTER TABLE project_locations
	ADD COLUMN daemon_fingerprint TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE project_locations
	SET daemon_fingerprint = COALESCE(
		(SELECT p.daemon_fingerprint FROM paired_agentreds p WHERE CAST(p.id AS TEXT) = project_locations.device_id),
		''
	)
	WHERE device_id != ''`).Error; err != nil {
				return err
			}
			// 反查不到指纹的孤儿行（device_id 指向已不存在的配对）直接丢弃：留着空
			// 指纹会在同一项目内互撞新的部分唯一索引。
			if err := tx.Exec(`DELETE FROM project_locations
	WHERE device_id != '' AND daemon_fingerprint = ''`).Error; err != nil {
				return err
			}
			// 换自然键会把多行塌缩成一行（两个 device_id 反查出同一指纹——paired_
			// agentreds 只在 url 上有部分唯一索引，从不对 daemon_fingerprint 去重；
			// 解除配对是软删且不清理指向它的路径记录；同一台机器换个 url 再配对就
			// 拿到新主键与同一指纹）。不先去重，下面的 CREATE UNIQUE INDEX 直接失败
			// → RunMigrations 报错 → 桌面端起不来（且本迁移不在事务里、ID 未进
			// ledger，下次启动死在重复 ADD COLUMN，不可自愈）。保留 id 最大的一条
			// （最后一次配对），落败行按既有软删语义置 status=2，不硬删——路径本身
			// 仍可追溯。
			if err := tx.Exec(`UPDATE project_locations
	SET status = 2
	WHERE status = 1
		AND daemon_fingerprint != ''
		AND id NOT IN (
			SELECT MAX(id) FROM project_locations
			WHERE status = 1 AND daemon_fingerprint != ''
			GROUP BY project_id, daemon_fingerprint
		)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_project_locations_proj_device`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_project_locations_proj_fingerprint
	ON project_locations(project_id, daemon_fingerprint) WHERE status = 1`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_project_locations_proj_fingerprint`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_project_locations_proj_device
	ON project_locations(project_id, device_id) WHERE status = 1`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE projects DROP COLUMN local_path_missing`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE chat_sessions DROP COLUMN exec_agent_backend_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE agent_exec_targets DROP COLUMN skills_json`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE project_locations DROP COLUMN daemon_fingerprint`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS agent_exec_targets`).Error
		},
	}
}
