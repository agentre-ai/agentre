// Package issue_entity 维护 Issue 的充血实体。
package issue_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

const (
	StateOpen   = "open"
	StateClosed = "closed"

	AgentStatusIdle = "idle"

	SourceManual = "manual"
)

// Issue 一条 issue 记录。
type Issue struct {
	ID          int64  `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID   int64  `gorm:"column:project_id;type:bigint;not null;default:0"`
	Title       string `gorm:"column:title;type:text;not null"`
	Body        string `gorm:"column:body;type:text;not null;default:''"`
	State       string `gorm:"column:state;type:text;not null;default:'open'"`
	AgentStatus string `gorm:"column:agent_status;type:text;not null;default:'idle'"`
	Source      string `gorm:"column:source;type:text;not null;default:'manual'"`
	ClosedAt    int64  `gorm:"column:closed_at;type:bigint;not null;default:0"`
	Status      int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime  int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime  int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Issue) TableName() string { return "issues" }

func (i *Issue) IsActive() bool { return i != nil && i.Status == consts.ACTIVE }
func (i *Issue) IsOpen() bool   { return i != nil && i.State == StateOpen }
func (i *Issue) IsClosed() bool { return i != nil && i.State == StateClosed }

// Close 关闭 issue：置 state=closed 并记录关闭时间（unix ms）。
func (i *Issue) Close(now int64) {
	i.State = StateClosed
	i.ClosedAt = now
}

// Reopen 重新打开 issue：置 state=open 并清空关闭时间。
func (i *Issue) Reopen() {
	i.State = StateOpen
	i.ClosedAt = 0
}

// Check 校验必填字段与枚举合法性。
func (i *Issue) Check(ctx context.Context) error {
	if i == nil {
		return i18n.NewError(ctx, code.IssueNotFound)
	}
	if strings.TrimSpace(i.Title) == "" {
		return i18n.NewError(ctx, code.IssueTitleRequired)
	}
	if i.State != StateOpen && i.State != StateClosed {
		return i18n.NewError(ctx, code.IssueInvalidState)
	}
	if i.ProjectID < 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
