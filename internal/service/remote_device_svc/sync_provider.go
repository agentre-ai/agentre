package remote_device_svc

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/daemon/handlers"
	"github.com/agentre-ai/agentre/internal/model/entity/llm_provider_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
)

// SyncProvider copies a local provider's metadata and API key to one paired
// daemon. It is intentionally explicit because the operation transfers a secret
// to another machine.
func (s *service) SyncProvider(ctx context.Context, deviceID int64, providerKey string) error {
	key := strings.TrimSpace(providerKey)
	if deviceID <= 0 || key == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if s.pool == nil {
		return errors.New("remote device connection pool unavailable")
	}

	repo := llm_provider_repo.LLMProvider()
	if repo == nil {
		return errors.New("llm provider repo unavailable")
	}
	p, err := repo.FindByKey(ctx, key)
	if err != nil {
		return err
	}
	if p == nil || !p.IsActive() {
		return i18n.NewError(ctx, code.LLMProviderNotFound)
	}

	defaultModelID, err := providerDefaultModelID(ctx, p)
	if err != nil {
		return err
	}

	lease, err := s.pool.Borrow(ctx, deviceID)
	if err != nil {
		return mapSyncBorrowError(ctx, err)
	}
	defer lease.Release()

	var ok handlers.OK
	if err := lease.Client().Call(ctx, "llm.upsert", providerToUpsertParams(p, defaultModelID), &ok); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return i18n.NewError(ctx, code.RemoteDeviceTimeout)
		}
		return fmt.Errorf("remote llm.upsert: %w", err)
	}
	s.upsertDeviceProviderCache(deviceID, ProviderSummary{
		Key:  p.ProviderKey,
		Name: p.Name,
		Type: p.Type,
	})
	return nil
}

// providerDefaultModelID 解析 Provider 当前启用的默认模型的 ModelID（spec：同步默认模型）。
// 无默认模型、默认模型缺失或停用时返回空串，保持 daemon 单模型字段「没有就空」的既有语义。
func providerDefaultModelID(ctx context.Context, p *llm_provider_entity.LLMProvider) (string, error) {
	if p == nil || !p.HasDefaultModel() {
		return "", nil
	}
	m, err := llm_provider_repo.LLMProvider().FindModelByKey(ctx, p.DefaultModelKey)
	if err != nil {
		return "", err
	}
	if m == nil || !m.IsEnabled() {
		return "", nil
	}
	return m.ModelID, nil
}

func providerToUpsertParams(p *llm_provider_entity.LLMProvider, defaultModelID string) handlers.LLMUpsertParams {
	return handlers.LLMUpsertParams{
		ProviderKey: p.ProviderKey,
		Name:        p.Name,
		Type:        p.Type,
		BaseURL:     p.BaseURL,
		Model:       defaultModelID,
		APIKey:      p.APIKey,
		UpdatedAt:   p.Updatetime,
	}
}

func mapSyncBorrowError(ctx context.Context, err error) error {
	switch {
	case errors.Is(err, ErrDeviceNotFound):
		return i18n.NewError(ctx, code.RemoteDeviceNotFound)
	case errors.Is(err, ErrDeviceUnauthorized):
		return i18n.NewError(ctx, code.RemoteDeviceUnauthorized)
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded):
		return i18n.NewError(ctx, code.RemoteDeviceTimeout)
	default:
		return i18n.NewError(ctx, code.RemoteDeviceDialFailed)
	}
}

func (s *service) upsertDeviceProviderCache(deviceID int64, p ProviderSummary) {
	s.providerCacheMu.Lock()
	defer s.providerCacheMu.Unlock()

	prev := s.providerCache[deviceID]
	next := make([]ProviderSummary, 0, len(prev)+1)
	replaced := false
	for _, existing := range prev {
		if existing.Key == p.Key {
			next = append(next, p)
			replaced = true
			continue
		}
		next = append(next, existing)
	}
	if !replaced {
		next = append(next, p)
	}
	s.providerCache[deviceID] = next
}
