package workflow_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
)

func TestWorkflowRepo_Create(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO .workflows.`).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	w := &workflow_entity.Workflow{Name: "产品开发流程", Content: "# 流程", Status: consts.ACTIVE}
	require.NoError(t, workflow_repo.NewWorkflow().Create(ctx, w))
	assert.NotZero(t, w.Createtime, "Create 必须补 createtime")
	assert.NotZero(t, w.Updatetime, "Create 必须补 updatetime")
	assert.NoError(t, mock.ExpectationsWereMet())
}

// TestWorkflowRepo_Update 钉死 Save 全列更新 + WHERE 主键:
// Save 按 PK 全列覆盖写，WHERE 必须只按主键 id 钉死。
func TestWorkflowRepo_Update(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec("(?s)UPDATE `workflows` SET .* WHERE `id` = \\?").
		WithArgs(
			// SET: 按实体字段序全列(name, content, status, createtime, updatetime)
			"产品开发流程", "# 流程", consts.ACTIVE, int64(0), sqlmock.AnyArg(),
			int64(3), // WHERE: id 主键
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	w := &workflow_entity.Workflow{
		ID:      3,
		Name:    "产品开发流程",
		Content: "# 流程",
		Status:  consts.ACTIVE,
	}
	require.NoError(t, workflow_repo.NewWorkflow().Update(ctx, w))
	assert.NotZero(t, w.Updatetime, "Update 必须刷新 updatetime")
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkflowRepo_Find(t *testing.T) {
	t.Run("找到返回实体", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `workflows` WHERE id = \\? ORDER BY `workflows`.`id` LIMIT \\?").
			WithArgs(int64(1), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id", "name", "status"}).
				AddRow(1, "产品开发流程", consts.ACTIVE))
		got, err := workflow_repo.NewWorkflow().Find(ctx, 1)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, int64(1), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("不存在返回 nil,nil", func(t *testing.T) {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `workflows` WHERE id = \\? ORDER BY `workflows`.`id` LIMIT \\?").
			WithArgs(int64(99), 1).
			WillReturnRows(sqlmock.NewRows([]string{"id"}))
		got, err := workflow_repo.NewWorkflow().Find(ctx, 99)
		require.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestWorkflowRepo_List(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectQuery(`SELECT \* FROM .workflows. WHERE status = \? ORDER BY updatetime DESC`).
		WithArgs(consts.ACTIVE).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name"}).AddRow(1, "a").AddRow(2, "b"))
	rows, err := workflow_repo.NewWorkflow().List(ctx)
	require.NoError(t, err)
	assert.Len(t, rows, 2)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestWorkflowRepo_Delete(t *testing.T) {
	ctx, _, mock := testutils.Database(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE .workflows. SET .status.=`).
		WithArgs(consts.DELETE, sqlmock.AnyArg(), int64(1)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, workflow_repo.NewWorkflow().Delete(ctx, 1))
	assert.NoError(t, mock.ExpectationsWereMet())
}
