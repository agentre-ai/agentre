package migrations

import (
	"encoding/json"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

func TestMigration202607080001_SeedsPresetFlows(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	type row struct {
		Name, Content, Template, Graph, Tags, Outline string
		Updatetime                                    int64
	}
	var rows []row
	assert.NoError(t, db.Table("workflows").Order("updatetime DESC").Scan(&rows).Error)

	// 恰好 4 个内置流程,顺序(updatetime DESC)= Parallel Decompose 第一
	names := make([]string, len(rows))
	for i, r := range rows {
		names[i] = r.Name
	}
	assert.Equal(t, []string{
		"Parallel Decompose",
		"Sequential Pipeline",
		"Research → Synthesize",
		"Generate → Review → Iterate",
	}, names)

	// is_default 列已 DROP
	var cols []struct{ Name string }
	assert.NoError(t, db.Raw("PRAGMA table_info(workflows)").Scan(&cols).Error)
	for _, c := range cols {
		assert.NotEqual(t, "is_default", c.Name)
	}

	// 一致性:每行 content==render(template)、outline==ProjectGraph(graph)、tags 非空
	for _, r := range rows {
		assert.NotEmpty(t, r.Tags, r.Name)
		gotContent, gotOutline, err := workflow_svc.RenderWorkflowContent(r.Name, r.Graph, r.Template)
		assert.NoError(t, err, r.Name)
		assert.Equal(t, gotContent, r.Content, "content 应等于 template 渲染产物: "+r.Name)

		var storedOutline []string
		assert.NoError(t, json.Unmarshal([]byte(r.Outline), &storedOutline), r.Name)
		assert.Equal(t, gotOutline, storedOutline, "outline 应等于 ProjectGraph 投影: "+r.Name)
	}
}
