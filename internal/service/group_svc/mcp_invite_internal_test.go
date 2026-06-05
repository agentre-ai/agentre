package group_svc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestGroupMCP_ToolsList_IncludesInvite(t *testing.T) {
	Convey("tools/list 同时广告 group_send 与 group_invite", t, func() {
		h := newGroupMCP(nil)
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/mcp/group/",
			strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		h.ServeHTTP(rr, req)
		So(rr.Body.String(), ShouldContainSubstring, "group_send")
		So(rr.Body.String(), ShouldContainSubstring, "group_invite")
	})
}

func TestGroupMCP_ToolsCall_RoutesInvite(t *testing.T) {
	Convey("tools/call group_invite → invite 回调收到解析后的 names/ids/reason", t, func() {
		var gotMember int64
		var gotNames []string
		var gotReason string
		h := newGroupMCP(nil)
		h.invite = func(_ context.Context, memberID int64, names []string, _ []int64, reason string) ([]InviteResult, error) {
			gotMember, gotNames, gotReason = memberID, names, reason
			return []InviteResult{{AgentID: 2, Name: "Bob"}}, nil
		}
		tok := h.MintToken(5, 100)
		rr := httptest.NewRecorder()
		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_invite","arguments":{"agentNames":["Bob"],"reason":"需要支援"}}}`
		req := httptest.NewRequest(http.MethodPost, "/mcp/group/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+tok)
		h.ServeHTTP(rr, req)
		So(gotMember, ShouldEqual, 100)
		So(gotNames, ShouldResemble, []string{"Bob"})
		So(gotReason, ShouldEqual, "需要支援")
		So(rr.Body.String(), ShouldContainSubstring, "Bob")
	})
}
