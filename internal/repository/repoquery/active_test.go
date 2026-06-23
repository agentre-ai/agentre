package repoquery

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
)

type activeRow struct {
	ID     int64
	Key    string
	Name   string
	Status int
}

func (activeRow) TableName() string { return "active_rows" }

func TestActiveMap_EmptyKeysReturnsEmptyMapWithoutQuery(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	got, err := ActiveMap[activeRow, int64](ctx, "id", nil, func(row *activeRow) int64 {
		return row.ID
	})

	assert.NoError(t, err)
	assert.Empty(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestActiveMap_QueriesActiveRowsAndIndexesByKey(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `active_rows` WHERE id IN \\(\\?,\\?\\) AND status = \\?").
		WithArgs(int64(1), int64(2), consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status"}).
			AddRow(int64(1), "one", consts.ACTIVE).
			AddRow(int64(2), "two", consts.ACTIVE))

	got, err := ActiveMap[activeRow, int64](ctx, "id", []int64{1, 2}, func(row *activeRow) int64 {
		return row.ID
	})

	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "one", got[1].Name)
	assert.Equal(t, "two", got[2].Name)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestActiveMap_DatabaseErrorPassesThrough(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery("SELECT \\* FROM `active_rows` WHERE key IN \\(\\?\\) AND status = \\?").
		WithArgs("k1", consts.ACTIVE).
		WillReturnError(errors.New("db error"))

	got, err := ActiveMap[activeRow, string](ctx, "key", []string{"k1"}, func(row *activeRow) string {
		return row.Key
	})

	assert.EqualError(t, err, "db error")
	assert.Nil(t, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}
