package server_svc

import (
	"context"
	"errors"
	"net/http"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"agentre/internal/pkg/keychain"
	"agentre/internal/repository/server_state_repo"
)

type refreshResp struct {
	AccessToken      string `json:"access_token"`
	ExpiresIn        int    `json:"expires_in"`
	RefreshToken     string `json:"refresh_token"`
	RefreshExpiresIn int    `json:"refresh_expires_in"`
}

// refresh exchanges the stored refresh_token for a new access token.
// Server-side refresh-token rotation: the response carries a fresh refresh_token
// and we overwrite the keychain entry. The desktop only keeps the latest one.
func (s *service) refresh(ctx context.Context) error {
	old, err := keychain.Default().Get(keychainAccountName)
	if err != nil {
		return ErrRefreshFailed
	}

	var env envelope[refreshResp]
	status, err := s.getClient().do(ctx, http.MethodPost, "/v1/oauth/token/refresh",
		map[string]string{"refresh_token": old}, &env)
	if err != nil {
		return err
	}
	if status != http.StatusOK || env.Code != 0 {
		return ErrRefreshFailed
	}

	if err := keychain.Default().Set(keychainAccountName, env.Data.RefreshToken); err != nil {
		return err
	}
	s.getClient().SetAccessToken(env.Data.AccessToken)

	return nil
}

// withAuth runs fn(ctx); on 401, refreshes once and retries. If the refresh fails,
// it tears down local login state and emits a logged_out event so the UI can react.
func (s *service) withAuth(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err == nil {
		return nil
	} else if !is401(err) {
		return err
	}
	// 命中 401 = access token 过期。先 refresh 一次再重试；refresh 失败说明
	// refresh token 也过期 / 被吊销,clearLogin 兜底让 UI 走重新登录。
	logger.Ctx(ctx).Info("server_svc.withAuth: 401 received, refreshing access token")
	if rerr := s.refresh(ctx); rerr != nil {
		logger.Ctx(ctx).Warn("server_svc.withAuth: refresh failed, clearing login",
			zap.Error(rerr))
		_ = s.clearLogin(ctx)
		return rerr
	}
	return fn(ctx)
}

// is401 returns true if err exposes HTTPStatus() == 401.
func is401(err error) bool {
	var he interface{ HTTPStatus() int }
	if errors.As(err, &he) {
		return he.HTTPStatus() == http.StatusUnauthorized
	}
	return false
}

// clearLogin tears down the persisted login: keychain entry, server_state user/device
// fields, and notifies the UI via emitState. Best-effort: each step is independent.
func (s *service) clearLogin(ctx context.Context) error {
	_ = keychain.Default().Delete(keychainAccountName)
	if err := server_state_repo.ServerState().ClearLoginFields(ctx); err != nil {
		logger.Ctx(ctx).Warn("server_svc.clearLogin: ClearLoginFields failed",
			zap.Error(err))
		return err
	}
	logger.Ctx(ctx).Info("server_svc.clearLogin: local login cleared, emitting logged_out")
	s.emit(map[string]any{"kind": "logged_out", "reason": "refresh_expired"})
	return nil
}

// ClearLogin is the exported wrapper around clearLogin for bootstrap-time use
// (e.g. when boot-time Refresh fails and we need to scrub stale login state).
func (s *service) ClearLogin(ctx context.Context) error { return s.clearLogin(ctx) }
