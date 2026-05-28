package project_repo_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/model/entity/project_entity"
	"agentre/internal/repository/project_repo"
)

func setupProjectRepo(t *testing.T) (context.Context, sqlmock.Sqlmock, project_repo.ProjectRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, project_repo.NewProject()
}

func TestProjectCreate(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `projects`").WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectCommit()

	err := repo.Create(ctx, &project_entity.Project{
		Name:      "Agentre",
		Path:      "/Users/foo/Code/agentre",
		SortOrder: 1,
		Status:    consts.ACTIVE,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectFindByName(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `projects` WHERE parent_id = \\? AND name = \\? AND status = \\? ORDER BY `projects`.`id` LIMIT \\?").
		WithArgs(int64(0), "Agentre", consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "parent_id"}).
			AddRow(int64(42), "Agentre", int64(0)))

	got, err := repo.FindByName(ctx, 0, "Agentre")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectFindByName_NotFound(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `projects` WHERE parent_id = \\? AND name = \\? AND status = \\?").
		WithArgs(int64(0), "Agentre", consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := repo.FindByName(ctx, 0, "Agentre")
	require.NoError(t, err)
	assert.Nil(t, got, "未找到时返回 nil,nil")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectList(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `projects` WHERE status = \\? ORDER BY parent_id ASC, sort_order ASC, id ASC").
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).
			AddRow(int64(1), "Agentre").
			AddRow(int64(2), "Side"))

	rows, err := repo.List(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectListByParent(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `projects` WHERE parent_id = \\? AND status = \\? ORDER BY sort_order ASC, id ASC").
		WithArgs(int64(7), consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(11)).AddRow(int64(12)))

	rows, err := repo.ListByParent(ctx, 7)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectHasActiveChildren(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT count\\(\\*\\) FROM `projects` WHERE parent_id = \\? AND status = \\?").
		WithArgs(int64(7), consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	has, err := repo.HasActiveChildren(ctx, 7)
	require.NoError(t, err)
	assert.True(t, has)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectDelete(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `projects` SET").
		WithArgs(consts.DELETE, sqlmock.AnyArg(), int64(42)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Delete(ctx, 42))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectUpdate(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `projects` SET").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Update(ctx, &project_entity.Project{
		ID:     42,
		Name:   "Agentre v2",
		Path:   "/Users/foo/Code/agentre",
		Status: consts.ACTIVE,
	}))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectNextSortOrder(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(sort_order\\), 0\\) FROM `projects` WHERE parent_id = \\? AND status = \\?").
		WithArgs(int64(7), consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"coalesce"}).AddRow(3))

	got, err := repo.NextSortOrder(ctx, 7)
	require.NoError(t, err)
	assert.Equal(t, 4, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestProjectReorderSiblings(t *testing.T) {
	ctx, mock, repo := setupProjectRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE projects SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND parent_id = \\? AND status = \\?").
		WithArgs(1, sqlmock.AnyArg(), int64(3), int64(7), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE projects SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND parent_id = \\? AND status = \\?").
		WithArgs(2, sqlmock.AnyArg(), int64(1), int64(7), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec("UPDATE projects SET sort_order = \\?, updatetime = \\? WHERE id = \\? AND parent_id = \\? AND status = \\?").
		WithArgs(3, sqlmock.AnyArg(), int64(2), int64(7), consts.ACTIVE).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.ReorderSiblings(ctx, 7, []int64{3, 1, 2})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
