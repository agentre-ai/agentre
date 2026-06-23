// Package project_location_entity 维护 ProjectLocation 的充血实体。
//
// ProjectLocation 装载一份"远端 device 下，某个 project 的绝对路径"。本地 path 仍
// 住在 projects.path（避免双源同步），本表不存空 device_id 的行 —— svc 层在
// FindByProjectAndDevice("") 时不会被调用（chat_svc.ResolveSessionCwd 已分流到
// projects.path）。
package project_location_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// ProjectLocation 远端 device 下某个 project 的绝对路径记录。
type ProjectLocation struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID  int64  `gorm:"column:project_id;type:bigint;not null"`
	DeviceID   string `gorm:"column:device_id;type:text;not null;default:''"`
	Path       string `gorm:"column:path;type:text;not null"`
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*ProjectLocation) TableName() string { return "project_locations" }

func (p *ProjectLocation) IsActive() bool { return p != nil && p.Status == consts.ACTIVE }
func (p *ProjectLocation) IsLocal() bool  { return p != nil && p.DeviceID == "" }

// Check 字段校验。绝对路径与否的检查放这里；外键存在性放 svc 层（依赖 paired_agentreds repo）。
func (p *ProjectLocation) Check(ctx context.Context) error {
	if p == nil {
		return i18n.NewError(ctx, code.ProjectLocationNotFound)
	}
	if p.ProjectID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if strings.TrimSpace(p.Path) == "" {
		return i18n.NewError(ctx, code.ProjectLocationInvalidPath)
	}
	if !strings.HasPrefix(p.Path, "/") {
		return i18n.NewError(ctx, code.ProjectLocationInvalidPath)
	}
	return nil
}
