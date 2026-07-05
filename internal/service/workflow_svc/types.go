// Package workflow_svc 暴露流程库的应用服务接口与请求/响应类型。
//
// 类型定义直接被 Wails 绑定层引用,会被 wails dev / wails build 提取为 TypeScript
// 类型暴露给前端,因此字段名要稳定、json tag 要明确。
package workflow_svc

// WorkflowItem 单条流程(含使用中 Run 数,给列表/预览/删除确认用)。
type WorkflowItem struct {
	ID         int64    `json:"id"`
	Name       string   `json:"name"`
	Content    string   `json:"content"`  // 渲染产物(展示/摘要用)
	Template   string   `json:"template"` // 用户模板真源(编辑载入用)
	Tags       []string `json:"tags"`
	Outline    []string `json:"outline"`
	Graph      string   `json:"graph"`
	IsDefault  bool     `json:"isDefault"`
	RunCount   int      `json:"runCount"`
	Createtime int64    `json:"createtime"`
	Updatetime int64    `json:"updatetime"`
}

// ListWorkflowsRequest 占位。
type ListWorkflowsRequest struct{}

// ListWorkflowsResponse 全部 active 流程(repo 按 updatetime 倒序)。
type ListWorkflowsResponse struct {
	Items []*WorkflowItem `json:"items"`
}

// CreateWorkflowRequest 新建流程(name 必填,trim 后校验)。
type CreateWorkflowRequest struct {
	Name     string   `json:"name" binding:"required"`
	Template string   `json:"template"` // 用户模板;空→回落 DefaultTemplate
	Tags     []string `json:"tags"`
	Outline  []string `json:"outline"`
	Graph    string   `json:"graph,omitempty"`
}

type CreateWorkflowResponse struct {
	Item *WorkflowItem `json:"item"`
}

// UpdateWorkflowRequest 编辑流程名称/正文;进行中的群下一轮注入即取到最新正文。
type UpdateWorkflowRequest struct {
	ID       int64    `json:"id" binding:"required"`
	Name     string   `json:"name" binding:"required"`
	Template string   `json:"template"`
	Tags     []string `json:"tags"`
	Outline  []string `json:"outline"`
	Graph    string   `json:"graph,omitempty"`
}

type UpdateWorkflowResponse struct {
	Item *WorkflowItem `json:"item"`
}

// DeleteWorkflowRequest 软删流程;已绑定的群按「不绑定」处理(注入侧 IsActive 门控)。
type DeleteWorkflowRequest struct {
	ID int64 `json:"id" binding:"required"`
}

type DeleteWorkflowResponse struct{}
