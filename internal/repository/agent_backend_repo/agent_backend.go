// Package agent_backend_repo 提供 Agent 后端的持久化访问。
package agent_backend_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/repository/repoquery"
)

//go:generate mockgen -source agent_backend.go -destination mock_agent_backend_repo/mock_agent_backend.go

// AgentBackendRepo Agent 后端仓储。
type AgentBackendRepo interface {
	Create(ctx context.Context, b *agent_backend_entity.AgentBackend) error
	Update(ctx context.Context, b *agent_backend_entity.AgentBackend) error
	Find(ctx context.Context, id int64) (*agent_backend_entity.AgentBackend, error)
	BatchFind(ctx context.Context, ids []int64) (map[int64]*agent_backend_entity.AgentBackend, error)
	FindByName(ctx context.Context, name string) (*agent_backend_entity.AgentBackend, error)
	List(ctx context.Context) ([]*agent_backend_entity.AgentBackend, error)
	Delete(ctx context.Context, id int64) error
}

var defaultAgentBackend AgentBackendRepo

// AgentBackend 取默认仓储单例。
func AgentBackend() AgentBackendRepo { return defaultAgentBackend }

// RegisterAgentBackend 注入仓储实现，由 bootstrap 调用一次。
func RegisterAgentBackend(impl AgentBackendRepo) { defaultAgentBackend = impl }

type agentBackendRepo struct{}

// NewAgentBackend 构造默认 GORM 实现。
func NewAgentBackend() AgentBackendRepo { return &agentBackendRepo{} }

func (r *agentBackendRepo) Create(ctx context.Context, b *agent_backend_entity.AgentBackend) error {
	return db.Ctx(ctx).Create(b).Error
}

func (r *agentBackendRepo) Update(ctx context.Context, b *agent_backend_entity.AgentBackend) error {
	return db.Ctx(ctx).Save(b).Error
}

func (r *agentBackendRepo) Find(ctx context.Context, id int64) (*agent_backend_entity.AgentBackend, error) {
	out := &agent_backend_entity.AgentBackend{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *agentBackendRepo) BatchFind(ctx context.Context, ids []int64) (map[int64]*agent_backend_entity.AgentBackend, error) {
	return repoquery.ActiveMap[agent_backend_entity.AgentBackend](ctx, "id", ids, func(b *agent_backend_entity.AgentBackend) int64 {
		return b.ID
	})
}

func (r *agentBackendRepo) FindByName(ctx context.Context, name string) (*agent_backend_entity.AgentBackend, error) {
	out := &agent_backend_entity.AgentBackend{}
	err := db.Ctx(ctx).Where("name = ? AND status = ?", name, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *agentBackendRepo) List(ctx context.Context) ([]*agent_backend_entity.AgentBackend, error) {
	var rows []*agent_backend_entity.AgentBackend
	if err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *agentBackendRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&agent_backend_entity.AgentBackend{}).
		Where("id = ?", id).
		Update("status", consts.DELETE).Error
}
