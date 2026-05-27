package canonical

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarshalTool_RoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   CanonicalTool
	}{
		{"file_write_basic", FileWrite{Path: "/a/b.txt", Content: "hi", Lines: 1, Bytes: 2}},
		{"file_write_truncated", FileWrite{Path: "/big", Content: "...", Truncated: true}},
		{"file_edit_modified", FileEdit{Files: []FileEditPatch{{
			Path: "/a.go", Kind: ChangeModified, Plus: 3, Minus: 1,
			Hunks: []DiffHunk{{OldStart: 1, OldLines: 2, NewStart: 1, NewLines: 4, Lines: []DiffLine{
				{Op: OpContext, Text: "ctx"},
				{Op: OpAdd, Text: "added"},
				{Op: OpRemove, Text: "removed"},
			}}},
		}}}},
		{"file_edit_empty", FileEdit{}},
		{"user_ask_basic", UserAsk{RequestID: "r1", Questions: []any{map[string]any{"q": "?"}}}},
		{"user_ask_resolved", UserAsk{RequestID: "r2", Answered: true, Skipped: true, Answers: []any{"a"}}},
		{"plan_update_steps", PlanUpdate{Steps: []PlanStep{
			{ID: "s1", Step: "do x", Status: StepCompleted},
			{ID: "s2", Step: "do y", Status: StepInProgress},
		}}},
		{"plan_update_text_only", PlanUpdate{Text: "## Plan\n- a\n- b"}},
		{"plan_update_with_actions", PlanUpdate{Actions: []PlanAction{
			{ID: "plan.execute", Kind: PlanActionApprove},
			{ID: "plan.refine", Kind: PlanActionRefine, RequiresFeedback: true},
		}}},
		{"plan_approve_request", PlanApproveRequest{
			RequestID: "p1", PlanText: "plan body",
			Actions: []PlanAction{{ID: "plan.approve.manual", Kind: PlanActionApprove}},
		}},
		{"plan_approve_resolved", PlanApproveRequest{
			RequestID: "p2", PlanText: "x", Resolved: true, Allowed: false, DenyReason: "no thanks",
		}},
		{"agent_spawn_running", AgentSpawn{
			TaskID: "t1", SubagentType: "researcher", TaskDescription: "look",
			Prompt: "p", Status: "running",
		}},
		{"agent_spawn_completed", AgentSpawn{
			TaskID: "t2", LastToolName: "Read", ToolUses: 5,
			TotalTokens: 1234, DurationMs: 9000, Status: "completed",
		}},
		{"tool_permission_request", ToolPermission{
			RequestID: "tp1", ToolName: "Bash",
			ToolInput: map[string]any{"cmd": "ls"},
		}},
		{"tool_permission_resolved_allow", ToolPermission{
			RequestID: "tp2", ToolName: "Write", Resolved: true, Allowed: true, AlwaysAllow: true,
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, err := MarshalTool(tc.in)
			require.NoError(t, err)

			// Wire shape sanity: kind is present at the top level.
			var head struct {
				Kind Kind `json:"kind"`
			}
			require.NoError(t, json.Unmarshal(b, &head))
			assert.Equal(t, KindOf(tc.in), head.Kind, "wire kind must match concrete type")

			out, err := UnmarshalTool(b)
			require.NoError(t, err)
			assert.Equal(t, tc.in, out)
		})
	}
}

func TestMarshalTool_NilRoundTrip(t *testing.T) {
	b, err := MarshalTool(nil)
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(b))

	out, err := UnmarshalTool(b)
	require.NoError(t, err)
	assert.Nil(t, out)
}

func TestMarshalTool_PointerTypes(t *testing.T) {
	// 客户端代码可能持有指针(*FileWrite 等);MarshalTool 应当跟值类型同结果。
	fw := FileWrite{Path: "/x", Content: "y", Lines: 1, Bytes: 1}
	bVal, _ := MarshalTool(fw)
	bPtr, err := MarshalTool(&fw)
	require.NoError(t, err)
	assert.JSONEq(t, string(bVal), string(bPtr))

	// nil pointer 必须降级为 null。
	var pfw *FileWrite
	bNil, err := MarshalTool(pfw)
	require.NoError(t, err)
	assert.JSONEq(t, "null", string(bNil))
}

func TestUnmarshalTool_EdgeCases(t *testing.T) {
	t.Run("empty bytes", func(t *testing.T) {
		out, err := UnmarshalTool(nil)
		require.NoError(t, err)
		assert.Nil(t, out)
	})
	t.Run("json null", func(t *testing.T) {
		out, err := UnmarshalTool([]byte("null"))
		require.NoError(t, err)
		assert.Nil(t, out)
	})
	t.Run("missing kind", func(t *testing.T) {
		_, err := UnmarshalTool([]byte(`{"path":"/a"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "missing kind")
	})
	t.Run("unknown kind", func(t *testing.T) {
		_, err := UnmarshalTool([]byte(`{"kind":"file.something_new"}`))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unknown kind")
	})
	t.Run("malformed json", func(t *testing.T) {
		_, err := UnmarshalTool([]byte(`{not json`))
		require.Error(t, err)
	})
}

// TestMarshalTool_AllKindsCovered locks in the symmetry: every Kind constant
// must have BOTH a marshaling arm (asserted by round-tripping a zero value of
// the concrete type) AND an unmarshaling arm. Adding an 8th canonical type
// without updating wire.go will fail here.
func TestMarshalTool_AllKindsCovered(t *testing.T) {
	specimens := map[Kind]CanonicalTool{
		KindFileWrite:          FileWrite{},
		KindFileEdit:           FileEdit{},
		KindUserAsk:            UserAsk{},
		KindPlanUpdate:         PlanUpdate{},
		KindPlanApproveRequest: PlanApproveRequest{},
		KindAgentSpawn:         AgentSpawn{},
		KindToolPermission:     ToolPermission{},
	}
	for k, sp := range specimens {
		t.Run(string(k), func(t *testing.T) {
			b, err := MarshalTool(sp)
			require.NoError(t, err)
			out, err := UnmarshalTool(b)
			require.NoError(t, err)
			assert.Equal(t, sp, out)
		})
	}
}
