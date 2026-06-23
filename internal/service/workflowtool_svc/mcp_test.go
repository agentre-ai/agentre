package workflowtool_svc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
	"github.com/agentre-ai/agentre/internal/service/workflowtool_svc/mock_workflowtool_svc"
)

// newTestSvc 构造一个全新的 workflowtoolSvc(避免 Default() 单例跨测试串台),只接
// AgentLookup 与 WorkflowQuery —— 本任务读路径只用到这两个依赖。
func newTestSvc(lookup AgentLookup, query WorkflowQuery) *workflowtoolSvc {
	s := &workflowtoolSvc{approvalTimeout: 4 * time.Minute}
	s.RegisterDeps(query, nil, lookup, nil)
	return s
}

// workflowEnabledAgent 返回一个 workflow 开关 ON 的 agent。
func workflowEnabledAgent(id int64) *agent_entity.Agent {
	a := &agent_entity.Agent{ID: id}
	a.SetTools([]agent_entity.AgentToolItem{{Key: "workflow", Enabled: true}})
	return a
}

// workflowDisabledAgent 返回一个 workflow 开关 OFF 的 agent。
func workflowDisabledAgent(id int64) *agent_entity.Agent {
	a := &agent_entity.Agent{ID: id}
	a.SetTools([]agent_entity.AgentToolItem{{Key: "workflow", Enabled: false}})
	return a
}

// rpcCall 发一次 JSON-RPC POST(可选 bearer token),返回 recorder。
func rpcCall(h http.Handler, body, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/mcp/workflow/", strings.NewReader(body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestWorkflowMCP_TokenRoundTrip(t *testing.T) {
	Convey("MintToken → lookup 解出原 (agent, session);篡改/格式非法 → 401", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_workflowtool_svc.NewMockAgentLookup(ctrl)
		query := mock_workflowtool_svc.NewMockWorkflowQuery(ctrl)
		// 合法 token 走到 workflow_list → 需要 Find + List 各一次。
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)
		query.EXPECT().List(gomock.Any(), gomock.Any()).Return(&workflow_svc.ListWorkflowsResponse{}, nil)

		s := newTestSvc(lookup, query)
		h := s.MCPHandler()
		token := s.mcpHandlerInit().MintToken(7, 99)

		Convey("合法 token → 200", func() {
			w := rpcCall(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`, token)
			So(w.Code, ShouldEqual, http.StatusOK)
		})
	})

	Convey("篡改签名 / 格式非法 token → 401(不调任何依赖)", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_workflowtool_svc.NewMockAgentLookup(ctrl) // 无 EXPECT:不该被调用
		s := newTestSvc(lookup, mock_workflowtool_svc.NewMockWorkflowQuery(ctrl))
		h := s.MCPHandler()
		good := s.mcpHandlerInit().MintToken(7, 99)

		body := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`
		So(rpcCall(h, body, good+"tampered").Code, ShouldEqual, http.StatusUnauthorized)
		So(rpcCall(h, body, "not-a-token").Code, ShouldEqual, http.StatusUnauthorized)
		So(rpcCall(h, body, "").Code, ShouldEqual, http.StatusUnauthorized)
	})
}

func TestWorkflowMCP_SwitchOffForbids(t *testing.T) {
	Convey("token 合法但 agent workflow 开关 OFF → 403,不查 workflow", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_workflowtool_svc.NewMockAgentLookup(ctrl)
		query := mock_workflowtool_svc.NewMockWorkflowQuery(ctrl) // 无 List EXPECT:不该被调用
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowDisabledAgent(7), nil)

		s := newTestSvc(lookup, query)
		h := s.MCPHandler()
		token := s.mcpHandlerInit().MintToken(7, 99)

		w := rpcCall(h, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`, token)
		So(w.Code, ShouldEqual, http.StatusForbidden)
	})

	Convey("Find 报错 / 返回 nil → 403", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_workflowtool_svc.NewMockAgentLookup(ctrl)
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(nil, nil)
		s := newTestSvc(lookup, mock_workflowtool_svc.NewMockWorkflowQuery(ctrl))
		token := s.mcpHandlerInit().MintToken(7, 99)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`, token)
		So(w.Code, ShouldEqual, http.StatusForbidden)
	})
}

func TestWorkflowMCP_DepsNotRegistered(t *testing.T) {
	Convey("bootstrap 窗口期(RegisterDeps 未执行)tools/call → 503 service unavailable,不 panic", t, func() {
		s := &workflowtoolSvc{} // 未 RegisterDeps:gateway 已挂 handler 但 deps 还没接线
		token := s.mcpHandlerInit().MintToken(7, 99)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`, token)
		So(w.Code, ShouldEqual, http.StatusServiceUnavailable)
		So(w.Body.String(), ShouldContainSubstring, "service unavailable")
	})
}

func TestWorkflowMCP_InitializeAndToolsList(t *testing.T) {
	Convey("initialize 回显 protocolVersion + serverInfo(agentre-workflow)", t, func() {
		s := newTestSvc(nil, nil)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25"}}`, "")
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "2025-11-25")
		So(w.Body.String(), ShouldContainSubstring, "agentre-workflow")
	})

	Convey("tools/list 暴露全部 4 个工具名", t, func() {
		s := newTestSvc(nil, nil)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, "")
		So(w.Code, ShouldEqual, http.StatusOK)
		body := w.Body.String()
		for _, name := range []string{
			"workflow_list",
			"workflow_create", "workflow_update", "workflow_delete",
		} {
			So(body, ShouldContainSubstring, name)
		}
		// 写工具描述注明需审批
		So(body, ShouldContainSubstring, "需要用户审批")
	})
}

func TestWorkflowMCP_ListReturnsProjection(t *testing.T) {
	Convey("workflow_list → content[0].text 含 workflows 数组，含 name/groupCount/content", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		lookup := mock_workflowtool_svc.NewMockAgentLookup(ctrl)
		query := mock_workflowtool_svc.NewMockWorkflowQuery(ctrl)
		lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)
		query.EXPECT().List(gomock.Any(), gomock.Any()).Return(&workflow_svc.ListWorkflowsResponse{
			Items: []*workflow_svc.WorkflowItem{
				{ID: 1, Name: "新员工入职", GroupCount: 3, Content: "步骤一:准备材料"},
				{ID: 2, Name: "代码评审", GroupCount: 1, Content: "步骤一:提交 PR"},
			},
		}, nil)

		s := newTestSvc(lookup, query)
		token := s.mcpHandlerInit().MintToken(7, 99)
		w := rpcCall(s.MCPHandler(), `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_list"}}`, token)

		So(w.Code, ShouldEqual, http.StatusOK)
		body := w.Body.String()
		So(body, ShouldContainSubstring, "workflows")
		So(body, ShouldContainSubstring, "新员工入职")
		So(body, ShouldContainSubstring, "代码评审")
		So(body, ShouldContainSubstring, "groupCount")
		So(body, ShouldContainSubstring, "步骤一")
		So(body, ShouldContainSubstring, `"type":"text"`)
	})
}
