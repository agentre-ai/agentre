// Package project_location_svc 维护 project × device 维度的工作目录配置。
// 本地路径仍住 projects.path；本 svc 只承接 device_id != "" 的远端路径。
package project_location_svc

import "context"

//go:generate mockgen -source svc.go -destination mock_project_location_svc/mock_svc.go

type ProjectLocationSvc interface {
	ListByProject(ctx context.Context, projectID int64) ([]*ProjectLocationView, error)
	Upsert(ctx context.Context, projectID int64, deviceID, path string) (*ProjectLocationView, error)
	RemoveByProjectAndDevice(ctx context.Context, projectID int64, deviceID string) error
}

type ProjectLocationView struct {
	ID         int64  `json:"id"`
	ProjectID  int64  `json:"projectId"`
	DeviceID   string `json:"deviceId"`
	Path       string `json:"path"`
	DeviceName string `json:"deviceName"`
	Online     bool   `json:"online"`
}

var defaultSvc ProjectLocationSvc = &projectLocationImpl{}

func Default() ProjectLocationSvc     { return defaultSvc }
func SetDefault(s ProjectLocationSvc) { defaultSvc = s }
