package orch_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

//go:generate mockgen -source dispatch.go -destination mock_orch_repo/mock_dispatch.go

// DispatchRepo 编排 Dispatch 仓储。
type DispatchRepo interface {
	Create(ctx context.Context, t *orch_entity.Dispatch) error
	Update(ctx context.Context, t *orch_entity.Dispatch) error
	Find(ctx context.Context, id int64) (*orch_entity.Dispatch, error)
	FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Dispatch, error)
	ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Dispatch, error)
	CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error)
}

var defaultDispatch DispatchRepo

func Dispatch() DispatchRepo             { return defaultDispatch }
func RegisterDispatch(impl DispatchRepo) { defaultDispatch = impl }
func NewDispatch() DispatchRepo          { return &dispatchRepo{} }

type dispatchRepo struct{}

func (r *dispatchRepo) Create(ctx context.Context, m *orch_entity.Dispatch) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *dispatchRepo) Update(ctx context.Context, m *orch_entity.Dispatch) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *dispatchRepo) Find(ctx context.Context, id int64) (*orch_entity.Dispatch, error) {
	var m orch_entity.Dispatch
	err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *dispatchRepo) FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Dispatch, error) {
	var m orch_entity.Dispatch
	err := db.Ctx(ctx).Where("session_id = ?", sessionID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *dispatchRepo) ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Dispatch, error) {
	var rows []*orch_entity.Dispatch
	err := db.Ctx(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *dispatchRepo) CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error) {
	var n int64
	err := db.Ctx(ctx).Model(&orch_entity.Dispatch{}).
		Where("run_id = ? AND agent_id = ? AND kind = ?", runID, agentID, orch_entity.DispatchKindDispatch).
		Count(&n).Error
	return n, err
}
