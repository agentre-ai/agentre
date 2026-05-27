// Package project_repo 提供 Project 的持久化访问。
package project_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/project_entity"
)

//go:generate mockgen -source project.go -destination mock_project_repo/mock_project.go

// ProjectRepo Project 仓储接口（消费方约束模式）。
type ProjectRepo interface {
	Create(ctx context.Context, p *project_entity.Project) error
	Update(ctx context.Context, p *project_entity.Project) error
	Find(ctx context.Context, id int64) (*project_entity.Project, error)
	FindByName(ctx context.Context, parentID int64, name string) (*project_entity.Project, error)
	List(ctx context.Context) ([]*project_entity.Project, error)
	ListByParent(ctx context.Context, parentID int64) ([]*project_entity.Project, error)
	HasActiveChildren(ctx context.Context, id int64) (bool, error)
	Delete(ctx context.Context, id int64) error
}

var defaultProject ProjectRepo

// Project 取默认仓储单例。
func Project() ProjectRepo { return defaultProject }

// RegisterProject 注入仓储实现，由 bootstrap 调用一次。
func RegisterProject(impl ProjectRepo) { defaultProject = impl }

// NewProject 构造默认 GORM 实现。
func NewProject() ProjectRepo { return &projectRepo{} }

type projectRepo struct{}

func (r *projectRepo) Create(ctx context.Context, p *project_entity.Project) error {
	now := time.Now().UnixMilli()
	if p.Createtime == 0 {
		p.Createtime = now
	}
	p.Updatetime = now
	return db.Ctx(ctx).Create(p).Error
}

func (r *projectRepo) Update(ctx context.Context, p *project_entity.Project) error {
	p.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Save(p).Error
}

func (r *projectRepo) Find(ctx context.Context, id int64) (*project_entity.Project, error) {
	out := &project_entity.Project{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FindByName 同父项目下的项目名唯一。parentID = 0 表示顶级。
func (r *projectRepo) FindByName(ctx context.Context, parentID int64, name string) (*project_entity.Project, error) {
	out := &project_entity.Project{}
	err := db.Ctx(ctx).
		Where("parent_id = ? AND name = ? AND status = ?", parentID, name, consts.ACTIVE).
		First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

// List 返回全部 active 项目，order 保证父先于子（id 升序即可，业务层组装树）。
func (r *projectRepo) List(ctx context.Context) ([]*project_entity.Project, error) {
	var rows []*project_entity.Project
	err := db.Ctx(ctx).
		Where("status = ?", consts.ACTIVE).
		Order("parent_id ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *projectRepo) ListByParent(ctx context.Context, parentID int64) ([]*project_entity.Project, error) {
	var rows []*project_entity.Project
	err := db.Ctx(ctx).
		Where("parent_id = ? AND status = ?", parentID, consts.ACTIVE).
		Order("id ASC").
		Find(&rows).Error
	return rows, err
}

// HasActiveChildren 删除项目前的预检 —— 有 active 子项目时拒绝。
func (r *projectRepo) HasActiveChildren(ctx context.Context, id int64) (bool, error) {
	var n int64
	err := db.Ctx(ctx).
		Model(&project_entity.Project{}).
		Where("parent_id = ? AND status = ?", id, consts.ACTIVE).
		Count(&n).Error
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// Delete 软删 —— 把 status 置为 DELETE，文件系统不动。
func (r *projectRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&project_entity.Project{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     consts.DELETE,
			"updatetime": time.Now().UnixMilli(),
		}).Error
}
