package project_svc_test

import (
	"context"
	"os"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/project_entity"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/chat_repo/mock_chat_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo"
	"github.com/agentre-ai/agentre/internal/repository/project_repo/mock_project_repo"
	"github.com/agentre-ai/agentre/internal/service/project_svc"
)

func setupProjectSvc(t *testing.T) (
	context.Context,
	*mock_project_repo.MockProjectRepo,
	*mock_project_repo.MockProjectAgentRepo,
	*mock_chat_repo.MockSessionRepo,
	project_svc.ProjectSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	mp := mock_project_repo.NewMockProjectRepo(ctrl)
	mpa := mock_project_repo.NewMockProjectAgentRepo(ctrl)
	ms := mock_chat_repo.NewMockSessionRepo(ctrl)
	project_repo.RegisterProject(mp)
	project_repo.RegisterProjectAgent(mpa)
	chat_repo.RegisterSession(ms)
	return context.Background(), mp, mpa, ms, project_svc.New()
}

func TestProjectSvcCreate_Happy(t *testing.T) {
	convey.Convey("Create 成功路径：未重名 + 路径存在 + 校验通过", t, func() {
		ctx, mp, _, _, svc := setupProjectSvc(t)
		tmp := t.TempDir()
		mp.EXPECT().FindByName(ctx, int64(0), "Agentre").Return(nil, nil)
		mp.EXPECT().NextSortOrder(ctx, int64(0)).Return(3, nil)
		mp.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, p *project_entity.Project) error {
			p.ID = 42
			convey.So(p.SortOrder, convey.ShouldEqual, 3)
			return nil
		})

		got, err := svc.Create(ctx, &project_svc.CreateProjectRequest{
			Name: "Agentre", Path: tmp, Color: "agent-1",
		})
		require.NoError(t, err)
		convey.So(got.ID, convey.ShouldEqual, 42)
		convey.So(got.Path, convey.ShouldEqual, tmp)
	})
}

func TestProjectSvcCreate_DuplicateName(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	tmp := t.TempDir()
	mp.EXPECT().FindByName(ctx, int64(0), "Agentre").Return(&project_entity.Project{ID: 1}, nil)

	_, err := svc.Create(ctx, &project_svc.CreateProjectRequest{
		Name: "Agentre", Path: tmp, Color: "agent-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "项目")
}

func TestProjectSvcReorder(t *testing.T) {
	convey.Convey("Reorder 按同级完整 ID 列表持久化排序", t, func() {
		ctx, mp, _, _, svc := setupProjectSvc(t)
		mp.EXPECT().ListByParent(ctx, int64(7)).Return([]*project_entity.Project{
			{ID: 1, ParentID: 7, Name: "A"},
			{ID: 2, ParentID: 7, Name: "B"},
			{ID: 3, ParentID: 7, Name: "C"},
		}, nil)
		mp.EXPECT().ReorderSiblings(ctx, int64(7), []int64{3, 1, 2}).Return(nil)

		err := svc.Reorder(ctx, &project_svc.ReorderProjectsRequest{
			ParentID:   7,
			OrderedIDs: []int64{3, 1, 2},
		})
		require.NoError(t, err)
	})

	convey.Convey("Reorder 拒绝缺失 sibling 的局部列表", t, func() {
		ctx, mp, _, _, svc := setupProjectSvc(t)
		mp.EXPECT().ListByParent(ctx, int64(7)).Return([]*project_entity.Project{
			{ID: 1, ParentID: 7, Name: "A"},
			{ID: 2, ParentID: 7, Name: "B"},
			{ID: 3, ParentID: 7, Name: "C"},
		}, nil)

		err := svc.Reorder(ctx, &project_svc.ReorderProjectsRequest{
			ParentID:   7,
			OrderedIDs: []int64{3, 1},
		})
		require.Error(t, err)
	})

	convey.Convey("Reorder 拒绝重复 ID", t, func() {
		ctx, mp, _, _, svc := setupProjectSvc(t)
		mp.EXPECT().ListByParent(ctx, int64(7)).Return([]*project_entity.Project{
			{ID: 1, ParentID: 7, Name: "A"},
			{ID: 2, ParentID: 7, Name: "B"},
		}, nil)

		err := svc.Reorder(ctx, &project_svc.ReorderProjectsRequest{
			ParentID:   7,
			OrderedIDs: []int64{1, 1},
		})
		require.Error(t, err)
	})
}

func TestProjectSvcCreate_PathNotExist(t *testing.T) {
	ctx, _, _, _, svc := setupProjectSvc(t)
	_, err := svc.Create(ctx, &project_svc.CreateProjectRequest{
		Name: "Foo", Path: "/nonexistent/path/should/fail", Color: "agent-1",
	})
	require.Error(t, err)
}

func TestProjectSvcCreate_ParentNotFound(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	tmp := t.TempDir()
	mp.EXPECT().Find(ctx, int64(99)).Return(nil, nil)
	_, err := svc.Create(ctx, &project_svc.CreateProjectRequest{
		ParentID: 99, Name: "Sub", Path: tmp, Color: "agent-1",
	})
	require.Error(t, err)
}

func TestProjectSvcDelete_HasChildren(t *testing.T) {
	ctx, mp, _, _, svc := setupProjectSvc(t)
	mp.EXPECT().Find(ctx, int64(1)).Return(&project_entity.Project{ID: 1, Status: consts.ACTIVE}, nil)
	mp.EXPECT().HasActiveChildren(ctx, int64(1)).Return(true, nil)

	err := svc.Delete(ctx, 1)
	require.Error(t, err)
}

func TestProjectSvcDelete_HasActiveSessions(t *testing.T) {
	ctx, mp, _, ms, svc := setupProjectSvc(t)
	mp.EXPECT().Find(ctx, int64(1)).Return(&project_entity.Project{ID: 1, Status: consts.ACTIVE}, nil)
	mp.EXPECT().HasActiveChildren(ctx, int64(1)).Return(false, nil)
	ms.EXPECT().CountActiveByProject(ctx, int64(1), []string{"running", "waiting"}).Return(int64(2), nil)

	err := svc.Delete(ctx, 1)
	require.Error(t, err)
}

func TestProjectSvcDelete_Success(t *testing.T) {
	ctx, mp, _, ms, svc := setupProjectSvc(t)
	mp.EXPECT().Find(ctx, int64(1)).Return(&project_entity.Project{ID: 1, Status: consts.ACTIVE}, nil)
	mp.EXPECT().HasActiveChildren(ctx, int64(1)).Return(false, nil)
	ms.EXPECT().CountActiveByProject(ctx, int64(1), []string{"running", "waiting"}).Return(int64(0), nil)
	mp.EXPECT().Delete(ctx, int64(1)).Return(nil)

	require.NoError(t, svc.Delete(ctx, 1))
}

func TestProjectSvcListTree_BuildsHierarchy(t *testing.T) {
	convey.Convey("ListTree 把扁平列表组装成树，父在前子在后", t, func() {
		ctx, mp, _, _, svc := setupProjectSvc(t)
		mp.EXPECT().List(ctx).Return([]*project_entity.Project{
			{ID: 1, Name: "A", ParentID: 0},
			{ID: 2, Name: "B", ParentID: 1},
			{ID: 3, Name: "C", ParentID: 2},
			{ID: 4, Name: "Side", ParentID: 0},
		}, nil)

		roots, err := svc.ListTree(ctx)
		require.NoError(t, err)
		convey.So(len(roots), convey.ShouldEqual, 2) // A, Side
		// 找 A
		var a *project_svc.ProjectNode
		for _, r := range roots {
			if r.Project.Name == "A" {
				a = r
			}
		}
		convey.So(a, convey.ShouldNotBeNil)
		convey.So(len(a.Children), convey.ShouldEqual, 1)
		convey.So(a.Children[0].Project.Name, convey.ShouldEqual, "B")
		convey.So(a.Children[0].Children[0].Project.Name, convey.ShouldEqual, "C")
	})
}

func TestProjectSvcDetectGitRepo_NonGitDir(t *testing.T) {
	ctx, _, _, _, svc := setupProjectSvc(t)
	tmp := t.TempDir()
	info, err := svc.DetectGitRepo(ctx, tmp)
	require.NoError(t, err)
	assert.False(t, info.IsGitRepo)
}

func TestProjectSvcDetectGitRepo_EmptyPath(t *testing.T) {
	ctx, _, _, _, svc := setupProjectSvc(t)
	info, err := svc.DetectGitRepo(ctx, "")
	require.NoError(t, err)
	assert.False(t, info.IsGitRepo)
}

func TestProjectSvcDetectGitRepo_GitDir(t *testing.T) {
	convey.Convey("DetectGitRepo 在 .git 子目录存在时返回 IsGitRepo=true", t, func() {
		ctx, _, _, _, svc := setupProjectSvc(t)
		tmp := t.TempDir()
		require.NoError(t, os.MkdirAll(tmp+"/.git", 0o755))
		info, err := svc.DetectGitRepo(ctx, tmp)
		require.NoError(t, err)
		convey.So(info.IsGitRepo, convey.ShouldBeTrue)
	})
}
