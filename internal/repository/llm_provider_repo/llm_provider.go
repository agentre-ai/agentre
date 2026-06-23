// Package llm_provider_repo 提供 LLM 供应商的持久化访问。
package llm_provider_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/repository/repoquery"
)

//go:generate mockgen -source llm_provider.go -destination mock_llm_provider_repo/mock_llm_provider.go

// LLMProviderRepo LLM 供应商仓储。
type LLMProviderRepo interface {
	Create(ctx context.Context, p *llm_provider_entity.LLMProvider) error
	Update(ctx context.Context, p *llm_provider_entity.LLMProvider) error
	Find(ctx context.Context, id int64) (*llm_provider_entity.LLMProvider, error)
	FindByKey(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error)
	BatchFindByKey(ctx context.Context, keys []string) (map[string]*llm_provider_entity.LLMProvider, error)
	FindByName(ctx context.Context, name string) (*llm_provider_entity.LLMProvider, error)
	List(ctx context.Context) ([]*llm_provider_entity.LLMProvider, error)
	Delete(ctx context.Context, id int64) error
}

var defaultLLMProvider LLMProviderRepo

// LLMProvider 取默认仓储单例。
func LLMProvider() LLMProviderRepo { return defaultLLMProvider }

// RegisterLLMProvider 注入仓储实现，由 bootstrap 调用一次。
func RegisterLLMProvider(impl LLMProviderRepo) { defaultLLMProvider = impl }

type llmProviderRepo struct{}

// NewLLMProvider 构造默认 GORM 实现。
func NewLLMProvider() LLMProviderRepo { return &llmProviderRepo{} }

func (r *llmProviderRepo) Create(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	return db.Ctx(ctx).Create(p).Error
}

func (r *llmProviderRepo) Update(ctx context.Context, p *llm_provider_entity.LLMProvider) error {
	return db.Ctx(ctx).Save(p).Error
}

func (r *llmProviderRepo) Find(ctx context.Context, id int64) (*llm_provider_entity.LLMProvider, error) {
	out := &llm_provider_entity.LLMProvider{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *llmProviderRepo) BatchFindByKey(ctx context.Context, keys []string) (map[string]*llm_provider_entity.LLMProvider, error) {
	return repoquery.ActiveMap[llm_provider_entity.LLMProvider](ctx, "provider_key", keys, func(p *llm_provider_entity.LLMProvider) string {
		return p.ProviderKey
	})
}

func (r *llmProviderRepo) FindByName(ctx context.Context, name string) (*llm_provider_entity.LLMProvider, error) {
	out := &llm_provider_entity.LLMProvider{}
	err := db.Ctx(ctx).Where("name = ? AND status = ?", name, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *llmProviderRepo) FindByKey(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error) {
	out := &llm_provider_entity.LLMProvider{}
	err := db.Ctx(ctx).Where("provider_key = ?", key).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *llmProviderRepo) List(ctx context.Context) ([]*llm_provider_entity.LLMProvider, error) {
	var rows []*llm_provider_entity.LLMProvider
	if err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *llmProviderRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&llm_provider_entity.LLMProvider{}).
		Where("id = ?", id).
		Update("status", consts.DELETE).Error
}
