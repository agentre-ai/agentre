// Package department_repo 提供部门的持久化访问。
package department_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/department_entity"
)

//go:generate mockgen -source department.go -destination mock_department_repo/mock_department.go

// DepartmentRepo 部门仓储。
type DepartmentRepo interface {
	Create(ctx context.Context, d *department_entity.Department) error
	Update(ctx context.Context, d *department_entity.Department) error
	Find(ctx context.Context, id int64) (*department_entity.Department, error)
	FindByName(ctx context.Context, name string, parentID int64) (*department_entity.Department, error)
	List(ctx context.Context) ([]*department_entity.Department, error)
	ListByParent(ctx context.Context, parentID int64) ([]*department_entity.Department, error)
	NextSortOrder(ctx context.Context, parentID int64) (int, error)
	Delete(ctx context.Context, id int64) error
	ReparentChildren(ctx context.Context, fromID, toParentID int64) error
}

var defaultDepartment DepartmentRepo

// Department 取默认仓储单例。
func Department() DepartmentRepo { return defaultDepartment }

// RegisterDepartment 注入仓储实现，由 bootstrap 调用一次。
func RegisterDepartment(impl DepartmentRepo) { defaultDepartment = impl }

type departmentRepo struct{}

// NewDepartment 构造默认 GORM 实现。
func NewDepartment() DepartmentRepo { return &departmentRepo{} }

func (r *departmentRepo) Create(ctx context.Context, d *department_entity.Department) error {
	return db.Ctx(ctx).Create(d).Error
}

func (r *departmentRepo) Update(ctx context.Context, d *department_entity.Department) error {
	return db.Ctx(ctx).Save(d).Error
}

func (r *departmentRepo) Find(ctx context.Context, id int64) (*department_entity.Department, error) {
	out := &department_entity.Department{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *departmentRepo) FindByName(ctx context.Context, name string, parentID int64) (*department_entity.Department, error) {
	out := &department_entity.Department{}
	err := db.Ctx(ctx).
		Where("name = ? AND parent_id = ? AND status = ?", name, parentID, consts.ACTIVE).
		First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *departmentRepo) List(ctx context.Context) ([]*department_entity.Department, error) {
	var rows []*department_entity.Department
	if err := db.Ctx(ctx).
		Where("status = ?", consts.ACTIVE).
		Order("parent_id ASC, sort_order ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *departmentRepo) ListByParent(ctx context.Context, parentID int64) ([]*department_entity.Department, error) {
	var rows []*department_entity.Department
	if err := db.Ctx(ctx).
		Where("parent_id = ? AND status = ?", parentID, consts.ACTIVE).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *departmentRepo) NextSortOrder(ctx context.Context, parentID int64) (int, error) {
	var maxOrder int
	err := db.Ctx(ctx).Table("departments").
		Where("parent_id = ? AND status = ?", parentID, consts.ACTIVE).
		Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (r *departmentRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&department_entity.Department{}).
		Where("id = ?", id).
		Update("status", consts.DELETE).Error
}

func (r *departmentRepo) ReparentChildren(ctx context.Context, fromID, toParentID int64) error {
	return db.Ctx(ctx).Model(&department_entity.Department{}).
		Where("parent_id = ? AND status = ?", fromID, consts.ACTIVE).
		Update("parent_id", toParentID).Error
}
