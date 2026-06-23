package group_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
)

//go:generate mockgen -source task.go -destination mock_group_repo/mock_task.go

// GroupTaskRepo 群任务卡仓储。
type GroupTaskRepo interface {
	Create(ctx context.Context, t *group_entity.GroupTask) error
	Update(ctx context.Context, t *group_entity.GroupTask) error
	FindByGroupAndNo(ctx context.Context, groupID int64, taskNo int) (*group_entity.GroupTask, error)
	ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupTask, error)
	NextTaskNo(ctx context.Context, groupID int64) (int, error)
}

var defaultTask GroupTaskRepo

func Task() GroupTaskRepo             { return defaultTask }
func RegisterTask(impl GroupTaskRepo) { defaultTask = impl }
func NewTask() GroupTaskRepo          { return &taskRepo{} }

type taskRepo struct{}

func (r *taskRepo) Create(ctx context.Context, t *group_entity.GroupTask) error {
	now := time.Now().UnixMilli()
	if t.Createtime == 0 {
		t.Createtime = now
	}
	t.Updatetime = now
	return db.Ctx(ctx).Create(t).Error
}

func (r *taskRepo) Update(ctx context.Context, t *group_entity.GroupTask) error {
	t.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(t).Error
}

func (r *taskRepo) FindByGroupAndNo(ctx context.Context, groupID int64, taskNo int) (*group_entity.GroupTask, error) {
	var t group_entity.GroupTask
	err := db.Ctx(ctx).Where("group_id = ? AND task_no = ?", groupID, taskNo).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *taskRepo) ListByGroup(ctx context.Context, groupID int64) ([]*group_entity.GroupTask, error) {
	var rows []*group_entity.GroupTask
	err := db.Ctx(ctx).Where("group_id = ?", groupID).Order("task_no ASC").Find(&rows).Error
	return rows, err
}

// NextTaskNo 读取-递增非原子:调用方须按群串行(group_svc ingestMu);
// uniq_group_tasks_group_no 唯一索引兜底。
func (r *taskRepo) NextTaskNo(ctx context.Context, groupID int64) (int, error) {
	var maxNo int
	err := db.Ctx(ctx).Model(&group_entity.GroupTask{}).
		Where("group_id = ?", groupID).
		Select("COALESCE(MAX(task_no), 0)").Scan(&maxNo).Error
	if err != nil {
		return 0, err
	}
	return maxNo + 1, nil
}
