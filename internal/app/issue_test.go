package app

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"agentre/internal/model/entity/issue_entity"
	"agentre/internal/service/issue_svc"
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
