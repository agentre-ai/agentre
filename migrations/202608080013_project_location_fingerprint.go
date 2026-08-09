package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202608080013 把 project_locations 的账号内自然键从 (project, 本地
// device_id) 换成 (project, agentred 指纹)（决策 26 / R2b）。
//
// device_id 保留列与取值形态，但语义降级为「由指纹解析出的本地缓存」：本机
// 配对表里查不到该指纹时它就留空——那台机器上的项目路径记录本机不参与工作
// 目录解析、也不出现在路径列表里；配对后由 project_location_svc 的读路径自愈
// 回填，解除配对后清空，但这些都是运行时行为，本迁移只负责一次性结构调整：
//
//  1. 加 daemon_fingerprint 列；
//  2. 把既有行的 device_id 反查 paired_agentreds 换算成 daemon_fingerprint；
//  3. 反查不到的行（device_id 指向一台已经不存在的 paired_agentreds——旧的部分
//     唯一索引本就要求配对存在，理论上不该出现）直接丢弃：留着空指纹的行会在
//     同一项目内彼此冲突，撞新的部分唯一索引；本项目未发布，不必为这类不可能
//     状态保留兼容路径；
//  4. 唯一索引由 (project_id, device_id) 换成 (project_id, daemon_fingerprint)。
func migration202608080013() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202608080013",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE project_locations
	ADD COLUMN daemon_fingerprint TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE project_locations
	SET daemon_fingerprint = COALESCE(
		(SELECT p.daemon_fingerprint FROM paired_agentreds p WHERE CAST(p.id AS TEXT) = project_locations.device_id),
		''
	)
	WHERE device_id != ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DELETE FROM project_locations
	WHERE device_id != '' AND daemon_fingerprint = ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_project_locations_proj_device`).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_project_locations_proj_fingerprint
	ON project_locations(project_id, daemon_fingerprint) WHERE status = 1`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP INDEX IF EXISTS uniq_project_locations_proj_fingerprint`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_project_locations_proj_device
	ON project_locations(project_id, device_id) WHERE status = 1`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE project_locations DROP COLUMN daemon_fingerprint`).Error
		},
	}
}
