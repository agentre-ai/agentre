package claudecode

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestBuildSkillsSettings(t *testing.T) {
	Convey("把 enabledPlugins 合进 settings", t, func() {
		Convey("空 base", func() {
			got := buildSkillsSettings(map[string]bool{"a@m": true, "b@m": false}, "")
			var m map[string]any
			So(json.Unmarshal([]byte(got), &m), ShouldBeNil)
			ep := m["enabledPlugins"].(map[string]any)
			So(ep["a@m"], ShouldEqual, true)
			So(ep["b@m"], ShouldEqual, false)
		})
		Convey("空 map → 原样返回 base", func() {
			So(buildSkillsSettings(map[string]bool{}, `{"x":1}`), ShouldEqual, `{"x":1}`)
			So(buildSkillsSettings(nil, ""), ShouldEqual, "")
		})
		Convey("base 已是 JSON 对象 → 合并保留既有键", func() {
			got := buildSkillsSettings(map[string]bool{"a@m": true}, `{"effortLevel":"high"}`)
			var m map[string]any
			So(json.Unmarshal([]byte(got), &m), ShouldBeNil)
			So(m["effortLevel"], ShouldEqual, "high")
			So(m["enabledPlugins"].(map[string]any)["a@m"], ShouldEqual, true)
		})
		Convey("非对象/坏 JSON base → 空起,只剩 enabledPlugins", func() {
			got := buildSkillsSettings(map[string]bool{"a@m": true}, "{bad json")
			var m map[string]any
			So(json.Unmarshal([]byte(got), &m), ShouldBeNil)
			So(m["enabledPlugins"].(map[string]any)["a@m"], ShouldEqual, true)
			So(len(m), ShouldEqual, 1) // 坏 base 内容被丢弃,不阻断
		})
	})
}
