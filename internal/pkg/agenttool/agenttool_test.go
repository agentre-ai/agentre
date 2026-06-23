package agenttool

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRegistry(t *testing.T) {
	defs := Registry()
	require.Len(t, defs, 4)
	require.Equal(t, "org", defs[0].Key)
	require.Equal(t, "/mcp/org/", defs[0].MCPPath)
	require.Contains(t, defs[0].ToolNames, "org_get")
	require.Len(t, defs[0].ToolNames, 7)

	d, ok := Lookup("org")
	require.True(t, ok)
	require.Equal(t, KeyOrg, d.Key)
	_, ok = Lookup("nope")
	require.False(t, ok)

	require.Equal(t, []string{"org", "workflow", "group_create", "subagent"}, Keys())
}

func TestRegistry_HasGroupCreate(t *testing.T) {
	d, ok := Lookup(KeyGroupCreate)
	require.True(t, ok)
	require.Equal(t, "group_create", d.Key)
	require.Equal(t, "/mcp/group/", d.MCPPath)
	require.Equal(t, []string{"group_create"}, d.ToolNames)
}

func TestRegistry_HasWorkflow(t *testing.T) {
	d, ok := Lookup(KeyWorkflow)
	if !ok {
		t.Fatal("workflow not registered")
	}
	if d.MCPPath != "/mcp/workflow/" {
		t.Fatalf("path=%s", d.MCPPath)
	}
	want := []string{"workflow_list", "workflow_create", "workflow_update", "workflow_delete"}
	if !slices.Equal(d.ToolNames, want) {
		t.Fatalf("tools=%v", d.ToolNames)
	}
	keys := Keys()
	if !slices.Contains(keys, "workflow") || !slices.Contains(keys, "org") {
		t.Fatalf("keys=%v", keys)
	}
}

func TestSubagentRegistered(t *testing.T) {
	def, ok := Lookup(KeySubagent)
	if !ok {
		t.Fatal("subagent tool not registered")
	}
	if def.MCPPath != "/mcp/subagent/" {
		t.Fatalf("MCPPath = %q", def.MCPPath)
	}
	want := []string{"agent_list", "agent_call"}
	if !slices.Equal(def.ToolNames, want) {
		t.Fatalf("ToolNames = %v, want %v", def.ToolNames, want)
	}
	if !slices.Contains(Keys(), KeySubagent) {
		t.Fatal("KeySubagent missing from Keys()")
	}
}
