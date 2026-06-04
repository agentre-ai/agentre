package issue_repo

import (
	"context"
	"errors"

	"github.com/cago-frame/cago/database/db"
	"github.com/cago-frame/cago/pkg/consts"
	"gorm.io/gorm"

	"agentre/internal/model/entity/issue_entity"
)

//go:generate mockgen -source label.go -destination mock_issue_repo/mock_label.go

// LabelRepo 标签目录仓储。
type LabelRepo interface {
	Find(ctx context.Context, id int64) (*issue_entity.Label, error)
	List(ctx context.Context) ([]*issue_entity.Label, error)
	ListByIDs(ctx context.Context, ids []int64) ([]*issue_entity.Label, error)
}

var defaultLabel LabelRepo

func Label() LabelRepo             { return defaultLabel }
func RegisterLabel(impl LabelRepo) { defaultLabel = impl }
func NewLabel() LabelRepo          { return &labelRepo{} }

type labelRepo struct{}

func (r *labelRepo) Find(ctx context.Context, id int64) (*issue_entity.Label, error) {
	out := &issue_entity.Label{}
	err := db.Ctx(ctx).Where("id = ? AND status = ?", id, consts.ACTIVE).First(out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (r *labelRepo) List(ctx context.Context) ([]*issue_entity.Label, error) {
	var rows []*issue_entity.Label
	err := db.Ctx(ctx).Where("status = ?", consts.ACTIVE).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

func (r *labelRepo) ListByIDs(ctx context.Context, ids []int64) ([]*issue_entity.Label, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rows []*issue_entity.Label
	err := db.Ctx(ctx).Where("id IN ? AND status = ?", ids, consts.ACTIVE).
		Order("sort_order ASC, id ASC").Find(&rows).Error
	return rows, err
}

// IssueLabelRepo issue ↔ label 关联仓储。
type IssueLabelRepo interface {
	SetLabels(ctx context.Context, issueID int64, labelIDs []int64) error
	ListByIssue(ctx context.Context, issueID int64) ([]int64, error)
	ListByIssues(ctx context.Context, issueIDs []int64) (map[int64][]int64, error)
}

var defaultIssueLabel IssueLabelRepo

func IssueLabel() IssueLabelRepo             { return defaultIssueLabel }
func RegisterIssueLabel(impl IssueLabelRepo) { defaultIssueLabel = impl }
func NewIssueLabel() IssueLabelRepo          { return &issueLabelRepo{} }

type issueLabelRepo struct{}

// SetLabels 用一次事务覆盖 issue 的全部标签关联。
func (r *issueLabelRepo) SetLabels(ctx context.Context, issueID int64, labelIDs []int64) error {
	return db.Ctx(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("issue_id = ?", issueID).
			Delete(&issue_entity.IssueLabel{}).Error; err != nil {
			return err
		}
		labelIDs = uniqueInt64s(labelIDs)
		if len(labelIDs) == 0 {
			return nil
		}
		rows := make([]issue_entity.IssueLabel, 0, len(labelIDs))
		for _, id := range labelIDs {
			rows = append(rows, issue_entity.IssueLabel{IssueID: issueID, LabelID: id})
		}
		return tx.Create(&rows).Error
	})
}

func (r *issueLabelRepo) ListByIssue(ctx context.Context, issueID int64) ([]int64, error) {
	var ids []int64
	err := db.Ctx(ctx).Model(&issue_entity.IssueLabel{}).
		Where("issue_id = ?", issueID).
		Order("label_id ASC").
		Pluck("label_id", &ids).Error
	return ids, err
}

func (r *issueLabelRepo) ListByIssues(ctx context.Context, issueIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(issueIDs) == 0 {
		return out, nil
	}
	var rows []issue_entity.IssueLabel
	if err := db.Ctx(ctx).Where("issue_id IN ?", issueIDs).
		Order("issue_id ASC, label_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.IssueID] = append(out[row.IssueID], row.LabelID)
	}
	return out, nil
}

func uniqueInt64s(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
