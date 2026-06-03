// Package issue_repo 提供 Issue / Label 的持久化访问。
package issue_repo

import (
	"context"
	"errors"
	"time"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/issue_entity"
)

//go:generate mockgen -source issue.go -destination mock_issue_repo/mock_issue.go

// ListFilter List 查询过滤条件。
type ListFilter struct {
	State     string  // "" = 不筛选；open / closed
	ProjectID int64   // 0 = 不筛选
	LabelIDs  []int64 // 非空 = 仅含这些 label 的 issue
	Sort      string  // 预留；当前恒按 updatetime DESC
}

// IssueRepo Issue 仓储接口。
type IssueRepo interface {
	Create(ctx context.Context, i *issue_entity.Issue) error
	Update(ctx context.Context, i *issue_entity.Issue) error
	Find(ctx context.Context, id int64) (*issue_entity.Issue, error)
	List(ctx context.Context, filter ListFilter) ([]*issue_entity.Issue, error)
	CountByState(ctx context.Context, projectID int64) (open int64, closed int64, err error)
	Delete(ctx context.Context, id int64) error
}

var defaultIssue IssueRepo

func Issue() IssueRepo             { return defaultIssue }
func RegisterIssue(impl IssueRepo) { defaultIssue = impl }
func NewIssue() IssueRepo          { return &issueRepo{} }

type issueRepo struct{}

func (r *issueRepo) Create(ctx context.Context, i *issue_entity.Issue) error {
	now := time.Now().UnixMilli()
	if i.Createtime == 0 {
		i.Createtime = now
	}
	i.Updatetime = now
	return db.Ctx(ctx).Create(i).Error
}

func (r *issueRepo) Update(ctx context.Context, i *issue_entity.Issue) error {
	i.Updatetime = time.Now().UnixMilli()
	return db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Where("id = ? AND status = ?", i.ID, consts.ACTIVE).
		Updates(map[string]any{
			"project_id":   i.ProjectID,
			"title":        i.Title,
			"body":         i.Body,
			"state":        i.State,
			"agent_status": i.AgentStatus,
			"closed_at":    i.ClosedAt,
			"updatetime":   i.Updatetime,
		}).Error
}

func (r *issueRepo) Find(ctx context.Context, id int64) (*issue_entity.Issue, error) {
	out := &issue_entity.Issue{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *issueRepo) List(ctx context.Context, filter ListFilter) ([]*issue_entity.Issue, error) {
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).Where("status = ?", consts.ACTIVE)
	if filter.State != "" {
		q = q.Where("state = ?", filter.State)
	}
	if filter.ProjectID > 0 {
		q = q.Where("project_id = ?", filter.ProjectID)
	}
	if len(filter.LabelIDs) > 0 {
		sub := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
			Select("issue_id").Where("label_id IN ?", filter.LabelIDs)
		q = q.Where("id IN (?)", sub)
	}
	var rows []*issue_entity.Issue
	err := q.Order("updatetime DESC, id DESC").Find(&rows).Error
	return rows, err
}

func (r *issueRepo) CountByState(ctx context.Context, projectID int64) (int64, int64, error) {
	type agg struct {
		State string
		Cnt   int64
	}
	q := db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Select("state, count(*) as cnt").
		Where("status = ?", consts.ACTIVE)
	if projectID > 0 {
		q = q.Where("project_id = ?", projectID)
	}
	var rows []agg
	if err := q.Group("state").Scan(&rows).Error; err != nil {
		return 0, 0, err
	}
	var open, closed int64
	for _, row := range rows {
		switch row.State {
		case issue_entity.StateOpen:
			open = row.Cnt
		case issue_entity.StateClosed:
			closed = row.Cnt
		}
	}
	return open, closed, nil
}

func (r *issueRepo) Delete(ctx context.Context, id int64) error {
	return db.Ctx(ctx).Model(&issue_entity.Issue{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":     consts.DELETE,
			"updatetime": time.Now().UnixMilli(),
		}).Error
}
