package migrations

import (
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

// TestMigration202607090003_RefreshesBuiltinFlowPrompts 断言四个内置流程正文已刷新到当前工具集：
// 删除死参 isolate / 死概念 node,并织入共享待办清单工具(task_add)。
func TestMigration202607090003_RefreshesBuiltinFlowPrompts(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	assert.NoError(t, err)
	assert.NoError(t, RunMigrations(db))

	contentOf := func(name string) string {
		var content string
		assert.NoError(t,
			db.Table("workflows").Select("content").Where("name = ?", name).Row().Scan(&content),
			name)
		return content
	}

	names := []string{
		"Parallel Decompose",
		"Sequential Pipeline",
		"Research → Synthesize",
		"Generate → Review → Iterate",
	}
	for _, name := range names {
		c := contentOf(name)
		// 每个内置流程都织入了共享待办清单(task_add)。
		assert.Contains(t, c, "task_add", name)
		// 死参 isolate / 死概念 node 已从所有正文清除。
		assert.NotContains(t, strings.ToLower(c), "isolate", name)
		assert.NotContains(t, c, "the same node", name)
	}

	// Parallel Decompose 不再教 isolate=true,改为「同文件不独立」的真实指引。
	assert.NotContains(t, contentOf("Parallel Decompose"), "isolate=true")
	// Generate → Review → Iterate 不再提「reuse the same node」。
	assert.NotContains(t, contentOf("Generate → Review → Iterate"), "reuse the same node")
}
