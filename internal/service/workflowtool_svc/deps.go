// Package workflowtool_svc 流程管理工具(agent 内置工具 key="workflow")的 MCP 接入与审批编排。
// 业务执行委托 workflow_svc,本包只做 token/开关校验 + 审批挂起。
package workflowtool_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc/blocks"
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

//go:generate mockgen -source deps.go -destination mock_workflowtool_svc/mock_deps.go

// WorkflowQuery 读流程(workflow_svc.List 的窄投影)。
type WorkflowQuery interface {
	List(ctx context.Context, req *workflow_svc.ListWorkflowsRequest) (*workflow_svc.ListWorkflowsResponse, error)
}

// WorkflowCommand 流程写操作(workflow_svc 的窄投影)。
type WorkflowCommand interface {
	Create(ctx context.Context, req *workflow_svc.CreateWorkflowRequest) (*workflow_svc.CreateWorkflowResponse, error)
	Update(ctx context.Context, req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error)
	Delete(ctx context.Context, req *workflow_svc.DeleteWorkflowRequest) (*workflow_svc.DeleteWorkflowResponse, error)
}

// AgentLookup 实时校验调用者 agent 的工具开关(agent_repo 的窄投影)。
type AgentLookup interface {
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
}

// ApprovalGateway 通用工具审批(chat_svc 的窄投影)。
type ApprovalGateway interface {
	BeginToolApproval(ctx context.Context, sessionID int64, blk *blocks.ToolApprovalBlock) (<-chan bool, error)
	FinishToolApproval(ctx context.Context, sessionID int64, requestID, status, result string) error
}
