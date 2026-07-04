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
	assert.Contains(t, resp.Content, "# P")
	assert.Contains(t, resp.Content, "finish with a summary @user")
}
