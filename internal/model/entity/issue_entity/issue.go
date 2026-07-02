// Package issue_entity 维护 Issue 的充血实体。
package issue_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

const (
	StateOpen   = "open"
	StateClosed = "closed"

	StageTodo   = "todo"
	StageDoing  = "doing"
	StageReview = "review"
	StageDone   = "done"

	AgentStatusIdle = "idle"

	SourceManual = "manual"
)

// IsKnownStage 空串视为 todo 合法（迁移默认 / 旧行兜底）。
func IsKnownStage(s string) bool {
	switch s {
	case "", StageTodo, StageDoing, StageReview, StageDone:
		return true
	default:
		return false
	}
}

// Issue 一条 issue 记录。
type Issue struct {
	ID              int64   `gorm:"column:id;primaryKey;autoIncrement"`
	ProjectID       int64   `gorm:"column:project_id;type:bigint;not null;default:0"`
	Title           string  `gorm:"column:title;type:text;not null"`
	Body            string  `gorm:"column:body;type:text;not null;default:''"`
	State           string  `gorm:"column:state;type:text;not null;default:'open'"`
	AgentStatus     string  `gorm:"column:agent_status;type:text;not null;default:'idle'"`
	Stage           string  `gorm:"column:stage;type:text;not null;default:'todo'"`
	Position        float64 `gorm:"column:position;type:real;not null;default:0"`
	AssigneeAgentID int64   `gorm:"column:assignee_agent_id;type:bigint;not null;default:0"`
	SessionID       int64   `gorm:"column:session_id;type:bigint;not null;default:0"`
	Source          string  `gorm:"column:source;type:text;not null;default:'manual'"`
	ClosedAt        int64   `gorm:"column:closed_at;type:bigint;not null;default:0"`
	Status          int     `gorm:"column:status;type:int;not null;default:1"`
	Createtime      int64   `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime      int64   `gorm:"column:updatetime;type:bigint;not null;default:0"`
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

// SetStage 置阶段并同步 state：done=关闭，离开 done=重开。
func (i *Issue) SetStage(stage string, now int64) {
	wasDone := i.Stage == StageDone
	i.Stage = stage
	if stage == StageDone {
		i.Close(now)
		return
	}
	if wasDone {
		i.Reopen()
	}
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
	if !IsKnownStage(i.Stage) {
		return i18n.NewError(ctx, code.IssueInvalidState)
	}
	if i.ProjectID < 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
