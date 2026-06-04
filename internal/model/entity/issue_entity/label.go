package issue_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"agentre/internal/pkg/code"
)

// allowedTones 与前端 labelToneClassNames 的 key 一致；颜色由前端设计系统统一管理。
var allowedTones = map[string]struct{}{
	"auth": {}, "bug": {}, "critical": {}, "docs": {}, "feature": {},
	"hook": {}, "ops": {}, "perf": {}, "refactor": {}, "ui": {},
}

// Label issue 标签目录项。
type Label struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name       string `gorm:"column:name;type:text;not null"`
	Tone       string `gorm:"column:tone;type:text;not null;default:''"`
	SortOrder  int    `gorm:"column:sort_order;type:int;not null;default:0"`
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Label) TableName() string { return "labels" }

func (l *Label) IsActive() bool { return l != nil && l.Status == consts.ACTIVE }

// Check 校验标签名与色调。
func (l *Label) Check(ctx context.Context) error {
	if l == nil || strings.TrimSpace(l.Name) == "" {
		return i18n.NewError(ctx, code.IssueLabelNameRequired)
	}
	if _, ok := allowedTones[l.Tone]; !ok {
		return i18n.NewError(ctx, code.IssueLabelInvalidTone)
	}
	return nil
}

// IssueLabel issue ↔ label 多对多关联。
type IssueLabel struct {
	IssueID int64 `gorm:"column:issue_id;primaryKey"`
	LabelID int64 `gorm:"column:label_id;primaryKey"`
}

func (*IssueLabel) TableName() string { return "issue_labels" }
