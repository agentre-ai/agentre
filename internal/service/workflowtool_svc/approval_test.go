package workflowtool_svc

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/mock/gomock"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
	"github.com/agentre-ai/agentre/internal/service/workflowtool_svc/mock_workflowtool_svc"
)

// writeDeps 写工具测试所需的全部 mock 依赖。
type writeDeps struct {
	lookup  *mock_workflowtool_svc.MockAgentLookup
	query   *mock_workflowtool_svc.MockWorkflowQuery
	command *mock_workflowtool_svc.MockWorkflowCommand
	apv     *mock_workflowtool_svc.MockApprovalGateway
}

// newWriteSvc 构造一个全新的 workflowtoolSvc(不碰 Default() 单例),接齐写工具审批所需的全部
// 依赖。approvalTimeout 默认给 4min,需要超时用例的测试自己改小。
func newWriteSvc(ctrl *gomock.Controller) (*workflowtoolSvc, *writeDeps) {
	d := &writeDeps{
		lookup:  mock_workflowtool_svc.NewMockAgentLookup(ctrl),
		query:   mock_workflowtool_svc.NewMockWorkflowQuery(ctrl),
		command: mock_workflowtool_svc.NewMockWorkflowCommand(ctrl),
		apv:     mock_workflowtool_svc.NewMockApprovalGateway(ctrl),
	}
	s := &workflowtoolSvc{approvalTimeout: 4 * time.Minute}
	s.RegisterDeps(d.query, d.command, d.lookup, d.apv)
	return s, d
}

// beginCh 让 BeginToolApproval mock 返回测试持有的审批 channel(buffered=1)——往里 push
// true/false 即模拟前端经 chat_svc.AnswerToolApproval 唤醒。不 push 则触发超时分支。
func beginCh(d *writeDeps, sessionID int64) chan bool {
	apvCh := make(chan bool, 1)
	d.apv.EXPECT().
		BeginToolApproval(gomock.Any(), sessionID, gomock.Any()).
		Return((<-chan bool)(apvCh), nil)
	return apvCh
}

// callWrite 在 goroutine 里发一次写工具 tools/call(handler 会同步挂起),返回 recorder
// 与一个 done channel(请求返回时关闭)。
func callWrite(s *workflowtoolSvc, body, token string) (*httptest.ResponseRecorder, <-chan struct{}) {
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		req := httptest.NewRequest("POST", "/mcp/workflow/", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		s.MCPHandler().ServeHTTP(w, req)
		close(done)
	}()
	return w, done
}

func TestWorkflowApproval_ApprovedExecutes(t *testing.T) {
	Convey("写工具挂起 → 应答 allow=true → Create 成功 → result 含成功文案;Begin/Finish(approved) 均调到", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		apvCh := make(chan bool, 1)
		var mu sync.Mutex
		var gotToolKey string
		d.apv.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).
			DoAndReturn(func(_ any, _ int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error) {
				mu.Lock()
				gotToolKey = blk.ToolKey
				mu.Unlock()
				return apvCh, nil
			})
		d.command.EXPECT().Create(gomock.Any(), gomock.Any()).Return(
			&workflow_svc.CreateWorkflowResponse{Item: &workflow_svc.WorkflowItem{ID: 5, Name: "新员工入职"}}, nil)
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_create","arguments":{"name":"新员工入职"}}}`, token)

		apvCh <- true
		<-done

		mu.Lock()
		So(gotToolKey, ShouldEqual, "workflow")
		mu.Unlock()
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "新员工入职")
		So(w.Body.String(), ShouldContainSubstring, "id=5")
		So(w.Body.String(), ShouldContainSubstring, "已创建流程")
	})
}

func TestWorkflowApproval_ApprovedButExecError(t *testing.T) {
	Convey("批准但 exec 业务错 → Finish(approved, 执行失败...) + result 含「已批准但执行失败」", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		apvCh := beginCh(d, 99)
		d.command.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil, assertErr("流程名重复"))

		var mu sync.Mutex
		var finishResult string
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).
			DoAndReturn(func(_ any, _ int64, _, _, result string) error {
				mu.Lock()
				finishResult = result
				mu.Unlock()
				return nil
			})

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_create","arguments":{"name":"重名"}}}`, token)

		apvCh <- true
		<-done

		mu.Lock()
		defer mu.Unlock()
		So(finishResult, ShouldContainSubstring, "执行失败")
		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "已批准但执行失败")
	})
}

func TestWorkflowApproval_Denied(t *testing.T) {
	Convey("应答 allow=false → Finish(denied) + result 含「用户拒绝」;不 exec", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		apvCh := beginCh(d, 99)
		// 无 command.Create EXPECT:拒绝不该执行
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "denied", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_create","arguments":{"name":"x"}}}`, token)

		apvCh <- false
		<-done

		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "用户拒绝")
	})
}

func TestWorkflowApproval_Timeout(t *testing.T) {
	Convey("不应答 → 超时 → Finish(expired) + result 含「审批超时」", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		s.approvalTimeout = 50 * time.Millisecond
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		beginCh(d, 99) // 返回 channel 但故意不 push,触发超时分支
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "expired", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_create","arguments":{"name":"x"}}}`, token)

		<-done

		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "审批超时")
	})
}

func TestWorkflowApproval_BeginFails(t *testing.T) {
	Convey("BeginToolApproval 报错(无活跃 turn)→ rpc error「审批通道不可用」,无挂起", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)
		d.apv.EXPECT().BeginToolApproval(gomock.Any(), int64(99), gomock.Any()).Return((<-chan bool)(nil), assertErr("no active turn"))
		// 无 Finish / 无 exec EXPECT

		token := s.mcpHandlerInit().MintToken(7, 99)
		req := httptest.NewRequest("POST", "/mcp/workflow/", strings.NewReader(
			`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_create","arguments":{"name":"x"}}}`))
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		s.MCPHandler().ServeHTTP(w, req)

		So(w.Code, ShouldEqual, http.StatusOK)
		So(w.Body.String(), ShouldContainSubstring, "审批通道不可用")
		So(w.Body.String(), ShouldContainSubstring, "error")
	})
}

func TestWorkflowApproval_UpdateMerge(t *testing.T) {
	Convey("workflow_update 只传 name → loadWorkflow(mock List)补 content → Update 收到合并值", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		// loadWorkflow 内部调 query.List 找现值
		d.query.EXPECT().List(gomock.Any(), gomock.Any()).Return(&workflow_svc.ListWorkflowsResponse{
			Items: []*workflow_svc.WorkflowItem{
				{ID: 3, Name: "旧名称", Content: "步骤一:原始内容", GroupCount: 2},
			},
		}, nil)
		apvCh := beginCh(d, 99)

		var mu sync.Mutex
		var updateReq *workflow_svc.UpdateWorkflowRequest
		d.command.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ any, req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error) {
				mu.Lock()
				updateReq = req
				mu.Unlock()
				return &workflow_svc.UpdateWorkflowResponse{Item: &workflow_svc.WorkflowItem{ID: 3, Name: "新名称"}}, nil
			})
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		// 只传 name,不传 content → content 应从 loadWorkflow 补回
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_update","arguments":{"id":3,"name":"新名称"}}}`, token)
		apvCh <- true
		<-done

		So(w.Code, ShouldEqual, http.StatusOK)
		mu.Lock()
		defer mu.Unlock()
		So(updateReq.ID, ShouldEqual, 3)
		So(updateReq.Name, ShouldEqual, "新名称")         // 显式传入的
		So(updateReq.Content, ShouldEqual, "步骤一:原始内容") // 沿用现值
	})
}

func TestWorkflowApproval_DeleteMessage(t *testing.T) {
	Convey("workflow_delete → result 文案含原 GroupCount", t, func() {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()
		s, d := newWriteSvc(ctrl)
		d.lookup.EXPECT().Find(gomock.Any(), int64(7)).Return(workflowEnabledAgent(7), nil)

		// loadWorkflow 取现值(含 GroupCount=5)
		d.query.EXPECT().List(gomock.Any(), gomock.Any()).Return(&workflow_svc.ListWorkflowsResponse{
			Items: []*workflow_svc.WorkflowItem{
				{ID: 4, Name: "待删流程", GroupCount: 5, Content: "内容"},
			},
		}, nil)
		apvCh := beginCh(d, 99)

		d.command.EXPECT().Delete(gomock.Any(), gomock.Any()).Return(&workflow_svc.DeleteWorkflowResponse{}, nil)
		d.apv.EXPECT().FinishToolApproval(gomock.Any(), int64(99), gomock.Any(), "approved", gomock.Any()).Return(nil)

		token := s.mcpHandlerInit().MintToken(7, 99)
		w, done := callWrite(s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"workflow_delete","arguments":{"id":4}}}`, token)
		apvCh <- true
		<-done

		So(w.Code, ShouldEqual, http.StatusOK)
		body := w.Body.String()
		So(body, ShouldContainSubstring, "已删除流程")
		So(body, ShouldContainSubstring, "待删流程")
		// result 应含原 GroupCount
		So(body, ShouldContainSubstring, "5")
	})
}

// assertErr 是测试用的简单 error。
type assertErr string

func (e assertErr) Error() string { return string(e) }
