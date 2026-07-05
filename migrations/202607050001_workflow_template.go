package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607050001 给 workflows 加 template 列并回填:
//   - 带图流程 template='{{ DAGPrompt }}'(其 content 已=投影,render('{{ DAGPrompt }}') 逐字节一致);
//   - legacy 无图流程 template=content(无占位符渲染即原样)。
//
// content 无需重算,故纯 SQL 回填(不调 Go 投影)。
func migration202607050001() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202607050001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN template TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`UPDATE workflows SET template = '{{ DAGPrompt }}' WHERE graph != ''`).Error; err != nil {
				return err
			}
			return tx.Exec(`UPDATE workflows SET template = content WHERE graph = ''`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN template`).Error
		},
	}
}
