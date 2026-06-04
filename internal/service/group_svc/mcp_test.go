package group_svc_test

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/service/group_svc"
)

func TestGroupMCP_ToolCallRoutesToIngest(t *testing.T) {
	Convey("合法 token 的 group_send tools/call → 调 IngestAgentMessage(memberID, body, mentions)", t, func() {
		var gotMember int64
		var gotBody string
		var gotMentions []string
		h := group_svc.NewGroupMCPForTest(func(_ context.Context, memberID int64, body string, mentions []string) error {
			gotMember, gotBody, gotMentions = memberID, body, mentions
			return nil
		})
		token := h.MintToken(5, 2)

		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{"body":"做好了","mentions":["前端"]}}}`
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		So(w.Code, ShouldEqual, 200)
		So(gotMember, ShouldEqual, 2)
		So(gotBody, ShouldEqual, "做好了")
		So(gotMentions, ShouldResemble, []string{"前端"})
	})

	Convey("无/坏 token → 拒绝, 不调 ingest", t, func() {
		called := false
		h := group_svc.NewGroupMCPForTest(func(context.Context, int64, string, []string) error { called = true; return nil })
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{}}}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		So(called, ShouldBeFalse)
		So(w.Code, ShouldNotEqual, 200) // 401
	})

	Convey("tools/list 暴露 group_send schema", t, func() {
		h := group_svc.NewGroupMCPForTest(nil)
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		So(w.Body.String(), ShouldContainSubstring, "group_send")
	})

	Convey("initialize echoes client protocolVersion + serverInfo", t, func() {
		h := group_svc.NewGroupMCPForTest(nil)
		req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		So(w.Code, ShouldEqual, 200)
		So(w.Body.String(), ShouldContainSubstring, "2025-11-25")
		So(w.Body.String(), ShouldContainSubstring, "serverInfo")
	})
}
