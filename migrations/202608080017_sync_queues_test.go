package migrations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration202608080017_SyncLostChangesIsReadWritable 「没能同步的改动」
// (R5/R5a、387-389 行):留住被覆盖那一版的同步标识与基版本,按账号分命名空间,
// 三类失效事件(被覆盖/被丢弃/被拒)共用一张表(决策 12)。
func TestMigration202608080017_SyncLostChangesIsReadWritable(t *testing.T) {
	gormDB := openTestDB(t, "202608080016")
	require.NoError(t, RunMigrations(gormDB))

	require.NoError(t, gormDB.Exec(`INSERT INTO sync_lost_changes
(sync_account_id, entity_type, entity_sync_id, base_version, reason, payload_json, origin_device, occurred_at, createtime)
VALUES (1, 'project', 'proj-sync-1', 3, 'overwritten', '{"name":"old"}', 'device-a', 1000, 1000)`).Error)

	var reason, entitySyncID string
	var baseVersion int64
	require.NoError(t, gormDB.Raw(
		`SELECT reason, entity_sync_id, base_version FROM sync_lost_changes WHERE sync_account_id = 1`,
	).Row().Scan(&reason, &entitySyncID, &baseVersion))
	assert.Equal(t, "overwritten", reason)
	assert.Equal(t, "proj-sync-1", entitySyncID)
	assert.Equal(t, int64(3), baseVersion)
}

// TestMigration202608080017_SyncOutboundQueueIsReadWritable 出站队列(R7):本地
// 待上行的改动,每条带基版本(R4a、R6a);本端新建、server 从未见过的行基版本
// 与 sync_id 都为空。
func TestMigration202608080017_SyncOutboundQueueIsReadWritable(t *testing.T) {
	gormDB := openTestDB(t, "202608080016")
	require.NoError(t, RunMigrations(gormDB))

	require.NoError(t, gormDB.Exec(`INSERT INTO sync_outbound_queue
(sync_account_id, entity_type, local_id, entity_sync_id, op, base_version, queued_at)
VALUES (1, 'department', 42, '', 'create', 0, 5000)`).Error)

	var op string
	var localID, baseVersion int64
	require.NoError(t, gormDB.Raw(
		`SELECT op, local_id, base_version FROM sync_outbound_queue WHERE sync_account_id = 1`,
	).Row().Scan(&op, &localID, &baseVersion))
	assert.Equal(t, "create", op)
	assert.Equal(t, int64(42), localID)
	assert.Equal(t, int64(0), baseVersion, "本端新建、server 从未见过的行基版本为空(R4a)")
}

// TestMigration202608080017_SyncInboundQueueIsReadWritable 入站队列(R2a):已收到
// 但因引用目标未到达而暂缓落地的行,留住等待的那个引用的同步标识。
func TestMigration202608080017_SyncInboundQueueIsReadWritable(t *testing.T) {
	gormDB := openTestDB(t, "202608080016")
	require.NoError(t, RunMigrations(gormDB))

	require.NoError(t, gormDB.Exec(`INSERT INTO sync_inbound_queue
(sync_account_id, entity_type, entity_sync_id, payload_json, missing_sync_id, received_at)
VALUES (1, 'agent', 'agent-sync-1', '{"name":"Eva"}', 'department-sync-missing', 9000)`).Error)

	var missingSyncID string
	require.NoError(t, gormDB.Raw(
		`SELECT missing_sync_id FROM sync_inbound_queue WHERE sync_account_id = 1`,
	).Row().Scan(&missingSyncID))
	assert.Equal(t, "department-sync-missing", missingSyncID)
}
