package migrations

import (
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// migration202607040001 给 workflows 加 graph/is_default 两列，并 seed 内置「Default Orchestration Flow」。
// graph = 流程 DAG 的 JSON 真源；content = 其确定性投影(注入 Leader)。二者手写保持一致。
func migration202607040001() *gormigrate.Migration {
	const graph = `{"version":1,"nodes":[` +
		`{"id":"see","label":"See members","kind":"leader"},` +
		`{"id":"break","label":"Break down","kind":"leader"},` +
		`{"id":"fe","label":"Frontend","kind":"task","brief":"Build the UI per the spec. Acceptance: renders, states covered.","sharedFiles":true},` +
		`{"id":"be","label":"Backend","kind":"task","brief":"Build the API per the spec. Acceptance: endpoints + tests."},` +
		`{"id":"int","label":"Integrate","kind":"leader"},` +
		`{"id":"ver","label":"Verify","kind":"task","brief":"Run review / tests. Acceptance: all pass, no regressions."},` +
		`{"id":"wrap","label":"Wrap up","kind":"leader"}` +
		`],"edges":[` +
		`{"from":"see","to":"break"},{"from":"break","to":"fe"},{"from":"break","to":"be"},` +
		`{"from":"fe","to":"int"},{"from":"be","to":"int"},{"from":"int","to":"ver"},` +
		`{"from":"ver","to":"wrap"},{"from":"ver","to":"fe","kind":"bounce"}` +
		`]}`

	const content = "# Default Orchestration Flow\n" +
		"You are the Leader. Every result returns to you; you decide the next move.\n\n" +
		"1. See members\n\n" +
		"2. Break down\n\n" +
		"3. In parallel:\n" +
		"   - Frontend — dispatch: Build the UI per the spec. Acceptance: renders, states covered.\n" +
		"   - Backend — dispatch: Build the API per the spec. Acceptance: endpoints + tests.\n" +
		"   (use isolate=true if they touch the same files)\n\n" +
		"4. Integrate\n\n" +
		"5. Dispatch to the Verify role: Run review / tests. Acceptance: all pass, no regressions.\n" +
		"   On fail → send back to Frontend (no new node).\n\n" +
		"6. Wrap up — finish with a summary @user.\n"

	const tags = `["Default","General"]`
	const outline = `["See members","Break down","Frontend ∥ …","Integrate","Verify","Wrap up"]`

	return &gormigrate.Migration{
		ID: "202607040001",
		Migrate: func(tx *gorm.DB) error {
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN graph TEXT NOT NULL DEFAULT ''`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE workflows ADD COLUMN is_default INTEGER NOT NULL DEFAULT 0`).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO workflows (name, content, tags, outline, graph, is_default, status, createtime, updatetime)
SELECT ?, ?, ?, ?, ?, 1, 1,
	CAST(strftime('%s','now') AS INTEGER) * 1000,
	CAST(strftime('%s','now') AS INTEGER) * 1000
WHERE NOT EXISTS (SELECT 1 FROM workflows WHERE is_default = 1)`,
				"Default Orchestration Flow", content, tags, outline, graph).Error
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Exec(`DELETE FROM workflows WHERE is_default = 1`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`ALTER TABLE workflows DROP COLUMN is_default`).Error; err != nil {
				return err
			}
			return tx.Exec(`ALTER TABLE workflows DROP COLUMN graph`).Error
		},
	}
}
