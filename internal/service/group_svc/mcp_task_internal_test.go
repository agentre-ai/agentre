package group_svc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// postMCP 用给定 token 向 group MCP handler 发一次 JSON-RPC POST,返回 recorder。
func postMCP(h http.Handler, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp/group/", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func TestMCP_TaskTools(t *testing.T) {
	Convey("tools/list 广告全部 5 个工具", t, func() {
		h := newGroupMCP(nil)
		rr := postMCP(h, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
		for _, name := range []string{"group_send", "group_invite", "group_task_create", "group_task_complete", "group_task_cancel"} {
			So(rr.Body.String(), ShouldContainSubstring, name)
		}
	})

	Convey("group_task_create → 回调收到 assignee/title/brief/parentTaskId, 响应含 task #1 created", t, func() {
		var gotMember int64
		var gotAssignee, gotTitle, gotBrief string
		var gotParent int
		h := newGroupMCP(nil)
		h.taskCreate = func(_ context.Context, memberID int64, assignee, title, brief string, parentTaskNo int) (int, error) {
			gotMember, gotAssignee, gotTitle, gotBrief, gotParent = memberID, assignee, title, brief, parentTaskNo
			return 1, nil
		}
		tok := h.MintToken(5, 100)
		rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_task_create","arguments":{"assignee":"前端工程师","title":"登录页","brief":"做登录页,验收:e2e 通过","parentTaskId":2}}}`)
		So(rr.Code, ShouldEqual, 200)
		So(gotMember, ShouldEqual, 100)
		So(gotAssignee, ShouldEqual, "前端工程师")
		So(gotTitle, ShouldEqual, "登录页")
		So(gotBrief, ShouldEqual, "做登录页,验收:e2e 通过")
		So(gotParent, ShouldEqual, 2)
		So(rr.Body.String(), ShouldContainSubstring, "task #1 created")
	})

	Convey("group_task_complete → 回调收到 taskId/result, 响应含 task #1 completed", t, func() {
		var gotMember int64
		var gotNo int
		var gotResult string
		h := newGroupMCP(nil)
		h.taskComplete = func(_ context.Context, memberID int64, taskNo int, result string) error {
			gotMember, gotNo, gotResult = memberID, taskNo, result
			return nil
		}
		tok := h.MintToken(5, 100)
		rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_task_complete","arguments":{"taskId":1,"result":"改了 login.tsx,自测通过"}}}`)
		So(rr.Code, ShouldEqual, 200)
		So(gotMember, ShouldEqual, 100)
		So(gotNo, ShouldEqual, 1)
		So(gotResult, ShouldEqual, "改了 login.tsx,自测通过")
		So(rr.Body.String(), ShouldContainSubstring, "task #1 completed")
	})

	Convey("group_task_cancel → 回调收到 taskId/reason, 响应含 task #1 canceled(US 拼写)", t, func() {
		var gotMember int64
		var gotNo int
		var gotReason string
		h := newGroupMCP(nil)
		h.taskCancel = func(_ context.Context, memberID int64, taskNo int, reason string) error {
			gotMember, gotNo, gotReason = memberID, taskNo, reason
			return nil
		}
		tok := h.MintToken(5, 100)
		rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_task_cancel","arguments":{"taskId":1,"reason":"需求变更"}}}`)
		So(rr.Code, ShouldEqual, 200)
		So(gotMember, ShouldEqual, 100)
		So(gotNo, ShouldEqual, 1)
		So(gotReason, ShouldEqual, "需求变更")
		So(rr.Body.String(), ShouldContainSubstring, "task #1 canceled")
	})

	Convey("回调返回 error → JSON-RPC error(-32000)", t, func() {
		h := newGroupMCP(nil)
		h.taskCreate = func(context.Context, int64, string, string, string, int) (int, error) {
			return 0, errors.New("assignee not found")
		}
		tok := h.MintToken(5, 100)
		rr := postMCP(h, tok, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"group_task_create","arguments":{"assignee":"路人","title":"x","brief":"y"}}}`)
		var resp struct {
			Error struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		So(json.Unmarshal(rr.Body.Bytes(), &resp), ShouldBeNil)
		So(resp.Error.Code, ShouldEqual, -32000)
		So(resp.Error.Message, ShouldContainSubstring, "assignee not found")
	})

	Convey("newGroupSvc 装配三个任务回调(非 nil)", t, func() {
		s := newGroupSvc(nil, nil)
		So(s.mcp.taskCreate, ShouldNotBeNil)
		So(s.mcp.taskComplete, ShouldNotBeNil)
		So(s.mcp.taskCancel, ShouldNotBeNil)
	})
}
