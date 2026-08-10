package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080014 把技能授权从 agents.skills_json 下沉到 agent_exec_targets
// 那张表（R15e / 决策 33）：执行目标列表可以混用不同 backend 类型，每一档各自持有
// 一份技能授权，不再是整个 Agent 共用一份。
//
//  1. 加 agent_exec_targets.skills_json 列；
//  2. 把每个 Agent 现有的 agents.skills_json 原样复制进它那唯一一行（本轮之前每个
//     Agent 至多一档，复制后语义不变，是无损搬迁不是转换）；没有执行目标行的
//     Agent（未配置后端）没有行可落，天然跳过；
//  3. agents.skills_json 列保留但不再被读取，与 agent_backend_id 同批由后续轮次
//     删除——本轮同时改结构又删列会让回滚窗口内的数据无处可去。
func migration202608080014() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080014",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE agent_exec_targets
	ADD COLUMN skills_json TEXT NOT NULL DEFAULT '[]'`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE agent_exec_targets
	SET skills_json = COALESCE(
		(SELECT a.skills_json FROM agents a WHERE a.id = agent_exec_targets.agent_id),
		'[]'
	)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE agent_exec_targets DROP COLUMN skills_json`).Error
		},
	}
}
