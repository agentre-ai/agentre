package app

import (
	"github.com/agentre-ai/agentre/internal/service/workflow_svc"
)

// WorkflowList 流程库列表(含每条流程的使用中 Run 数)。
func (a *App) WorkflowList() (*workflow_svc.ListWorkflowsResponse, error) {
	return workflow_svc.Workflow().List(a.ctx, &workflow_svc.ListWorkflowsRequest{})
}

// WorkflowCreate 新建流程。
func (a *App) WorkflowCreate(req *workflow_svc.CreateWorkflowRequest) (*workflow_svc.CreateWorkflowResponse, error) {
	return workflow_svc.Workflow().Create(a.ctx, req)
}

// WorkflowUpdate 编辑流程(名称/正文);进行中的 Run 下一轮即注入最新正文。
func (a *App) WorkflowUpdate(req *workflow_svc.UpdateWorkflowRequest) (*workflow_svc.UpdateWorkflowResponse, error) {
	return workflow_svc.Workflow().Update(a.ctx, req)
}

// WorkflowDelete 软删流程;已绑定的 Run 按「不绑定」处理。
func (a *App) WorkflowDelete(req *workflow_svc.DeleteWorkflowRequest) (*workflow_svc.DeleteWorkflowResponse, error) {
	return workflow_svc.Workflow().Delete(a.ctx, req)
}

// WorkflowPreviewRequest 设计器实时预览入参（未落库的草稿 graph）。
type WorkflowPreviewRequest struct {
	Name  string `json:"name"`
	Graph string `json:"graph"`
}

// WorkflowPreviewResponse 投影结果（content 即将注入 Leader 的正文；outline 仅展示）。
type WorkflowPreviewResponse struct {
	Content string   `json:"content"`
	Outline []string `json:"outline"`
}

// WorkflowPreviewGraph 把草稿 graph 投影成正文/大纲，供 DAG 设计器实时预览（投影只有后端一份实现）。
func (a *App) WorkflowPreviewGraph(req *WorkflowPreviewRequest) (*WorkflowPreviewResponse, error) {
	g, ok := workflow_svc.ParseFlowGraph(req.Graph)
	if !ok {
		return &WorkflowPreviewResponse{}, nil
	}
	content, outline := workflow_svc.ProjectGraph(req.Name, g)
	return &WorkflowPreviewResponse{Content: content, Outline: outline}, nil
}
