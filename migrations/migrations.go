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
		migration202605220001(), // llm_providers
		migration202605220002(), // departments
		migration202605220003(), // agent_backends
		migration202605220004(), // agents + CEO seed
		migration202605220005(), // projects + project_agents + project_locations
		migration202605220006(), // chat_sessions + chat_messages
		migration202605220007(), // hook_sources + hook_rules + hook_events
		migration202605220008(), // app_settings + proxy host/port seed
		migration202605220009(), // server_state + paired_agentreds
		migration202605220010(), // projects.sort_order
		migration202605220011(), // issues + labels + issue_labels + label seed
		migration202606030001(), // group chat baseline
		migration202606100001(), // agent_backends.default_model
		migration202606160001(), // 群协作:agent 工具(org/group_create)+ 群任务/流程 + 群成员昵称
		migration202606160002(), // chat_sessions.purpose(隐藏 subagent 委派会话)
		migration202606240001(), // 编排能力基座:Run/Task 表 + chat_sessions.run_id + orchestrate 工具种子
		migration202606240002(), // Hooks 脚本驱动重构:删 source/rule/event,建 hooks + hook_events
		migration202606240003(), // 删群聊(能力并入编排):DROP 群 4 表 + chat_sessions.group_id + 重置工具种子
		migration202606240004(), // hook_events 加 kind 列:区分脚本产出(output)与运行失败留痕(failure)
		migration202606250001(), // hooks.interpreter_path:自定义解释器二进制路径
		migration202606250002(), // workflows.tags/outline:流程库展示层(标签/步骤概览)
		migration202607020001(), // issue 看板:stage/position/assignee_agent_id/session_id + 回填
		migration202607030001(), // 编排可参与范围:orchestration_runs.allowed_agent_ids
		migration202607030002(), // 回报分层:orch_tasks.summary
		migration202607040001(), // workflows.graph/is_default + seed 默认流程
		migration202607040002(), // 运行时进度 overlay:orch_tasks.node_ref + orchestration_runs.flow_graph
		migration202607050001(), // workflows.template + 回填(带图=占位符 / 无图=content)
		migration202607080001(), // 四内置流程取代旧默认 + DROP is_default
		migration202607080002(), // 删步骤/DAG 列:workflows.graph/template/outline, runs.flow_graph, orch_tasks.node_ref
	}
}
