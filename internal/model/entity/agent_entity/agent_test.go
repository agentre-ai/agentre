package agent_entity

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAgentCheck(t *testing.T) {
	ctx := context.Background()

	cases := []struct {
		name    string
		input   *Agent
		wantErr bool
	}{
		{"nil receiver", nil, true},
		{"empty name", &Agent{Name: "", DepartmentID: 1, AgentBackendID: 1}, true},
		{"invalid avatar color", &Agent{Name: "x", AvatarColor: "rainbow", DepartmentID: 1, AgentBackendID: 1}, true},
		{"non-system missing placement", &Agent{Name: "x", AgentBackendID: 1}, true},
		{"non-system with both department and parent", &Agent{Name: "x", DepartmentID: 1, ParentAgentID: 2, AgentBackendID: 1}, true},
		{"non-system missing backend", &Agent{Name: "x", DepartmentID: 1}, true},
		{"non-system happy", &Agent{Name: "Eva", AvatarColor: "agent-2", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, false},
		{"non-system extended color happy", &Agent{Name: "Eva", AvatarColor: "agent-10", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, false},
		{"non-system parent agent happy", &Agent{Name: "Eva", AvatarColor: "agent-2", ParentAgentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, false},
		{"system zero department ok", &Agent{Name: "CEO", SystemBadge: "DEFAULT", AvatarColor: "agent-1", PromptJSON: "[]", SkillsJSON: "[]"}, false},
		{"system with department rejected", &Agent{Name: "CEO", SystemBadge: "DEFAULT", DepartmentID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, true},
		{"system with parent rejected", &Agent{Name: "CEO", SystemBadge: "DEFAULT", ParentAgentID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, true},
		{"bad prompt json", &Agent{Name: "Eva", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "{", SkillsJSON: "[]"}, true},
		{"bad skills json", &Agent{Name: "Eva", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "x"}, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.input.Check(ctx)
			if tc.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAgentPromptRoundtrip(t *testing.T) {
	a := &Agent{}
	a.SetPrompt([]string{"line a", "line b"})
	got := a.GetPrompt()
	assert.Equal(t, []string{"line a", "line b"}, got)

	empty := &Agent{}
	assert.Equal(t, []string{}, empty.GetPrompt())
}

func TestAgentSkillsRoundtrip(t *testing.T) {
	a := &Agent{}
	in := []AgentSkillItem{{Label: "read_file", Enabled: true}, {Label: "send_email", Enabled: false}}
	a.SetSkills(in)
	assert.Equal(t, in, a.GetSkills())
}

func TestAgentHelpers(t *testing.T) {
	assert.False(t, (*Agent)(nil).IsActive())
	assert.True(t, (&Agent{Status: 1}).IsActive())
	assert.True(t, (&Agent{SystemBadge: "DEFAULT"}).IsSystem())
	assert.False(t, (&Agent{}).IsSystem())
}

func TestAgent_PinnedField(t *testing.T) {
	assert.True(t, (&Agent{Pinned: true}).Pinned)
	assert.False(t, (&Agent{}).Pinned)
}
