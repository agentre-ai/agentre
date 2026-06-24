package orch_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

//go:generate mockgen -source task.go -destination mock_orch_repo/mock_task.go

// TaskRepo 编排 Task 仓储。
type TaskRepo interface {
	Create(ctx context.Context, t *orch_entity.Task) error
	Update(ctx context.Context, t *orch_entity.Task) error
	Find(ctx context.Context, id int64) (*orch_entity.Task, error)
	FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Task, error)
	ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error)
	CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error)
}

var defaultTask TaskRepo

func Task() TaskRepo             { return defaultTask }
func RegisterTask(impl TaskRepo) { defaultTask = impl }
func NewTask() TaskRepo          { return &taskRepo{} }

type taskRepo struct{}

func (r *taskRepo) Create(ctx context.Context, m *orch_entity.Task) error {
	now := time.Now().UnixMilli()
	if m.Createtime == 0 {
		m.Createtime = now
	}
	m.Updatetime = now
	return db.Ctx(ctx).Create(m).Error
}

func (r *taskRepo) Update(ctx context.Context, m *orch_entity.Task) error {
	m.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(m).Error
}

func (r *taskRepo) Find(ctx context.Context, id int64) (*orch_entity.Task, error) {
	var m orch_entity.Task
	err := db.Ctx(ctx).Where("id = ?", id).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *taskRepo) FindBySession(ctx context.Context, sessionID int64) (*orch_entity.Task, error) {
	var m orch_entity.Task
	err := db.Ctx(ctx).Where("session_id = ?", sessionID).First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *taskRepo) ListByRun(ctx context.Context, runID int64) ([]*orch_entity.Task, error) {
	var rows []*orch_entity.Task
	err := db.Ctx(ctx).Where("run_id = ?", runID).Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *taskRepo) CountByRunAgent(ctx context.Context, runID, agentID int64) (int64, error) {
	var n int64
	err := db.Ctx(ctx).Model(&orch_entity.Task{}).
		Where("run_id = ? AND agent_id = ? AND kind = ?", runID, agentID, orch_entity.TaskKindDispatch).
		Count(&n).Error
	return n, err
}
