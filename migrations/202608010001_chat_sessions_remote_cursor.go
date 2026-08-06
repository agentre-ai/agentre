package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608010001 给 chat_sessions 增三列,记住「这条会话在哪跑的、跑它的那台
// daemon 是不是还是原来那台、桌面端已经消费到它通知流的哪一条」:
//
//   - exec_device_id           执行该会话的配对 daemon(paired_agentreds.id);0 = 本机执行
//   - exec_daemon_fingerprint  该 daemon 的实例标识(rpc.DaemonFingerprint 由 daemon
//     instance uuid 派生的 "sha256:<hex>",与 paired_agentreds.daemon_fingerprint 同值)
//   - event_cursor             桌面端已消费到的 daemon 通知 seq
//
// 三列默认值(0 / 空串 / 0)即「本机执行、无游标」,老数据的语义与加列前完全一致。
func migration202608010001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608010001",
		Migrate: func(tx *gorm.DB) error {
			for _, stmt := range []string{
				`ALTER TABLE chat_sessions ADD COLUMN exec_device_id BIGINT NOT NULL DEFAULT 0`,
				`ALTER TABLE chat_sessions ADD COLUMN exec_daemon_fingerprint TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE chat_sessions ADD COLUMN event_cursor BIGINT NOT NULL DEFAULT 0`,
			} {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, stmt := range []string{
				`ALTER TABLE chat_sessions DROP COLUMN event_cursor`,
				`ALTER TABLE chat_sessions DROP COLUMN exec_daemon_fingerprint`,
				`ALTER TABLE chat_sessions DROP COLUMN exec_device_id`,
			} {
				if err := tx.Exec(stmt).Error; err != nil {
					return err
				}
			}
			return nil
		},
	}
}
