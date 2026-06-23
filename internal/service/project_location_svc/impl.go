package project_location_svc

import (
	"context"
	"errors"
	"strconv"

	"github.com/cago-frame/cago/pkg/i18n"
	"gorm.io/gorm"

	"github.com/agentre-ai/agentre/internal/model/entity/project_location_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/project_location_repo"
	"github.com/agentre-ai/agentre/internal/service/remote_device_svc"
)

type projectLocationImpl struct{}

func (s *projectLocationImpl) ListByProject(ctx context.Context, projectID int64) ([]*ProjectLocationView, error) {
	rows, err := project_location_repo.ProjectLocation().ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]*ProjectLocationView, 0, len(rows))
	for _, r := range rows {
		out = append(out, s.toView(ctx, r))
	}
	return out, nil
}

func (s *projectLocationImpl) Upsert(ctx context.Context, projectID int64, deviceID, path string) (*ProjectLocationView, error) {
	// 远端 device 校验：必须能解析为 int64 AND 在 paired_agentreds 中存在。
	if deviceID != "" {
		id, ok := parseDeviceID(deviceID)
		if !ok {
			return nil, i18n.NewError(ctx, code.AgentBackendInvalidDevice)
		}
		if _, err := remote_device_svc.Default().Get(ctx, id); err != nil {
			return nil, i18n.NewError(ctx, code.AgentBackendInvalidDevice)
		}
	}

	// entity 自校验
	entity := &project_location_entity.ProjectLocation{ProjectID: projectID, DeviceID: deviceID, Path: path}
	if err := entity.Check(ctx); err != nil {
		return nil, err
	}

	existing, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, deviceID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if existing != nil {
		if err := project_location_repo.ProjectLocation().UpdatePath(ctx, existing.ID, path); err != nil {
			return nil, err
		}
		existing.Path = path
		return s.toView(ctx, existing), nil
	}
	if err := project_location_repo.ProjectLocation().Create(ctx, entity); err != nil {
		return nil, err
	}
	return s.toView(ctx, entity), nil
}

func (s *projectLocationImpl) RemoveByProjectAndDevice(ctx context.Context, projectID int64, deviceID string) error {
	row, err := project_location_repo.ProjectLocation().FindByProjectAndDevice(ctx, projectID, deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return i18n.NewError(ctx, code.ProjectLocationNotFound)
		}
		return err
	}
	return project_location_repo.ProjectLocation().Delete(ctx, row.ID)
}

func (s *projectLocationImpl) toView(ctx context.Context, p *project_location_entity.ProjectLocation) *ProjectLocationView {
	v := &ProjectLocationView{
		ID:        p.ID,
		ProjectID: p.ProjectID,
		DeviceID:  p.DeviceID,
		Path:      p.Path,
	}
	if id, ok := parseDeviceID(p.DeviceID); ok {
		if dv, err := remote_device_svc.Default().Get(ctx, id); err == nil && dv != nil {
			v.DeviceName = dv.Name
			v.Online = dv.Online
		}
	}
	return v
}

func parseDeviceID(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return id, true
}
