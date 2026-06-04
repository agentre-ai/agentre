package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202605220011 建 issues / labels / issue_labels 三张表，并 seed 10 个内置标签。
func migration202605220011() *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202605220011",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS issues (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id INTEGER NOT NULL DEFAULT 0,
	title TEXT NOT NULL,
	body TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL DEFAULT 'open',
	agent_status TEXT NOT NULL DEFAULT 'idle',
	source TEXT NOT NULL DEFAULT 'manual',
	closed_at INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_state ON issues(status, state, updatetime)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issues_project ON issues(project_id, status)`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS labels (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	tone TEXT NOT NULL DEFAULT '',
	sort_order INTEGER NOT NULL DEFAULT 0,
	status INTEGER NOT NULL DEFAULT 1,
	createtime INTEGER NOT NULL DEFAULT 0,
	updatetime INTEGER NOT NULL DEFAULT 0
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uniq_labels_name_active ON labels(name) WHERE status = 1`).Error; err != nil {
				return err
			}

			if err := tx.Exec(`CREATE TABLE IF NOT EXISTS issue_labels (
	issue_id INTEGER NOT NULL,
	label_id INTEGER NOT NULL,
	PRIMARY KEY (issue_id, label_id)
)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`CREATE INDEX IF NOT EXISTS idx_issue_labels_label ON issue_labels(label_id)`).Error; err != nil {
				return err
			}

			return tx.Exec(`INSERT INTO labels (name, tone, sort_order, status, createtime, updatetime)
SELECT name, name, sort_order, 1,
	CAST(strftime('%s','now') AS INTEGER) * 1000,
	CAST(strftime('%s','now') AS INTEGER) * 1000
FROM (
	SELECT 'auth' AS name, 1 AS sort_order
	UNION ALL SELECT 'bug', 2
	UNION ALL SELECT 'critical', 3
	UNION ALL SELECT 'docs', 4
	UNION ALL SELECT 'feature', 5
	UNION ALL SELECT 'hook', 6
	UNION ALL SELECT 'ops', 7
	UNION ALL SELECT 'perf', 8
	UNION ALL SELECT 'refactor', 9
	UNION ALL SELECT 'ui', 10
) seed
WHERE NOT EXISTS (SELECT 1 FROM labels WHERE labels.name = seed.name AND labels.status = 1)`).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DROP TABLE IF EXISTS issue_labels`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DROP TABLE IF EXISTS labels`).Error; err != nil {
				return err
			}
			return tx.Exec(`DROP TABLE IF EXISTS issues`).Error
		},
	}
}
