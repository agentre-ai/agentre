package orch_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
)

func TestTaskRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `orch_tasks`").WillReturnResult(sqlmock.NewResult(9, 1))
	mock.ExpectCommit()

	m := &orch_entity.Task{RunID: 10, Seq: 1, Text: "写测试"}
	require.NoError(t, orch_repo.NewTask().Create(ctx, m))
	assert.Equal(t, int64(9), m.ID)
	assert.NotZero(t, m.Createtime)
	assert.NotZero(t, m.Updatetime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `orch_tasks`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	m := &orch_entity.Task{ID: 7, Status: orch_entity.TaskStatusDone}
	require.NoError(t, orch_repo.NewTask().Update(ctx, m))
	assert.NotZero(t, m.Updatetime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskRepo_Find(t *testing.T) {
	t.Run("找到返回实体", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `orch_tasks` WHERE id = \\? ORDER BY `orch_tasks`.`id` LIMIT \\?").
			WithArgs(int64(5), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "status"}).AddRow(5, "pending"))

		got, err := orch_repo.NewTask().Find(ctx, 5)
		require.NoError(t, err)
		assert.Equal(t, int64(5), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil,nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `orch_tasks` WHERE id = \\? ORDER BY `orch_tasks`.`id` LIMIT \\?").
			WithArgs(int64(99), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		got, err := orch_repo.NewTask().Find(ctx, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestTaskRepo_ListByRun(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `orch_tasks` WHERE run_id = \\? ORDER BY seq ASC").
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "run_id", "seq"}).
			AddRow(1, 10, 1).
			AddRow(2, 10, 2))

	rows, err := orch_repo.NewTask().ListByRun(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.Equal(t, int64(1), rows[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestTaskRepo_MaxSeq(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\),0\\) AS m FROM `orch_tasks` WHERE run_id = \\?").
		WithArgs(int64(10)).
		WillReturnRows(sqlmock.NewRows([]string{"m"}).AddRow(3))

	got, err := orch_repo.NewTask().MaxSeq(ctx, 10)
	require.NoError(t, err)
	assert.Equal(t, 3, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}
