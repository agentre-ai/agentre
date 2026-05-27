package agent_backend_repo_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"agentre/internal/model/entity/agent_backend_entity"
	"agentre/internal/repository/agent_backend_repo"
)

// setupAgentBackendRepoTest 起一个 sqlmock 数据库，返回 ctx / mock / repo。
// 业务代码通过 db.Ctx(ctx) 命中 mock，断言落在「拼出的 SQL 与参数」上。
func setupAgentBackendRepoTest(t *testing.T) (context.Context, sqlmock.Sqlmock, agent_backend_repo.AgentBackendRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, agent_backend_repo.NewAgentBackend()
}

func TestAgentBackendRepo_Create(t *testing.T) {
	convey.Convey("Create", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("写入成功并回填自增 ID", func() {
			b := &agent_backend_entity.AgentBackend{
				Type:           string(agent_backend_entity.TypeBuiltin),
				Name:           "默认助手",
				LLMProviderKey: "test-key-uuid-1",
				Status:         consts.ACTIVE,
			}
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `agent_backends`").
				WillReturnResult(sqlmock.NewResult(42, 1))
			mock.ExpectCommit()

			assert.NoError(t, repo.Create(ctx, b))
			assert.Equal(t, int64(42), b.ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传", func() {
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `agent_backends`").
				WillReturnError(errors.New("boom"))
			mock.ExpectRollback()

			err := repo.Create(ctx, &agent_backend_entity.AgentBackend{
				Type: string(agent_backend_entity.TypeBuiltin), Name: "x", LLMProviderKey: "test-key-uuid-1", Status: consts.ACTIVE,
			})
			assert.EqualError(t, err, "boom")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestAgentBackendRepo_Find(t *testing.T) {
	convey.Convey("Find", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("命中时返回实体", func() {
			rows := sqlmock.NewRows([]string{"id", "type", "name", "llm_provider_key", "cli_path", "status", "createtime", "updatetime"}).
				AddRow(1, string(agent_backend_entity.TypeBuiltin), "默认助手", "test-key-uuid-2", "", consts.ACTIVE, int64(0), int64(0))
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE id = \\? AND status = \\? ORDER BY `agent_backends`.`id` LIMIT \\?").
				WithArgs(int64(1), consts.ACTIVE, 1).
				WillReturnRows(rows)

			got, err := repo.Find(ctx, 1)
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, "默认助手", got.Name)
			assert.Equal(t, "test-key-uuid-2", got.LLMProviderKey)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("ErrRecordNotFound 静默吞掉返回 nil", func() {
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE id = \\? AND status = \\?").
				WithArgs(int64(99), consts.ACTIVE, 1).
				WillReturnError(gorm.ErrRecordNotFound)

			got, err := repo.Find(ctx, 99)
			assert.NoError(t, err)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("其它错误向上抛", func() {
			mock.ExpectQuery("SELECT \\* FROM `agent_backends`").
				WithArgs(int64(1), consts.ACTIVE, 1).
				WillReturnError(sql.ErrConnDone)

			got, err := repo.Find(ctx, 1)
			assert.ErrorIs(t, err, sql.ErrConnDone)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestAgentBackendRepo_FindByName(t *testing.T) {
	convey.Convey("FindByName", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("命中时返回实体", func() {
			rows := sqlmock.NewRows([]string{"id", "type", "name", "llm_provider_key", "cli_path", "status", "createtime", "updatetime"}).
				AddRow(7, string(agent_backend_entity.TypeBuiltin), "alpha", "test-key-uuid-1", "", consts.ACTIVE, int64(0), int64(0))
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE name = \\? AND status = \\? ORDER BY `agent_backends`.`id` LIMIT \\?").
				WithArgs("alpha", consts.ACTIVE, 1).
				WillReturnRows(rows)

			got, err := repo.FindByName(ctx, "alpha")
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, int64(7), got.ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("找不到时返回 nil 而非错误", func() {
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE name = \\? AND status = \\?").
				WithArgs("beta", consts.ACTIVE, 1).
				WillReturnError(gorm.ErrRecordNotFound)

			got, err := repo.FindByName(ctx, "beta")
			assert.NoError(t, err)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestAgentBackendRepo_List(t *testing.T) {
	convey.Convey("List", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("按 id 升序过滤 status=ACTIVE", func() {
			rows := sqlmock.NewRows([]string{"id", "type", "name", "llm_provider_key", "cli_path", "status", "createtime", "updatetime"}).
				AddRow(1, string(agent_backend_entity.TypeBuiltin), "a", "test-key-uuid-1", "", consts.ACTIVE, int64(0), int64(0)).
				AddRow(2, string(agent_backend_entity.TypeBuiltin), "b", "test-key-uuid-1", "", consts.ACTIVE, int64(0), int64(0)).
				AddRow(3, string(agent_backend_entity.TypeBuiltin), "c", "test-key-uuid-1", "", consts.ACTIVE, int64(0), int64(0))
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE status = \\? ORDER BY id ASC").
				WithArgs(consts.ACTIVE).
				WillReturnRows(rows)

			got, err := repo.List(ctx)
			assert.NoError(t, err)
			assert.Len(t, got, 3)
			assert.Equal(t, "a", got[0].Name)
			assert.Equal(t, "c", got[2].Name)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传", func() {
			mock.ExpectQuery("SELECT \\* FROM `agent_backends` WHERE status = \\?").
				WithArgs(consts.ACTIVE).
				WillReturnError(sql.ErrConnDone)

			got, err := repo.List(ctx)
			assert.ErrorIs(t, err, sql.ErrConnDone)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestAgentBackendRepo_Delete(t *testing.T) {
	convey.Convey("Delete", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("软删除：UPDATE status=DELETE WHERE id=?", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `agent_backends` SET `status`=\\?(,`updatetime`=\\?)? WHERE id = \\?").
				WithArgs(consts.DELETE, int64(5)).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			assert.NoError(t, repo.Delete(ctx, 5))
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传并回滚", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `agent_backends` SET `status`=\\? WHERE id = \\?").
				WithArgs(consts.DELETE, int64(5)).
				WillReturnError(errors.New("write failed"))
			mock.ExpectRollback()

			err := repo.Delete(ctx, 5)
			assert.EqualError(t, err, "write failed")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestAgentBackendRepo_Update(t *testing.T) {
	convey.Convey("Update", t, func() {
		ctx, mock, repo := setupAgentBackendRepoTest(t)

		convey.Convey("Save 全字段更新", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `agent_backends` SET").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			err := repo.Update(ctx, &agent_backend_entity.AgentBackend{
				ID:             8,
				Type:           string(agent_backend_entity.TypeBuiltin),
				Name:           "renamed",
				LLMProviderKey: "test-key-uuid-2",
				Status:         consts.ACTIVE,
			})
			assert.NoError(t, err)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}
