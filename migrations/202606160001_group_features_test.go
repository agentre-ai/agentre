package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMigration202606160001GroupFeatures(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	// 前置:agents + CEO seed(004,依赖 002 departments / 003 backends)、chat_sessions(006)、群聊基线(030001)。
	require.NoError(t, migration202605220002().Migrate(gdb))
	require.NoError(t, migration202605220003().Migrate(gdb))
	require.NoError(t, migration202605220004().Migrate(gdb))
	require.NoError(t, migration202605220006().Migrate(gdb))
	require.NoError(t, migration202606030001().Migrate(gdb))

	require.NoError(t, migration202606160001().Migrate(gdb))

	// agents.tools_json:CEO(DEFAULT) 默认 org + group_create;非 CEO 默认 '[]'。
	var ceoTools string
	require.NoError(t, gdb.Table("agents").
		Where("system_badge = ?", "DEFAULT").
		Pluck("tools_json", &ceoTools).Error)
	require.JSONEq(t, `[{"key":"org","enabled":true},{"key":"group_create","enabled":true}]`, ceoTools)

	require.NoError(t, gdb.Exec(`INSERT INTO agents (name, department_id, parent_agent_id, agent_backend_id, status) VALUES ('t', 0, 0, 0, 1)`).Error)
	var plain string
	require.NoError(t, gdb.Table("agents").Where("name = 't'").Pluck("tools_json", &plain).Error)
	require.Equal(t, `[]`, plain)

	// group_tasks 可写入且 (group_id, task_no) 唯一。
	require.NoError(t, gdb.Exec(`INSERT INTO group_tasks
		(group_id, task_no, title, brief, creator_member_id, assignee_member_id, status)
		VALUES (1, 1, '重构设置页', '按新稿', 100, 101, 'open')`).Error)
	require.Error(t, gdb.Exec(`INSERT INTO group_tasks
		(group_id, task_no, title, creator_member_id, assignee_member_id, status)
		VALUES (1, 1, '重复编号', 100, 101, 'open')`).Error, "同群同 task_no 必须被唯一索引拒绝")

	// group_messages.task_id/task_event 默认值。
	require.NoError(t, gdb.Exec(`INSERT INTO group_messages (group_id, seq, content) VALUES (1, 1, 'x')`).Error)
	var msg struct {
		TaskID    int64
		TaskEvent string
	}
	require.NoError(t, gdb.Table("group_messages").Select("task_id, task_event").
		Where("group_id = 1").Scan(&msg).Error)
	require.Equal(t, int64(0), msg.TaskID)
	require.Equal(t, "", msg.TaskEvent)

	// workflows 表 + groups.workflow_id。
	require.NoError(t, gdb.Exec(`INSERT INTO workflows (name, content, status) VALUES ('产品开发流程', '# 流程', 1)`).Error)
	require.NoError(t, gdb.Exec(`INSERT INTO groups (title, host_agent_id, workflow_id) VALUES ('g', 1, 1)`).Error)
	var wf int64
	require.NoError(t, gdb.Table("groups").Select("workflow_id").Where("title = 'g'").Scan(&wf).Error)
	require.Equal(t, int64(1), wf)

	// group_members.nickname 默认空 + 可写入。
	require.NoError(t, gdb.Exec(`INSERT INTO group_members (group_id, agent_id, backing_session_id) VALUES (1, 2, 3)`).Error)
	var nick string
	require.NoError(t, gdb.Table("group_members").Select("nickname").Where("group_id = 1").Scan(&nick).Error)
	require.Equal(t, "", nick)
	require.NoError(t, gdb.Exec(`UPDATE group_members SET nickname = '前端工程师' WHERE group_id = 1`).Error)
	require.NoError(t, gdb.Table("group_members").Select("nickname").Where("group_id = 1").Scan(&nick).Error)
	require.Equal(t, "前端工程师", nick)

	// Rollback 干净:新增表 / 列均消失。
	require.NoError(t, migration202606160001().Rollback(gdb))
	require.Error(t, gdb.Exec(`SELECT 1 FROM group_tasks`).Error)
	require.Error(t, gdb.Exec(`SELECT 1 FROM workflows`).Error)
	require.Error(t, gdb.Exec(`SELECT nickname FROM group_members`).Error)
	require.Error(t, gdb.Exec(`SELECT tools_json FROM agents`).Error)
}
