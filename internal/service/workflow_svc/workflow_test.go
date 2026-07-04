package workflow_svc

import (
	"context"
	"errors"
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/workflow_entity"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo"
	"github.com/agentre-ai/agentre/internal/repository/workflow_repo/mock_workflow_repo"
)

func setupSvc(t *testing.T) (
	context.Context,
	*mock_workflow_repo.MockWorkflowRepo,
	*mock_orch_repo.MockRunRepo,
	*workflowSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	wfMock := mock_workflow_repo.NewMockWorkflowRepo(ctrl)
	runMock := mock_orch_repo.NewMockRunRepo(ctrl)
	workflow_repo.RegisterWorkflow(wfMock)
	orch_repo.RegisterRun(runMock)
	return context.Background(), wfMock, runMock, &workflowSvc{}
}

func TestListWorkflows(t *testing.T) {
	convey.Convey("流程列表", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)

		convey.Convey("返回流程并统计使用中 Run 数", func() {
			wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
				{ID: 1, Name: "产品开发流程", Content: "# 产品开发流程", Status: 1, Updatetime: 200},
				{ID: 2, Name: "紧急修复流程", Content: "# 紧急修复流程", Status: 1, Updatetime: 100},
			}, nil)
			runMock.EXPECT().List(gomock.Any()).Return([]*orch_entity.OrchestrationRun{
				{ID: 10, FlowID: 1},
				{ID: 11, FlowID: 1},
				{ID: 12, FlowID: 0},
			}, nil)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.NoError(t, err)
			assert.Len(t, resp.Items, 2)
			assert.Equal(t, int64(1), resp.Items[0].ID)
			assert.Equal(t, 2, resp.Items[0].RunCount)
			assert.Equal(t, 0, resp.Items[1].RunCount)
		})

		convey.Convey("空库返回空列表(非 nil)", func() {
			wfMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
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

		convey.Convey("orch run repo 出错时透传 error", func() {
			wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{{ID: 1, Name: "x"}}, nil)
			runErr := errors.New("db error")
			runMock.EXPECT().List(gomock.Any()).Return(nil, runErr)
			resp, err := svc.List(ctx, &ListWorkflowsRequest{})
			assert.ErrorIs(t, err, runErr)
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
			assert.Equal(t, 0, resp.Item.RunCount)
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
		ctx, wfMock, runMock, svc := setupSvc(t)

		convey.Convey("成功:改名改正文并回带 Run 数", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "新名", w.Name)
					assert.Equal(t, "## 新正文", w.Content)
					return nil
				})
			runMock.EXPECT().List(gomock.Any()).
				Return([]*orch_entity.OrchestrationRun{{ID: 1, FlowID: 3}}, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: " 新名 ", Content: "## 新正文"})
			assert.NoError(t, err)
			assert.Equal(t, 1, resp.Item.RunCount)
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

		convey.Convey("Update 成功但 runCounts 出错时报错", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			runErr := errors.New("db error")
			runMock.EXPECT().List(gomock.Any()).Return(nil, runErr)
			_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "x"})
			assert.ErrorIs(t, err, runErr)
		})
	})
}

func TestCreateWorkflow_TagsOutline(t *testing.T) {
	convey.Convey("新建流程带 tags/outline", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)

		convey.Convey("tags/outline 编码成 JSON 落库,DTO 平铺回 []string", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, `["通用","新功能"]`, w.Tags)
					assert.Equal(t, `["需求拆解","方案设计"]`, w.Outline)
					w.ID = 5
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{
				Name:    "标准功能开发流",
				Content: "# 标准功能开发流",
				Tags:    []string{"通用", "新功能"},
				Outline: []string{"需求拆解", "方案设计"},
			})
			assert.NoError(t, err)
			assert.Equal(t, []string{"通用", "新功能"}, resp.Item.Tags)
			assert.Equal(t, []string{"需求拆解", "方案设计"}, resp.Item.Outline)
		})

		convey.Convey("空 tags/outline 编码成 []，DTO 平铺成 nil", func() {
			wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, "[]", w.Tags)
					assert.Equal(t, "[]", w.Outline)
					return nil
				})
			resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "x"})
			assert.NoError(t, err)
			assert.Empty(t, resp.Item.Tags)
			assert.Empty(t, resp.Item.Outline)
		})
	})
}

func TestListWorkflow_DecodesTagsOutline(t *testing.T) {
	convey.Convey("列表解码 tags/outline", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)
		wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
			{ID: 1, Name: "x", Tags: `["修复"]`, Outline: `["复现","定位"]`, Status: 1},
		}, nil)
		runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
		resp, err := svc.List(ctx, &ListWorkflowsRequest{})
		assert.NoError(t, err)
		assert.Equal(t, []string{"修复"}, resp.Items[0].Tags)
		assert.Equal(t, []string{"复现", "定位"}, resp.Items[0].Outline)
	})
}

func TestUpdateWorkflow_TagsOutline(t *testing.T) {
	convey.Convey("编辑流程带 tags/outline", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)

		convey.Convey("tags/outline 编码成 JSON 落库,DTO 平铺回 []string", func() {
			wfMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&workflow_entity.Workflow{ID: 3, Name: "旧名", Status: 1}, nil)
			wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, w *workflow_entity.Workflow) error {
					assert.Equal(t, `["a"]`, w.Tags)
					assert.Equal(t, `["b"]`, w.Outline)
					return nil
				})
			runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
			resp, err := svc.Update(ctx, &UpdateWorkflowRequest{
				ID:      3,
				Name:    "新名",
				Tags:    []string{"a"},
				Outline: []string{"b"},
			})
			assert.NoError(t, err)
			assert.Equal(t, []string{"a"}, resp.Item.Tags)
			assert.Equal(t, []string{"b"}, resp.Item.Outline)
		})
	})
}

func TestDecodeStringList_MalformedJSON(t *testing.T) {
	convey.Convey("decodeStringList 异常输入返回 nil", t, func() {
		convey.Convey("非 JSON 文本返回 nil", func() {
			assert.Nil(t, decodeStringList("not-json"))
		})
		convey.Convey("类型不匹配(含数字)返回 nil", func() {
			assert.Nil(t, decodeStringList(`["a",123]`))
		})
		convey.Convey("空字符串返回 nil", func() {
			assert.Nil(t, decodeStringList(""))
		})
		convey.Convey("空数组 [] 返回 nil", func() {
			assert.Nil(t, decodeStringList("[]"))
		})
		convey.Convey("encodeStringList nil 返回 []", func() {
			assert.Equal(t, "[]", encodeStringList(nil))
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

func TestCreateWorkflow_ProjectsGraphIntoContent(t *testing.T) {
	convey.Convey("Create 传 graph → content/outline 被投影覆写", t, func() {
		ctx, wfMock, _, svc := setupSvc(t)
		var saved *workflow_entity.Workflow
		wfMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, w *workflow_entity.Workflow) error { saved = w; w.ID = 5; return nil },
		)
		graph := `{"version":1,"nodes":[{"id":"a","label":"Plan","kind":"leader"},{"id":"b","label":"Do","kind":"task","brief":"do it"}],"edges":[{"from":"a","to":"b"}]}`
		resp, err := svc.Create(ctx, &CreateWorkflowRequest{Name: "F", Content: "ignored user text", Graph: graph})
		assert.NoError(t, err)
		assert.Contains(t, saved.Content, "# F")
		assert.Contains(t, saved.Content, "finish with a summary @user") // sink=Do
		assert.NotEqual(t, "ignored user text", saved.Content)           // 图存在时投影覆写
		assert.Equal(t, graph, saved.Graph)
		assert.Contains(t, resp.Item.Content, "# F")
	})
}

func TestListWorkflows_ExposesDefaultAndGraph(t *testing.T) {
	convey.Convey("List DTO 带 isDefault/graph", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)
		wfMock.EXPECT().List(gomock.Any()).Return([]*workflow_entity.Workflow{
			{ID: 1, Name: "D", Content: "# D", Graph: `{"version":1}`, IsDefault: 1, Status: 1},
		}, nil)
		runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
		resp, err := svc.List(ctx, &ListWorkflowsRequest{})
		assert.NoError(t, err)
		assert.True(t, resp.Items[0].IsDefault)
		assert.Equal(t, `{"version":1}`, resp.Items[0].Graph)
	})
}

func TestUpdateWorkflow_EmptyGraphPreservesStoredGraph(t *testing.T) {
	convey.Convey("Update 不传 graph → 已存 graph 不被清空", t, func() {
		ctx, wfMock, runMock, svc := setupSvc(t)
		wfMock.EXPECT().Find(gomock.Any(), int64(3)).Return(&workflow_entity.Workflow{
			ID: 3, Name: "旧名", Status: 1,
			Graph:   `{"version":1,"nodes":[{"id":"a","label":"A","kind":"leader"}],"edges":[]}`,
			Content: "# 旧名",
		}, nil)
		var saved *workflow_entity.Workflow
		wfMock.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, w *workflow_entity.Workflow) error { saved = w; return nil })
		runMock.EXPECT().List(gomock.Any()).Return(nil, nil)
		_, err := svc.Update(ctx, &UpdateWorkflowRequest{ID: 3, Name: "新名", Content: "## body"})
		assert.NoError(t, err)
		assert.NotEmpty(t, saved.Graph) // 已存 graph 未被清空
		assert.Contains(t, saved.Graph, `"kind":"leader"`)
	})
}
