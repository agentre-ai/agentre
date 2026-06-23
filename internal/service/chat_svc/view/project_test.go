package view

import (
	"testing"

	cagoblocks "github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
)

func TestProjectMessageBlocks_Text(t *testing.T) {
	Convey("TextBlock 投影到 type=text", t, func() {
		out := ProjectMessageBlocks([]cagoblocks.ContentBlock{
			&cagoblocks.TextBlock{Text: "hi"},
		})
		So(out, ShouldHaveLength, 1)
		So(out[0].Type, ShouldEqual, "text")
		So(out[0].Text, ShouldEqual, "hi")
	})
}

func TestProjectMessageBlocks_ToolUseAndResult(t *testing.T) {
	Convey("ToolUse + ToolResult 配对投影", t, func() {
		out := ProjectMessageBlocks([]cagoblocks.ContentBlock{
			&cagoblocks.ToolUseBlock{ID: "tu-1", Name: "Bash", Input: map[string]any{"cmd": "ls"}},
			&cagoblocks.ToolResultBlock{
				ToolUseID: "tu-1",
				Content:   []cagoblocks.ContentBlock{cagoblocks.TextBlock{Text: "out"}},
			},
		})
		So(out, ShouldHaveLength, 2)
		So(out[0].Type, ShouldEqual, "tool_use")
		So(out[0].ToolName, ShouldEqual, "Bash")
		So(out[1].Type, ShouldEqual, "tool_result")
		So(out[1].ToolResult, ShouldEqual, "out")
	})
}

func TestProjectMessageBlocks_NestedTool(t *testing.T) {
	Convey("Nested tool 带 ParentToolCallID", t, func() {
		out := ProjectMessageBlocks([]cagoblocks.ContentBlock{
			&blocks.NestedToolUseBlock{ID: "n-1", Name: "Read", ParentToolCallID: "task-1"},
		})
		So(out[0].ParentToolCallID, ShouldEqual, "task-1")
	})
}

func TestProjectMessageBlocks_UserAsk(t *testing.T) {
	Convey("UserAskBlock 投影到 type=user_ask + UserAsk 子字段", t, func() {
		out := ProjectMessageBlocks([]cagoblocks.ContentBlock{
			&blocks.UserAskBlock{RequestID: "r"},
		})
		So(out[0].Type, ShouldEqual, "user_ask")
		So(out[0].UserAsk, ShouldNotBeNil)
		So(out[0].UserAsk.RequestID, ShouldEqual, "r")
	})
}
