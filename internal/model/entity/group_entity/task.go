package group_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// 任务卡状态机刻意小:open → done / canceled(spec §3.1)。
const (
	TaskStatusOpen     = "open"
	TaskStatusDone     = "done"
	TaskStatusCanceled = "canceled"
)

// GroupTask 群内一张任务卡(派活-交付的结构化痕迹)。
type GroupTask struct {
	ID               int64  `gorm:"column:id;primaryKey;autoIncrement"`
	GroupID          int64  `gorm:"column:group_id;type:bigint;not null;default:0"`
	TaskNo           int    `gorm:"column:task_no;type:int;not null;default:0"`
	Title            string `gorm:"column:title;type:text;not null;default:''"`
	Brief            string `gorm:"column:brief;type:text;not null;default:''"`
	CreatorMemberID  int64  `gorm:"column:creator_member_id;type:bigint;not null;default:0"`
	AssigneeMemberID int64  `gorm:"column:assignee_member_id;type:bigint;not null;default:0"`
	Status           string `gorm:"column:status;type:text;not null;default:'open'"`
	Result           string `gorm:"column:result;type:text;not null;default:''"`
	ParentTaskNo     int    `gorm:"column:parent_task_no;type:int;not null;default:0"`
	Createtime       int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime       int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*GroupTask) TableName() string { return "group_tasks" }

// Check 字段校验(单实体规则)。
func (t *GroupTask) Check(ctx context.Context) error {
	if t == nil {
		return i18n.NewError(ctx, code.GroupTaskNotFound)
	}
	if t.GroupID <= 0 || strings.TrimSpace(t.Title) == "" ||
		t.CreatorMemberID <= 0 || t.AssigneeMemberID <= 0 {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	switch t.Status {
	case TaskStatusOpen, TaskStatusDone, TaskStatusCanceled:
		return nil
	default:
		return i18n.NewError(ctx, code.InvalidParameter)
	}
}

func (t *GroupTask) IsOpen() bool { return t != nil && t.Status == TaskStatusOpen }

// CanComplete 仅执行人可交付,且卡必须仍 open。
func (t *GroupTask) CanComplete(memberID int64) bool {
	return t.IsOpen() && t.AssigneeMemberID == memberID
}

// CanCancel 仅建卡人或主持人可取消,且卡必须仍 open。
func (t *GroupTask) CanCancel(memberID int64, isHost bool) bool {
	return t.IsOpen() && (t.CreatorMemberID == memberID || isHost)
}
