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

func TestRunRepo_Create_FillsTimestamps(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .orchestration_runs.`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	r := &orch_entity.OrchestrationRun{Goal: "做个登录页", LeaderAgentID: 2, Status: orch_entity.RunPending}
	require.NoError(t, orch_repo.NewRun().Create(ctx, r))
	assert.NotZero(t, r.Createtime)
	assert.NotZero(t, r.Updatetime)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunRepo_Find(t *testing.T) {
	t.Run("找到返回实体", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `orchestration_runs` WHERE id = \\? ORDER BY `orchestration_runs`.`id` LIMIT \\?").
			WithArgs(int64(7), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "goal", "status"}).AddRow(7, "g", "running"))

		got, err := orch_repo.NewRun().Find(ctx, 7)
		require.NoError(t, err)
		assert.Equal(t, int64(7), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil,nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `orchestration_runs` WHERE id = \\? ORDER BY `orchestration_runs`.`id` LIMIT \\?").
			WithArgs(int64(99), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))

		got, err := orch_repo.NewRun().Find(ctx, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestRunRepo_List(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .orchestration_runs. ORDER BY updatetime DESC`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "goal"}).AddRow(1, "a").AddRow(2, "b"))

	rows, err := orch_repo.NewRun().List(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.Equal(t, int64(1), rows[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRunRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE .orchestration_runs.`).WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	m := &orch_entity.OrchestrationRun{ID: 3, Status: orch_entity.RunRunning}
	require.NoError(t, orch_repo.NewRun().Update(ctx, m))
	assert.NotZero(t, m.Updatetime)
	assert.NoError(t, mock.ExpectationsWereMet())
}
