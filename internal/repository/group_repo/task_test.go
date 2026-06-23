package group_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
)

func TestGroupTaskRepo_NextTaskNo(t *testing.T) {
	t.Run("有任务时返回 max+1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT COALESCE\(MAX\(task_no\), 0\) FROM .group_tasks. WHERE group_id = \?`).
			WithArgs(int64(5)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(3))
		n, err := group_repo.NewTask().NextTaskNo(ctx, 5)
		require.NoError(t, err)
		assert.Equal(t, 4, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
	t.Run("无任务返回 1", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery(`SELECT COALESCE\(MAX\(task_no\), 0\) FROM .group_tasks. WHERE group_id = \?`).
			WithArgs(int64(9)).
			WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(0))
		n, err := group_repo.NewTask().NextTaskNo(ctx, 9)
		require.NoError(t, err)
		assert.Equal(t, 1, n)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupTaskRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .group_tasks.`).
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()

	task := &group_entity.GroupTask{GroupID: 5, TaskNo: 1, Title: "t",
		CreatorMemberID: 1, AssigneeMemberID: 2, Status: group_entity.TaskStatusOpen}
	require.NoError(t, group_repo.NewTask().Create(ctx, task))
	assert.NotZero(t, task.Createtime, "Create 必须补 createtime/updatetime")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestGroupTaskRepo_Update 钉死 Save 全列更新 + WHERE 主键:
// Save 按 PK 全列覆盖写(svc 层在 per-group ingestMu 下串行修改,无并发覆盖风险),
// WHERE 必须只按主键 id 钉死——任何把 WHERE 改宽/改掉的改动都会让本测试失败。
func TestGroupTaskRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec("(?s)UPDATE `group_tasks` SET .* WHERE `id` = \\?").
		WithArgs(
			// SET: 按实体字段序全列(group_id, task_no, title, brief, creator_member_id,
			// assignee_member_id, status, result, parent_task_no, createtime, updatetime)
			int64(5), 1, "updated", "", int64(1), int64(2),
			group_entity.TaskStatusDone, "", 0, int64(0), sqlmock.AnyArg(),
			int64(7), // WHERE: id 主键
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	task := &group_entity.GroupTask{
		ID:               7,
		GroupID:          5,
		TaskNo:           1,
		Title:            "updated",
		CreatorMemberID:  1,
		AssigneeMemberID: 2,
		Status:           group_entity.TaskStatusDone,
	}
	require.NoError(t, group_repo.NewTask().Update(ctx, task))
	assert.NotZero(t, task.Updatetime, "Update 必须刷新 updatetime")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestGroupTaskRepo_FindByGroupAndNo(t *testing.T) {
	t.Run("找到返回实体", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `group_tasks` WHERE group_id = \\? AND task_no = \\? ORDER BY `group_tasks`.`id` LIMIT \\?").
			WithArgs(int64(5), 3, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "group_id", "task_no", "status"}).
				AddRow(7, 5, 3, "open"))
		got, err := group_repo.NewTask().FindByGroupAndNo(ctx, 5, 3)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(7), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `group_tasks` WHERE group_id = \\? AND task_no = \\? ORDER BY `group_tasks`.`id` LIMIT \\?").
			WithArgs(int64(5), 99, 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		got, err := group_repo.NewTask().FindByGroupAndNo(ctx, 5, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestGroupTaskRepo_ListByGroup(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .group_tasks. WHERE group_id = \? ORDER BY task_no ASC`).
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_no"}).AddRow(1, 1).AddRow(2, 2))
	rows, err := group_repo.NewTask().ListByGroup(ctx, 5)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}
