// Package workflow_repo 流程(剧本库)仓储。
package workflow_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
)

//go:generate mockgen -source workflow.go -destination mock_workflow_repo/mock_workflow.go

// WorkflowRepo 流程仓储。
type WorkflowRepo interface {
	Create(ctx context.Context, w *workflow_entity.Workflow) error
	Update(ctx context.Context, w *workflow_entity.Workflow) error
	Find(ctx context.Context, id int64) (*workflow_entity.Workflow, error)
	List(ctx context.Context) ([]*workflow_entity.Workflow, error)
	Delete(ctx context.Context, id int64) error
}

var defaultWorkflow WorkflowRepo

func Workflow() WorkflowRepo             { return defaultWorkflow }
func RegisterWorkflow(impl WorkflowRepo) { defaultWorkflow = impl }
func NewWorkflow() WorkflowRepo          { return &workflowRepo{} }

type workflowRepo struct{}

func (r *workflowRepo) Create(ctx context.Context, w *workflow_entity.Workflow) error {
	now := time.Now().UnixMilli()
	if w.Createtime == 0 {
		w.Createtime = now
	}
	w.Updatetime = now
	return db.Ctx(ctx).Create(w).Error
}

func (r *workflowRepo) Update(ctx context.Context, w *workflow_entity.Workflow) error {
	w.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(w).Error
}

// Find 按 id 查询,不过滤软删——调用方须用 wf.IsActive() 门控(如提示注入)。
func (r *workflowRepo) Find(ctx context.Context, id int64) (*workflow_entity.Workflow, error) {
	var w workflow_entity.Workflow
	err := db.Ctx(ctx).Where("id = ?", id).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *workflowRepo) List(ctx context.Context) ([]*workflow_entity.Workflow, error) {
	var rows []*workflow_entity.Workflow
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("updatetime DESC").Find(&rows).Error
	return rows, err
}

// Delete 软删(status=DELETE)，已绑定该流程的群按「不绑定」处理(注入侧以 wf.IsActive() 门控跳过)。
func (r *workflowRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&workflow_entity.Workflow{}).
		Where("id = ?", id).
		Updates(map[string]any{"status": consts.DELETE, "updatetime": time.Now().UnixMilli()}).Error
}
