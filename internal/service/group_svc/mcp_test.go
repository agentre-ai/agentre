package group_svc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/service/group_svc"
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

// TestGroupMCP_AuthzGatesToolCall 锁住发言权门控:token 验签通过后, 还要过 authz(按 DB 成员
// 资格判定)才放行 group_send。失权(离群 / 群归档)→ 403 且不调 ingest;有发言权 → 200。
// 取代旧的内存 token 吊销(无状态 token 不再 delete;吊销语义转移到 authz)。
func TestGroupMCP_AuthzGatesToolCall(t *testing.T) {
	noop := func(context.Context, int64, string, []string) error { return nil }

	Convey("无发言权(authz=false)的 token → 403, 不调 ingest", t, func() {
		called := false
		h := group_svc.NewGroupMCPForTestWithAuthz(
			func(context.Context, int64, string, []string) error { called = true; return nil },
			func(context.Context, int64, int64) bool { return false },
		)
		So(groupSendCode(h, h.MintToken(5, 2)), ShouldEqual, http.StatusForbidden)
		So(called, ShouldBeFalse)
	})

	Convey("有发言权(authz=true)的 token → 200", t, func() {
		h := group_svc.NewGroupMCPForTestWithAuthz(noop, func(context.Context, int64, int64) bool { return true })
		So(groupSendCode(h, h.MintToken(5, 2)), ShouldEqual, 200)
	})

	Convey("authz 收到 token 解出的 (group, member)", t, func() {
		var gotG, gotM int64
		h := group_svc.NewGroupMCPForTestWithAuthz(noop, func(_ context.Context, g, m int64) bool {
			gotG, gotM = g, m
			return true
		})
		So(groupSendCode(h, h.MintToken(5, 2)), ShouldEqual, 200)
		So(gotG, ShouldEqual, 5)
		So(gotM, ShouldEqual, 2)
	})

	Convey("无状态 token 跨实例不通用(secret 各进程独立)", t, func() {
		h1 := group_svc.NewGroupMCPForTest(noop)
		h2 := group_svc.NewGroupMCPForTest(noop)
		tok := h1.MintToken(5, 2)
		So(groupSendCode(h1, tok), ShouldEqual, 200)                     // 自家签的验得过
		So(groupSendCode(h2, tok), ShouldEqual, http.StatusUnauthorized) // 别家 secret 验不过
	})
}
