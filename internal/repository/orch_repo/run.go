// Package orch_repo 编排 Run/Task 仓储。
package orch_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

//go:generate mockgen -source run.go -destination mock_orch_repo/mock_run.go

// RunRepo 编排 Run 仓储。
type RunRepo interface {
	Create(ctx context.Context, r *orch_entity.OrchestrationRun) error
	Update(ctx context.Context, r *orch_entity.OrchestrationRun) error
	Find(ctx context.Context, id int64) (*orch_entity.OrchestrationRun, error)
	List(ctx context.Context) ([]*orch_entity.OrchestrationRun, error)
}

var defaultRun RunRepo

func Run() RunRepo             { return defaultRun }
func RegisterRun(impl RunRepo) { defaultRun = impl }
func NewRun() RunRepo          { return &runRepo{} }

type runRepo struct{}

func (r *runRepo) Create(ctx context.Context, m *orch_entity.OrchestrationRun) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *runRepo) Update(ctx context.Context, m *orch_entity.OrchestrationRun) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *runRepo) Find(ctx context.Context, id int64) (*orch_entity.OrchestrationRun, error) {
	var m orch_entity.OrchestrationRun
	err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *runRepo) List(ctx context.Context) ([]*orch_entity.OrchestrationRun, error) {
	var rows []*orch_entity.OrchestrationRun
	err := db.Ctx(ctx).Order("updatetime DESC").Find(&rows).Error
	return rows, err
}
