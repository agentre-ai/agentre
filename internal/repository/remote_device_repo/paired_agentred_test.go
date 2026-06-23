package remote_device_repo_test

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/cago-frame/cago/pkg/utils/testutils"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/paired_agentred_entity"
	"github.com/agentre-ai/agentre/internal/repository/remote_device_repo"
)

func TestPairedAgentredRepo_Create(t *testing.T) {
	convey.Convey("Create inserts and assigns id", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec("INSERT INTO `paired_agentreds`").
			WillReturnResult(sqlmock.NewResult(42, 1))
		mock.ExpectCommit()

		p := &paired_agentred_entity.PairedAgentred{
			Name: "x", URL: "ws://h/rpc", DaemonFingerprint: "fp",
			InstanceUUID: "u", TLSMode: "default", PairedAt: 1, Status: 1,
		}
		err := remote_device_repo.NewPairedAgentred().Create(ctx, p)
		assert.NoError(t, err)
		assert.Equal(t, int64(42), p.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_Get(t *testing.T) {
	convey.Convey("Get returns row by id", t, func() {
		ctx, _, mock := testutils.Database(t)
		rows := sqlmock.NewRows([]string{"id", "name", "url", "daemon_fingerprint", "instance_uuid", "tls_mode", "tls_cert_pem", "paired_at", "last_seen_at", "last_error", "status", "createtime", "updatetime"}).
			AddRow(int64(7), "x", "ws://h/rpc", "fp", "u", "default", "", int64(1), int64(2), "", 1, int64(0), int64(0))
		mock.ExpectQuery("SELECT \\* FROM `paired_agentreds` WHERE `paired_agentreds`.`id` = \\?").
			WithArgs(int64(7), 1).
			WillReturnRows(rows)

		got, err := remote_device_repo.NewPairedAgentred().Get(ctx, 7)
		assert.NoError(t, err)
		assert.Equal(t, int64(7), got.ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	convey.Convey("Get returns (nil, nil) when not found", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectQuery("SELECT \\* FROM `paired_agentreds`").
			WillReturnError(gorm.ErrRecordNotFound)

		got, err := remote_device_repo.NewPairedAgentred().Get(ctx, 99)
		assert.NoError(t, err)
		assert.Nil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_FindByURL(t *testing.T) {
	convey.Convey("FindByURL only returns active rows", t, func() {
		ctx, _, mock := testutils.Database(t)
		rows := sqlmock.NewRows([]string{"id", "name", "url", "daemon_fingerprint", "instance_uuid", "tls_mode", "tls_cert_pem", "paired_at", "last_seen_at", "last_error", "status", "createtime", "updatetime"}).
			AddRow(int64(3), "x", "ws://h/rpc", "fp", "u", "default", "", int64(1), int64(0), "", 1, int64(0), int64(0))
		mock.ExpectQuery("SELECT \\* FROM `paired_agentreds` WHERE url = \\? AND status = \\? ORDER BY `paired_agentreds`.`id` LIMIT").
			WithArgs("ws://h/rpc", 1, 1).
			WillReturnRows(rows)

		got, err := remote_device_repo.NewPairedAgentred().FindByURL(ctx, "ws://h/rpc")
		assert.NoError(t, err)
		assert.NotNil(t, got)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_List(t *testing.T) {
	convey.Convey("List only returns active rows ordered by id desc", t, func() {
		ctx, _, mock := testutils.Database(t)
		rows := sqlmock.NewRows([]string{"id", "name", "url", "daemon_fingerprint", "instance_uuid", "tls_mode", "tls_cert_pem", "paired_at", "last_seen_at", "last_error", "status", "createtime", "updatetime"}).
			AddRow(int64(2), "b", "ws://b/rpc", "fp", "u", "default", "", int64(2), int64(0), "", 1, int64(0), int64(0)).
			AddRow(int64(1), "a", "ws://a/rpc", "fp", "u", "default", "", int64(1), int64(0), "", 1, int64(0), int64(0))
		mock.ExpectQuery("SELECT \\* FROM `paired_agentreds` WHERE status = \\? ORDER BY id DESC").
			WithArgs(1).
			WillReturnRows(rows)

		got, err := remote_device_repo.NewPairedAgentred().List(ctx)
		assert.NoError(t, err)
		assert.Len(t, got, 2)
		assert.Equal(t, int64(2), got[0].ID)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_UpdateTLS(t *testing.T) {
	convey.Convey("UpdateTLS sets mode + pem + touches updatetime", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `paired_agentreds` SET").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := remote_device_repo.NewPairedAgentred().UpdateTLS(ctx, 7, "pin-cert", "PEM")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_UpdateLastSeen(t *testing.T) {
	convey.Convey("UpdateLastSeen sets last_seen + last_error", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `paired_agentreds` SET").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := remote_device_repo.NewPairedAgentred().UpdateLastSeen(ctx, 7, 1234, "")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_Rename(t *testing.T) {
	convey.Convey("Rename sets name + updatetime", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `paired_agentreds` SET").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := remote_device_repo.NewPairedAgentred().Rename(ctx, 7, "new-name")
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}

func TestPairedAgentredRepo_Delete(t *testing.T) {
	convey.Convey("Delete soft-deletes (status=2)", t, func() {
		ctx, _, mock := testutils.Database(t)
		mock.ExpectBegin()
		mock.ExpectExec("UPDATE `paired_agentreds` SET").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mock.ExpectCommit()

		err := remote_device_repo.NewPairedAgentred().Delete(ctx, 7)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})
}
