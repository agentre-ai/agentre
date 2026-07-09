package orch_svc_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
	"github.com/agentre-ai/agentre/internal/service/orch_svc/mock_orch_svc"
)

func TestMCP_ToolsList(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	tok := orch_svc.Default().MintToken(2, 500)

	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code)
	out := rw.Body.String()
	for _, name := range []string{"dispatch", "ask", "send", "finish", "agent_list", "reply"} {
		assert.Contains(t, out, `"`+name+`"`)
	}
}

func TestMCP_RejectsBadToken(t *testing.T) {
	h := orch_svc.Default().MCPHandler()
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer garbage")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	assert.Equal(t, http.StatusForbidden, rw.Code)
}

// TestMCP_ToolsCall_AllowsOrchestrationSessionEvenWhenDisabled 回归:经编排创建的会话
// (绑定了编排 Task)即便其 agent 未勾 orchestrate,tools/call(如 agent_list)也应放行 ——
// 否则注入门(turn.go)已把工具塞进去、调用门(mcp.go)却仍按 ToolEnabled 401→403,
// Leader 看得到工具却每次调用都 403,整条 Run 静默降级成它一个人干(dev sess-53 根因)。
func TestMCP_ToolsCall_AllowsOrchestrationSessionEvenWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		agentID   = int64(1)
		sessionID = int64(900)
		runID     = int64(100)
	)
	disabled := &agent_entity.Agent{ID: agentID} // orchestrate 未开

	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	agents.EXPECT().Find(gomock.Any(), agentID).Return(disabled, nil).AnyTimes()
	agents.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{disabled}, nil).AnyTimes()

	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	tasks.EXPECT().FindBySession(gomock.Any(), sessionID).
		Return(&orch_entity.Task{ID: 7, RunID: runID, SessionID: sessionID, ParentTaskID: 0}, nil).AnyTimes()

	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	runs.EXPECT().Find(gomock.Any(), runID).
		Return(&orch_entity.OrchestrationRun{ID: runID, RootTaskID: 7}, nil).AnyTimes()

	orch_svc.Default().RegisterDeps(nil, agents, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	h := orch_svc.Default().MCPHandler()
	tok := orch_svc.Default().MintToken(agentID, sessionID)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_list"}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	require.Equal(t, http.StatusOK, rw.Code, "编排会话即便 agent 未开 orchestrate 也应放行 tools/call,不该 403")
	assert.Contains(t, rw.Body.String(), `"result"`)
}

// TestMCP_ToolsCall_RejectsNonOrchestrationSessionWhenDisabled 边界护栏:普通会话(未绑定
// 编排 Task)且 agent 未勾 orchestrate → tools/call 仍应 403,确保上面的放行不是把门拆了。
func TestMCP_ToolsCall_RejectsNonOrchestrationSessionWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	const (
		agentID   = int64(1)
		sessionID = int64(901)
	)
	disabled := &agent_entity.Agent{ID: agentID} // orchestrate 未开

	agents := mock_orch_svc.NewMockAgentLookup(ctrl)
	agents.EXPECT().Find(gomock.Any(), agentID).Return(disabled, nil).AnyTimes()

	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	tasks.EXPECT().FindBySession(gomock.Any(), sessionID).Return(nil, nil).AnyTimes() // 非编排会话

	orch_svc.Default().RegisterDeps(nil, agents, nil, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	h := orch_svc.Default().MCPHandler()
	tok := orch_svc.Default().MintToken(agentID, sessionID)
	body := bytes.NewBufferString(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_list"}}`)
	req := httptest.NewRequest(http.MethodPost, "/mcp/orchestrate/", body)
	req.Header.Set("Authorization", "Bearer "+tok)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	assert.Equal(t, http.StatusForbidden, rw.Code, "普通会话+未开 orchestrate 应保持 403")
}

func TestOrchestrateToolNamesCoverSchemas(t *testing.T) {
	def, ok := agenttool.Lookup(agenttool.KeyOrchestrate)
	require.True(t, ok)
	allow := map[string]bool{}
	for _, n := range def.ToolNames {
		allow[n] = true
	}
	for _, name := range orch_svc.OrchToolSchemaNames() {
		assert.Truef(t, allow[name],
			"工具 %q 有 schema 却不在 agenttool ToolNames 白名单里"+
				"(CLI --allowedTools / codex enabled_tools / piagent allow 会拦掉,agent 调不到)", name)
	}
}
