package agent_entity

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
)

// TestAgentExecTargetSkillsRoundtrip 锁住 R15e 的存放位置:技能授权(SetSkills /
// GetSkills)现在挂在 AgentExecTarget 上,不再挂在 Agent 上 —— 每一档各自持有一份。
func TestAgentExecTargetSkillsRoundtrip(t *testing.T) {
	target := &AgentExecTarget{}
	in := []AgentSkillItem{{ID: "read_file", Enabled: true}, {ID: "send_email", Enabled: false}}
	target.SetSkills(in)
	assert.Equal(t, in, target.GetSkills())
}

func TestAgentExecTargetSkillPack(t *testing.T) {
	Convey("skill pack 序列化与查询(挂在执行目标行上,R15e)", t, func() {
		target := &AgentExecTarget{}
		target.SetSkills([]AgentSkillItem{
			{ID: "superpowers@claude-plugins-official", Enabled: true},
			{ID: "opsctl@opskat", Enabled: false},
		})

		Convey("GetEnabledPackIDs 只回 enabled 的 id", func() {
			So(target.GetEnabledPackIDs(), ShouldResemble, []string{"superpowers@claude-plugins-official"})
		})
		Convey("SkillPackEnabled 命中", func() {
			So(target.SkillPackEnabled("superpowers@claude-plugins-official"), ShouldBeTrue)
			So(target.SkillPackEnabled("opsctl@opskat"), ShouldBeFalse)
			So(target.SkillPackEnabled("missing@x"), ShouldBeFalse)
		})
		Convey("坏 JSON / 空串 → 空", func() {
			b := &AgentExecTarget{SkillsJSON: "not json"}
			So(b.GetSkills(), ShouldResemble, []AgentSkillItem{})
			So(b.GetEnabledPackIDs(), ShouldResemble, []string{})
		})
	})
}
