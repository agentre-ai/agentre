package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608010001 建 agentred 侧持久化地基的两张表(见规格「持久化数据变化 /
// agentred 侧」)。
//
// daemon_sessions —— 会话表。复合主键 (peer_fingerprint, peer_session_id):会话 id
// 是各客户端本地自增的,不同客户端必然重号,单独拿 peer_session_id 当主键会把不同对端
// 的同号会话错当同一条(R16)。不含"等待输入"列——那是 running 之上的实时叠加,不落库
// (R11,后续任务处理)。agent_id 是对端(桌面端)本地的数字 agent 主键,原样透传保存,
// 供后续任务展示用。latest_seq 是 notification log 的 denormalized 游标,由后续任务的
// 写入路径维护;本迁移只建列,不建默认写入者。
//
// daemon_notification_logs —— 通知日志表。复合主键 (peer_fingerprint,
// peer_session_id, seq):日志的一行 = 一条本该发出的通知,method/payload 是原样的
// JSON-RPC (method, params)。追加写、永久保留,不设 Rollback 之外的清理路径。
func migration202608010001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608010001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS daemon_sessions (
	peer_fingerprint TEXT NOT NULL,
	peer_session_id TEXT NOT NULL,
	agent_id INTEGER NOT NULL DEFAULT 0,
	cwd TEXT NOT NULL DEFAULT '',
	backend_type TEXT NOT NULL DEFAULT '',
	lifecycle_state TEXT NOT NULL DEFAULT '',
	latest_seq INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL DEFAULT 0,
	updated_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (peer_fingerprint, peer_session_id)
)`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE TABLE IF NOT EXISTS daemon_notification_logs (
	peer_fingerprint TEXT NOT NULL,
	peer_session_id TEXT NOT NULL,
	seq INTEGER NOT NULL,
	method TEXT NOT NULL,
	payload TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (peer_fingerprint, peer_session_id, seq)
)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS daemon_notification_logs`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS daemon_sessions`).Error
		},
	}
}
