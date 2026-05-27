package blocks

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"
)

func TestSubagentStateBlock(t *testing.T) {
	Convey("SubagentStateBlock 类型/Audience/round-trip", t, func() {
		b := &SubagentStateBlock{
			ParentToolCallID:  "task-1",
			Status:            "running",
			ToolUses:          2,
			NestedToolCallIDs: []string{"n-1", "n-2"},
		}
		So(b.Type(), ShouldEqual, "subagent_state")
		So(b.Audience(), ShouldEqual, cagoblocks.ToUI)

		sb, err := cagoblocks.Encode(b)
		So(err, ShouldBeNil)
		decoded, err := cagoblocks.Decode(sb)
		So(err, ShouldBeNil)
		got, ok := decoded.(SubagentStateBlock)
		So(ok, ShouldBeTrue)
		So(got.NestedToolCallIDs, ShouldHaveLength, 2)
	})
}
