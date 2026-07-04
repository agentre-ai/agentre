package workflow_svc

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func defaultSeedGraph() FlowGraph {
	return FlowGraph{
		Version: 1,
		Nodes: []FlowNode{
			{ID: "see", Label: "See members", Kind: "leader"},
			{ID: "break", Label: "Break down", Kind: "leader"},
			{ID: "fe", Label: "Frontend", Kind: "task", Brief: "Build the UI.", SharedFiles: true},
			{ID: "be", Label: "Backend", Kind: "task", Brief: "Build the API."},
			{ID: "int", Label: "Integrate", Kind: "leader"},
			{ID: "ver", Label: "Verify", Kind: "task", Brief: "Run tests / review."},
			{ID: "wrap", Label: "Wrap up", Kind: "leader"},
		},
		Edges: []FlowEdge{
			{From: "see", To: "break"},
			{From: "break", To: "fe"}, {From: "break", To: "be"},
			{From: "fe", To: "int"}, {From: "be", To: "int"},
			{From: "int", To: "ver"}, {From: "ver", To: "wrap"},
			{From: "ver", To: "fe", Kind: "bounce"},
		},
	}
}

func TestProjectGraph_DefaultSeed(t *testing.T) {
	content, outline := ProjectGraph("Default Orchestration Flow", defaultSeedGraph())

	assert.True(t, strings.HasPrefix(content, "# Default Orchestration Flow\n"))
	assert.Contains(t, content, "You are the Leader.")
	// 并行组
	assert.Contains(t, content, "In parallel:")
	assert.Contains(t, content, "- Frontend — dispatch: Build the UI.")
	assert.Contains(t, content, "- Backend — dispatch: Build the API.")
	assert.Contains(t, content, "isolate=true")
	// 打回边挂在 Verify
	assert.Contains(t, content, "On fail → send back to Frontend (no new node).")
	// sink → finish
	assert.Contains(t, content, "finish with a summary @user")
	// 顺序：See members 在 Frontend 之前
	assert.Less(t, strings.Index(content, "See members"), strings.Index(content, "Frontend"))
	// outline 为各层代表
	assert.Equal(t, []string{"See members", "Break down", "Frontend ∥ …", "Integrate", "Verify", "Wrap up"}, outline)
}

func TestParseFlowGraph_EmptyOrInvalid(t *testing.T) {
	_, ok := ParseFlowGraph("")
	assert.False(t, ok)
	_, ok = ParseFlowGraph("not json")
	assert.False(t, ok)
	g, ok := ParseFlowGraph(`{"version":1,"nodes":[{"id":"a","label":"A","kind":"leader"}],"edges":[]}`)
	assert.True(t, ok)
	assert.Len(t, g.Nodes, 1)
}
