// Package hook_repo provides persistence access for Hook signal sources,
// routing rules, and event logs.
package hook_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/hook_entity"
)

//go:generate mockgen -source hook.go -destination mock_hook_repo/mock_hook.go

type HookSourceRepo interface {
	Create(ctx context.Context, s *hook_entity.HookSource) error
	Update(ctx context.Context, s *hook_entity.HookSource) error
	Find(ctx context.Context, id int64) (*hook_entity.HookSource, error)
	FindByName(ctx context.Context, name string) (*hook_entity.HookSource, error)
	List(ctx context.Context) ([]*hook_entity.HookSource, error)
	Delete(ctx context.Context, id int64) error
}

type HookRuleRepo interface {
	Create(ctx context.Context, r *hook_entity.HookRule) error
	Update(ctx context.Context, r *hook_entity.HookRule) error
	Find(ctx context.Context, id int64) (*hook_entity.HookRule, error)
	List(ctx context.Context) ([]*hook_entity.HookRule, error)
	ListBySource(ctx context.Context, sourceID int64) ([]*hook_entity.HookRule, error)
	NextSortOrder(ctx context.Context, sourceID int64) (int, error)
	Delete(ctx context.Context, id int64) error
}

type HookEventRepo interface {
	Create(ctx context.Context, e *hook_entity.HookEvent) error
	Update(ctx context.Context, e *hook_entity.HookEvent) error
	Find(ctx context.Context, id int64) (*hook_entity.HookEvent, error)
	FindBySourceRef(ctx context.Context, sourceID int64, sourceRef string) (*hook_entity.HookEvent, error)
	ListRecent(ctx context.Context, limit int) ([]*hook_entity.HookEvent, error)
	ListBySource(ctx context.Context, sourceID int64, limit int) ([]*hook_entity.HookEvent, error)
}

var (
	defaultSource HookSourceRepo
	defaultRule   HookRuleRepo
	defaultEvent  HookEventRepo
)

func HookSource() HookSourceRepo { return defaultSource }
func HookRule() HookRuleRepo     { return defaultRule }
func HookEvent() HookEventRepo   { return defaultEvent }

func RegisterHookSource(impl HookSourceRepo) { defaultSource = impl }
func RegisterHookRule(impl HookRuleRepo)     { defaultRule = impl }
func RegisterHookEvent(impl HookEventRepo)   { defaultEvent = impl }

type hookSourceRepo struct{}
type hookRuleRepo struct{}
type hookEventRepo struct{}

func NewHookSource() HookSourceRepo { return &hookSourceRepo{} }
func NewHookRule() HookRuleRepo     { return &hookRuleRepo{} }
func NewHookEvent() HookEventRepo   { return &hookEventRepo{} }

func (r *hookSourceRepo) Create(ctx context.Context, s *hook_entity.HookSource) error {
	return db.Ctx(ctx).Create(s).Error
}

func (r *hookSourceRepo) Update(ctx context.Context, s *hook_entity.HookSource) error {
	return db.Ctx(ctx).Save(s).Error
}

func (r *hookSourceRepo) Find(ctx context.Context, id int64) (*hook_entity.HookSource, error) {
	out := &hook_entity.HookSource{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookSourceRepo) FindByName(ctx context.Context, name string) (*hook_entity.HookSource, error) {
	out := &hook_entity.HookSource{}
	err := db.Ctx(ctx).Where("name = ? AND status = ?", name, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookSourceRepo) List(ctx context.Context) ([]*hook_entity.HookSource, error) {
	var rows []*hook_entity.HookSource
	if err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookSourceRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&hook_entity.HookSource{}).Where("id = ?", id).Update("status", consts.DELETE).Error
}

func (r *hookRuleRepo) Create(ctx context.Context, rule *hook_entity.HookRule) error {
	return db.Ctx(ctx).Create(rule).Error
}

func (r *hookRuleRepo) Update(ctx context.Context, rule *hook_entity.HookRule) error {
	return db.Ctx(ctx).Save(rule).Error
}

func (r *hookRuleRepo) Find(ctx context.Context, id int64) (*hook_entity.HookRule, error) {
	out := &hook_entity.HookRule{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookRuleRepo) List(ctx context.Context) ([]*hook_entity.HookRule, error) {
	var rows []*hook_entity.HookRule
	if err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("source_id ASC, sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookRuleRepo) ListBySource(ctx context.Context, sourceID int64) ([]*hook_entity.HookRule, error) {
	var rows []*hook_entity.HookRule
	if err := db.Ctx(ctx).Where("source_id = ? AND status = ?", sourceID, consts.ACTIVE).Order("sort_order ASC, id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookRuleRepo) NextSortOrder(ctx context.Context, sourceID int64) (int, error) {
	var maxOrder int
	if err := db.Ctx(ctx).Model(&hook_entity.HookRule{}).
		Where("source_id = ? AND status = ?", sourceID, consts.ACTIVE).
		Select("COALESCE(MAX(sort_order), 0)").
		Scan(&maxOrder).Error; err != nil {
		return 0, err
	}
	return maxOrder + 1, nil
}

func (r *hookRuleRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&hook_entity.HookRule{}).Where("id = ?", id).Update("status", consts.DELETE).Error
}

func (r *hookEventRepo) Create(ctx context.Context, event *hook_entity.HookEvent) error {
	return db.Ctx(ctx).Create(event).Error
}

func (r *hookEventRepo) Update(ctx context.Context, event *hook_entity.HookEvent) error {
	return db.Ctx(ctx).Save(event).Error
}

func (r *hookEventRepo) Find(ctx context.Context, id int64) (*hook_entity.HookEvent, error) {
	out := &hook_entity.HookEvent{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookEventRepo) FindBySourceRef(ctx context.Context, sourceID int64, sourceRef string) (*hook_entity.HookEvent, error) {
	out := &hook_entity.HookEvent{}
	err := db.Ctx(ctx).Where("source_id = ? AND source_ref = ? AND status = ?", sourceID, sourceRef, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *hookEventRepo) ListRecent(ctx context.Context, limit int) ([]*hook_entity.HookEvent, error) {
	var rows []*hook_entity.HookEvent
	q := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).Order("received_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

func (r *hookEventRepo) ListBySource(ctx context.Context, sourceID int64, limit int) ([]*hook_entity.HookEvent, error) {
	var rows []*hook_entity.HookEvent
	q := db.Ctx(ctx).Where("source_id = ? AND status = ?", sourceID, consts.ACTIVE).Order("received_at DESC, id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
