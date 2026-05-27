package blocks

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNestedToolBlocks_TypeAndAudience(t *testing.T) {
	Convey("Nested tool blocks 是 ToUI(防 LLM context 泄漏)", t, func() {
		So(NestedToolUseBlock{}.Audience(), ShouldEqual, cagoblocks.ToUI)
		So(NestedToolResultBlock{}.Audience(), ShouldEqual, cagoblocks.ToUI)
		So(NestedToolUseBlock{}.Type(), ShouldEqual, "nested_tool_use")
		So(NestedToolResultBlock{}.Type(), ShouldEqual, "nested_tool_result")
	})
}

func TestNestedToolUse_RoundTrip(t *testing.T) {
	Convey("NestedToolUseBlock round-trip 保留 ParentToolCallID", t, func() {
		b := &NestedToolUseBlock{
			ID: "nested-1", Name: "Read", ParentToolCallID: "task-1",
			Input: map[string]any{"file_path": "/tmp/x"},
		}
		sb, err := cagoblocks.Encode(b)
		So(err, ShouldBeNil)
		decoded, err := cagoblocks.Decode(sb)
		So(err, ShouldBeNil)
		got, ok := decoded.(NestedToolUseBlock)
		So(ok, ShouldBeTrue)
		So(got.ParentToolCallID, ShouldEqual, "task-1")
	})
}
