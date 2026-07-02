package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

func TestToIssueItem(t *testing.T) {
	item := toIssueItem(&issue_svc.IssueDetail{
		Issue:  &issue_entity.Issue{ID: 4, Title: "t", State: "open", AgentStatus: "idle"},
		Labels: []*issue_entity.Label{{ID: 1, Name: "bug", Tone: "bug"}},
	})
	require.NotNil(t, item)
	assert.Equal(t, int64(4), item.ID)
	assert.Equal(t, "open", item.State)
	require.Len(t, item.Labels, 1)
	assert.Equal(t, "bug", item.Labels[0].Tone)
}

func TestToIssueItem_NoLabels(t *testing.T) {
	item := toIssueItem(&issue_svc.IssueDetail{Issue: &issue_entity.Issue{ID: 1, Title: "x", State: "open"}})
	assert.NotNil(t, item.Labels) // 非 nil 空切片，便于前端
	assert.Len(t, item.Labels, 0)
}

func TestToIssueItem_MapsStagePosition(t *testing.T) {
	d := &issue_svc.IssueDetail{Issue: &issue_entity.Issue{
		ID: 1, Stage: issue_entity.StageDoing, Position: 12.5, AssigneeAgentID: 3, SessionID: 4,
	}}
	item := toIssueItem(d)
	assert.Equal(t, "doing", item.Stage)
	assert.Equal(t, 12.5, item.Position)
	assert.Equal(t, int64(3), item.AssigneeAgentID)
	assert.Equal(t, int64(4), item.SessionID)
}
