package department_svc

import (
	"context"
	"testing"

	"github.com/smartystreets/goconvey/convey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/department_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/pkg/code"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_backend_repo/mock_agent_backend_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/agent_repo/mock_agent_repo"
	"github.com/agentre-ai/agentre/internal/repository/department_repo"
	"github.com/agentre-ai/agentre/internal/repository/department_repo/mock_department_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo"
	"github.com/agentre-ai/agentre/internal/repository/llm_provider_repo/mock_llm_provider_repo"
)

func setupSvc(t *testing.T) (
	context.Context,
	*mock_department_repo.MockDepartmentRepo,
	*mock_agent_repo.MockAgentRepo,
	*departmentSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	deptMock := mock_department_repo.NewMockDepartmentRepo(ctrl)
	agentMock := mock_agent_repo.NewMockAgentRepo(ctrl)
	department_repo.RegisterDepartment(deptMock)
	agent_repo.RegisterAgent(agentMock)
	return context.Background(), deptMock, agentMock, &departmentSvc{now: func() int64 { return 1700000000 }}
}

func TestCreateDepartment(t *testing.T) {
	convey.Convey("创建部门", t, func() {
		ctx, deptMock, _, svc := setupSvc(t)

		convey.Convey("顶级部门成功", func() {
			deptMock.EXPECT().FindByName(gomock.Any(), "工程部", int64(0)).Return(nil, nil)
			deptMock.EXPECT().NextSortOrder(gomock.Any(), int64(0)).Return(1, nil)
			deptMock.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, d *department_entity.Department) error {
				d.ID = 7
				return nil
			})
			resp, err := svc.Create(ctx, &CreateDepartmentRequest{Name: "工程部", AccentColor: "agent-2"})
			assert.NoError(t, err)
			assert.Equal(t, int64(7), resp.Item.ID)
		})

		convey.Convey("同父重名拒绝", func() {
			deptMock.EXPECT().FindByName(gomock.Any(), "工程部", int64(0)).
				Return(&department_entity.Department{ID: 1, Name: "工程部"}, nil)
			_, err := svc.Create(ctx, &CreateDepartmentRequest{Name: "工程部", AccentColor: "agent-2"})
			assert.Error(t, err)
		})

		convey.Convey("父部门不存在", func() {
			deptMock.EXPECT().Find(gomock.Any(), int64(99)).Return(nil, nil)
			_, err := svc.Create(ctx, &CreateDepartmentRequest{Name: "x", AccentColor: "agent-1", ParentID: 99})
			assert.Error(t, err)
		})

		convey.Convey("非法颜色拒绝（entity Check）", func() {
			_, err := svc.Create(ctx, &CreateDepartmentRequest{Name: "x", AccentColor: "rainbow"})
			assert.Error(t, err)
		})
	})
}

func TestMoveDepartment(t *testing.T) {
	convey.Convey("Move 部门", t, func() {
		ctx, deptMock, _, svc := setupSvc(t)

		convey.Convey("正常 Move 到另一父", func() {
			deptMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&department_entity.Department{ID: 3, ParentID: 1, Status: 1}, nil)
			deptMock.EXPECT().Find(gomock.Any(), int64(2)).
				Return(&department_entity.Department{ID: 2, ParentID: 0, Status: 1}, nil)
			deptMock.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
				{ID: 1, ParentID: 0}, {ID: 2, ParentID: 0}, {ID: 3, ParentID: 1},
			}, nil)
			deptMock.EXPECT().NextSortOrder(gomock.Any(), int64(2)).Return(1, nil)
			deptMock.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			_, err := svc.Move(ctx, &MoveDepartmentRequest{ID: 3, NewParentID: 2})
			assert.NoError(t, err)
		})

		convey.Convey("环：3 → 5（5 是 3 的子）", func() {
			deptMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&department_entity.Department{ID: 3, ParentID: 0, Status: 1}, nil)
			deptMock.EXPECT().Find(gomock.Any(), int64(5)).
				Return(&department_entity.Department{ID: 5, ParentID: 3, Status: 1}, nil)
			deptMock.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
				{ID: 3, ParentID: 0}, {ID: 5, ParentID: 3},
			}, nil)
			_, err := svc.Move(ctx, &MoveDepartmentRequest{ID: 3, NewParentID: 5})
			assert.Error(t, err)
		})
	})
}

func TestHasCycle(t *testing.T) {
	all := []*department_entity.Department{
		{ID: 1, ParentID: 0},
		{ID: 2, ParentID: 1},
		{ID: 3, ParentID: 2},
	}
	cases := []struct {
		name          string
		startParentID int64
		selfID        int64
		expectCycle   bool
	}{
		{"move to top", 0, 1, false},
		{"move under self direct child", 2, 1, true},
		{"move under self deep descendant", 3, 1, true},
		{"move under unrelated", 1, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.expectCycle, hasCycle(all, tc.startParentID, tc.selfID))
		})
	}
}

func TestCollectSubtree(t *testing.T) {
	all := []*department_entity.Department{
		{ID: 1, ParentID: 0},
		{ID: 2, ParentID: 1},
		{ID: 3, ParentID: 2},
		{ID: 4, ParentID: 1},
		{ID: 5, ParentID: 0},
	}
	got := collectSubtree(all, 1)
	assert.ElementsMatch(t, []int64{1, 2, 3, 4}, got)
}

func TestCollectAgentsInDepartments(t *testing.T) {
	all := []*agent_entity.Agent{
		{ID: 10, DepartmentID: 1, ParentAgentID: 0},
		{ID: 11, DepartmentID: 0, ParentAgentID: 10},
		{ID: 12, DepartmentID: 0, ParentAgentID: 11},
		{ID: 20, DepartmentID: 2, ParentAgentID: 0},
	}

	got := collectAgentsInDepartments(all, []int64{1})

	assert.ElementsMatch(t, []int64{10, 11, 12}, got)
}

func TestUpdateDepartmentLeadValidation(t *testing.T) {
	convey.Convey("Update 部门 lead 校验", t, func() {
		ctx, deptMock, agentMock, svc := setupSvc(t)

		convey.Convey("lead 不属于本部门 → 拒绝", func() {
			deptMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&department_entity.Department{ID: 3, Name: "old", Status: 1}, nil)
			deptMock.EXPECT().FindByName(gomock.Any(), "工程部", int64(0)).Return(nil, nil)
			agentMock.EXPECT().Find(gomock.Any(), int64(42)).
				Return(&agent_entity.Agent{ID: 42, DepartmentID: 99}, nil)
			_, err := svc.Update(ctx, &UpdateDepartmentRequest{
				ID: 3, Name: "工程部", AccentColor: "agent-2", LeadAgentID: 42,
			})
			assert.Error(t, err)
		})

		convey.Convey("lead 属于本部门 → 通过", func() {
			deptMock.EXPECT().Find(gomock.Any(), int64(3)).
				Return(&department_entity.Department{ID: 3, Name: "old", Status: 1}, nil)
			deptMock.EXPECT().FindByName(gomock.Any(), "工程部", int64(0)).Return(nil, nil)
			agentMock.EXPECT().Find(gomock.Any(), int64(42)).
				Return(&agent_entity.Agent{ID: 42, DepartmentID: 3}, nil)
			deptMock.EXPECT().Update(gomock.Any(), gomock.Any()).Return(nil)
			_, err := svc.Update(ctx, &UpdateDepartmentRequest{
				ID: 3, Name: "工程部", AccentColor: "agent-2", LeadAgentID: 42,
			})
			assert.NoError(t, err)
		})
	})
}

func setupLoadSvc(t *testing.T) (
	context.Context,
	*mock_department_repo.MockDepartmentRepo,
	*mock_agent_repo.MockAgentRepo,
	*mock_agent_backend_repo.MockAgentBackendRepo,
	*mock_llm_provider_repo.MockLLMProviderRepo,
	*departmentSvc,
) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	deptMock := mock_department_repo.NewMockDepartmentRepo(ctrl)
	agentMock := mock_agent_repo.NewMockAgentRepo(ctrl)
	backendMock := mock_agent_backend_repo.NewMockAgentBackendRepo(ctrl)
	providerMock := mock_llm_provider_repo.NewMockLLMProviderRepo(ctrl)
	department_repo.RegisterDepartment(deptMock)
	agent_repo.RegisterAgent(agentMock)
	agent_backend_repo.RegisterAgentBackend(backendMock)
	llm_provider_repo.RegisterLLMProvider(providerMock)
	return context.Background(), deptMock, agentMock, backendMock, providerMock, &departmentSvc{now: func() int64 { return 1700000000 }}
}

func TestLoad_ToolsProjectionAndAvailableTools(t *testing.T) {
	convey.Convey("Load 部门+Agent", t, func() {
		ctx, deptMock, agentMock, backendMock, providerMock, svc := setupLoadSvc(t)

		convey.Convey("AgentItem.Tools 投影 + LoadOrgResponse.AvailableTools == agenttool.Keys()", func() {
			deptMock.EXPECT().List(gomock.Any()).Return([]*department_entity.Department{
				{ID: 1, Name: "工程部", Status: 1},
			}, nil)
			agentMock.EXPECT().List(gomock.Any()).Return([]*agent_entity.Agent{
				{
					ID: 10, Name: "Eva", DepartmentID: 1, Status: 1,
					PromptJSON: "[]", SkillsJSON: "[]",
					ToolsJSON: `[{"key":"org","enabled":true}]`,
				},
			}, nil)
			backendMock.EXPECT().List(gomock.Any()).Return([]*agent_backend_entity.AgentBackend{}, nil)
			providerMock.EXPECT().List(gomock.Any()).Return(nil, nil)

			resp, err := svc.Load(ctx, &LoadOrgRequest{})
			assert.NoError(t, err)
			assert.Len(t, resp.Agents, 1)
			assert.Len(t, resp.Agents[0].Tools, 1)
			assert.Equal(t, "org", resp.Agents[0].Tools[0].Key)
			assert.True(t, resp.Agents[0].Tools[0].Enabled)
			assert.Equal(t, agenttool.Keys(), resp.AvailableTools)
		})
	})
}

var _ = code.DepartmentLeadNotInDepartment
