package issue_svc_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/issue_entity"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo"
	"github.com/agentre-ai/agentre/internal/repository/issue_repo/mock_issue_repo"
	"github.com/agentre-ai/agentre/internal/service/issue_svc"
)

func setupIssueSvc(t *testing.T) (
	context.Context,
	*mock_issue_repo.MockIssueRepo,
	*mock_issue_repo.MockLabelRepo,
	*mock_issue_repo.MockIssueLabelRepo,
	issue_svc.IssueSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mi := mock_issue_repo.NewMockIssueRepo(ctrl)
	ml := mock_issue_repo.NewMockLabelRepo(ctrl)
	mil := mock_issue_repo.NewMockIssueLabelRepo(ctrl)
	issue_repo.RegisterIssue(mi)
	issue_repo.RegisterLabel(ml)
	issue_repo.RegisterIssueLabel(mil)
	return context.Background(), mi, ml, mil, issue_svc.New()
}

func TestIssueSvcCreate_Happy(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)
	mi.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		i.ID = 9
		assert.Equal(t, issue_entity.StateOpen, i.State)
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(9), []int64{2}).Return(nil)

	got, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo", LabelIDs: []int64{2}})
	require.NoError(t, err)
	assert.Equal(t, int64(9), got.Issue.ID)
	require.Len(t, got.Labels, 1)
}

func TestIssueSvcCreate_EmptyTitleRejected(t *testing.T) {
	ctx, _, _, _, svc := setupIssueSvc(t)
	_, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "   "})
	assert.Error(t, err) // 校验在 repo.Create 之前拦截，无 mock 调用
}

func TestIssueSvcCreate_LabelNotFound(t *testing.T) {
	ctx, _, ml, _, svc := setupIssueSvc(t)
	// 请求两个 label，仓储只返回一个 → resolveLabels 报错，且不触达 Create/SetLabels。
	ml.EXPECT().ListByIDs(ctx, []int64{2, 3}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)

	_, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo", LabelIDs: []int64{2, 3}})
	assert.Error(t, err)
}

func TestIssueSvcCreate_DeduplicatesLabelIDs(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	ml.EXPECT().ListByIDs(ctx, []int64{2, 3}).
		Return([]*issue_entity.Label{
			{ID: 2, Name: "bug", Tone: "bug"},
			{ID: 3, Name: "feature", Tone: "feature"},
		}, nil)
	mi.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		i.ID = 9
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(9), []int64{2, 3}).Return(nil)

	got, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo", LabelIDs: []int64{2, 2, 3}})
	require.NoError(t, err)
	require.Len(t, got.Labels, 2)
	assert.Equal(t, int64(2), got.Labels[0].ID)
	assert.Equal(t, int64(3), got.Labels[1].ID)
}

func TestIssueSvcUpdate_Happy(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(5)).
		Return(&issue_entity.Issue{ID: 5, State: issue_entity.StateOpen, Title: "old"}, nil)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, int64(5), i.ID)
		assert.Equal(t, "new title", i.Title)
		assert.Equal(t, int64(7), i.ProjectID)
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(5), []int64{2}).Return(nil)

	got, err := svc.Update(ctx, &issue_svc.UpdateIssueRequest{
		ID: 5, ProjectID: 7, Title: "  new title  ", Body: "b", LabelIDs: []int64{2},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.Issue.ID)
	assert.Equal(t, "new title", got.Issue.Title)
	require.Len(t, got.Labels, 1)
}

func TestIssueSvcUpdate_NotFound(t *testing.T) {
	ctx, mi, _, _, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(404)).Return(nil, nil)

	_, err := svc.Update(ctx, &issue_svc.UpdateIssueRequest{ID: 404, Title: "x"})
	assert.Error(t, err)
}

func TestIssueSvcSetState_Close(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(3)).Return(&issue_entity.Issue{ID: 3, State: issue_entity.StateOpen}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, issue_entity.StateClosed, i.State)
		assert.NotZero(t, i.ClosedAt)
		return nil
	})
	mil.EXPECT().ListByIssue(ctx, int64(3)).Return(nil, nil)
	ml.EXPECT().ListByIDs(ctx, gomock.Nil()).Return(nil, nil)

	got, err := svc.SetState(ctx, 3, issue_entity.StateClosed)
	require.NoError(t, err)
	assert.True(t, got.Issue.IsClosed())
}

func TestIssueSvcSetState_InvalidState(t *testing.T) {
	ctx, _, _, _, svc := setupIssueSvc(t)
	// 非法 state 在 Find 之前被拦截，无任何 mock 调用。
	_, err := svc.SetState(ctx, 3, "weird")
	assert.Error(t, err)
}

func TestIssueSvcDelete_NotFound(t *testing.T) {
	ctx, mi, _, _, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(404)).Return(nil, nil)
	// Find 返回 nil → IssueNotFound，不应调用 Delete。

	err := svc.Delete(ctx, 404)
	assert.Error(t, err)
}

func TestIssueSvcList(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	req := &issue_svc.ListIssuesRequest{State: issue_entity.StateOpen, ProjectID: 7, Sort: "updated"}
	mi.EXPECT().List(ctx, issue_repo.ListFilter{
		State: issue_entity.StateOpen, ProjectID: 7, LabelIDs: nil, Sort: "updated",
	}).Return([]*issue_entity.Issue{
		{ID: 1, State: issue_entity.StateOpen},
		{ID: 2, State: issue_entity.StateOpen},
	}, nil)
	mil.EXPECT().ListByIssues(ctx, []int64{1, 2}).Return(map[int64][]int64{
		1: {10},
		2: {10, 20},
	}, nil)
	ml.EXPECT().List(ctx).Return([]*issue_entity.Label{
		{ID: 10, Name: "bug", Tone: "bug"},
		{ID: 20, Name: "feature", Tone: "feature"},
	}, nil)
	mi.EXPECT().CountByState(ctx, int64(7)).Return(int64(2), int64(5), nil)

	got, err := svc.List(ctx, req)
	require.NoError(t, err)
	require.Len(t, got.Issues, 2)
	assert.Equal(t, int64(2), got.OpenCount)
	assert.Equal(t, int64(5), got.ClosedCount)

	require.Len(t, got.Issues[0].Labels, 1)
	assert.Equal(t, int64(10), got.Issues[0].Labels[0].ID)
	require.Len(t, got.Issues[1].Labels, 2)
	assert.Equal(t, int64(10), got.Issues[1].Labels[0].ID)
	assert.Equal(t, int64(20), got.Issues[1].Labels[1].ID)
}

// 防御：确保 List 在底层仓储报错时把错误透传出来。
func TestIssueSvcList_RepoError(t *testing.T) {
	ctx, mi, _, _, svc := setupIssueSvc(t)
	mi.EXPECT().List(ctx, gomock.Any()).Return(nil, errors.New("boom"))

	_, err := svc.List(ctx, &issue_svc.ListIssuesRequest{})
	assert.Error(t, err)
}

// Create 已落库后 SetLabels 失败，错误必须透传给调用方。
func TestIssueSvcCreate_SetLabelsFail(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)
	mi.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		i.ID = 9
		return nil
	})
	mil.EXPECT().SetLabels(ctx, int64(9), []int64{2}).Return(errors.New("db error"))

	_, err := svc.Create(ctx, &issue_svc.CreateIssueRequest{Title: "demo", LabelIDs: []int64{2}})
	assert.Error(t, err)
}

// Update 已落库后 SetLabels 失败，错误必须透传给调用方。
func TestIssueSvcUpdate_SetLabelsFail(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(5)).
		Return(&issue_entity.Issue{ID: 5, State: issue_entity.StateOpen, Title: "old"}, nil)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).Return(nil)
	mil.EXPECT().SetLabels(ctx, int64(5), []int64{2}).Return(errors.New("db error"))

	_, err := svc.Update(ctx, &issue_svc.UpdateIssueRequest{ID: 5, Title: "new", LabelIDs: []int64{2}})
	assert.Error(t, err)
}

func TestIssueSvcGet_Happy(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(5)).Return(&issue_entity.Issue{ID: 5, State: issue_entity.StateOpen}, nil)
	mil.EXPECT().ListByIssue(ctx, int64(5)).Return([]int64{2}, nil)
	ml.EXPECT().ListByIDs(ctx, []int64{2}).
		Return([]*issue_entity.Label{{ID: 2, Name: "bug", Tone: "bug"}}, nil)

	got, err := svc.Get(ctx, 5)
	require.NoError(t, err)
	assert.Equal(t, int64(5), got.Issue.ID)
	require.Len(t, got.Labels, 1)
	assert.Equal(t, int64(2), got.Labels[0].ID)
}

func TestIssueSvcGet_NotFound(t *testing.T) {
	ctx, mi, _, _, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(404)).Return(nil, nil)
	// Find 返回 nil → IssueNotFound，不应触发 hydrate（ListByIssue/ListByIDs）。

	_, err := svc.Get(ctx, 404)
	assert.Error(t, err)
}

func TestIssueSvcDelete_Happy(t *testing.T) {
	ctx, mi, _, _, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(5)).Return(&issue_entity.Issue{ID: 5, State: issue_entity.StateOpen}, nil)
	mi.EXPECT().Delete(ctx, int64(5)).Return(nil)

	err := svc.Delete(ctx, 5)
	require.NoError(t, err)
}

func TestIssueSvcSetState_Reopen(t *testing.T) {
	ctx, mi, ml, mil, svc := setupIssueSvc(t)
	mi.EXPECT().Find(ctx, int64(3)).
		Return(&issue_entity.Issue{ID: 3, State: issue_entity.StateClosed, ClosedAt: 123}, nil)
	mi.EXPECT().Update(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, i *issue_entity.Issue) error {
		assert.Equal(t, issue_entity.StateOpen, i.State)
		assert.Equal(t, int64(0), i.ClosedAt)
		return nil
	})
	mil.EXPECT().ListByIssue(ctx, int64(3)).Return(nil, nil)
	ml.EXPECT().ListByIDs(ctx, gomock.Nil()).Return(nil, nil)

	got, err := svc.SetState(ctx, 3, issue_entity.StateOpen)
	require.NoError(t, err)
	assert.True(t, got.Issue.IsOpen())
}

func TestIssueSvcListLabels(t *testing.T) {
	ctx, _, ml, _, svc := setupIssueSvc(t)
	ml.EXPECT().List(ctx).Return([]*issue_entity.Label{
		{ID: 10, Name: "bug", Tone: "bug"},
		{ID: 20, Name: "feature", Tone: "feature"},
	}, nil)

	got, err := svc.ListLabels(ctx)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, int64(10), got[0].ID)
	assert.Equal(t, int64(20), got[1].ID)
}
