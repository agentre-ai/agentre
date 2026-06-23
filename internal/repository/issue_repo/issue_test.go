package issue_repo_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

func setupIssueRepo(t *testing.T) (context.Context, sqlmock.Sqlmock, issue_repo.IssueRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, issue_repo.NewIssue()
}

func TestIssueCreate(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `issues`").WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectCommit()

	err := repo.Create(ctx, &issue_entity.Issue{
		Title: "demo", State: issue_entity.StateOpen, Status: consts.ACTIVE,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueDeleteSoft(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `issues` SET").
		WithArgs(consts.DELETE, sqlmock.AnyArg(), int64(7)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Delete(ctx, 7))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueFind(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `issues` WHERE id = \\? AND status = \\? ORDER BY `issues`.`id` LIMIT \\?").
		WithArgs(int64(7), consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "state", "status"}).
			AddRow(int64(7), "demo", issue_entity.StateOpen, consts.ACTIVE))

	got, err := repo.Find(ctx, 7)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(7), got.ID)
	assert.Equal(t, issue_entity.StateOpen, got.State)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueFindNotFound(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `issues` WHERE id = \\? AND status = \\?").
		WithArgs(int64(99), consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := repo.Find(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, got, "未找到时返回 nil,nil")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueList_StateFilter(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `issues` WHERE status = \\? AND state = \\? ORDER BY updatetime DESC, id DESC").
		WithArgs(consts.ACTIVE, issue_entity.StateOpen).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "state"}).
			AddRow(int64(7), "demo", issue_entity.StateOpen))

	rows, err := repo.List(ctx, issue_repo.ListFilter{State: issue_entity.StateOpen})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, issue_entity.StateOpen, rows[0].State)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueList_LabelFilter(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `issues` WHERE status = \\? AND id IN \\(SELECT `issue_id` FROM `issue_labels` WHERE label_id IN \\(\\?,\\?\\)\\) ORDER BY updatetime DESC, id DESC").
		WithArgs(consts.ACTIVE, int64(1), int64(2)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).
			AddRow(int64(7), "demo"))

	rows, err := repo.List(ctx, issue_repo.ListFilter{LabelIDs: []int64{1, 2}})
	require.NoError(t, err)
	assert.Len(t, rows, 1)
	assert.Equal(t, int64(7), rows[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueCountByState(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectQuery("SELECT state, count\\(\\*\\) as cnt FROM `issues` WHERE status = \\? GROUP BY `state`").
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"state", "cnt"}).
			AddRow(issue_entity.StateOpen, int64(3)).
			AddRow(issue_entity.StateClosed, int64(1)))

	open, closed, err := repo.CountByState(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(3), open)
	assert.Equal(t, int64(1), closed)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueUpdate(t *testing.T) {
	ctx, mock, repo := setupIssueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `issues` SET `agent_status`=\\?,`body`=\\?,`closed_at`=\\?,`project_id`=\\?,`state`=\\?,`title`=\\?,`updatetime`=\\? WHERE id = \\? AND status = \\?").
		WithArgs(
			issue_entity.AgentStatusIdle, // agent_status
			"body",                       // body
			int64(0),                     // closed_at
			int64(5),                     // project_id
			issue_entity.StateOpen,       // state
			"new title",                  // title
			sqlmock.AnyArg(),             // updatetime
			int64(7),                     // id
			consts.ACTIVE,                // status
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.Update(ctx, &issue_entity.Issue{
		ID:          7,
		ProjectID:   5,
		Title:       "new title",
		Body:        "body",
		State:       issue_entity.StateOpen,
		AgentStatus: issue_entity.AgentStatusIdle,
		Status:      consts.ACTIVE,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
