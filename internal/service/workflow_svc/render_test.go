package workflow_svc

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderTemplate(t *testing.T) {
	t.Run("占位符渲染为 dagPrompt", func(t *testing.T) {
		out, err := RenderTemplate("{{ DAGPrompt }}", "F", "STEP-A\nSTEP-B")
		require.NoError(t, err)
		assert.Equal(t, "STEP-A\nSTEP-B", out)
	})
	t.Run("前后包裹文本", func(t *testing.T) {
		out, err := RenderTemplate("intro\n{{ DAGPrompt }}\nouttro", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "intro\nDAG\nouttro", out)
	})
	t.Run("FlowName 变量", func(t *testing.T) {
		out, err := RenderTemplate("# {{ .FlowName }}", "标准流", "")
		require.NoError(t, err)
		assert.Equal(t, "# 标准流", out)
	})
	t.Run("if 条件(空 DAG 走 else)", func(t *testing.T) {
		out, err := RenderTemplate(`{{ if DAGPrompt }}has{{ else }}none{{ end }}`, "F", "")
		require.NoError(t, err)
		assert.Equal(t, "none", out)
	})
	t.Run("无占位符=纯文本原样", func(t *testing.T) {
		out, err := RenderTemplate("just prose", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "just prose", out)
	})
	t.Run("空模板→空串", func(t *testing.T) {
		out, err := RenderTemplate("", "F", "DAG")
		require.NoError(t, err)
		assert.Equal(t, "", out)
	})
	t.Run("未定义函数→报错", func(t *testing.T) {
		_, err := RenderTemplate("{{ DAGPromt }}", "F", "DAG")
		require.Error(t, err)
	})
	t.Run("坏语法→报错", func(t *testing.T) {
		_, err := RenderTemplate("{{ if }}", "F", "DAG")
		require.Error(t, err)
	})
}
