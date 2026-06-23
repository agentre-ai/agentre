package workflowtool_svc

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

type workflowtoolSvc struct {
	mcp             *workflowMCP
	mcpOnce         sync.Once
	gatewayBaseURL  string
	approvalTimeout time.Duration

	query    WorkflowQuery
	command  WorkflowCommand
	lookup   AgentLookup
	approval ApprovalGateway
}

var defaultWorkflowtool = &workflowtoolSvc{approvalTimeout: 4 * time.Minute} // spike 实测 CLI 硬顶 ~285s,留 25s 余量

// Default 取默认服务单例。
func Default() *workflowtoolSvc { return defaultWorkflowtool }

// RegisterDeps bootstrap 接线(生产传 workflow_svc.Workflow()/workflow_svc.Workflow()/
// agent_repo.Agent()/chat_svc.Chat());测试注 mock。
func (s *workflowtoolSvc) RegisterDeps(q WorkflowQuery, c WorkflowCommand, l AgentLookup, ap ApprovalGateway) {
	s.query, s.command, s.lookup, s.approval = q, c, l, ap
}

// mcpHandlerInit 懒初始化 workflowMCP(per-process HMAC secret 在首次访问时生成)。
func (s *workflowtoolSvc) mcpHandlerInit() *workflowMCP {
	s.mcpOnce.Do(func() { s.mcp = newWorkflowMCP(s) })
	return s.mcp
}

// MCPHandler 返回挂到 gateway /mcp/workflow/ 的 HTTP handler。
func (s *workflowtoolSvc) MCPHandler() http.Handler { return s.mcpHandlerInit() }

// SetGatewayBaseURL 由 bootstrap 在 gateway 起好后注入(用于拼 MCP server URL)。
func (s *workflowtoolSvc) SetGatewayBaseURL(u string) { s.gatewayBaseURL = u }

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider:agent 开启 workflow 工具时返回注入 spec。
func (s *workflowtoolSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID int64, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeyWorkflow) || s.gatewayBaseURL == "" {
		return nil
	}
	def, ok := agenttool.Lookup(agenttool.KeyWorkflow)
	if !ok {
		return nil
	}
	return []agentruntime.MCPServerSpec{{
		Name:    def.Key,
		URL:     s.gatewayBaseURL + def.MCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + s.mcpHandlerInit().MintToken(a.ID, sessionID)},
		Tools:   def.ToolNames,
	}}
}
