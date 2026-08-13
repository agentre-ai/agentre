package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608100001 给 daemon_sessions 补上 R7 / 决策 8 的三列:
//
//   - title —— 会话标题。桌面端每轮随 RunParams 携带当轮值、daemon 幂等覆盖
//     (R7);老会话没落过这列,保持空串,session.list 如实留空、不猜不填。
//   - agent_sync_id —— 该会话所属 Agent 的账号级同步标识(块 1 决策 3 的 ULID,
//     不是本地自增 agent_id)。会话列表据此解析 Agent 名与头像(R5 / R7)。
//   - provider_session_id —— 那台 agentred 上续话要用的 provider 原生会话身份
//     (决策 8)。daemon 每轮从 RunAck 那条路径收回并落库;续话不再需要调用方提供。
//
// SQLite 的 ALTER TABLE ADD COLUMN 一次只加一列,故逐条执行。
func migration202608100001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608100001",
		Migrate: func(tx *gorm.DB) error {
			for _, ddl := range []string{
				`ALTER TABLE daemon_sessions ADD COLUMN title TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE daemon_sessions ADD COLUMN agent_sync_id TEXT NOT NULL DEFAULT ''`,
				`ALTER TABLE daemon_sessions ADD COLUMN provider_session_id TEXT NOT NULL DEFAULT ''`,
			} {
				if err := tx.Exec(ddl).Error; err != nil {
					return err
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			// SQLite 不支持 DROP COLUMN,回滚只把空串值清回去;列结构保留。
			return tx.Exec(`UPDATE daemon_sessions SET title = '', agent_sync_id = '', provider_session_id = ''`).Error
		},
	}
}
