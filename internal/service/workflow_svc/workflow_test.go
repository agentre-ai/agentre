package workflow_svc

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/group_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/group_repo"
	"github.com/agentre-ai/agentre/internal/repository/group_repo/mock_group_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo/mock_workflow_repo"
)

func setupSvc(t *testing.T) (
	context.Context,
	*mock_workflow_repo.MockWorkflowRepo,
	*mock_group_repo.MockGroupRepo,
	*workflowSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	wfMock := mock_workflow_repo.NewMockWorkflowRepo(ctrl)
	groupMock := mock_group_repo.NewMockGroupRepo(ctrl)
	workflow_repo.RegisterWorkflow(wfMock)
	group_repo.RegisterGroup(groupMock)
	return context.Background(), wfMock, groupMock, &workflowSvc{}
}

func TestListWorkflows(t *testing.T) {
	convey.Convey("流程列表", t, func() {
		ctx, wfMock, groupMock, svc := setupSvc(t)

		convey.Convey("返回流程并统计使用中群数", func() {
			wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
				{ID: 1, Name: "产品开发流程", Content: "# 产品开发流程", Status: 1, Updatetime: 200},
				{ID: 2, Name: "紧急修复流程", Content: "# 紧急修复流程", Status: 1, Updatetime: 100},
			}, nil)
			groupMock.EXPECT().List(gomock.Any()).Return([]*group_entity.Group{
				{ID: 10, WorkflowID: 1},
				{ID: 11, WorkflowID: 1},
				{ID: 12, WorkflowID: 0},
			}, nil)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.NoError(t, err)
			assert.Len(t, resp.Items, 2)
			assert.Equal(t, int64(1), resp.Items[0].ID)
			assert.Equal(t, 2, resp.Items[0].GroupCount)
			assert.Equal(t, 0, resp.Items[1].GroupCount)
		})

		convey.Convey("空库返回空列表(非 nil)", func() {
			wfMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			groupMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.NoError(t, err)
			assert.NotNil(t, resp.Items)
			assert.Empty(t, resp.Items)
		})

		convey.Convey("workflow repo 出错时透传 error", func() {
			wfErr := errors.New("db error")
			wfMock.EXPECT().List(gomock.Any()).Return(nil, wfErr)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.ErrorIs(t, err, wfErr)
			assert.Nil(t, resp)
		})

		convey.Convey("group repo 出错时透传 error", func() {
			wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{{ID: 1, Name: "x"}}, nil)
			groupErr := errors.New("db error")
			groupMock.EXPECT().List(gomock.Any()).Return(nil, groupErr)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.ErrorIs(t, err, groupErr)
			assert.Nil(t, resp)
		})
	})
}

func TestCreateWorkflow(t *testing.T) {
	convey.Convey("新建流程", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("成功:trim 名称并落库", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "产品开发流程", w.Name)
					assert.Equal(t, consts.ACTIVE, w.Status)
					w.ID = 7
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "  产品开发流程 ", Content: "# 产品开发流程"})
			assert.NoError(t, err)
			assert.Equal(t, int64(7), resp.Item.ID)
			assert.Equal(t, 0, resp.Item.GroupCount)
		})

		convey.Convey("空名拒绝", func() {
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "   "})
			assert.Error(t, err)
			assert.Nil(t, resp)
		})
	})
}

func TestUpdateWorkflow(t *testing.T) {
	convey.Convey("编辑流程", t, func() {
		ctx, wfMock, groupMock, svc := setupSvc(t)

		convey.Convey("成功:改名改正文并回带群数", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "新名", w.Name)
					assert.Equal(t, "## 新正文", w.Content)
					return nil
				})
			groupMock.EXPECT().List(gomock.Any()).
				Return([]*group_entity.Group{{ID: 1, WorkflowID: 3}}, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: " 新名 ", Content: "## 新正文"})
			assert.NoError(t, err)
			assert.Equal(t, 1, resp.Item.GroupCount)
		})

		convey.Convey("不存在报 WorkflowNotFound", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 99, Name: "x"})
			assert.Error(t, err)
			assert.Nil(t, resp)
		})

		convey.Convey("已软删的报 WorkflowNotFound", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(4)).
				Return(&workflow_entity.Workflow{ID: 4, Name: "已删", Status: 0}, nil)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 4, Name: "x"})
			assert.Error(t, err)
		})

		convey.Convey("改成空名拒绝", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "  "})
			assert.Error(t, err)
		})

		convey.Convey("Find 出错时透传 error", func() {
			dbErr := errors.New("db error")
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).Return(nil, dbErr)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "x"})
			assert.ErrorIs(t, err, dbErr)
		})

		convey.Convey("Update 成功但 groupCounts 出错时报错", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			groupErr := errors.New("db error")
			groupMock.EXPECT().List(gomock.Any()).Return(nil, groupErr)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "x"})
			assert.ErrorIs(t, err, groupErr)
		})
	})
}

func TestDeleteWorkflow(t *testing.T) {
	convey.Convey("删除流程", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("成功软删", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧", Status: 1}, nil)
			wfMock.EXPECT().Delete(gomock.Any(), int64(3)).Return(nil)
			_, err := svc.Delete(ctx, &DeleteWorkflowRequest{ID: 3})
			assert.NoError(t, err)
		})

		convey.Convey("不存在报错", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(9)).Return(nil, nil)
			_, err := svc.Delete(ctx, &DeleteWorkflowRequest{ID: 9})
			assert.Error(t, err)
		})

		convey.Convey("Find 出错时透传 error", func() {
			dbErr := errors.New("db error")
			wfMock.EXPECT().Find(gomock.Any(), int64(9)).Return(nil, dbErr)
			_, err := svc.Delete(ctx, &DeleteWorkflowRequest{ID: 9})
			assert.ErrorIs(t, err, dbErr)
		})
	})
}
