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

// WorkflowPreviewRequest 设计器实时预览入参(未落库的草稿 graph + template)。
type WorkflowPreviewRequest struct {
	Name     string `json:"name"`
	Graph    string `json:"graph"`
	Template string `json:"template"`
}

// WorkflowPreviewResponse 预览结果:content=渲染后即将注入 Leader 的正文;
// outline 仅展示;error=模板 parse/execute 失败时的说明(前端展示报错态,不算 Go error)。
type WorkflowPreviewResponse struct {
	Content string   `json:"content"`
	Outline []string `json:"outline"`
	Error   string   `json:"error"`
}

// WorkflowPreviewGraph 与保存渲染同源:先投影 graph 得 DAG 提示词,再渲染用户 template。
func (a *App) WorkflowPreviewGraph(req *WorkflowPreviewRequest) (*WorkflowPreviewResponse, error) {
	content, outline, err := workflow_svc.RenderWorkflowContent(req.Name, req.Graph, req.Template)
	if err != nil {
		// 模板 parse/execute 失败经响应 Error 字段回传前端(设计器展示报错态、置灰保存),
		// 不作为 Go error——预览调用永不失败,故此处 return nil error 是有意的。
		return &WorkflowPreviewResponse{Error: err.Error()}, nil //nolint:nilerr // 错误走响应 Error 字段,见上
	}
	return &WorkflowPreviewResponse{Content: content, Outline: outline}, nil
}
