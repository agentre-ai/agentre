package server_svc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/agentre-ai/agentre/internal/pkg/syncwire"
	"github.com/agentre-ai/agentre/internal/repository/server_state_repo"
)

// 本文件是工作区多端同步的网络出入口：只做 HTTP 与编解码，一切判定（冲突、墓碑、
// 暂缓、队列）在 sync_svc 里。它不 import sync_svc——线上结构住在叶子包 syncwire，
// 两边各自依赖它。
//
// 载荷内容一律不进日志：里面有项目路径、prompt 与 EnvJSON。

type syncPushReqItem struct {
	Kind                string          `json:"kind"`
	SyncID              string          `json:"sync_id"`
	BaseVersion         int64           `json:"base_version"`
	UpdatedAt           int64           `json:"updated_at"`
	Deleted             bool            `json:"deleted"`
	AgentredFingerprint string          `json:"agentred_fingerprint"`
	ProjectSyncID       string          `json:"project_sync_id"`
	Payload             json.RawMessage `json:"payload"`
}

type syncPushReq struct {
	Items []syncPushReqItem `json:"items"`
}

type syncPushResp struct {
	Results []syncwire.PushResult `json:"results"`
}

type syncPullRespItem struct {
	Kind                string          `json:"kind"`
	SyncID              string          `json:"sync_id"`
	ProjectSyncID       string          `json:"project_sync_id"`
	AgentredFingerprint string          `json:"agentred_fingerprint"`
	Payload             json.RawMessage `json:"payload"`
	Version             int64           `json:"version"`
	UpdatedAt           int64           `json:"updated_at"`
	SourceDeviceID      int64           `json:"source_device_id"`
	Deleted             bool            `json:"deleted"`
}

type syncPullResp struct {
	Items      []syncPullRespItem `json:"items"`
	NextCursor int64              `json:"next_cursor"`
	HasMore    bool               `json:"has_more"`
}

// SyncPush 把一批本地改动上行。未登录时不发任何网络请求（R12）。
//
// server 判「设备距上次成功同步已超过墓碑保留窗口」时返回 CodeResyncRequired，
// 这里翻成 syncwire.ErrResyncRequired——调用方据此先拉一份全量快照（R6a）。
func (s *service) SyncPush(ctx context.Context, items []syncwire.PushItem) ([]syncwire.PushResult, error) {
	if len(items) == 0 {
		return nil, nil
	}
	if err := s.requireLogin(ctx); err != nil {
		return nil, err
	}

	req := syncPushReq{Items: make([]syncPushReqItem, 0, len(items))}
	for _, it := range items {
		req.Items = append(req.Items, syncPushReqItem{
			Kind:                it.Kind,
			SyncID:              it.SyncID,
			BaseVersion:         it.BaseVersion,
			UpdatedAt:           it.UpdatedAt,
			Deleted:             it.Deleted,
			AgentredFingerprint: it.AgentredFingerprint,
			ProjectSyncID:       it.ProjectSyncID,
			Payload:             json.RawMessage(it.Payload),
		})
	}

	var out []syncwire.PushResult
	err := s.withAuth(ctx, func(ctx context.Context) error {
		var env envelope[syncPushResp]
		_, doErr := s.getClient().do(ctx, http.MethodPost, "/v1/sync/push", req, &env)
		if env.Code == syncwire.CodeResyncRequired {
			return syncwire.ErrResyncRequired
		}
		if doErr != nil {
			return doErr
		}
		if env.Code != 0 {
			return fmt.Errorf("server: sync push rejected with code %d", env.Code)
		}
		out = env.Data.Results
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// SyncPull 按版本游标增量下行；cursor = 0 拉全量（R6a 的重同步用它）。
func (s *service) SyncPull(ctx context.Context, cursor int64, limit int) (*syncwire.PullPage, error) {
	if err := s.requireLogin(ctx); err != nil {
		return nil, err
	}

	path := "/v1/sync/pull?cursor=" + strconv.FormatInt(cursor, 10)
	if limit > 0 {
		path += "&limit=" + strconv.Itoa(limit)
	}

	page := &syncwire.PullPage{}
	err := s.withAuth(ctx, func(ctx context.Context) error {
		var env envelope[syncPullResp]
		_, doErr := s.getClient().do(ctx, http.MethodGet, path, nil, &env)
		if doErr != nil {
			return doErr
		}
		if env.Code != 0 {
			return fmt.Errorf("server: sync pull rejected with code %d", env.Code)
		}
		items := make([]syncwire.PullItem, 0, len(env.Data.Items))
		for _, it := range env.Data.Items {
			items = append(items, syncwire.PullItem{
				Kind:                it.Kind,
				SyncID:              it.SyncID,
				ProjectSyncID:       it.ProjectSyncID,
				AgentredFingerprint: it.AgentredFingerprint,
				Payload:             []byte(it.Payload),
				Version:             it.Version,
				UpdatedAt:           it.UpdatedAt,
				SourceDeviceID:      it.SourceDeviceID,
				Deleted:             it.Deleted,
			})
		}
		page.Items = items
		page.NextCursor = env.Data.NextCursor
		page.HasMore = env.Data.HasMore
		return nil
	})
	if err != nil {
		return nil, err
	}
	return page, nil
}

// requireLogin 未登录时挡在网络请求之前（R12：未登录不产生任何同步网络请求）。
func (s *service) requireLogin(ctx context.Context) error {
	row, err := server_state_repo.ServerState().Get(ctx)
	if err != nil {
		return err
	}
	if row == nil || !row.IsLoggedIn() {
		return ErrNotLoggedIn
	}
	return nil
}
