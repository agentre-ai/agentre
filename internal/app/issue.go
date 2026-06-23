package app

import (
	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

// IssueItem issue 摘要（含标签），列表 / 看板 / 详情共用。
type IssueItem struct {
	ID          int64        `json:"id"`
	ProjectID   int64        `json:"projectID"`
	Title       string       `json:"title"`
	Body        string       `json:"body"`
	State       string       `json:"state"`
	AgentStatus string       `json:"agentStatus"`
	Source      string       `json:"source"`
	ClosedAt    int64        `json:"closedAt"`
	Createtime  int64        `json:"createtime"`
	Updatetime  int64        `json:"updatetime"`
	Labels      []*LabelItem `json:"labels"`
}

// LabelItem 标签 DTO。
type LabelItem struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Tone string `json:"tone"`
}

type IssueListRequest struct {
	State     string  `json:"state"`
	ProjectID int64   `json:"projectID"`
	LabelIDs  []int64 `json:"labelIDs"`
	Sort      string  `json:"sort"`
}

type IssueListResponse struct {
	Issues      []*IssueItem `json:"issues"`
	OpenCount   int64        `json:"openCount"`
	ClosedCount int64        `json:"closedCount"`
}

type IssueCreateRequest struct {
	ProjectID int64   `json:"projectID"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	LabelIDs  []int64 `json:"labelIDs"`
}

type IssueUpdateRequest struct {
	ID        int64   `json:"id"`
	ProjectID int64   `json:"projectID"`
	Title     string  `json:"title"`
	Body      string  `json:"body"`
	LabelIDs  []int64 `json:"labelIDs"`
}

type IssueSetStateRequest struct {
	ID    int64  `json:"id"`
	State string `json:"state"`
}

func toLabelItem(l *issue_entity.Label) *LabelItem {
	return &LabelItem{ID: l.ID, Name: l.Name, Tone: l.Tone}
}

func toIssueItem(d *issue_svc.IssueDetail) *IssueItem {
	labels := make([]*LabelItem, 0, len(d.Labels))
	for _, l := range d.Labels {
		labels = append(labels, toLabelItem(l))
	}
	return &IssueItem{
		ID:          d.Issue.ID,
		ProjectID:   d.Issue.ProjectID,
		Title:       d.Issue.Title,
		Body:        d.Issue.Body,
		State:       d.Issue.State,
		AgentStatus: d.Issue.AgentStatus,
		Source:      d.Issue.Source,
		ClosedAt:    d.Issue.ClosedAt,
		Createtime:  d.Issue.Createtime,
		Updatetime:  d.Issue.Updatetime,
		Labels:      labels,
	}
}

// IssueList 列出 issue。
func (a *App) IssueList(req *IssueListRequest) (*IssueListResponse, error) {
	resp, err := issue_svc.Default().List(a.ctx, &issue_svc.ListIssuesRequest{
		State: req.State, ProjectID: req.ProjectID, LabelIDs: req.LabelIDs, Sort: req.Sort,
	})
	if err != nil {
		return nil, err
	}
	items := make([]*IssueItem, 0, len(resp.Issues))
	for _, d := range resp.Issues {
		items = append(items, toIssueItem(d))
	}
	return &IssueListResponse{Issues: items, OpenCount: resp.OpenCount, ClosedCount: resp.ClosedCount}, nil
}

// IssueGet 取单条 issue。
func (a *App) IssueGet(id int64) (*IssueItem, error) {
	d, err := issue_svc.Default().Get(a.ctx, id)
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueCreate 创建 issue。
func (a *App) IssueCreate(req *IssueCreateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().Create(a.ctx, &issue_svc.CreateIssueRequest{
		ProjectID: req.ProjectID, Title: req.Title, Body: req.Body, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueUpdate 更新 issue。
func (a *App) IssueUpdate(req *IssueUpdateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().Update(a.ctx, &issue_svc.UpdateIssueRequest{
		ID: req.ID, ProjectID: req.ProjectID, Title: req.Title, Body: req.Body, LabelIDs: req.LabelIDs,
	})
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueSetState 关闭 / 重新打开 issue。
func (a *App) IssueSetState(req *IssueSetStateRequest) (*IssueItem, error) {
	d, err := issue_svc.Default().SetState(a.ctx, req.ID, req.State)
	if err != nil {
		return nil, err
	}
	return toIssueItem(d), nil
}

// IssueDelete 软删 issue。
func (a *App) IssueDelete(id int64) error {
	return issue_svc.Default().Delete(a.ctx, id)
}

// IssueListLabels 列出全部标签。
func (a *App) IssueListLabels() ([]*LabelItem, error) {
	labels, err := issue_svc.Default().ListLabels(a.ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*LabelItem, 0, len(labels))
	for _, l := range labels {
		items = append(items, toLabelItem(l))
	}
	return items, nil
}
