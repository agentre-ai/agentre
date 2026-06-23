// Package workflow_entity 维护流程(剧本库)的充血实体。流程是写给群主持人读的
// 自由 Markdown SOP,与部门/项目正交(spec §3.2/§6.1)。
package workflow_entity

import (
	"context"
	"strings"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"

	"github.com/agentre-ai/agentre/internal/pkg/code"
)

// Workflow 一条流程(SOP 剧本)。
type Workflow struct {
	ID         int64  `gorm:"column:id;primaryKey;autoIncrement"`
	Name       string `gorm:"column:name;type:text;not null;default:''"`
	Content    string `gorm:"column:content;type:text;not null;default:''"`
	Status     int    `gorm:"column:status;type:int;not null;default:1"`
	Createtime int64  `gorm:"column:createtime;type:bigint;not null;default:0"`
	Updatetime int64  `gorm:"column:updatetime;type:bigint;not null;default:0"`
}

func (*Workflow) TableName() string { return "workflows" }

func (w *Workflow) IsActive() bool { return w != nil && w.Status == consts.ACTIVE }

// Check 字段校验。
func (w *Workflow) Check(ctx context.Context) error {
	if w == nil {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	if strings.TrimSpace(w.Name) == "" {
		return i18n.NewError(ctx, code.InvalidParameter)
	}
	return nil
}
