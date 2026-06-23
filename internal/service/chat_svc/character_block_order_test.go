package chat_svc

import (
	"testing"

	"github.com/cago-frame/agents/agent/blocks"
	. "github.com/smartystreets/goconvey/convey"

	chatblocks "github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/turn"
)

// §1.6 ContentBlock 顺序约束 — characterization tests
// 参见 docs/superpowers/specs/2026-05-22-agentruntime-canonical-refactor-design.md §"TDD/BDD §1.6"。
//
// 关键 pin (turn.Accumulator):
//   - thinking 必须在 turn 最前(index 0) —— Anthropic 协议要求
//   - text delta 在 AddToolUse / AddBlock 时 flush(否则 tool_use 后面文字会黏前面文字)
//   - text delta 在 AddToolResult 时 *不* flush(tool_use→tool_result 之间一般无穿插)
func TestCharacterization_BlockOrder_ThinkingFirst(t *testing.T) {
	Convey("§1.6 Finalize() 把 thinking 放在 index 0", t, func() {
		acc := turn.New()
		acc.AddText("hello ")
		acc.AddToolUse(&blocks.ToolUseBlock{ID: "tu-1", Name: "X"}, "")
		acc.AddText("world")
		acc.AddThinking("chain of thought")

		final := acc.Finalize()
		So(len(final), ShouldBeGreaterThanOrEqualTo, 3)
		_, isThink := final[0].(*blocks.ThinkingBlock)
		So(isThink, ShouldBeTrue)
	})
}

func TestCharacterization_BlockOrder_TextFlushOnToolUse(t *testing.T) {
	Convey("§1.6 text delta 遇 tool_use flush", t, func() {
		acc := turn.New()
		acc.AddText("before ")
		acc.AddToolUse(&blocks.ToolUseBlock{ID: "tu-1", Name: "X"}, "")

		final := acc.Finalize()
		So(final, ShouldHaveLength, 2)
		txt, _ := final[0].(*blocks.TextBlock)
		So(txt, ShouldNotBeNil)
		So(txt.Text, ShouldEqual, "before ")
	})
}

func TestCharacterization_BlockOrder_TextNotFlushedOnToolResult(t *testing.T) {
	Convey("§1.6 text delta 遇 tool_result 不 flush", t, func() {
		acc := turn.New()
		acc.AddToolUse(&blocks.ToolUseBlock{ID: "tu-1", Name: "X"}, "")
		acc.AddText("intermixed")
		acc.AddToolResult(&blocks.ToolResultBlock{ToolUseID: "tu-1"})

		final := acc.Finalize()
		// 期望顺序: [ToolUseBlock, ToolResultBlock, TextBlock]
		So(final, ShouldHaveLength, 3)
		_, isText := final[2].(*blocks.TextBlock)
		So(isText, ShouldBeTrue)
	})
}

func TestCharacterization_BlockOrder_AddBlockFlushesText(t *testing.T) {
	Convey("§1.6 AddBlock(自定义 block) 与 tool_use 同样先 flush text", t, func() {
		acc := turn.New()
		acc.AddText("hi ")
		acc.AddBlock(chatblocks.UserAskBlock{RequestID: "r-1"}, "")

		final := acc.Finalize()
		So(final, ShouldHaveLength, 2)
		txt, _ := final[0].(*blocks.TextBlock)
		So(txt, ShouldNotBeNil)
		So(txt.Text, ShouldEqual, "hi ")
	})
}
