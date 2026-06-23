package group_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
)

// ── GroupRepo ────────────────────────────────────────────────────────────────

func TestGroupRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .groups.`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	g := &group_entity.Group{
		Title:       "队",
		HostAgentID: 1,
		Status:      consts.ACTIVE,
	}
	err := group_repo.NewGroup().Create(ctx, g)
	require.NoError(t, err)
	assert.Equal(t, int64(1), g.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupRepo_List(t *testing.T) {
	t.Run("只返回 active 群", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery(`SELECT \* FROM .groups. WHERE status = \? ORDER BY updatetime DESC, id DESC`).
			WithArgs(consts.ACTIVE).
			WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).
				AddRow(1, "队"))

		rows, err := group_repo.NewGroup().List(ctx)
		require.NoError(t, err)
		assert.Len(t, rows, 1)
		assert.Equal(t, "队", rows[0].Title)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("无记录返回空切片", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery(`SELECT \* FROM .groups. WHERE status = \? ORDER BY updatetime DESC, id DESC`).
			WithArgs(consts.ACTIVE).
			WillReturnRows(sqlmock.NewRows([]string{"id", "title"}))

		rows, err := group_repo.NewGroup().List(ctx)
		require.NoError(t, err)
		assert.Empty(t, rows)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupRepo_Find(t *testing.T) {
	t.Run("找到返回实体", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `groups` WHERE id = \\? AND status = \\? ORDER BY `groups`.`id` LIMIT \\?").
			WithArgs(int64(1), consts.ACTIVE, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "title", "status"}).
				AddRow(1, "队", consts.ACTIVE))

		got, err := group_repo.NewGroup().Find(ctx, 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(1), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `groups` WHERE id = \\? AND status = \\? ORDER BY `groups`.`id` LIMIT \\?").
			WithArgs(int64(99), consts.ACTIVE, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "title"}))

		got, err := group_repo.NewGroup().Find(ctx, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// TestGroupRepo_Update 钉死列白名单 + status 守卫:
// Update 只写 title/run_status/round_count/status/updatetime, 且 WHERE 带
// status = ACTIVE —— 已软删的群不能被复活。任何后续把守卫去掉 / 删列的改动都会让
// 本测试失败。
func TestGroupRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	group_repo.RegisterGroup(group_repo.NewGroup())

	mock.ExpectBegin()
	// gorm 把 SET 子句的参数排在 WHERE 参数之前; Updates(map) 的 SET 列按列名字母序
	// 排列(round_count < run_status < status < title < updatetime), 用 .* 逐列断言列名
	// 出现而非死钉整段; WHERE 守卫(status = ?)则严格钉死, 防止后续改动复活软删群。
	mock.ExpectExec("(?s)UPDATE `groups` SET .*`round_count`.*`run_status`.*`status`.*`title`.*`updatetime`.* WHERE id = \\? AND status = \\?").
		WithArgs(
			0, group_entity.RunRunning, consts.ACTIVE, "x", sqlmock.AnyArg(), // SET: round_count, run_status, status, title, updatetime
			int64(5), consts.ACTIVE, // WHERE: id, status 守卫
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	g := &group_entity.Group{
		ID:         5,
		Title:      "x",
		RunStatus:  group_entity.RunRunning,
		RoundCount: 0,
		Status:     consts.ACTIVE,
	}
	err := group_repo.Group().Update(ctx, g)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// ── GroupMemberRepo ──────────────────────────────────────────────────────────

func TestGroupMemberRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .group_members.`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	m := &group_entity.GroupMember{
		GroupID: 1,
		AgentID: 2,
		Role:    group_entity.RoleMember,
		Status:  group_entity.MemberActive,
	}
	err := group_repo.NewMember().Create(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, int64(1), m.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGroupMemberRepo_Update 钉死成员列白名单:
// 只写 backing_session_id/role/status, WHERE 仅按主键 id。删任一列都会让本测试失败。
func TestGroupMemberRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	group_repo.RegisterMember(group_repo.NewMember())

	mock.ExpectBegin()
	mock.ExpectExec("(?s)UPDATE `group_members` SET .*`backing_session_id`.*`role`.*`status`.* WHERE id = \\?").
		WithArgs(
			int64(9), group_entity.RoleMember, group_entity.MemberActive, // SET: backing_session_id, role, status
			int64(3), // WHERE: id
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m := &group_entity.GroupMember{
		ID:               3,
		BackingSessionID: 9,
		Role:             group_entity.RoleMember,
		Status:           group_entity.MemberActive,
	}
	err := group_repo.Member().Update(ctx, m)
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGroupMemberRepo_SetNickname 钉死群昵称只走 nickname 单列的定向 UPDATE,
// 不碰 backing_session_id/role/status(避免 partial-member Update 误清昵称)。
func TestGroupMemberRepo_SetNickname(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	group_repo.RegisterMember(group_repo.NewMember())

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `group_members` SET `nickname`=\\? WHERE id = \\?").
		WithArgs("前端工程师", int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, group_repo.Member().SetNickname(ctx, 3, "前端工程师"))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupMemberRepo_ListByGroup(t *testing.T) {
	t.Run("只返回 active 成员", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `group_members` WHERE group_id = \\? AND status = \\? ORDER BY id ASC").
			WithArgs(int64(5), group_entity.MemberActive).
			WillReturnRows(sqlmock.NewRows([]string{"id", "agent_id"}).
				AddRow(1, 3))

		rows, err := group_repo.NewMember().ListByGroup(ctx, 5)
		require.NoError(t, err)
		assert.Len(t, rows, 1)
		assert.Equal(t, int64(3), rows[0].AgentID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupMemberRepo_FindByGroupAndAgent(t *testing.T) {
	t.Run("找到返回实体(不过滤 status)", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `group_members` WHERE group_id = \\? AND agent_id = \\? ORDER BY `group_members`.`id` LIMIT \\?").
			WithArgs(int64(1), int64(2), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "agent_id", "status"}).
				AddRow(1, 1, 2, group_entity.MemberLeft))

		got, err := group_repo.NewMember().FindByGroupAndAgent(ctx, 1, 2)
		require.NoError(t, err)
		require.NotNil(t, got)
		// FindByGroupAndAgent 不过滤 status，left 成员也能找到，供 service 层判断是否 reactivate
		assert.Equal(t, group_entity.MemberLeft, got.Status)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery("SELECT \\* FROM `group_members` WHERE group_id = \\? AND agent_id = \\? ORDER BY `group_members`.`id` LIMIT \\?").
			WithArgs(int64(1), int64(99), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		got, err := group_repo.NewMember().FindByGroupAndAgent(ctx, 1, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

// ── GroupMessageRepo ─────────────────────────────────────────────────────────

func TestGroupMessageRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .group_messages.`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	m := &group_entity.GroupMessage{
		GroupID:    5,
		Seq:        1,
		SenderKind: group_entity.SenderKindUser,
		Content:    "hello",
	}
	err := group_repo.NewMessage().Create(ctx, m)
	require.NoError(t, err)
	assert.Equal(t, int64(1), m.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupMessageRepo_ListByGroup(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery(`SELECT \* FROM .group_messages. WHERE group_id = \? ORDER BY seq ASC, id ASC`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "seq", "content"}).
			AddRow(1, 5, 1, "hello").
			AddRow(2, 5, 2, "world"))

	rows, err := group_repo.NewMessage().ListByGroup(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.Equal(t, 1, rows[0].Seq)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupMessageRepo_NextSeq(t *testing.T) {
	t.Run("有消息时返回 max+1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery(`SELECT COALESCE\(MAX\(seq\), 0\) FROM .group_messages. WHERE group_id = \?`).
			WithArgs(int64(5)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(7))

		n, err := group_repo.NewMessage().NextSeq(ctx, 5)
		require.NoError(t, err)
		assert.Equal(t, 8, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("无消息时 COALESCE 返回 0, NextSeq 返回 1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)

		mock.ExpectQuery(`SELECT COALESCE\(MAX\(seq\), 0\) FROM .group_messages. WHERE group_id = \?`).
			WithArgs(int64(9)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))

		n, err := group_repo.NewMessage().NextSeq(ctx, 9)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupRepo_SetPinned(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	group_repo.RegisterGroup(group_repo.NewGroup())

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `groups` SET `pinned`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\?").
		WithArgs(true, sqlmock.AnyArg(), int64(5), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, group_repo.Group().SetPinned(ctx, 5, true))
	assert.NoError(t, mock.ExpectationsWereMet())
}
