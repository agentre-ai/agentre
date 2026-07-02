// Package issue_svc 提供 Issue 模块的应用服务。
package issue_svc

import (
	"context"
	"strings"
	"time"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/cago-frame/cago/pkg/i18n"
	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
)

const positionStep = 65536.0

// IssueSvc Issue 模块应用服务。
type IssueSvc interface {
	Create(ctx context.Context, req *CreateIssueRequest) (*IssueDetail, error)
	Update(ctx context.Context, req *UpdateIssueRequest) (*IssueDetail, error)
	SetState(ctx context.Context, id int64, state string) (*IssueDetail, error)
	Delete(ctx context.Context, id int64) error
	Get(ctx context.Context, id int64) (*IssueDetail, error)
	List(ctx context.Context, req *ListIssuesRequest) (*ListIssuesResponse, error)
	ListLabels(ctx context.Context) ([]*issue_entity.Label, error)
	Move(ctx context.Context, req *MoveIssueRequest) (*IssueDetail, error)
}

type issueSvc struct {
	now func() int64
}

var defaultIssue IssueSvc = &issueSvc{now: func() int64 { return time.Now().UnixMilli() }}

// Default 取默认服务单例。
func Default() IssueSvc { return defaultIssue }

// SetDefault 注入服务实现（测试 / bootstrap 装配用）。
func SetDefault(svc IssueSvc) { defaultIssue = svc }

// New 构造默认实现。
func New() IssueSvc {
	return &issueSvc{now: func() int64 { return time.Now().UnixMilli() }}
}

func (s *issueSvc) Create(ctx context.Context, req *CreateIssueRequest) (*IssueDetail, error) {
	now := s.now()
	labelIDs := uniqueInt64s(req.LabelIDs)
	stage := req.Stage
	if stage == "" {
		stage = issue_entity.StageTodo
	}
	issue := &issue_entity.Issue{
		ProjectID:   req.ProjectID,
		Title:       strings.TrimSpace(req.Title),
		Body:        req.Body,
		State:       issue_entity.StateOpen,
		Stage:       stage,
		AgentStatus: issue_entity.AgentStatusIdle,
		Source:      issue_entity.SourceManual,
		Status:      consts.ACTIVE,
		Createtime:  now,
		Updatetime:  now,
	}
	if stage == issue_entity.StageDone {
		issue.SetStage(issue_entity.StageDone, now)
	}
	if err := issue.Check(ctx); err != nil {
		return nil, err
	}
	pos, err := s.appendPosition(ctx, stage)
	if err != nil {
		return nil, err
	}
	issue.Position = pos
	labels, err := s.resolveLabels(ctx, labelIDs)
	if err != nil {
		return nil, err
	}
	if err := issue_repo.Issue().Create(ctx, issue); err != nil {
		return nil, err
	}
	// TODO(v1): Create 与 SetLabels 目前非原子——SetLabels 失败会留下无标签的 issue 行。
	// 维持非事务以保证 service 可纯 mock 单测（项目规约：service 单测不接 DB）；
	// 若后续标签写入可靠性变重要，按 agent_svc.Delete 的 db.Ctx(ctx).Transaction 模式包裹。
	if err := issue_repo.IssueLabel().SetLabels(ctx, issue.ID, labelIDs); err != nil {
		logger.Ctx(ctx).Warn("issue_svc.Create: set labels failed",
			zap.Int64("issueId", issue.ID), zap.Error(err))
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}

func (s *issueSvc) Update(ctx context.Context, req *UpdateIssueRequest) (*IssueDetail, error) {
	labelIDs := uniqueInt64s(req.LabelIDs)
	issue, err := issue_repo.Issue().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	issue.ProjectID = req.ProjectID
	issue.Title = strings.TrimSpace(req.Title)
	issue.Body = req.Body
	if err := issue.Check(ctx); err != nil {
		return nil, err
	}
	labels, err := s.resolveLabels(ctx, labelIDs)
	if err != nil {
		return nil, err
	}
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	// TODO(v1): Update 与 SetLabels 目前非原子——SetLabels 失败会留下标签未更新的 issue 行。
	// 维持非事务以保证 service 可纯 mock 单测（项目规约：service 单测不接 DB）；
	// 若后续标签写入可靠性变重要，按 agent_svc.Delete 的 db.Ctx(ctx).Transaction 模式包裹。
	if err := issue_repo.IssueLabel().SetLabels(ctx, issue.ID, labelIDs); err != nil {
		logger.Ctx(ctx).Warn("issue_svc.Update: set labels failed",
			zap.Int64("issueId", issue.ID), zap.Error(err))
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}

func (s *issueSvc) SetState(ctx context.Context, id int64, state string) (*IssueDetail, error) {
	if state != issue_entity.StateOpen && state != issue_entity.StateClosed {
		return nil, i18n.NewError(ctx, code.IssueInvalidState)
	}
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	if state == issue_entity.StateClosed {
		issue.Close(s.now())
	} else {
		issue.Reopen()
	}
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	return s.hydrate(ctx, issue)
}

func (s *issueSvc) Delete(ctx context.Context, id int64) error {
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return err
	}
	if issue == nil {
		return i18n.NewError(ctx, code.IssueNotFound)
	}
	return issue_repo.Issue().Delete(ctx, id)
}

func (s *issueSvc) Get(ctx context.Context, id int64) (*IssueDetail, error) {
	issue, err := issue_repo.Issue().Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	return s.hydrate(ctx, issue)
}

func (s *issueSvc) List(ctx context.Context, req *ListIssuesRequest) (*ListIssuesResponse, error) {
	issues, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{
		State:     req.State,
		ProjectID: req.ProjectID,
		LabelIDs:  req.LabelIDs,
		Sort:      req.Sort,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(issues))
	for _, it := range issues {
		ids = append(ids, it.ID)
	}
	labelMap, err := issue_repo.IssueLabel().ListByIssues(ctx, ids)
	if err != nil {
		return nil, err
	}
	allLabels, err := issue_repo.Label().List(ctx)
	if err != nil {
		return nil, err
	}
	byID := map[int64]*issue_entity.Label{}
	for _, l := range allLabels {
		byID[l.ID] = l
	}
	details := make([]*IssueDetail, 0, len(issues))
	for _, it := range issues {
		labels := make([]*issue_entity.Label, 0)
		for _, lid := range labelMap[it.ID] {
			if l := byID[lid]; l != nil {
				labels = append(labels, l)
			}
		}
		details = append(details, &IssueDetail{Issue: it, Labels: labels})
	}
	open, closed, err := issue_repo.Issue().CountByState(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	stageCounts, err := issue_repo.Issue().StageCounts(ctx, issue_repo.ListFilter{
		ProjectID: req.ProjectID, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return &ListIssuesResponse{Issues: details, OpenCount: open, ClosedCount: closed, StageCounts: stageCounts}, nil
}

func (s *issueSvc) ListLabels(ctx context.Context) ([]*issue_entity.Label, error) {
	return issue_repo.Label().List(ctx)
}

func (s *issueSvc) resolveLabels(ctx context.Context, ids []int64) ([]*issue_entity.Label, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	labels, err := issue_repo.Label().ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	if len(labels) != len(ids) {
		return nil, i18n.NewError(ctx, code.IssueLabelNotFound)
	}
	return labels, nil
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

func (s *issueSvc) hydrate(ctx context.Context, issue *issue_entity.Issue) (*IssueDetail, error) {
	ids, err := issue_repo.IssueLabel().ListByIssue(ctx, issue.ID)
	if err != nil {
		return nil, err
	}
	labels, err := issue_repo.Label().ListByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	return &IssueDetail{Issue: issue, Labels: labels}, nil
}

// appendPosition 返回目标 stage 末位之后的 position。
func (s *issueSvc) appendPosition(ctx context.Context, stage string) (float64, error) {
	rows, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{Stage: stage, Sort: "position"})
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return positionStep, nil
	}
	return rows[len(rows)-1].Position + positionStep, nil
}

// Move 改 stage + 计算列内 position（AfterID 之后 / 顶部）。
func (s *issueSvc) Move(ctx context.Context, req *MoveIssueRequest) (*IssueDetail, error) {
	if !issue_entity.IsKnownStage(req.Stage) || req.Stage == "" {
		return nil, i18n.NewError(ctx, code.IssueInvalidState)
	}
	issue, err := issue_repo.Issue().Find(ctx, req.ID)
	if err != nil {
		return nil, err
	}
	if issue == nil {
		return nil, i18n.NewError(ctx, code.IssueNotFound)
	}
	siblings, err := issue_repo.Issue().List(ctx, issue_repo.ListFilter{Stage: req.Stage, Sort: "position"})
	if err != nil {
		return nil, err
	}
	pos := computePosition(siblings, req.ID, req.AfterID)
	issue.SetStage(req.Stage, s.now())
	issue.Position = pos
	logger.Ctx(ctx).Info("issue_svc.Move: reposition",
		zap.Int64("issueId", req.ID), zap.String("stage", req.Stage), zap.Float64("position", pos))
	if err := issue_repo.Issue().Update(ctx, issue); err != nil {
		return nil, err
	}
	return s.hydrate(ctx, issue)
}

// computePosition 在 siblings（按 position 升序，可能含自身）中，
// 把卡片放到 afterID 之后。afterID=0 → 顶部。落点相邻两卡取中点；顶/底外扩 step。
func computePosition(siblings []*issue_entity.Issue, movingID, afterID int64) float64 {
	// 过滤掉自身，得到目标列的稳定序列。
	seq := make([]*issue_entity.Issue, 0, len(siblings))
	for _, it := range siblings {
		if it.ID != movingID {
			seq = append(seq, it)
		}
	}
	if len(seq) == 0 {
		return positionStep
	}
	if afterID == 0 {
		return seq[0].Position - positionStep
	}
	for idx, it := range seq {
		if it.ID != afterID {
			continue
		}
		if idx == len(seq)-1 {
			return it.Position + positionStep
		}
		return (it.Position + seq[idx+1].Position) / 2
	}
	// afterID 不在目标列（异常）→ 落底。
	return seq[len(seq)-1].Position + positionStep
}
