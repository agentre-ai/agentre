package llmcatalog

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestLookup(t *testing.T) {
	Convey("精确匹配命中 catalog", t, func() {
		info, ok := Lookup("claude-opus-4-7")
		So(ok, ShouldBeTrue)
		So(info.ContextWindow, ShouldEqual, 1_000_000)
	})

	Convey("当前主推模型 claude-opus-4-8 命中 catalog", t, func() {
		// 回归:catalog 落后于在用模型时,claudecode 后端 system.init 查不到
		// 窗口 → ContextWindowUpdated 不 emit → 前端 Composer 上下文用量条整块隐藏。
		info, ok := Lookup("claude-opus-4-8")
		So(ok, ShouldBeTrue)
		So(info.ContextWindow, ShouldEqual, 1_000_000)

		Convey("带日期后缀仍前缀命中", func() {
			info, ok := Lookup("claude-opus-4-8-20260529")
			So(ok, ShouldBeTrue)
			So(info.ContextWindow, ShouldEqual, 1_000_000)
		})
	})

	Convey("大小写不敏感", t, func() {
		info, ok := Lookup("Claude-Opus-4-7")
		So(ok, ShouldBeTrue)
		So(info.ContextWindow, ShouldEqual, 1_000_000)
	})

	Convey("斜杠归一化", t, func() {
		Convey("单层 vendor 前缀", func() {
			info, ok := Lookup("anthropic/claude-opus-4-7")
			So(ok, ShouldBeTrue)
			So(info.ContextWindow, ShouldEqual, 1_000_000)
		})
		Convey("多层路径取末段", func() {
			info, ok := Lookup("openrouter/anthropic/claude-opus-4-7")
			So(ok, ShouldBeTrue)
			So(info.ContextWindow, ShouldEqual, 1_000_000)
		})
	})

	Convey("前缀匹配 - provider 带日期/版本后缀", t, func() {
		info, ok := Lookup("claude-opus-4-7-20251201")
		So(ok, ShouldBeTrue)
		So(info.ContextWindow, ShouldEqual, 1_000_000)
	})

	Convey("前缀匹配 - 最长优先", t, func() {
		// catalog 同时有 gpt-5.3 / gpt-5.4 / gpt-5.5，应选最长者 gpt-5.5
		info, ok := Lookup("gpt-5.5-2026-snapshot")
		So(ok, ShouldBeTrue)
		So(info.ID, ShouldEqual, "gpt-5.5")
		So(info.ContextWindow, ShouldEqual, 1_050_000)
	})

	Convey("前缀匹配 - 边界字符", t, func() {
		Convey("gpt-5.5 不被 gpt-5.5xyz 命中（无分段边界）", func() {
			_, ok := Lookup("gpt-5.5xyz")
			So(ok, ShouldBeFalse)
		})
		Convey("glm-5 不抢 glm-5.1 的输入", func() {
			info, ok := Lookup("glm-5.1-extra")
			So(ok, ShouldBeTrue)
			So(info.ID, ShouldEqual, "glm-5.1")
		})
		Convey("括号后缀 gpt-5.5(xhigh) 命中 gpt-5.5", func() {
			info, ok := Lookup("gpt-5.5(xhigh)")
			So(ok, ShouldBeTrue)
			So(info.ID, ShouldEqual, "gpt-5.5")
		})
		Convey("方括号后缀 gpt-5.5[fast] 命中 gpt-5.5", func() {
			info, ok := Lookup("gpt-5.5[fast]")
			So(ok, ShouldBeTrue)
			So(info.ID, ShouldEqual, "gpt-5.5")
		})
		Convey("冒号后缀 gpt-5.5:beta 命中 gpt-5.5", func() {
			info, ok := Lookup("gpt-5.5:beta")
			So(ok, ShouldBeTrue)
			So(info.ID, ShouldEqual, "gpt-5.5")
		})
		Convey("斜杠 + 括号 anthropic/claude-opus-4-7(thinking) 命中", func() {
			info, ok := Lookup("anthropic/claude-opus-4-7(thinking)")
			So(ok, ShouldBeTrue)
			So(info.ID, ShouldEqual, "claude-opus-4-7")
		})
	})

	Convey("斜杠 + 前缀组合", t, func() {
		info, ok := Lookup("anthropic/claude-opus-4-7-20251201")
		So(ok, ShouldBeTrue)
		So(info.ContextWindow, ShouldEqual, 1_000_000)
	})

	Convey("完全未知 ID 返回 false", t, func() {
		_, ok := Lookup("totally-unknown-model")
		So(ok, ShouldBeFalse)
	})

	Convey("空白输入返回 false", t, func() {
		_, ok := Lookup("   ")
		So(ok, ShouldBeFalse)
	})
}
