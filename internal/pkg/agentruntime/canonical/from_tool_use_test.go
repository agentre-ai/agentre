package canonical

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestFromToolUse(t *testing.T) {
	Convey("FromToolUse", t, func() {
		Convey("Write tool → FileWrite with lines/bytes", func() {
			c, ok := FromToolUse("Write", map[string]any{
				"file_path": "/tmp/a.txt",
				"content":   "hello\nworld\n",
			})
			So(ok, ShouldBeTrue)
			fw, ok := c.(FileWrite)
			So(ok, ShouldBeTrue)
			So(fw.Path, ShouldEqual, "/tmp/a.txt")
			So(fw.Content, ShouldEqual, "hello\nworld\n")
			So(fw.Lines, ShouldEqual, 2)
			So(fw.Bytes, ShouldEqual, 12)
			So(fw.Truncated, ShouldBeFalse)
		})

		Convey("Write tool 内容超 WriteContentByteCap → 截断+Truncated=true", func() {
			big := strings.Repeat("x", WriteContentByteCap+10)
			c, ok := FromToolUse("Write", map[string]any{
				"file_path": "/tmp/b.txt",
				"content":   big,
			})
			So(ok, ShouldBeTrue)
			fw := c.(FileWrite)
			So(len(fw.Content), ShouldEqual, WriteContentByteCap)
			So(fw.Bytes, ShouldEqual, WriteContentByteCap+10)
			So(fw.Truncated, ShouldBeTrue)
		})

		Convey("Write tool 没有 content → 不识别", func() {
			_, ok := FromToolUse("Write", map[string]any{"file_path": "/tmp/c.txt"})
			So(ok, ShouldBeFalse)
		})

		Convey("Edit tool → FileEdit 单 patch", func() {
			c, ok := FromToolUse("Edit", map[string]any{
				"file_path":  "/tmp/d.txt",
				"old_string": "foo",
				"new_string": "bar",
			})
			So(ok, ShouldBeTrue)
			fe, ok := c.(FileEdit)
			So(ok, ShouldBeTrue)
			So(len(fe.Files), ShouldEqual, 1)
			So(fe.Files[0].Path, ShouldEqual, "/tmp/d.txt")
			So(fe.Files[0].Kind, ShouldEqual, ChangeModified)
		})

		Convey("Edit 带 replace_all=true → patch.ReplaceAll=true", func() {
			c, ok := FromToolUse("Edit", map[string]any{
				"file_path":   "/tmp/d.txt",
				"old_string":  "foo",
				"new_string":  "bar",
				"replace_all": true,
			})
			So(ok, ShouldBeTrue)
			fe := c.(FileEdit)
			So(fe.Files[0].ReplaceAll, ShouldBeTrue)
		})

		Convey("MultiEdit edits 全空 → 不识别", func() {
			_, ok := FromToolUse("MultiEdit", map[string]any{
				"file_path": "/tmp/e.txt",
				"edits":     []any{},
			})
			So(ok, ShouldBeFalse)
		})

		Convey("MultiEdit 合法 edits → FileEdit 合并 hunks", func() {
			c, ok := FromToolUse("MultiEdit", map[string]any{
				"file_path": "/tmp/e.txt",
				"edits": []any{
					map[string]any{"old_string": "a", "new_string": "A"},
					map[string]any{"old_string": "b", "new_string": "B"},
				},
			})
			So(ok, ShouldBeTrue)
			fe := c.(FileEdit)
			So(len(fe.Files), ShouldEqual, 1)
			So(len(fe.Files[0].Hunks), ShouldBeGreaterThan, 0)
		})

		Convey("file_change codex tool → FileEdit", func() {
			c, ok := FromToolUse("file_change", map[string]any{
				"changes": []any{
					map[string]any{
						"path": "/tmp/f.txt",
						"kind": "modified",
						"diff": "@@ -1,1 +1,1 @@\n-foo\n+bar\n",
					},
				},
			})
			So(ok, ShouldBeTrue)
			fe, ok := c.(FileEdit)
			So(ok, ShouldBeTrue)
			So(fe.Files[0].Path, ShouldEqual, "/tmp/f.txt")
			So(fe.Files[0].Kind, ShouldEqual, ChangeModified)
		})

		Convey("update_plan codex tool → PlanUpdate", func() {
			c, ok := FromToolUse("update_plan", map[string]any{
				"explanation": "demo",
				"plan": []any{
					map[string]any{"step": "inspect", "status": "completed"},
					map[string]any{"step": "report", "status": "in_progress"},
				},
			})
			So(ok, ShouldBeTrue)
			pu, ok := c.(PlanUpdate)
			So(ok, ShouldBeTrue)
			So(pu.Steps, ShouldHaveLength, 2)
			So(pu.Steps[0].Step, ShouldEqual, "inspect")
			So(pu.Steps[0].Status, ShouldEqual, StepCompleted)
			So(pu.Steps[1].Step, ShouldEqual, "report")
			So(pu.Steps[1].Status, ShouldEqual, StepInProgress)
		})

		Convey("update_plan 空 plan → 不识别", func() {
			_, ok := FromToolUse("update_plan", map[string]any{"plan": []any{}})
			So(ok, ShouldBeFalse)
		})

		Convey("Task tool → AgentSpawn 静态字段", func() {
			c, ok := FromToolUse("Task", map[string]any{
				"description":   "review PR",
				"subagent_type": "code-reviewer",
				"prompt":        "please review the diff",
			})
			So(ok, ShouldBeTrue)
			as, ok := c.(AgentSpawn)
			So(ok, ShouldBeTrue)
			So(as.TaskDescription, ShouldEqual, "review PR")
			So(as.SubagentType, ShouldEqual, "code-reviewer")
			So(as.Prompt, ShouldEqual, "please review the diff")
			// 运行时累计态在 replay 路径由 SubagentStateBlock 单独承载
			So(as.ToolUses, ShouldEqual, 0)
			So(as.Status, ShouldEqual, "")
		})

		Convey("Agent tool (新版 claudecode CLI 命名) → AgentSpawn", func() {
			c, ok := FromToolUse("Agent", map[string]any{
				"description":   "probe",
				"subagent_type": "general-purpose",
				"prompt":        "Run echo hello",
			})
			So(ok, ShouldBeTrue)
			as := c.(AgentSpawn)
			So(as.TaskDescription, ShouldEqual, "probe")
			So(as.SubagentType, ShouldEqual, "general-purpose")
		})

		Convey("subagent 工具名大小写不敏感(task/agent/AGENT)", func() {
			input := map[string]any{"description": "x"}
			_, ok := FromToolUse("task", input)
			So(ok, ShouldBeTrue)
			_, ok = FromToolUse("agent", input)
			So(ok, ShouldBeTrue)
			_, ok = FromToolUse("AGENT", input)
			So(ok, ShouldBeTrue)
		})

		Convey("Task 三字段全空 → 不识别(走 raw 路径)", func() {
			_, ok := FromToolUse("Task", map[string]any{})
			So(ok, ShouldBeFalse)
		})

		Convey("Bash / 普通工具 → 不识别", func() {
			_, ok := FromToolUse("Bash", map[string]any{"command": "ls"})
			So(ok, ShouldBeFalse)
			_, ok = FromToolUse("Read", map[string]any{"file_path": "/tmp/x"})
			So(ok, ShouldBeFalse)
		})
	})
}
