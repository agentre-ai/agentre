package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202606160001 群协作特性基线 —— 未发布,合并自原 202606110001~202606150002 五个迁移
// (整理:同一批群协作特性的 schema 收敛为一个迁移,先例:202606030001 群聊基线)。包含:
//   - agents.tools_json:agent 级内置工具开关(key=org 组织架构读写 / key=group_create 拉群带流程);
//     CEO(system_badge=DEFAULT) 默认开启 org + group_create,其余 agent 默认空数组。
//   - group_tasks 表 + 索引、group_messages.task_id/task_event:群任务卡编排。
//   - workflows 表 + groups.workflow_id:流程实体与「拉群带流程」。
//   - group_members.nickname:群昵称(群内有效显示名,为空回落 agent 全局名)。
//
// 原 202606120001(skills_json 重置)是空操作 —— skills_json 在 202605220004 建表时即默认 '[]',
// 旧数据未发布,故合并时直接省略。
func migration202606160001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202606160001",
		Migrate: func(tx *gorm.DB) error {
			// agents.tools_json + CEO 默认工具(org + group_create)。
			if err := tx.Exec(`ALTER TABLE agents ADD COLUMN tools_json TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE agents SET tools_json = '[{"key":"org","enabled":true},{"key":"group_create","enabled":true}]' WHERE system_badge = 'DEFAULT'`).Error; err != nil {
				return err
			}

			// 群任务卡编排:group_tasks 表 + 索引 + group_messages 任务列。
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS group_tasks (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	group_id INTEGER NOT NULL DEFAULT 0,
	task_no INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL DEFAULT '',
	brief TEXT NOT NULL DEFAULT '',
	creator_member_id INTEGER NOT NULL DEFAULT 0,
	assignee_member_id INTEGER NOT NULL DEFAULT 0,
	status TEXT NOT NULL DEFAULT 'open',
	result TEXT NOT NULL DEFAULT '',
	parent_task_no INTEGER NOT NULL DEFAULT 0,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_group_tasks_group ON group_tasks(group_id, status, task_no)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_group_tasks_group_no ON group_tasks(group_id, task_no)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages ADD COLUMN task_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages ADD COLUMN task_event TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}

			// 流程实体 + 拉群带流程。
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS workflows (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL DEFAULT '',
	content TEXT NOT NULL DEFAULT '',
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE groups ADD COLUMN workflow_id INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}

			// 群昵称(群内有效显示名)。
			return tx.Exec(`ALTER TABLE group_members ADD COLUMN nickname TEXT NOT NULL DEFAULT ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE group_members DROP COLUMN nickname`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE groups DROP COLUMN workflow_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS workflows`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages DROP COLUMN task_event`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE group_messages DROP COLUMN task_id`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_group_tasks_group_no`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS idx_group_tasks_group`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS group_tasks`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE agents DROP COLUMN tools_json`).Error
		},
	}
}
