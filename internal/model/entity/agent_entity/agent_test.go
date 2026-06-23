package agent_entity

import (
	"context"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		{"non-system without backend ok", &Agent{Name: "x", DepartmentID: 1}, false},
		{"non-system happy", &Agent{Name: "Eva", AvatarColor: "agent-2", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, false},
		{"non-system extended color happy", &Agent{Name: "Eva", AvatarColor: "agent-16", DepartmentID: 1, AgentBackendID: 1, PromptJSON: "[]", SkillsJSON: "[]"}, false},
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
	in := []AgentSkillItem{{ID: "read_file", Enabled: true}, {ID: "send_email", Enabled: false}}
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

func TestAgentTools(t *testing.T) {
	t.Run("空串/坏 JSON 返回空列表", func(t *testing.T) {
		a := &Agent{}
		require.Equal(t, []AgentToolItem{}, a.GetTools())
		a.ToolsJSON = "{bad"
		require.Equal(t, []AgentToolItem{}, a.GetTools())
	})
	t.Run("SetTools/GetTools round-trip + ToolEnabled", func(t *testing.T) {
		a := &Agent{}
		a.SetTools([]AgentToolItem{{Key: "org", Enabled: true}})
		require.Equal(t, `[{"key":"org","enabled":true}]`, a.ToolsJSON)
		require.True(t, a.ToolEnabled("org"))
		require.False(t, a.ToolEnabled("other"))
		a.SetTools([]AgentToolItem{{Key: "org", Enabled: false}})
		require.False(t, a.ToolEnabled("org")) // 存在但已关闭
		a.SetTools(nil)
		require.Equal(t, `[]`, a.ToolsJSON)
		require.False(t, a.ToolEnabled("org"))
	})
	t.Run("Check 校验 ToolsJSON 必须是 JSON 数组", func(t *testing.T) {
		a := &Agent{Name: "x", DepartmentID: 1, AgentBackendID: 1, ToolsJSON: "{bad"}
		require.Error(t, a.Check(context.Background()))
	})
}

func TestAgentSkillPack(t *testing.T) {
	Convey("skill pack 序列化与查询", t, func() {
		a := &Agent{}
		a.SetSkills([]AgentSkillItem{
			{ID: "superpowers@claude-plugins-official", Enabled: true},
			{ID: "opsctl@opskat", Enabled: false},
		})

		Convey("GetEnabledPackIDs 只回 enabled 的 id", func() {
			So(a.GetEnabledPackIDs(), ShouldResemble, []string{"superpowers@claude-plugins-official"})
		})
		Convey("SkillPackEnabled 命中", func() {
			So(a.SkillPackEnabled("superpowers@claude-plugins-official"), ShouldBeTrue)
			So(a.SkillPackEnabled("opsctl@opskat"), ShouldBeFalse)
			So(a.SkillPackEnabled("missing@x"), ShouldBeFalse)
		})
		Convey("坏 JSON / 空串 → 空", func() {
			b := &Agent{SkillsJSON: "not json"}
			So(b.GetSkills(), ShouldResemble, []AgentSkillItem{})
			So(b.GetEnabledPackIDs(), ShouldResemble, []string{})
		})
	})
}
