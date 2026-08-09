// Package project_location_repo 提供 ProjectLocation 的持久化访问。
package project_location_repo

import (
	"context"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"

	"github.com/agentre-ai/agentre/internal/model/entity/project_location_entity"
)

//go:generate mockgen -source project_location.go -destination mock_project_location_repo/mock_project_location.go

// ProjectLocationRepo 仓储接口（消费方约束模式）。
//
// 远端 agentred 上的 path 入本表；本地 path 仍住 projects.path。账号内自然键是
// (project, daemon_fingerprint)——FindByProjectAndFingerprint 按这个自然键查找/去重。
//
// FindByProjectAndDevice 按 device_id 缓存列查找，签名与行为保持不变：它是
// project_svc / chat_svc 两处 cwd 解析调用点的既有等价约束（工作目录解析的行
// 为不因本次自然键调整而改变）。
type ProjectLocationRepo interface {
	Create(ctx context.Context, p *project_location_entity.ProjectLocation) error
	Get(ctx context.Context, id int64) (*project_location_entity.ProjectLocation, error)
	FindByProjectAndDevice(ctx context.Context, projectID int64, deviceID string) (*project_location_entity.ProjectLocation, error)
	FindByProjectAndFingerprint(ctx context.Context, projectID int64, fingerprint string) (*project_location_entity.ProjectLocation, error)
	ListByProject(ctx context.Context, projectID int64) ([]*project_location_entity.ProjectLocation, error)
	UpdatePath(ctx context.Context, id int64, path string) error
	// UpdateDeviceID 单独更新 device_id 缓存列，不动 path/status。配对成功后回填
	// 本地 paired_agentreds.id 的字符串形式；解除配对后传空串清空（R2b）。
	UpdateDeviceID(ctx context.Context, id int64, deviceID string) error
	Delete(ctx context.Context, id int64) error
}

var defaultProjectLocation ProjectLocationRepo

// ProjectLocation 取默认仓储单例。
func ProjectLocation() ProjectLocationRepo { return defaultProjectLocation }

// RegisterProjectLocation 注入仓储实现，由 bootstrap 调用一次。
func RegisterProjectLocation(impl ProjectLocationRepo) { defaultProjectLocation = impl }

// NewProjectLocation 构造默认 GORM 实现。
func NewProjectLocation() ProjectLocationRepo { return &projectLocationRepo{} }

type projectLocationRepo struct{}

func (r *projectLocationRepo) Create(ctx context.Context, p *project_location_entity.ProjectLocation) error {
	now := time.Now().UnixMilli()
	if p.Createtime == 0 {
		p.Createtime = now
	}
	p.Updatetime = now
	if p.Status == 0 {
		p.Status = consts.ACTIVE
	}
	return db.Ctx(ctx).Create(p).Error
}

func (r *projectLocationRepo) Get(ctx context.Context, id int64) (*project_location_entity.ProjectLocation, error) {
	out := &project_location_entity.ProjectLocation{}
	if err := db.Ctx(ctx).Where("id = ?", id).Take(out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *projectLocationRepo) FindByProjectAndDevice(ctx context.Context, projectID int64, deviceID string) (*project_location_entity.ProjectLocation, error) {
	out := &project_location_entity.ProjectLocation{}
	if err := db.Ctx(ctx).Where(
		"project_id = ? AND device_id = ? AND status = ?", projectID, deviceID, consts.ACTIVE,
	).Take(out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *projectLocationRepo) FindByProjectAndFingerprint(ctx context.Context, projectID int64, fingerprint string) (*project_location_entity.ProjectLocation, error) {
	out := &project_location_entity.ProjectLocation{}
	if err := db.Ctx(ctx).Where(
		"project_id = ? AND daemon_fingerprint = ? AND status = ?", projectID, fingerprint, consts.ACTIVE,
	).Take(out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *projectLocationRepo) ListByProject(ctx context.Context, projectID int64) ([]*project_location_entity.ProjectLocation, error) {
	var out []*project_location_entity.ProjectLocation
	if err := db.Ctx(ctx).Where(
		"project_id = ? AND status = ?", projectID, consts.ACTIVE,
	).Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

func (r *projectLocationRepo) UpdatePath(ctx context.Context, id int64, path string) error {
	return db.Ctx(ctx).Model(&project_location_entity.ProjectLocation{}).Where("id = ?", id).Updates(map[string]any{
		"path":       path,
		"updatetime": time.Now().UnixMilli(),
	}).Error
}

func (r *projectLocationRepo) UpdateDeviceID(ctx context.Context, id int64, deviceID string) error {
	return db.Ctx(ctx).Model(&project_location_entity.ProjectLocation{}).Where("id = ?", id).Updates(map[string]any{
		"device_id":  deviceID,
		"updatetime": time.Now().UnixMilli(),
	}).Error
}

func (r *projectLocationRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&project_location_entity.ProjectLocation{}).Where("id = ?", id).Updates(map[string]any{
		"status":     consts.DELETE,
		"updatetime": time.Now().UnixMilli(),
	}).Error
}
