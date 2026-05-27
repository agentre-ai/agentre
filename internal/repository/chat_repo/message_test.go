package chat_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"

	"agentre/internal/model/entity/chat_entity"
	"agentre/internal/repository/chat_repo"
)

func TestMessageRepo_List(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE session_id = \\? ORDER BY seq ASC").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(1, 3, "user", `[]`, 1).
			AddRow(2, 3, "assistant", `[]`, 2))

	got, err := chat_repo.NewMessage().List(ctx, 3)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "user", got[0].Role)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_NextSeq(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT COALESCE\\(MAX\\(seq\\), 0\\) \\+ 1 FROM `chat_messages` WHERE session_id = \\?").
		WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows([]string{"next"}).AddRow(5))

	got, err := chat_repo.NewMessage().NextSeq(ctx, 3)
	assert.NoError(t, err)
	assert.Equal(t, 5, got)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO `chat_messages`").
		WithArgs(
			int64(3), "", "user", "[]", "",
			0, 0, 0, 0, 0, 0, 0,
			"", "", 1,
			sqlmock.AnyArg(), sqlmock.AnyArg(),
		).
		WillReturnResult(sqlmock.NewResult(42, 1))
	mock.ExpectCommit()

	m := &chat_entity.Message{SessionID: 3, Role: "user", BlocksJSON: "[]", Seq: 1}
	err := chat_repo.NewMessage().Create(ctx, m)
	assert.NoError(t, err)
	assert.Equal(t, int64(42), m.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Find(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE id = \\? ORDER BY `chat_messages`.`id` LIMIT \\?").
		WithArgs(int64(42), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "session_id", "role", "blocks_json", "seq"}).
			AddRow(42, 3, "assistant", `[]`, 4))

	got, err := chat_repo.NewMessage().Find(ctx, 42)
	assert.NoError(t, err)
	assert.NotNil(t, got)
	assert.Equal(t, int64(42), got.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Find_NotFound(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectQuery("SELECT \\* FROM `chat_messages` WHERE id = \\? ORDER BY `chat_messages`.`id` LIMIT \\?").
		WithArgs(int64(99), 1).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	got, err := chat_repo.NewMessage().Find(ctx, 99)
	assert.NoError(t, err)
	assert.Nil(t, got, "missing row 应返回 nil 而不是 ErrRecordNotFound")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_DeleteFromSeq(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("DELETE FROM `chat_messages` WHERE session_id = \\? AND seq >= \\?").
		WithArgs(int64(3), 5).
		WillReturnResult(sqlmock.NewResult(0, 4))
	mock.ExpectCommit()

	deleted, err := chat_repo.NewMessage().DeleteFromSeq(ctx, 3, 5)
	assert.NoError(t, err)
	assert.Equal(t, int64(4), deleted)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestMessageRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)

	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `chat_messages` SET ").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	m := &chat_entity.Message{ID: 42, SessionID: 3, Role: "assistant", BlocksJSON: `[{"type":"text"}]`, Seq: 2}
	err := chat_repo.NewMessage().Update(ctx, m)
	assert.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
