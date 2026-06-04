package group_svc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"agentre/internal/service/group_svc"
)

// groupSendCode 用给定 token 发一次 group_send tools/call, 返回 HTTP 状态码
// (200=token 有效 → 落库; 401=token 无效/已吊销 → 拒绝)。
func groupSendCode(h http.Handler, token string) int {
	req := httptest.NewRequest("POST", "/mcp/group/", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_send","arguments":{"body":"x"}}}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w.Code
}

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

// TestGroupMCP_RevokeInvalidatesToken 锁住 token 生命周期(spec §17 必修):
// 成员离群 → RevokeMember; 群 stop/归档 → RevokeGroup; 吊销后该 token 的 group_send 必被拒。
func TestGroupMCP_RevokeInvalidatesToken(t *testing.T) {
	noop := func(context.Context, int64, string, []string) error { return nil }

	Convey("RevokeMember 吊销该成员 token, 不连累同群其它成员", t, func() {
		h := group_svc.NewGroupMCPForTest(noop)
		tokA := h.MintToken(5, 2) // group 5 / member 2
		tokB := h.MintToken(5, 3) // group 5 / member 3
		So(groupSendCode(h, tokA), ShouldEqual, 200)

		h.RevokeMember(2)
		So(groupSendCode(h, tokA), ShouldEqual, http.StatusUnauthorized) // 被吊销
		So(groupSendCode(h, tokB), ShouldEqual, 200)                     // 同群其它成员不受影响
	})

	Convey("RevokeGroup 吊销该群所有 token, 不连累其它群", t, func() {
		h := group_svc.NewGroupMCPForTest(noop)
		tokA := h.MintToken(5, 2)
		tokB := h.MintToken(5, 3)
		other := h.MintToken(9, 4) // 另一个群

		h.RevokeGroup(5)
		So(groupSendCode(h, tokA), ShouldEqual, http.StatusUnauthorized)
		So(groupSendCode(h, tokB), ShouldEqual, http.StatusUnauthorized)
		So(groupSendCode(h, other), ShouldEqual, 200) // 其它群 token 仍有效
	})
}
