package issue_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

func TestLabelList(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewLabel()
	mock.ExpectQuery("SELECT \\* FROM `labels`").
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "tone"}).
			AddRow(int64(2), "bug", "bug"))

	got, err := repo.List(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "bug", got[0].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestLabelListByIDs(t *testing.T) {
	t.Run("returns matching labels", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		repo := issue_repo.NewLabel()
		mock.ExpectQuery("SELECT \\* FROM `labels` WHERE id IN \\(\\?,\\?\\) AND status = \\?").
			WithArgs(int64(1), int64(2), consts.ACTIVE).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "tone"}).
				AddRow(int64(1), "feature", "feature").
				AddRow(int64(2), "bug", "bug"))

		got, err := repo.ListByIDs(ctx, []int64{1, 2})
		require.NoError(t, err)
		require.Len(t, got, 2)
		assert.Equal(t, "feature", got[0].Name)
		assert.Equal(t, "bug", got[1].Name)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty ids returns nil with no query", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		repo := issue_repo.NewLabel()

		got, err := repo.ListByIDs(ctx, []int64{})
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestLabelFindNotFound(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewLabel()
	mock.ExpectQuery("SELECT \\* FROM `labels` WHERE id = \\? AND status = \\? ORDER BY `labels`.`id` LIMIT \\?").
		WithArgs(int64(99), consts.ACTIVE, 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := repo.Find(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, got, "not found should return nil,nil")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueLabelSetLabels(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewIssueLabel()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `issue_labels`").
		WithArgs(int64(5)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("INSERT INTO `issue_labels`").
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	require.NoError(t, repo.SetLabels(ctx, 5, []int64{1, 2}))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueLabelSetLabels_DeduplicatesLabelIDs(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewIssueLabel()
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `issue_labels`").
		WithArgs(int64(5)).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectExec("INSERT INTO `issue_labels`").
		WithArgs(int64(5), int64(1), int64(5), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	require.NoError(t, repo.SetLabels(ctx, 5, []int64{1, 1, 2}))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueLabelListByIssue(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := issue_repo.NewIssueLabel()
	mock.ExpectQuery("SELECT `label_id` FROM `issue_labels` WHERE issue_id = \\? ORDER BY label_id ASC").
		WithArgs(int64(5)).
		WillReturnRows(sqlmock.NewRows([]string{"label_id"}).
			AddRow(int64(1)).
			AddRow(int64(3)))

	ids, err := repo.ListByIssue(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, []int64{1, 3}, ids)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestIssueLabelListByIssues(t *testing.T) {
	t.Run("groups rows into map", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		repo := issue_repo.NewIssueLabel()
		mock.ExpectQuery("SELECT \\* FROM `issue_labels` WHERE issue_id IN \\(\\?,\\?\\) ORDER BY issue_id ASC, label_id ASC").
			WithArgs(int64(5), int64(6)).
			WillReturnRows(sqlmock.NewRows([]string{"issue_id", "label_id"}).
				AddRow(int64(5), int64(1)).
				AddRow(int64(5), int64(2)).
				AddRow(int64(6), int64(1)))

		got, err := repo.ListByIssues(ctx, []int64{5, 6})
		require.NoError(t, err)
		assert.Equal(t, map[int64][]int64{5: {1, 2}, 6: {1}}, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("empty input returns empty map with no query", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		repo := issue_repo.NewIssueLabel()

		got, err := repo.ListByIssues(ctx, []int64{})
		require.NoError(t, err)
		assert.Equal(t, map[int64][]int64{}, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
