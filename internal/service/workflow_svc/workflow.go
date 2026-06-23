// Package workflow_svc 流程(剧本库)业务服务:列表/增改删,供 Wails 绑定层调用。
// 流程注入(主持人每轮读绑定流程)在 group_svc,不在本包。
package workflow_svc

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
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

// groupCounts 统计每个流程被多少个 active 群绑定(列表「使用中群数」与删除确认提示用)。
func (s *workflowSvc) groupCounts(ctx context.Context) (map[int64]int, error) {
	groups, err := group_repo.Group().List(ctx)
	if err != nil {
		return nil, err
	}
	counts := make(map[int64]int)
	for _, g := range groups {
		if g.WorkflowID > 0 {
			counts[g.WorkflowID]++
		}
	}
	return counts, nil
}

func toItem(w *workflow_entity.Workflow, groupCount int) *WorkflowItem {
	return &WorkflowItem{
		ID:         w.ID,
		Name:       w.Name,
		Content:    w.Content,
		GroupCount: groupCount,
		Createtime: w.Createtime,
		Updatetime: w.Updatetime,
	}
}

// List 返回全部 active 流程 + 各自使用中群数。
func (s *workflowSvc) List(ctx context.Context, _ *ListWorkflowsRequest) (*ListWorkflowsResponse, error) {
	rows, err := workflow_repo.Workflow().List(ctx)
	if err != nil {
		return nil, err
	}
	counts, err := s.groupCounts(ctx)
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
	counts, err := s.groupCounts(ctx)
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
