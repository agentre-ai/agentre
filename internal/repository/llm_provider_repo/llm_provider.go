// Package llm_provider_repo 提供 LLM 供应商及其稳定模型的持久化访问。
package llm_provider_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_model_entity"
	"github.com/agentre-ai/agentre/internal/repository/repoquery"
)

//go:generate mockgen -source llm_provider.go -destination mock_llm_provider_repo/mock_llm_provider.go

// ProviderRefCounts 一个 Provider 的引用影响计数（Backend / Session / Route 三路）。
// 用于「改默认模型」或「删除 Provider」前的引用保护确认。
type ProviderRefCounts struct {
	// Backends 主绑定到该 provider_key 的 agent_backends 数。
	Backends int64
	// Sessions 会话级钉住该 provider_key 的 chat_sessions 数。
	Sessions int64
	// Routes 其 model_routes 结构化 target 引用了该 provider_key 的 agent_backends 数。
	Routes int64
}

// ModelRefCounts 一个 Model（model_key）的引用影响计数。
// 用于「编辑被引用 Model 的 model_id」或「删除 Model」前的引用保护确认。
type ModelRefCounts struct {
	Backends int64
	Sessions int64
	Routes   int64
}

// LLMProviderRepo LLM 供应商 + 模型仓储。单一口子覆盖 Provider CRUD、Model
// CRUD/list/find、原子 create/import/default 变更与 Backend/Session/Route
// 引用影响计数。ModelKey 不可变；展示读取不暴露明文 API Key（由实体 MaskedAPIKey
// 承担脱敏边界）。
type LLMProviderRepo interface {
	// ── Provider CRUD ──
	Create(ctx context.Context, p *llm_provider_entity.LLMProvider) error
	Update(ctx context.Context, p *llm_provider_entity.LLMProvider) error
	Find(ctx context.Context, id int64) (*llm_provider_entity.LLMProvider, error)
	FindByKey(ctx context.Context, key string) (*llm_provider_entity.LLMProvider, error)
	BatchFindByKey(ctx context.Context, keys []string) (map[string]*llm_provider_entity.LLMProvider, error)
	FindByName(ctx context.Context, name string) (*llm_provider_entity.LLMProvider, error)
	List(ctx context.Context) ([]*llm_provider_entity.LLMProvider, error)
	// DeleteWithModels 在同一事务内软删除一个 Provider 及其全部 Models（spec 决策 17：
	// 无引用 Provider 的删除事务同时移除其 Models，任一步失败整体回滚，不留半批）。
	DeleteWithModels(ctx context.Context, id int64) error

	// ── Provider + Models 原子操作（成功或失败，不留半批） ──
	// CreateWithModels 在同一事务内写 Provider、其全部 Models 并落 default_model_key。
	CreateWithModels(ctx context.Context, p *llm_provider_entity.LLMProvider, models []*llm_provider_model_entity.LLMProviderModel, defaultModelKey string) error
	// BatchImportModels 原子批量导入同一 Provider 的一组 Models（发现结果人工确认后落地）。
	BatchImportModels(ctx context.Context, models []*llm_provider_model_entity.LLMProviderModel) error
	// SetDefaultModel 原子切换 Provider 的 default_model_key。
	SetDefaultModel(ctx context.Context, providerID int64, defaultModelKey string) error

	// ── Model CRUD ──
	CreateModel(ctx context.Context, m *llm_provider_model_entity.LLMProviderModel) error
	// UpdateModel 更新模型可编辑字段；model_key 不可变，绝不进入 UPDATE。
	UpdateModel(ctx context.Context, m *llm_provider_model_entity.LLMProviderModel) error
	FindModel(ctx context.Context, id int64) (*llm_provider_model_entity.LLMProviderModel, error)
	// FindModelByKey 按稳定 model_key 查（含 enabled=0 的停用模型，供 fixed-model 失效提示）。
	FindModelByKey(ctx context.Context, modelKey string) (*llm_provider_model_entity.LLMProviderModel, error)
	ListModels(ctx context.Context, providerID int64) ([]*llm_provider_model_entity.LLMProviderModel, error)
	DeleteModel(ctx context.Context, id int64) error

	// ── 引用影响计数 ──
	CountProviderReferences(ctx context.Context, providerKey string) (ProviderRefCounts, error)
	CountModelReferences(ctx context.Context, modelKey string) (ModelRefCounts, error)
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

// DeleteWithModels 在同一事务内软删除 Provider 及其全部 Models（spec 决策 17）。
func (r *llmProviderRepo) DeleteWithModels(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		ctx = db.WithContextDB(ctx, tx)
		if err := db.Ctx(ctx).Model(&llm_provider_entity.LLMProvider{}).
			Where("id = ?", id).
			Update("status", consts.DELETE).Error; err != nil {
			return err
		}
		return db.Ctx(ctx).Model(&llm_provider_model_entity.LLMProviderModel{}).
			Where("provider_id = ?", id).
			Update("status", consts.DELETE).Error
	})
}

// CreateWithModels 事务内原子写入 Provider + Models + 默认模型。
func (r *llmProviderRepo) CreateWithModels(ctx context.Context, p *llm_provider_entity.LLMProvider, models []*llm_provider_model_entity.LLMProviderModel, defaultModelKey string) error {
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		ctx = db.WithContextDB(ctx, tx)
		if err := db.Ctx(ctx).Create(p).Error; err != nil {
			return err
		}
		for _, m := range models {
			m.ProviderID = p.ID
			if err := db.Ctx(ctx).Create(m).Error; err != nil {
				return err
			}
		}
		return db.Ctx(ctx).Model(&llm_provider_entity.LLMProvider{}).
			Where("id = ?", p.ID).
			Update("default_model_key", defaultModelKey).Error
	})
}

// BatchImportModels 原子批量导入；任一条失败整体回滚，不留半批状态。
func (r *llmProviderRepo) BatchImportModels(ctx context.Context, models []*llm_provider_model_entity.LLMProviderModel) error {
	if len(models) == 0 {
		return nil
	}
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		ctx = db.WithContextDB(ctx, tx)
		for _, m := range models {
			if err := db.Ctx(ctx).Create(m).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *llmProviderRepo) SetDefaultModel(ctx context.Context, providerID int64, defaultModelKey string) error {
	return db.Ctx(ctx).Model(&llm_provider_entity.LLMProvider{}).
		Where("id = ?", providerID).
		Update("default_model_key", defaultModelKey).Error
}

func (r *llmProviderRepo) CreateModel(ctx context.Context, m *llm_provider_model_entity.LLMProviderModel) error {
	return db.Ctx(ctx).Create(m).Error
}

// UpdateModel 只更新可编辑字段；model_key 不可变，Omit 后绝不进入 UPDATE 语句。
func (r *llmProviderRepo) UpdateModel(ctx context.Context, m *llm_provider_model_entity.LLMProviderModel) error {
	return db.Ctx(ctx).Omit("model_key").Save(m).Error
}

func (r *llmProviderRepo) FindModel(ctx context.Context, id int64) (*llm_provider_model_entity.LLMProviderModel, error) {
	out := &llm_provider_model_entity.LLMProviderModel{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *llmProviderRepo) FindModelByKey(ctx context.Context, modelKey string) (*llm_provider_model_entity.LLMProviderModel, error) {
	out := &llm_provider_model_entity.LLMProviderModel{}
	err := db.Ctx(ctx).Where("model_key = ? AND status = ?", modelKey, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *llmProviderRepo) ListModels(ctx context.Context, providerID int64) ([]*llm_provider_model_entity.LLMProviderModel, error) {
	var rows []*llm_provider_model_entity.LLMProviderModel
	if err := db.Ctx(ctx).Where("provider_id = ? AND status = ?", providerID, consts.ACTIVE).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *llmProviderRepo) DeleteModel(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&llm_provider_model_entity.LLMProviderModel{}).
		Where("id = ?", id).
		Update("status", consts.DELETE).Error
}

// CountProviderReferences 统计某 Provider 被 Backend / Session / Route 引用的数量。
// Route 引用按结构化 target 中出现的 provider_key 字符串匹配（provider_key 是稳定
// UUID，LIKE 无歧义）。
func (r *llmProviderRepo) CountProviderReferences(ctx context.Context, providerKey string) (ProviderRefCounts, error) {
	var out ProviderRefCounts
	if err := db.Ctx(ctx).Table("agent_backends").
		Where("llm_provider_key = ? AND status = ?", providerKey, consts.ACTIVE).
		Count(&out.Backends).Error; err != nil {
		return out, err
	}
	if err := db.Ctx(ctx).Table("chat_sessions").
		Where("provider_key = ? AND status = ?", providerKey, consts.ACTIVE).
		Count(&out.Sessions).Error; err != nil {
		return out, err
	}
	if err := db.Ctx(ctx).Table("agent_backends").
		Where("status = ? AND model_routes LIKE ?", consts.ACTIVE, "%"+providerKey+"%").
		Count(&out.Routes).Error; err != nil {
		return out, err
	}
	return out, nil
}

// CountModelReferences 统计某 Model（model_key）被 Backend / Session / Route 引用的数量。
func (r *llmProviderRepo) CountModelReferences(ctx context.Context, modelKey string) (ModelRefCounts, error) {
	var out ModelRefCounts
	if err := db.Ctx(ctx).Table("agent_backends").
		Where("llm_model_key = ? AND status = ?", modelKey, consts.ACTIVE).
		Count(&out.Backends).Error; err != nil {
		return out, err
	}
	if err := db.Ctx(ctx).Table("chat_sessions").
		Where("model_key = ? AND status = ?", modelKey, consts.ACTIVE).
		Count(&out.Sessions).Error; err != nil {
		return out, err
	}
	if err := db.Ctx(ctx).Table("agent_backends").
		Where("status = ? AND model_routes LIKE ?", consts.ACTIVE, "%"+modelKey+"%").
		Count(&out.Routes).Error; err != nil {
		return out, err
	}
	return out, nil
}
