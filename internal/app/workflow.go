package app

import (
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// WorkflowList 流程库列表(含每条流程的使用中群数)。
func (a *App) WorkflowList() (*workflow_svc.ListWorkflowsResponse, error) {
	return workflow_svc.Workflow().List(a.ctx, &workflow_svc.ListWorkflowsRequest{})
}

// WorkflowCreate 新建流程。
func (a *App) WorkflowCreate(req *workflow_svc.CreateWorkflowRequest) (*workflow_svc.CreateWorkflowResponse, error) {
	return workflow_svc.Workflow().Create(a.ctx, req)
}

// WorkflowUpdate 编辑流程(名称/正文);进行中的群下一轮即注入最新正文。
func (a *App) WorkflowUpdate(req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error) {
	return workflow_svc.Workflow().Update(a.ctx, req)
}

// WorkflowDelete 软删流程;已绑定的群按「不绑定」处理。
func (a *App) WorkflowDelete(req *workflow_svc.DeleteWorkflowRequest) (*workflow_svc.DeleteWorkflowResponse, error) {
	return workflow_svc.Workflow().Delete(a.ctx, req)
}
