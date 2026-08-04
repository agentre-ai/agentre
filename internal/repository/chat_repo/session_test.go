package chat_repo_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/chat_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
)

func assertResetActiveSessions(t *testing.T, ctx context.Context, mock sqlmock.Sqlmock, affectedRows int64) {
	t.Helper()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_sessions` SET `agent_status`=\\?,`updatetime`=\\? WHERE agent_status IN \\(\\?,\\?\\) AND status = \\? "+
		"AND \\(exec_device_id <= \\? OR exec_daemon_fingerprint = \\?\\)").
		WithArgs("error", sqlmock.AnyArg(), "running", "waiting", consts.ACTIVE, int64(0), "").
		WillReturnResult(sqlmock.NewResult(0, affectedRows))
	mock.ExpectCommit()

	n, err := chat_repo.NewSession().ResetActiveSessions(ctx)
	assert.NoError(t, err)
	assert.Equal(t, affectedRows, n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_Find(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE id = \\? AND status = \\? ORDER BY `chat_sessions`.`id` LIMIT \\?").
		WithArgs(int64(1), consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
			AddRow(1, 7, "hi", "waiting", 1700000000000, consts.ACTIVE))

	got, err := chat_repo.NewSession().Find(ctx, 1)
	assert.NoError(t, err)
	assert.Equal(t, int64(7), got.AgentID)
	assert.True(t, got.NeedsAttention, "needsAttention is derived from agent_status=waiting, not stored as a DB column")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_ListByAgent(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
			AddRow(2, 7, "later", "idle", 1700000020000, consts.ACTIVE).
			AddRow(1, 7, "earlier", "idle", 1700000010000, consts.ACTIVE))

	got, err := chat_repo.NewSession().ListByAgent(ctx, 7, 5)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, int64(2), got[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_CountRunningByAgents(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	// 只计入 agent_status='running' 且未软删除的会话 —— 历史 idle 会话不应让前端亮"运行中"呼吸灯。
	// 子 agent 委派会话被 purpose 过滤排除(亮灯却点不进去会留死角)。
	mock.ExpectQuery("SELECT agent_id, COUNT\\(\\*\\) AS n FROM `chat_sessions` WHERE .agent_id IN \\(\\?,\\?\\) AND agent_status = \\? AND status = \\?. AND purpose <> \\? GROUP BY `agent_id`").
		WithArgs(int64(1), int64(2), "running", consts.ACTIVE, chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "n"}).
			AddRow(1, 2))

	got, err := chat_repo.NewSession().CountRunningByAgents(ctx, []int64{1, 2})
	assert.NoError(t, err)
	assert.Equal(t, 2, got[1])
	assert.Equal(t, 0, got[2], "agent 2 只有 idle 会话，GROUP BY 不返回行，map 缺省读出 0")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_ListAttentionByAgent(t *testing.T) {
	t.Run("running / waiting / error 三种各 1 行", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\? AND agent_status IN \\(\\?,\\?,\\?\\). AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(7), consts.ACTIVE, "running", "waiting", "error", chat_entity.SessionPurposeSubagent, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
				AddRow(3, 7, "approve me", "waiting", 1700000030000, consts.ACTIVE).
				AddRow(2, 7, "boom", "error", 1700000020000, consts.ACTIVE).
				AddRow(1, 7, "live", "running", 1700000010000, consts.ACTIVE))

		got, err := chat_repo.NewSession().ListAttentionByAgent(ctx, 7, 20)
		assert.NoError(t, err)
		assert.Len(t, got, 3)
		assert.Equal(t, int64(3), got[0].ID)
		assert.True(t, got[0].NeedsAttention)
		assert.Equal(t, "error", got[1].AgentStatus)
		assert.Equal(t, "running", got[2].AgentStatus)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("全部 idle → 返回空", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\? AND agent_status IN \\(\\?,\\?,\\?\\). AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(7), consts.ACTIVE, "running", "waiting", "error", chat_entity.SessionPurposeSubagent, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		got, err := chat_repo.NewSession().ListAttentionByAgent(ctx, 7, 20)
		assert.NoError(t, err)
		assert.Empty(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSessionRepo_ListByAgentPaged(t *testing.T) {
	t.Run("正常分页 offset>0", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\? OFFSET \\?").
			WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 20, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
				AddRow(22, 7, "session-22", "idle", 1700000220000, consts.ACTIVE).
				AddRow(21, 7, "session-21", "idle", 1700000210000, consts.ACTIVE))

		got, err := chat_repo.NewSession().ListByAgentPaged(ctx, 7, 20, 20)
		assert.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Equal(t, int64(22), got[0].ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("首页 offset=0 不带 OFFSET 子句", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?$").
			WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
				AddRow(1, 7, "only", "idle", 1700000010000, consts.ACTIVE))

		got, err := chat_repo.NewSession().ListByAgentPaged(ctx, 7, 0, 20)
		assert.NoError(t, err)
		assert.Len(t, got, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("agent 无任何会话返回空切片", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(99), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		got, err := chat_repo.NewSession().ListByAgentPaged(ctx, 99, 0, 20)
		assert.NoError(t, err)
		assert.Empty(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSessionRepo_ListIDsByAgents(t *testing.T) {
	t.Run("Given multiple agents and active sessions, When listing ids, Then groups active ids by agent in sidebar order", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT agent_id, id FROM `chat_sessions` WHERE .agent_id IN \\(\\?,\\?\\) AND status = \\?. AND purpose <> \\? ORDER BY agent_id ASC, last_message_at DESC, id DESC").
			WithArgs(int64(7), int64(8), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
			WillReturnRows(sqlmock.NewRows([]string{"agent_id", "id"}).
				AddRow(7, 12).
				AddRow(7, 11).
				AddRow(8, 21))

		got, err := chat_repo.NewSession().ListIDsByAgents(ctx, []int64{7, 8})
		assert.NoError(t, err)
		assert.Equal(t, []int64{12, 11}, got[7])
		assert.Equal(t, []int64{21}, got[8])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("Given no agent ids, When listing ids, Then it returns empty map without SQL", func(t *testing.T) {
		ctx, _, _ := testutils.Database(t)

		got, err := chat_repo.NewSession().ListIDsByAgents(ctx, nil)
		assert.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestSessionRepo_ListIDsByAgentsIncludingGroups(t *testing.T) {
	t.Run("Given IncludingGroups variant, When listing ids for sidebar, Then it produces the same query as the plain variant", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT agent_id, id FROM `chat_sessions` WHERE .agent_id IN \\(\\?,\\?\\) AND status = \\?. AND purpose <> \\? ORDER BY agent_id ASC, last_message_at DESC, id DESC").
			WithArgs(int64(7), int64(8), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
			WillReturnRows(sqlmock.NewRows([]string{"agent_id", "id"}).
				AddRow(7, 12).
				AddRow(7, 11).
				AddRow(8, 21))

		got, err := chat_repo.NewSession().ListIDsByAgentsIncludingGroups(ctx, []int64{7, 8})
		assert.NoError(t, err)
		assert.Equal(t, []int64{12, 11}, got[7])
		assert.Equal(t, []int64{21}, got[8])
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSessionRepo_CountByAgents(t *testing.T) {
	t.Run("批量返回每个 agent 的会话数；缺席 agent 在 map 里读出 0", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT agent_id, COUNT\\(\\*\\) AS n FROM `chat_sessions` WHERE .agent_id IN \\(\\?,\\?,\\?\\) AND status = \\?. AND purpose <> \\? GROUP BY `agent_id`").
			WithArgs(int64(1), int64(2), int64(3), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
			WillReturnRows(sqlmock.NewRows([]string{"agent_id", "n"}).
				AddRow(1, 12).
				AddRow(2, 3))

		got, err := chat_repo.NewSession().CountByAgents(ctx, []int64{1, 2, 3})
		assert.NoError(t, err)
		assert.Equal(t, int64(12), got[1])
		assert.Equal(t, int64(3), got[2])
		assert.Equal(t, int64(0), got[3], "agent 3 无会话，GROUP BY 不返回行，map 缺省读 0")
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("空 agentIDs 不发 SQL，直接返回空 map", func(t *testing.T) {
		ctx, _, _ := testutils.Database(t)
		got, err := chat_repo.NewSession().CountByAgents(ctx, nil)
		assert.NoError(t, err)
		assert.Empty(t, got)
	})
}

func TestSessionRepo_CountByAgentsIncludingGroups(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT agent_id, COUNT\\(\\*\\) AS n FROM `chat_sessions` WHERE .agent_id IN \\(\\?,\\?\\) AND status = \\?. AND purpose <> \\? GROUP BY `agent_id`").
		WithArgs(int64(1), int64(2), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"agent_id", "n"}).
			AddRow(1, 2).
			AddRow(2, 1))

	got, err := chat_repo.NewSession().CountByAgentsIncludingGroups(ctx, []int64{1, 2})
	assert.NoError(t, err)
	assert.Equal(t, int64(2), got[1])
	assert.Equal(t, int64(1), got[2])
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_CountByAgent(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\?").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	got, err := chat_repo.NewSession().CountByAgent(ctx, 7)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_CountByAgentIncludingGroups(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\?").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(43))

	got, err := chat_repo.NewSession().CountByAgentIncludingGroups(ctx, 7)
	assert.NoError(t, err)
	assert.Equal(t, int64(43), got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `chat_sessions`").
		WithArgs(
			int64(7), "draft", "idle", int64(0), int64(0), "", // agent_id, title, agent_status, last_message_at, last_read_at, provider_session_id
			int64(0),  // project_id
			"",        // purpose
			0, "", "", // context_window, permission_mode, permission_mode_at_launch
			int64(0), "", int64(0), // exec_device_id, exec_daemon_fingerprint, event_cursor —— 新建会话默认本机执行、无游标
			consts.ACTIVE, sqlmock.AnyArg(), sqlmock.AnyArg(), // status, createtime, updatetime
		).
		WillReturnResult(sqlmock.NewResult(99, 1))
	mock.ExpectCommit()

	s := &chat_entity.Session{AgentID: 7, Title: "draft", AgentStatus: "idle", Status: consts.ACTIVE}
	err := chat_repo.NewSession().Create(ctx, s)
	assert.NoError(t, err)
	assert.Equal(t, int64(99), s.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_ListByProject(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .project_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "project_id"}).
			AddRow(int64(101), int64(42), int64(7)).
			AddRow(int64(102), int64(43), int64(7)))

	rows, err := chat_repo.NewSession().ListByProject(ctx, 7)
	assert.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_CountActiveByProject(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `chat_sessions`").
		WithArgs(int64(7), consts.ACTIVE, "running", "waiting", chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	n, err := chat_repo.NewSession().CountActiveByProject(ctx, 7, []string{"running", "waiting"})
	assert.NoError(t, err)
	assert.Equal(t, int64(3), n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_CountActive(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `chat_sessions`").
		WithArgs(consts.ACTIVE, "running", "waiting", chat_entity.SessionPurposeSubagent).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(4))

	n, err := chat_repo.NewSession().CountActive(ctx, []string{"running", "waiting"})
	assert.NoError(t, err)
	assert.Equal(t, int64(4), n)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_MarkRead(t *testing.T) {
	t.Run("ts > current last_read_at 时正常 UPDATE 1 行", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `chat_sessions` SET `last_read_at`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\? AND last_read_at < \\?").
			WithArgs(int64(5000), sqlmock.AnyArg(), int64(7), consts.ACTIVE, int64(5000)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := chat_repo.NewSession().MarkRead(ctx, 7, 5000)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("ts <= current 时 WHERE 不命中，0 行更新仍算成功", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `chat_sessions` SET `last_read_at`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\? AND last_read_at < \\?").
			WithArgs(int64(1000), sqlmock.AnyArg(), int64(7), consts.ACTIVE, int64(1000)).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		err := chat_repo.NewSession().MarkRead(ctx, 7, 1000)
		assert.NoError(t, err, "未匹配到行不应当报错 —— MarkRead 语义是「尝试推进」")
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestSessionRepo_SoftDelete(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_sessions` SET `status`=\\?,`updatetime`=\\? WHERE id = \\?").
		WithArgs(consts.DELETE, sqlmock.AnyArg(), int64(5)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := chat_repo.NewSession().SoftDelete(ctx, 5)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSessionRepo_ResetActiveSessions 钉死启动期残留清理 SQL:任何 agent_status
// 是 running / waiting 的未软删 session 都翻成 error。
// 主 Wails 实例 Startup 后调一次,防止 app crash / restart 留下永远卡 RUNNING 的会话。
//
// **跑在远端 daemon 上的会话被排除在外**:那一轮的执行者是另一台机器上的进程,它不随
// 桌面 App 退出而消亡(R4:断连不终止会话)。把它一并翻成 error 是在报一个假失败 ——
// 会话此刻很可能正在远端跑着,而且如果它在桌面端离线期间什么新内容都没产出,补齐重放
// 不会写任何状态,这条假失败就永久留在界面上。它们的去向改由补齐按 daemon 交回的
// 生命周期逐条判定(见 chat_svc.CatchUpRemoteSessions / ResetActiveSessionsByIDs)。
func TestSessionRepo_ResetActiveSessions(t *testing.T) {
	t.Run("有残留时把 running / waiting 翻成 error 并返回受影响行数", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		assertResetActiveSessions(t, ctx, mock, 3)
	})

	t.Run("没残留时也走 SQL,返回 0 行不报错", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		assertResetActiveSessions(t, ctx, mock, 0)
	})
}

// TestSessionRepo_ResetActiveSessionsByIDs 钉死「补齐判完之后收尾这几条」的 SQL:
// 只动点名的那些会话,且只动其中还停在 running / waiting 的行。
//
// 它是启动期清理在远端那一半的落点:blanket 的 ResetActiveSessions 不再碰远端会话,
// 因为在连上 daemon 之前根本无从知道它是不是还在跑;连上之后 daemon 说它不在跑了,
// 才由这一条按 id 收尾。空 id 列表不发 SQL —— 那会退化成 WHERE id IN () 的全表扫描。
func TestSessionRepo_ResetActiveSessionsByIDs(t *testing.T) {
	t.Run("点名的会话里还在 running / waiting 的翻成 error", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `chat_sessions` SET `agent_status`=\\?,`updatetime`=\\? "+
			"WHERE agent_status IN \\(\\?,\\?\\) AND status = \\? AND id IN \\(\\?,\\?\\)").
			WithArgs("error", sqlmock.AnyArg(), "running", "waiting", consts.ACTIVE, int64(100), int64(101)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		n, err := chat_repo.NewSession().ResetActiveSessionsByIDs(ctx, []int64{100, 101})
		assert.NoError(t, err)
		assert.Equal(t, int64(1), n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("没有点名任何会话时一条 SQL 都不发", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		n, err := chat_repo.NewSession().ResetActiveSessionsByIDs(ctx, nil)
		assert.NoError(t, err)
		assert.Zero(t, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ListByAgentIncludingGroups 与 ListByAgent 目前产出相同的 SQL(仅无条件的 purpose
// 过滤排除子 agent 会话);两个变体的公开方法名保留供调用方按语义选择。
func TestSessionRepo_ListByAgentIncludingGroups(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title"}).
			AddRow(12, 7, "支付小队 / 后端"))

	got, err := chat_repo.NewSession().ListByAgentIncludingGroups(ctx, 7, 5)
	assert.NoError(t, err)
	if assert.Len(t, got, 1) {
		assert.Equal(t, int64(12), got[0].ID)
	}
	assert.NoError(t, mock.ExpectationsWereMet())
}

// 子 agent 委派会话(purpose='subagent_call')必须从 IncludingGroups 变体的侧栏查询里
// 也被排除 —— 无条件的 purpose 过滤对两个变体都生效。
func TestSessionRepo_ListByAgentIncludingGroups_FiltersSubagentSessions(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE .agent_id = \\? AND status = \\?. AND purpose <> \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
		WithArgs(int64(7), consts.ACTIVE, chat_entity.SessionPurposeSubagent, 5).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id"}).AddRow(12, 7))

	got, err := chat_repo.NewSession().ListByAgentIncludingGroups(ctx, 7, 5)
	assert.NoError(t, err)
	assert.Len(t, got, 1)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSessionRepo_UpdatePermissionModeAtLaunch 验证 spawn 时 runner 调用的
// 单字段更新 SQL —— 不能把 permission_mode 一起冲掉。
func TestSessionRepo_UpdatePermissionModeAtLaunch(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := chat_repo.NewSession()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_sessions`").
		WithArgs("bypassPermissions", sqlmock.AnyArg(), int64(42), 1).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.UpdatePermissionModeAtLaunch(ctx, 42, "bypassPermissions"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSessionRepo_UpdateExecDaemon 钉死「这条会话跑在哪台 daemon 上」的写入 SQL。
// 关键不变式:(实例标识, 游标) 必须始终是同一条通知日志上的一对 —— 改绑到另一台
// daemon 时,老游标指的是老 daemon 日志里的位置,必须在同一条语句里归零;换成两次
// 写(先改绑再清游标)会留下一个「游标看起来对新 daemon 有效」的崩溃窗口。
func TestSessionRepo_UpdateExecDaemon(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := chat_repo.NewSession()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_sessions` SET `event_cursor`=CASE WHEN exec_daemon_fingerprint = \\? THEN event_cursor ELSE 0 END,`exec_daemon_fingerprint`=\\?,`exec_device_id`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\?").
		WithArgs("sha256:beef", "sha256:beef", int64(3), sqlmock.AnyArg(), int64(42), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.UpdateExecDaemon(ctx, 42, 3, "sha256:beef"))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSessionRepo_UpdateEventCursor 钉死游标推进的写入 SQL:只动 event_cursor,
// 不能把执行位置或实例标识一起改掉(否则游标与 daemon 的绑定关系就断了);
// 且 WHERE 必须带上实例标识 —— 会话已改绑到别的 daemon 后,老连接上迟到的一条
// 通知不得把老日志的 seq 写到新 daemon 的记录上。
func TestSessionRepo_UpdateEventCursor(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := chat_repo.NewSession()

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_sessions` SET `event_cursor`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\? AND exec_daemon_fingerprint = \\?").
		WithArgs(int64(17), sqlmock.AnyArg(), int64(42), consts.ACTIVE, "sha256:beef").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.UpdateEventCursor(ctx, 42, "sha256:beef", 17))
	require.NoError(t, mock.ExpectationsWereMet())
}

// TestSessionRepo_ListRemoteExecSessions 钉死「App 启动后该连谁」那一问的读取 SQL。
//
// exec_device_id 在此之前是只写列:写进去、再没人读。桌面端因此在重启后不知道哪些
// 会话跑在哪台 daemon 上,补齐三步无从发起 —— 用户故事「退出 App 后下次打开看到这
// 段时间发生的全部内容」直接不成立。
//
// 过滤条件的两半都是硬的:exec_device_id > 0 排除本机会话(它们的真相源是本地库,
// 没有可补齐的远端日志),exec_daemon_fingerprint <> ” 排除没有实例标识的行 ——
// 游标只在它所属的那条通知日志里有意义,标识为空时 LoadCursor 一律判失效,拿它去
// attach 只会白发一轮 RPC。
func TestSessionRepo_ListRemoteExecSessions(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE exec_device_id > \\? AND exec_daemon_fingerprint <> \\? AND status = \\? ORDER BY id").
		WithArgs(int64(0), "", consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "agent_status", "status", "exec_device_id", "exec_daemon_fingerprint", "event_cursor"}).
			AddRow(1, 7, "running", consts.ACTIVE, 3, "sha256:beef", 17).
			AddRow(2, 7, "idle", consts.ACTIVE, 4, "sha256:cafe", 0))

	got, err := chat_repo.NewSession().ListRemoteExecSessions(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(3), got[0].ExecDeviceID)
	assert.Equal(t, "sha256:beef", got[0].ExecDaemonFingerprint)
	assert.Equal(t, int64(17), got[0].EventCursor)
	assert.Equal(t, int64(4), got[1].ExecDeviceID)
	assert.NoError(t, mock.ExpectationsWereMet())
}
