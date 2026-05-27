package view

import (
	"encoding/json"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/pkg/agentruntime/canonical"
)

func TestChatBlock_OmitsEmptyFields(t *testing.T) {
	Convey("ChatBlock JSON 序列化空字段省略", t, func() {
		b := ChatBlock{Type: "text", Text: "hi"}
		raw, err := json.Marshal(b)
		So(err, ShouldBeNil)
		So(string(raw), ShouldEqual, `{"type":"text","text":"hi"}`)
	})
}

func TestChatBlock_CanonicalDTO(t *testing.T) {
	Convey("ChatBlock 带 Canonical 时 JSON 含 canonical 节点", t, func() {
		fw := canonical.FileWrite{Path: "/tmp/x", Content: "data"}
		b := ChatBlock{
			Type: "tool_use", ToolUseID: "tu-1", ToolName: "Write",
			Canonical: &CanonicalDTO{Kind: canonical.KindFileWrite, FileWrite: &fw},
		}
		raw, err := json.Marshal(b)
		So(err, ShouldBeNil)
		So(string(raw), ShouldContainSubstring, `"canonical":{"kind":"file.write"`)
	})
}
