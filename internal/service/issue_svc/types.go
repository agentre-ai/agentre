package issue_svc

import "github.com/agentre-ai/agentre/internal/model/entity/issue_entity"

type CreateIssueRequest struct {
	ProjectID int64
	Title     string
	Body      string
	Stage     string // "" = todo
	LabelIDs  []int64
}

type MoveIssueRequest struct {
	ID      int64
	Stage   string
	AfterID int64 // 0 = 落在目标列顶部
}

type UpdateIssueRequest struct {
	ID        int64
	ProjectID int64
	Title     string
	Body      string
	LabelIDs  []int64
}

type ListIssuesRequest struct {
	State     string
	ProjectID int64
	LabelIDs  []int64
	Sort      string
}

// IssueDetail issue + 已水合标签。
type IssueDetail struct {
	Issue  *issue_entity.Issue
	Labels []*issue_entity.Label
}

// ListIssuesResponse 列表结果 + open/closed 计数 + 各 stage 数量。
type ListIssuesResponse struct {
	Issues      []*IssueDetail
	OpenCount   int64
	ClosedCount int64
	StageCounts map[string]int64
}
