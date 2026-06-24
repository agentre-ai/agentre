// Package workflow_svc 流程(剧本库)业务服务:列表/增改删,供 Wails 绑定层调用。
// 流程注入(Leader 每次 Run 启动时读取快照)在编排 runtime,不在本包。
package workflow_svc

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
)

// WorkflowSvc 流程库应用服务。
type WorkflowSvc interface {
	List(ctx context.Context, req *ListWorkflowsRequest) (*ListWorkflowsResponse, error)
	Create(ctx context.Context, req *CreateWorkflowRequest) (*CreateWorkflowResponse, error)
	Update(ctx context.Context, req *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error)
	Delete(ctx context.Context, req *DeleteWorkflowRequest) (*DeleteWorkflowResponse, error)
}

type workflowSvc struct{}

var defaultWorkflow WorkflowSvc = &workflowSvc{}

// Workflow 取默认服务单例。
func Workflow() WorkflowSvc { return defaultWorkflow }

// runCounts 统计每个流程被多少个编排 Run 引用(列表「使用中 Run 数」与删除确认提示用)。
func (s *workflowSvc) runCounts(ctx context.Context) (map[int64]int, error) {
	runs, err := orch_repo.Run().List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int)
	for _, r := range runs {
		if r.FlowID > 0 {
			counts[r.FlowID]++
		}
	}
	return counts, nil
}

func toItem(w *workflow_entity.Workflow, runCount int) *WorkflowItem {
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		RunCount:   runCount,
		Createtime: w.Createtime,
		Updatetime: w.Updatetime,
	}
}

// List 返回全部 active 流程 + 各自使用中 Run 数。
func (s *workflowSvc) List(ctx context.Context, _ *ListWorkflowsRequest) (*ListWorkflowsResponse, error) {
	rows, err := workflow_repo.Workflow().List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.runCounts(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*WorkflowItem, 0, len(rows))
	for _, w := range rows {
		items = append(items, toItem(w, counts[w.ID]))
	}
	return &ListWorkflowsResponse{Items: items}, nil
}

// findActive 取 active 流程;不存在或已软删返回 WorkflowNotFound。
func (s *workflowSvc) findActive(ctx context.Context, id int64) (*workflow_entity.Workflow, error) {
	w, err := workflow_repo.Workflow().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if !w.IsActive() {
		return nil, i18n.NewError(ctx, code.WorkflowNotFound)
	}
	return w, nil
}

// Create 新建流程。
func (s *workflowSvc) Create(ctx context.Context, req *CreateWorkflowRequest) (*CreateWorkflowResponse, error) {
	w := &workflow_entity.Workflow{
		Name:    strings.TrimSpace(req.Name),
		Content: req.Content,
		Status:  consts.ACTIVE,
	}
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Create(ctx, w); err != nil {
		return nil, err
	}
	return &CreateWorkflowResponse{Item: toItem(w, 0)}, nil
}

// Update 编辑流程名称/正文;改动对已绑定的进行中群下一轮即生效(spec §6.1)。
func (s *workflowSvc) Update(ctx context.Context, req *UpdateWorkflowRequest) (*UpdateWorkflowResponse, error) {
	w, err := s.findActive(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	w.Name = strings.TrimSpace(req.Name)
	w.Content = req.Content
	if err := w.Check(ctx); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Update(ctx, w); err != nil {
		return nil, err
	}
	counts, err := s.runCounts(ctx)
	if err != nil {
		return nil, err
	}
	return &UpdateWorkflowResponse{Item: toItem(w, counts[w.ID])}, nil
}

// Delete 软删流程;已绑定该流程的群按「不绑定」处理(注入侧跳过,不报错)。
func (s *workflowSvc) Delete(ctx context.Context, req *DeleteWorkflowRequest) (*DeleteWorkflowResponse, error) {
	if _, err := s.findActive(ctx, req.ID); err != nil {
		return nil, err
	}
	if err := workflow_repo.Workflow().Delete(ctx, req.ID); err != nil {
		return nil, err
	}
	return &DeleteWorkflowResponse{}, nil
}
