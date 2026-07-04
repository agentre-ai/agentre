package migrations

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func TestMigration202607040002_AddsFlowOverlayColumns(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	// node_ref 列可写可读
	assert.NoError(t, db.Exec(`INSERT INTO orch_tasks (run_id, node_ref) VALUES (1, 'FE')`).Error)
	var nodeRef string
	assert.NoError(t, db.Raw(`SELECT node_ref FROM orch_tasks WHERE run_id = 1`).Scan(&nodeRef).Error)
	assert.Equal(t, "FE", nodeRef)

	// flow_graph 列可写可读
	assert.NoError(t, db.Exec(`INSERT INTO orchestration_runs (goal, flow_graph) VALUES ('g', '{"version":1}')`).Error)
	var fg string
	assert.NoError(t, db.Raw(`SELECT flow_graph FROM orchestration_runs WHERE goal = 'g'`).Scan(&fg).Error)
	assert.Equal(t, `{"version":1}`, fg)
}
