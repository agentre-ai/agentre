package llm_provider_repo_test

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

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
)

// setupLLMProviderRepoTest 起一个 sqlmock 数据库，返回 ctx / mock / repo。
// 业务代码通过 db.Ctx(ctx) 命中 mock，断言落在「拼出的 SQL 与参数」上。
func setupLLMProviderRepoTest(t *testing.T) (context.Context, sqlmock.Sqlmock, llm_provider_repo.LLMProviderRepo) {
	t.Helper()
	ctx, _, mock := testutils.Database(t)
	return ctx, mock, llm_provider_repo.NewLLMProvider()
}

// providerRows 构造一行 sqlmock 返回值，避免每个用例重复列声明。
func providerRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "type", "name", "api_key", "base_url", "model",
		"max_output", "context_window", "status", "createtime", "updatetime",
	})
}

func TestLLMProviderRepo_Create(t *testing.T) {
	convey.Convey("Create", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("写入成功并回填自增 ID", func() {
			p := &llm_provider_entity.LLMProvider{
				Type:   string(llm_provider_entity.TypeAnthropic),
				Name:   "claude",
				APIKey: "sk-test",
				Status: consts.ACTIVE,
			}
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `llm_providers`").
				WillReturnResult(sqlmock.NewResult(7, 1))
			mock.ExpectCommit()

			assert.NoError(t, repo.Create(ctx, p))
			assert.Equal(t, int64(7), p.ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传并回滚", func() {
			mock.ExpectBegin()
			mock.ExpectExec("INSERT INTO `llm_providers`").
				WillReturnError(errors.New("boom"))
			mock.ExpectRollback()

			err := repo.Create(ctx, &llm_provider_entity.LLMProvider{
				Type: string(llm_provider_entity.TypeAnthropic), Name: "x", Status: consts.ACTIVE,
			})
			assert.EqualError(t, err, "boom")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_Find(t *testing.T) {
	convey.Convey("Find", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("命中时返回实体", func() {
			rows := providerRows().AddRow(
				1, string(llm_provider_entity.TypeAnthropic), "claude", "sk-test", "", "claude-sonnet-4-6",
				4096, 200000, consts.ACTIVE, int64(0), int64(0),
			)
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE id = \\? AND status = \\? ORDER BY `llm_providers`.`id` LIMIT \\?").
				WithArgs(int64(1), consts.ACTIVE, 1).
				WillReturnRows(rows)

			got, err := repo.Find(ctx, 1)
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, "claude", got.Name)
			assert.Equal(t, "sk-test", got.APIKey)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("ErrRecordNotFound 静默吞掉返回 nil", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE id = \\? AND status = \\?").
				WithArgs(int64(99), consts.ACTIVE, 1).
				WillReturnError(gorm.ErrRecordNotFound)

			got, err := repo.Find(ctx, 99)
			assert.NoError(t, err)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("其它错误向上抛", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers`").
				WithArgs(int64(1), consts.ACTIVE, 1).
				WillReturnError(sql.ErrConnDone)

			got, err := repo.Find(ctx, 1)
			assert.ErrorIs(t, err, sql.ErrConnDone)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_FindByName(t *testing.T) {
	convey.Convey("FindByName", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("命中时返回实体", func() {
			rows := providerRows().AddRow(
				5, string(llm_provider_entity.TypeOpenAIChat), "openai", "sk-1", "", "",
				0, 0, consts.ACTIVE, int64(0), int64(0),
			)
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE name = \\? AND status = \\? ORDER BY `llm_providers`.`id` LIMIT \\?").
				WithArgs("openai", consts.ACTIVE, 1).
				WillReturnRows(rows)

			got, err := repo.FindByName(ctx, "openai")
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, int64(5), got.ID)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("找不到时返回 nil 而非错误", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE name = \\? AND status = \\?").
				WithArgs("missing", consts.ACTIVE, 1).
				WillReturnError(gorm.ErrRecordNotFound)

			got, err := repo.FindByName(ctx, "missing")
			assert.NoError(t, err)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_List(t *testing.T) {
	convey.Convey("List", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("按 id 升序过滤 status=ACTIVE", func() {
			rows := providerRows().
				AddRow(1, string(llm_provider_entity.TypeAnthropic), "a", "k1", "", "", 0, 0, consts.ACTIVE, int64(0), int64(0)).
				AddRow(2, string(llm_provider_entity.TypeOpenAIChat), "b", "k2", "", "", 0, 0, consts.ACTIVE, int64(0), int64(0))
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE status = \\? ORDER BY id ASC").
				WithArgs(consts.ACTIVE).
				WillReturnRows(rows)

			got, err := repo.List(ctx)
			assert.NoError(t, err)
			assert.Len(t, got, 2)
			assert.Equal(t, "a", got[0].Name)
			assert.Equal(t, "b", got[1].Name)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE status = \\?").
				WithArgs(consts.ACTIVE).
				WillReturnError(sql.ErrConnDone)

			got, err := repo.List(ctx)
			assert.ErrorIs(t, err, sql.ErrConnDone)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_Delete(t *testing.T) {
	convey.Convey("Delete", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("软删除：UPDATE status=DELETE WHERE id=?", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `llm_providers` SET `status`=\\?(,`updatetime`=\\?)? WHERE id = \\?").
				WithArgs(consts.DELETE, int64(3)).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			assert.NoError(t, repo.Delete(ctx, 3))
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传并回滚", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `llm_providers` SET `status`=\\? WHERE id = \\?").
				WithArgs(consts.DELETE, int64(3)).
				WillReturnError(errors.New("write failed"))
			mock.ExpectRollback()

			err := repo.Delete(ctx, 3)
			assert.EqualError(t, err, "write failed")
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_FindByKey(t *testing.T) {
	convey.Convey("FindByKey", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("命中", func() {
			rows := sqlmock.NewRows([]string{"id", "provider_key", "type", "name", "status"}).
				AddRow(int64(5), "uuid-abc", "anthropic", "huu-glm", 1)
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE provider_key = \\? ORDER BY `llm_providers`.`id` LIMIT \\?").
				WithArgs("uuid-abc", 1).WillReturnRows(rows)

			got, err := repo.FindByKey(ctx, "uuid-abc")
			assert.NoError(t, err)
			assert.NotNil(t, got)
			assert.Equal(t, int64(5), got.ID)
			assert.Equal(t, "huu-glm", got.Name)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
		convey.Convey("未命中返 nil, nil(GORM RecordNotFound)", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers`").
				WithArgs("missing", 1).WillReturnError(gorm.ErrRecordNotFound)

			got, err := repo.FindByKey(ctx, "missing")
			assert.NoError(t, err)
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_BatchFindByKey(t *testing.T) {
	convey.Convey("BatchFindByKey", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("空 keys 快速返回空 map", func() {
			got, err := repo.BatchFindByKey(ctx, []string{})
			assert.NoError(t, err)
			assert.Empty(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("命中多行时按 provider_key 索引", func() {
			rows := sqlmock.NewRows([]string{"id", "provider_key", "type", "name", "status"}).
				AddRow(int64(1), "key-1", "anthropic", "prov-1", 1).
				AddRow(int64(2), "key-2", "openai-chat", "prov-2", 1)
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE provider_key IN \\(\\?,\\?\\) AND status = \\?").
				WithArgs("key-1", "key-2", consts.ACTIVE).
				WillReturnRows(rows)

			got, err := repo.BatchFindByKey(ctx, []string{"key-1", "key-2"})
			assert.NoError(t, err)
			assert.Len(t, got, 2)
			assert.Equal(t, "prov-1", got["key-1"].Name)
			assert.Equal(t, "prov-2", got["key-2"].Name)
			assert.NoError(t, mock.ExpectationsWereMet())
		})

		convey.Convey("驱动报错时透传", func() {
			mock.ExpectQuery("SELECT \\* FROM `llm_providers` WHERE provider_key IN \\(\\?\\) AND status = \\?").
				WithArgs("key-1", consts.ACTIVE).
				WillReturnError(errors.New("db error"))

			got, err := repo.BatchFindByKey(ctx, []string{"key-1"})
			assert.EqualError(t, err, "db error")
			assert.Nil(t, got)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}

func TestLLMProviderRepo_Update(t *testing.T) {
	convey.Convey("Update", t, func() {
		ctx, mock, repo := setupLLMProviderRepoTest(t)

		convey.Convey("Save 全字段更新", func() {
			mock.ExpectBegin()
			mock.ExpectExec("UPDATE `llm_providers` SET").
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			err := repo.Update(ctx, &llm_provider_entity.LLMProvider{
				ID:     8,
				Type:   string(llm_provider_entity.TypeOpenAIChat),
				Name:   "renamed",
				APIKey: "sk-new",
				Status: consts.ACTIVE,
			})
			assert.NoError(t, err)
			assert.NoError(t, mock.ExpectationsWereMet())
		})
	})
}
