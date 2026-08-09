package syncqueue_repo_test

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/syncqueue_entity"
	"github.com/agentre-ai/agentre/internal/repository/syncqueue_repo"
)

func setupOutboundQueueRepo(t *testing.T) (context.Context, sqlmock.Sqlmock, syncqueue_repo.OutboundQueueRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, syncqueue_repo.NewOutboundQueue()
}

func TestOutboundQueueRepo_Create(t *testing.T) {
	ctx, mock, repo := setupOutboundQueueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `sync_outbound_queue`").WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	err := repo.Create(ctx, &syncqueue_entity.OutboundQueueItem{
		SyncAccountID: 1,
		EntityType:    "department",
		LocalID:       42,
		Op:            syncqueue_entity.OpCreate,
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboundQueueRepo_ListByAccount(t *testing.T) {
	ctx, mock, repo := setupOutboundQueueRepo(t)
	mock.ExpectQuery("SELECT \\* FROM `sync_outbound_queue` WHERE sync_account_id = \\? ORDER BY queued_at ASC, id ASC").
		WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "sync_account_id", "op"}).
			AddRow(int64(1), int64(1), syncqueue_entity.OpCreate))

	rows, err := repo.ListByAccount(ctx, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, syncqueue_entity.OpCreate, rows[0].Op)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestOutboundQueueRepo_Delete(t *testing.T) {
	ctx, mock, repo := setupOutboundQueueRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `sync_outbound_queue` WHERE id = \\?").
		WithArgs(int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	require.NoError(t, repo.Delete(ctx, 1))
	assert.NoError(t, mock.ExpectationsWereMet())
}
