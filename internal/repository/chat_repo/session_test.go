package chat_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/repository/chat_repo"
)

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

	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
		WithArgs(int64(7), consts.ACTIVE, 5).
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
	mock.ExpectQuery("SELECT agent_id, COUNT\\(\\*\\) AS n FROM `chat_sessions` WHERE agent_id IN \\(\\?,\\?\\) AND agent_status = \\? AND status = \\? GROUP BY `agent_id`").
		WithArgs(int64(1), int64(2), "running", consts.ACTIVE).
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

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? AND agent_status IN \\(\\?,\\?,\\?\\) ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(7), consts.ACTIVE, "running", "waiting", "error", 20).
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

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? AND agent_status IN \\(\\?,\\?,\\?\\) ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(7), consts.ACTIVE, "running", "waiting", "error", 20).
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

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? ORDER BY last_message_at DESC, id DESC LIMIT \\? OFFSET \\?").
			WithArgs(int64(7), consts.ACTIVE, 20, 20).
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

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?$").
			WithArgs(int64(7), consts.ACTIVE, 20).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id", "title", "agent_status", "last_message_at", "status"}).
				AddRow(1, 7, "only", "idle", 1700000010000, consts.ACTIVE))

		got, err := chat_repo.NewSession().ListByAgentPaged(ctx, 7, 0, 20)
		assert.NoError(t, err)
		assert.Len(t, got, 1)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("agent 无任何会话返回空切片", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE agent_id = \\? AND status = \\? ORDER BY last_message_at DESC, id DESC LIMIT \\?").
			WithArgs(int64(99), consts.ACTIVE, 20).
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

		mock.ExpectQuery("SELECT agent_id, id FROM `chat_sessions` WHERE agent_id IN \\(\\?,\\?\\) AND status = \\? ORDER BY agent_id ASC, last_message_at DESC, id DESC").
			WithArgs(int64(7), int64(8), consts.ACTIVE).
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

func TestSessionRepo_CountByAgents(t *testing.T) {
	t.Run("批量返回每个 agent 的会话数；缺席 agent 在 map 里读出 0", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT agent_id, COUNT\\(\\*\\) AS n FROM `chat_sessions` WHERE agent_id IN \\(\\?,\\?,\\?\\) AND status = \\? GROUP BY `agent_id`").
			WithArgs(int64(1), int64(2), int64(3), consts.ACTIVE).
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

func TestSessionRepo_CountByAgent(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `chat_sessions` WHERE agent_id = \\? AND status = \\?").
		WithArgs(int64(7), consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(42))

	got, err := chat_repo.NewSession().CountByAgent(ctx, 7)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestSessionRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `chat_sessions`").
		WithArgs(
			int64(7), "draft", "idle", int64(0), int64(0), "", // agent_id, title, agent_status, last_message_at, last_read_at, provider_session_id
			int64(0),  // project_id
			0, "", "", // context_window, permission_mode, permission_mode_at_launch
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
	mock.ExpectQuery("SELECT \\* FROM `chat_sessions` WHERE project_id = \\? AND status = \\? ORDER BY last_message_at DESC, id DESC").
		WithArgs(int64(7), consts.ACTIVE).
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
		WithArgs(int64(7), consts.ACTIVE, "running", "waiting").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))

	n, err := chat_repo.NewSession().CountActiveByProject(ctx, 7, []string{"running", "waiting"})
	assert.NoError(t, err)
	assert.Equal(t, int64(3), n)
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
func TestSessionRepo_ResetActiveSessions(t *testing.T) {
	t.Run("有残留时把 running / waiting 翻成 error 并返回受影响行数", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `chat_sessions` SET `agent_status`=\\?,`updatetime`=\\? WHERE agent_status IN \\(\\?,\\?\\) AND status = \\?").
			WithArgs("error", sqlmock.AnyArg(), "running", "waiting", consts.ACTIVE).
			WillReturnResult(sqlmock.NewResult(0, 3))
		mock.ExpectCommit()

		n, err := chat_repo.NewSession().ResetActiveSessions(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(3), n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("没残留时也走 SQL,返回 0 行不报错", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `chat_sessions` SET `agent_status`=\\?,`updatetime`=\\? WHERE agent_status IN \\(\\?,\\?\\) AND status = \\?").
			WithArgs("error", sqlmock.AnyArg(), "running", "waiting", consts.ACTIVE).
			WillReturnResult(sqlmock.NewResult(0, 0))
		mock.ExpectCommit()

		n, err := chat_repo.NewSession().ResetActiveSessions(ctx)
		assert.NoError(t, err)
		assert.Equal(t, int64(0), n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
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
