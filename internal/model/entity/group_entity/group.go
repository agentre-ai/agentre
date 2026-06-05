// Package group_entity 维护群聊编排的充血实体(Group / GroupMember / GroupMessage)。
package group_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

const (
	RunIdle        = "idle"
	RunRunning     = "running"
	RunPaused      = "paused"
	RunWaitingUser = "waiting_user"
	RunError       = "error"
)

// Group 一个群聊房间。
type Group struct {
	ID                 int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Title              string `gorm:"column:title;type:text;not null;default:''"`
	CoordinatorAgentID int64  `gorm:"column:coordinator_agent_id;type:bigint;not null;default:0"`
	DepartmentID       int64  `gorm:"column:department_id;type:bigint;not null;default:0"`
	ProjectID          int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	RunStatus          string `gorm:"column:run_status;type:text;not null;default:'idle'"`
	RoundCount         int    `gorm:"column:round_count;type:int;not null;default:0"`
	Status             int    `gorm:"column:status;type:int;not null;default:1"`
	Pinned             bool   `gorm:"column:pinned;type:boolean;not null;default:0"`
	Createtime         int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime         int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Group) TableName() string { return "groups" }

func (g *Group) IsActive() bool { return g != nil && g.Status == consts.ACTIVE }

// CanAdvance 是否允许调度推进(无轮数上限, 仅看 run_status)。paused/error 不推进。
func (g *Group) CanAdvance() bool {
	if g == nil {
		return false
	}
	switch g.RunStatus {
	case RunIdle, RunRunning, RunWaitingUser:
		return true
	default:
		return false
	}
}

// Check 校验必填。
func (g *Group) Check(ctx context.Context) error {
	if g == nil {
		return i18n.NewError(ctx, code.GroupNotFound)
	}
	if strings.TrimSpace(g.Title) == "" {
		return i18n.NewError(ctx, code.GroupTitleRequired)
	}
	if g.CoordinatorAgentID <= 0 {
		return i18n.NewError(ctx, code.GroupCoordinatorRequired)
	}
	return nil
}
