package migrations

import (
	"fmt"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// syncMetaTables 是账号级的七张表（R1、决策 3、docs/specs/2026-08-07-workspace-sync.md
// 366 行）：projects、departments、agents、agent_backends、project_agents、
// project_locations，加 task 1 新建的执行目标表 agent_exec_targets。每张表用
// 同一份六列同步元数据（决策 3/4/19/27）。
var syncMetaTables = []string{
	"projects",
	"departments",
	"agents",
	"agent_backends",
	"project_agents",
	"project_locations",
	"agent_exec_targets",
}

// syncMetaColumns 与 internal/model/entity/syncmeta_entity.SyncMeta 的字段顺序
// 一一对应，DDL 类型跟随宿主表既有 bigint/text 惯例。
var syncMetaColumns = []struct {
	name string
	ddl  string
}{
	{"sync_id", "TEXT NOT NULL DEFAULT ''"},
	{"sync_account_id", "BIGINT NOT NULL DEFAULT 0"},
	{"sync_version", "BIGINT NOT NULL DEFAULT 0"},
	{"sync_updated_at", "BIGINT NOT NULL DEFAULT 0"},
	{"sync_origin", "TEXT NOT NULL DEFAULT ''"},
	{"sync_deleted_at", "BIGINT NOT NULL DEFAULT 0"},
}

// migration202608080016 给账号级七张表加同步元数据（R1、366 行）：
//
//   - sync_id           账号内全局唯一、终身不变的同步标识（决策 3，ULID）；未登录
//     期间创建的行也照常生成（R12a）。本仓 DDL 一律 not null default ”，因此
//     这条唯一索引必须是 WHERE sync_id != ” 的部分索引——否则多行空标识会互撞。
//   - sync_account_id   所属账号（server_state.ServerUserID 的值）；0 = 尚未被
//     任何账号认领——未登录时创建的行、以及本迁移执行时已存在的历史行都落在
//     这里，直到后续任务的「首次登录认领」把它们收进当前账号（R12a）。
//   - sync_version      server 分配的账号级单调序列值（决策 27），本地只存不改；
//     出站队列另存每条改动的基版本（R4a），不复用这一列。
//   - sync_updated_at   最后一次修改的本地时间，只用于展示与 30 天窗口计算，不
//     参与胜负比较（决策 19/27）。
//   - sync_origin       最后一次修改来自哪台设备，决策 4 的字典序打破平局用它。
//   - sync_deleted_at   墓碑时间（R6）；0 = 未删除，与各表既有的 status 软删语义
//     正交——status 是本地可见性，这一列是跨机墓碑标记。
//
// 本迁移只加列与索引，不回填任何既有行的 sync_id：真正的生成时机是「行创建 /
// 下一次落库时」（R1，syncmeta_entity.SyncMeta.EnsureSyncID），历史行落在
// sync_id = ” 上，等它们下一次被仓储层 Create/Update 触达时按同一条 JIT 规则
// 补齐——不需要在迁移里为每一行造随机值。本任务只铺数据与骨架：真正的上行 /
// 下行 / 冲突落地是后续任务。
func migration202608080016() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080016",
		Migrate: func(tx *gorm.DB) error {
			for _, table := range syncMetaTables {
				for _, col := range syncMetaColumns {
					stmt := fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, col.name, col.ddl)
					if err := tx.Exec(stmt).Error; err != nil {
						return fmt.Errorf("table %s: add column %s: %w", table, col.name, err)
					}
				}
				idxStmt := fmt.Sprintf(
					`CREATE UNIQUE INDEX IF NOT EXISTS uniq_%s_sync_id ON %s(sync_id) WHERE sync_id != ''`,
					table, table,
				)
				if err := tx.Exec(idxStmt).Error; err != nil {
					return fmt.Errorf("table %s: create sync_id index: %w", table, err)
				}
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			for _, table := range syncMetaTables {
				if err := tx.Exec(fmt.Sprintf(`DROP INDEX IF EXISTS uniq_%s_sync_id`, table)).Error; err != nil {
					return err
				}
				for _, col := range syncMetaColumns {
					stmt := fmt.Sprintf(`ALTER TABLE %s DROP COLUMN %s`, table, col.name)
					if err := tx.Exec(stmt).Error; err != nil {
						return err
					}
				}
			}
			return nil
		},
	}
}
