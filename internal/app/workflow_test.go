package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWorkflowPreviewGraph(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{
		Name:  "P",
		Graph: `{"version":1,"nodes":[{"id":"a","label":"Do","kind":"task","brief":"x"}],"edges":[]}`,
	})
	assert.NoError(t, err)
	assert.Contains(t, resp.Content, "# P") // 空模板回落占位符 → 投影
	assert.Empty(t, resp.Error)
}

func TestWorkflowPreviewGraph_Template(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{
		Name:     "P",
		Graph:    `{"version":1,"nodes":[{"id":"a","label":"Do","kind":"task","brief":"x"}],"edges":[]}`,
		Template: "head\n{{ DAGPrompt }}",
	})
	assert.NoError(t, err)
	assert.Contains(t, resp.Content, "head\n")
	assert.Contains(t, resp.Content, "# P")
}

func TestWorkflowPreviewGraph_Error(t *testing.T) {
	a := &App{}
	resp, err := a.WorkflowPreviewGraph(&WorkflowPreviewRequest{Name: "P", Template: "{{ DAGPromt }}"})
	assert.NoError(t, err) // 错误走响应字段,不是 Go error
	assert.NotEmpty(t, resp.Error)
	assert.Empty(t, resp.Content)
}
