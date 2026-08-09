package syncstate_repo

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/syncmeta_entity"
	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
)

// TestFindLocalID 按同步标识取本机主键——跨机引用落地时靠它翻回本地 ID（R2）。
func TestFindLocalID(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := NewSyncState()
	mock.ExpectQuery("SELECT id FROM `agents` WHERE sync_id = \\?").
		WithArgs("agent-1").
		WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(int64(42)))

	id, err := repo.FindLocalID(ctx, syncwire.KindAgent, "agent-1")
	require.NoError(t, err)
	assert.Equal(t, int64(42), id)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestFindLocalID_GivenUnknownSyncID_ReturnsZero 查不到就是「引用目标还没到」，
// 由同步层按 R2a 暂缓落地，不是错误。
func TestFindLocalID_GivenUnknownSyncID_ReturnsZero(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := NewSyncState()
	mock.ExpectQuery("SELECT id FROM `projects` WHERE sync_id = \\?").
		WithArgs("nope").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	id, err := repo.FindLocalID(ctx, syncwire.KindProject, "nope")
	require.NoError(t, err)
	assert.Zero(t, id)
}

// TestFindLocalID_GivenUnknownKind_Errors 表名会被拼进 SQL，白名单之外一律报错。
func TestFindLocalID_GivenUnknownKind_Errors(t *testing.T) {
	ctx, _, _ := testutils.Database(t)
	_, err := NewSyncState().FindLocalID(ctx, "chat_sessions; DROP TABLE agents", "x")
	assert.ErrorIs(t, err, ErrUnknownKind)
}

// TestFindVersion 版本守卫读的就是它：本机已有的版本与墓碑标记（R4/R6）。
func TestFindVersion(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := NewSyncState()
	mock.ExpectQuery("SELECT sync_version,sync_deleted_at FROM `departments` WHERE sync_id = \\?").
		WithArgs("dept-1").
		WillReturnRows(sqlmock.NewRows([]string{"sync_version", "sync_deleted_at"}).
			AddRow(int64(9), int64(1700)))

	version, deleted, found, err := repo.FindVersion(ctx, syncwire.KindDepartment, "dept-1")
	require.NoError(t, err)
	assert.Equal(t, int64(9), version)
	assert.True(t, deleted)
	assert.True(t, found)
}

// TestFindRow_ReadsTombstonedRowsToo 落地一条删除时也要读得到那一行：查询不带
// status 过滤。
func TestFindRow_ReadsTombstonedRowsToo(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := NewSyncState()
	mock.ExpectQuery("SELECT \\* FROM `projects` WHERE sync_id = \\?").
		WithArgs("proj-1", 1).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status"}).
			AddRow(int64(3), "Gone", 0))

	row := &project_entity.Project{}
	found, err := repo.FindRow(ctx, syncwire.KindProject, "proj-1", row)
	require.NoError(t, err)
	assert.True(t, found)
	assert.Equal(t, int64(3), row.ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestSaveMeta 只写六列同步元数据，一个业务列都不碰。
func TestSaveMeta(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	repo := NewSyncState()
	mock.ExpectBegin()
	mock.ExpectExec("UPDATE `project_locations` SET `sync_account_id`=\\?,`sync_deleted_at`=\\?,`sync_origin`=\\?,`sync_updated_at`=\\?,`sync_version`=\\? WHERE sync_id = \\?").
		WithArgs(int64(7), int64(0), "3", int64(1700), int64(12), "loc-1").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	err := repo.SaveMeta(ctx, syncwire.KindProjectLocation, "loc-1", syncmeta_entity.SyncMeta{
		SyncID: "loc-1", SyncAccountID: 7, SyncVersion: 12, SyncUpdatedAt: 1700, SyncOrigin: "3",
	})
	require.NoError(t, err)
	assert.NoError(t, mock.ExpectationsWereMet())
}
