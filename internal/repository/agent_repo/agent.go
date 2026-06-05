// Package agent_repo 提供 Agent 的持久化访问。
package agent_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/agent_entity"
)

//go:generate mockgen -source agent.go -destination mock_agent_repo/mock_agent.go

type AgentRepo interface {
	Create(ctx context.Context, a *agent_entity.Agent) error
	Update(ctx context.Context, a *agent_entity.Agent) error
	Find(ctx context.Context, id int64) (*agent_entity.Agent, error)
	FindByName(ctx context.Context, name string) (*agent_entity.Agent, error)
	FindSystem(ctx context.Context) (*agent_entity.Agent, error)
	List(ctx context.Context) ([]*agent_entity.Agent, error)
	ListByDepartment(ctx context.Context, departmentID int64) ([]*agent_entity.Agent, error)
	ListByParent(ctx context.Context, parentAgentID int64) ([]*agent_entity.Agent, error)
	ListByBackend(ctx context.Context, backendID int64) ([]*agent_entity.Agent, error)
	CountByBackends(ctx context.Context, backendIDs []int64) (map[int64]int64, error)
	NextSortOrder(ctx context.Context, departmentID int64) (int, error)
	NextSortOrderByParent(ctx context.Context, parentAgentID int64) (int, error)
	UpdateDepartment(ctx context.Context, id, departmentID int64, sortOrder int) error
	UpdatePlacement(ctx context.Context, id, departmentID, parentAgentID int64, sortOrder int) error
	UpdateAvatar(ctx context.Context, id int64, avatarDataURL string, updatetime int64) error
	SetPinned(ctx context.Context, id int64, pinned bool) error
	ReparentChildren(ctx context.Context, fromParentAgentID, toDepartmentID, toParentAgentID int64) error
	ClearLeadOfDepartment(ctx context.Context, agentID int64) error
	Delete(ctx context.Context, id int64) error
	DeleteByDepartment(ctx context.Context, departmentID int64) error
}

var defaultAgent AgentRepo

func Agent() AgentRepo             { return defaultAgent }
func RegisterAgent(impl AgentRepo) { defaultAgent = impl }
func NewAgent() AgentRepo          { return &agentRepo{} }

type agentRepo struct{}

func (r *agentRepo) Create(ctx context.Context, a *agent_entity.Agent) error {
	return db.Ctx(ctx).Create(a).Error
}

func (r *agentRepo) Update(ctx context.Context, a *agent_entity.Agent) error {
	return db.Ctx(ctx).Save(a).Error
}

func (r *agentRepo) Find(ctx context.Context, id int64) (*agent_entity.Agent, error) {
	out := &agent_entity.Agent{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *agentRepo) FindByName(ctx context.Context, name string) (*agent_entity.Agent, error) {
	out := &agent_entity.Agent{}
	err := db.Ctx(ctx).Where("name = ? AND status = ?", name, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *agentRepo) FindSystem(ctx context.Context) (*agent_entity.Agent, error) {
	out := &agent_entity.Agent{}
	err := db.Ctx(ctx).
		Where("system_badge = ? AND status = ?", agent_entity.SystemBadgeDefault, consts.ACTIVE).
		First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *agentRepo) List(ctx context.Context) ([]*agent_entity.Agent, error) {
	var rows []*agent_entity.Agent
	err := db.Ctx(ctx).
		Where("status = ?", consts.ACTIVE).
		Order("department_id ASC, parent_agent_id ASC, sort_order ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *agentRepo) ListByDepartment(ctx context.Context, departmentID int64) ([]*agent_entity.Agent, error) {
	var rows []*agent_entity.Agent
	err := db.Ctx(ctx).
		Where("department_id = ? AND parent_agent_id = ? AND status = ?", departmentID, int64(0), consts.ACTIVE).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *agentRepo) ListByParent(ctx context.Context, parentAgentID int64) ([]*agent_entity.Agent, error) {
	var rows []*agent_entity.Agent
	err := db.Ctx(ctx).
		Where("parent_agent_id = ? AND status = ?", parentAgentID, consts.ACTIVE).
		Order("sort_order ASC, id ASC").
		Find(&rows).Error
	return rows, err
}

func (r *agentRepo) ListByBackend(ctx context.Context, backendID int64) ([]*agent_entity.Agent, error) {
	var rows []*agent_entity.Agent
	err := db.Ctx(ctx).
		Where("agent_backend_id = ? AND status = ?", backendID, consts.ACTIVE).
		Find(&rows).Error
	return rows, err
}

func (r *agentRepo) CountByBackends(ctx context.Context, backendIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(backendIDs))
	if len(backendIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		AgentBackendID int64 `gorm:"column:agent_backend_id"`
		Cnt            int64 `gorm:"column:cnt"`
	}
	err := db.Ctx(ctx).Table("agents").
		Select("agent_backend_id, COUNT(*) AS cnt").
		Where("agent_backend_id IN ? AND status = ?", backendIDs, consts.ACTIVE).
		Group("agent_backend_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.AgentBackendID] = row.Cnt
	}
	return out, nil
}

func (r *agentRepo) NextSortOrder(ctx context.Context, departmentID int64) (int, error) {
	var maxOrder int
	err := db.Ctx(ctx).Table("agents").
		Where("department_id = ? AND parent_agent_id = ? AND status = ?", departmentID, int64(0), consts.ACTIVE).
		Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (r *agentRepo) NextSortOrderByParent(ctx context.Context, parentAgentID int64) (int, error) {
	var maxOrder int
	err := db.Ctx(ctx).Table("agents").
		Where("parent_agent_id = ? AND status = ?", parentAgentID, consts.ACTIVE).
		Select("COALESCE(MAX(sort_order), 0)").Row().Scan(&maxOrder)
	if err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (r *agentRepo) UpdateDepartment(ctx context.Context, id, departmentID int64, sortOrder int) error {
	return r.UpdatePlacement(ctx, id, departmentID, 0, sortOrder)
}

func (r *agentRepo) UpdatePlacement(ctx context.Context, id, departmentID, parentAgentID int64, sortOrder int) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"department_id":   departmentID,
			"parent_agent_id": parentAgentID,
			"sort_order":      sortOrder,
		}).Error
}

func (r *agentRepo) UpdateAvatar(ctx context.Context, id int64, avatarDataURL string, updatetime int64) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"avatar_data_url": avatarDataURL,
			"updatetime":      updatetime,
		}).Error
}

func (r *agentRepo) SetPinned(ctx context.Context, id int64, pinned bool) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("id = ?", id).
		Update("pinned", pinned).Error
}

func (r *agentRepo) ReparentChildren(ctx context.Context, fromParentAgentID, toDepartmentID, toParentAgentID int64) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("parent_agent_id = ? AND status = ?", fromParentAgentID, consts.ACTIVE).
		Updates(map[string]any{
			"department_id":   toDepartmentID,
			"parent_agent_id": toParentAgentID,
		}).Error
}

func (r *agentRepo) ClearLeadOfDepartment(ctx context.Context, agentID int64) error {
	return db.Ctx(ctx).Table("departments").
		Where("lead_agent_id = ?", agentID).
		Update("lead_agent_id", 0).Error
}

func (r *agentRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("id = ?", id).
		Update("status", consts.DELETE).Error
}

func (r *agentRepo) DeleteByDepartment(ctx context.Context, departmentID int64) error {
	return db.Ctx(ctx).Model(&agent_entity.Agent{}).
		Where("department_id = ? AND status = ?", departmentID, consts.ACTIVE).
		Update("status", consts.DELETE).Error
}
