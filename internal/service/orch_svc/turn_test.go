package orch_svc_test

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
	"github.com/agentre-ai/agentre/internal/repository/orch_repo/mock_orch_repo"
	"github.com/agentre-ai/agentre/internal/service/orch_svc"
)

func enableOrch(a *agent_entity.Agent) *agent_entity.Agent {
	a.SetTools([]agent_entity.AgentToolItem{{Key: agenttool.KeyOrchestrate, Enabled: true}})
	return a
}

func TestBuildTurnMCP_InjectsWhenEnabled(t *testing.T) {
	// Set gatewayBaseURL on the Default() singleton, reset on cleanup.
	orch_svc.Default().SetGatewayBaseURL("http://127.0.0.1:9/")
	t.Cleanup(func() { orch_svc.Default().SetGatewayBaseURL("") })

	// BuildTurnMCP 现会对未开 orchestrate 的 agent 查会话是否为编排会话;显式接线一个
	// 返回「无绑定」的 tasks mock,把「非编排会话」这个前置条件钉死,不受包内其他用例
	// 遗留在 Default() 单例上的 tasks mock 干扰。
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	tasks.EXPECT().FindBySession(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	// Disabled agent + 非编排会话 → nil.
	disabled := &agent_entity.Agent{ID: 1}
	if got := orch_svc.Default().BuildTurnMCP(context.Background(), disabled, 500, 0); got != nil {
		t.Fatalf("disabled agent should get no spec, got %v", got)
	}

	// Enabled agent → 1 spec.
	a := enableOrch(&agent_entity.Agent{ID: 2, Name: "架构师"})
	specs := orch_svc.Default().BuildTurnMCP(context.Background(), a, 500, 0)
	assert.NotEmpty(t, specs)
	assert.Equal(t, agenttool.KeyOrchestrate, specs[0].Name)
	assert.Equal(t, "http://127.0.0.1:9/mcp/orchestrate/", specs[0].URL)
	// 断言注入的 Tools == 注册表 ToolNames 本尊(随白名单增减自适应,只验 BuildTurnMCP 把注册表原样透传)。
	def, _ := agenttool.Lookup(agenttool.KeyOrchestrate)
	assert.ElementsMatch(t, def.ToolNames, specs[0].Tools)
	assert.NotEmpty(t, specs[0].Headers["Authorization"])

	// No gateway → nil even when enabled.
	orch_svc.Default().SetGatewayBaseURL("")
	if got := orch_svc.Default().BuildTurnMCP(context.Background(), a, 500, 0); got != nil {
		t.Fatalf("no gateway should get nil, got %v", got)
	}
}

func TestBuildTurnExtras_AppendsFlowForRootSession(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	// Inject mock deps; restore via cleanup (re-register with nils).
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).
		Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500, ParentDispatchID: 0}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(100)).
		Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, FlowContent: "先拆分再并行"}, nil)

	a := enableOrch(&agent_entity.Agent{ID: 2})
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 500, 0)
	assert.True(t, ok)
	assert.Contains(t, suffix, "先拆分再并行")
	assert.Contains(t, suffix, "一切结果")
}

func TestBuildTurnExtras_ReturnsFalseWhenDisabled(t *testing.T) {
	a := &agent_entity.Agent{ID: 1} // tool NOT enabled
	_, _, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 500, 0)
	assert.False(t, ok)
}

// TestBuildTurnExtras_NeverInjectsTagsOutline 是护栏测试：验证 BuildTurnExtras 注入的
// suffix 严格等于「编排指引」+ 「本次编排流程」两个段落，绝不包含任何 workflow 衍生内容
// （workflow.Tags / workflow.Outline 等展示层字段）。
//
// 结构说明：workflow.Tags 和 workflow.Outline 存在于 Workflow 实体（不注入，仅展示），
// BuildTurnExtras 没有接收 workflow repo 的路径，只通过 tasks → runs 读取
// OrchestrationRun.FlowContent。
//
// tripwire 设计：orchGuidance 是 orch_svc 包私有常量，外部测试包无法直接引用，
// 因此使用结构性断言替代字面等值比较：
//  1. suffix 恰好含 2 个 "### " 小节标题（编排指引 + 本次编排流程），
//     若注入第三段（如 tags/outline）则计数变为 3+，断言失败。
//  2. suffix 以 flowBody 结尾，确保 FlowContent 之后没有额外内容追加。
//  3. suffix 包含 flowBody（正向确认 FlowContent 路径正常）。
//
// 任何将 workflow.Tags / workflow.Outline 拼入 suffix 的改动，都会使断言 1 或 2 失败。
func TestBuildTurnExtras_NeverInjectsTagsOutline(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	const flowBody = "正文-FLOW-BODY"

	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).
		Return(&orch_entity.Dispatch{ID: 50, RunID: 200, SessionID: 600, ParentDispatchID: 0}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(200)).
		Return(&orch_entity.OrchestrationRun{ID: 200, RootTaskID: 50, FlowContent: flowBody}, nil)

	a := enableOrch(&agent_entity.Agent{ID: 3})
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 600, 0)
	assert.True(t, ok)
	// 正向：FlowContent 路径正常。
	assert.Contains(t, suffix, flowBody)
	// tripwire 1：suffix 恰好包含 2 个 "### " 节标题（编排指引 + 本次编排流程），
	// 任何新增段落（如注入 tags/outline）都会使计数变为 3+，断言失败。
	assert.Equal(t, 2, strings.Count(suffix, "### "), "suffix 应只含两个 ### 段落，多则说明有额外注入")
	// tripwire 2：suffix 以 flowBody 结尾，确保 FlowContent 之后没有额外内容追加。
	assert.True(t, strings.HasSuffix(suffix, flowBody), "suffix 应以 FlowContent 结尾，有额外追加则失败")
}

// TestBuildTurnMCP_InjectsForOrchestrationSessionEvenWhenDisabled 回归：
// 经编排创建的会话(绑定了编排 Dispatch)即便其 agent 未勾 orchestrate 工具,也应注入
// 全套编排 MCP 工具 —— 否则 Leader/子任务拿不到 dispatch/report/finish 等能力,
// 整条 Run 静默降级成普通聊天(dev sess-53 根因)。
func TestBuildTurnMCP_InjectsForOrchestrationSessionEvenWhenDisabled(t *testing.T) {
	orch_svc.Default().SetGatewayBaseURL("http://127.0.0.1:9/")
	t.Cleanup(func() { orch_svc.Default().SetGatewayBaseURL("") })

	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	// 会话 700 绑定了编排 Dispatch(被派发子任务)→ 视为编排会话。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(700)).
		Return(&orch_entity.Dispatch{ID: 7, RunID: 100, SessionID: 700, ParentDispatchID: 3}, nil).AnyTimes()

	disabled := &agent_entity.Agent{ID: 1} // orchestrate 未开
	specs := orch_svc.Default().BuildTurnMCP(context.Background(), disabled, 700, 0)
	require.NotEmpty(t, specs, "编排会话即便 agent 未开 orchestrate 也应注入工具")
	assert.Equal(t, agenttool.KeyOrchestrate, specs[0].Name)

	// 对照:非编排会话(FindBySession 无绑定)+ 未开 → 仍不注入。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(701)).Return(nil, nil).AnyTimes()
	if got := orch_svc.Default().BuildTurnMCP(context.Background(), disabled, 701, 0); got != nil {
		t.Fatalf("非编排会话且未开 orchestrate 应得 nil, got %v", got)
	}
}

// TestBuildTurnExtras_InjectsForRootSessionEvenWhenDisabled 回归：根(Leader)会话即便
// 其 agent 未勾 orchestrate,也应注入编排指引 + 本次编排流程正文(dev sess-53 根因)。
func TestBuildTurnExtras_InjectsForRootSessionEvenWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).
		Return(&orch_entity.Dispatch{ID: 9, RunID: 100, SessionID: 500, ParentDispatchID: 0}, nil).AnyTimes()
	runs.EXPECT().Find(gomock.Any(), int64(100)).
		Return(&orch_entity.OrchestrationRun{ID: 100, RootTaskID: 9, FlowContent: "先拆分再并行"}, nil).AnyTimes()

	disabled := &agent_entity.Agent{ID: 1} // orchestrate 未开
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), disabled, 500, 0)
	assert.True(t, ok, "根会话即便 agent 未开 orchestrate 也应注入")
	assert.Contains(t, suffix, "先拆分再并行") // 流程正文
	assert.Contains(t, suffix, "一切结果")   // 编排指引
}

// TestBuildTurnExtras_InjectsGuidanceForChildEvenWhenDisabled 回归：被派发的子任务
// 即便其 agent 未勾 orchestrate,也应注入编排指引(让注进去的工具可用),但**不**注入
// 流程正文(整条 Run 的计划仅归 Leader)。
func TestBuildTurnExtras_InjectsGuidanceForChildEvenWhenDisabled(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	// 子任务:ParentDispatchID != 0 → 不进流程正文分支(不查 runs)。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(800)).
		Return(&orch_entity.Dispatch{ID: 12, RunID: 100, ParentDispatchID: 9, SessionID: 800}, nil).AnyTimes()

	disabled := &agent_entity.Agent{ID: 1} // orchestrate 未开
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), disabled, 800, 0)
	assert.True(t, ok, "子任务即便 agent 未开 orchestrate 也应注入指引")
	assert.Contains(t, suffix, "一切结果") // 编排指引在
	assert.Equal(t, 1, strings.Count(suffix, "### "), "子任务只含「编排指引」一段,无流程正文")
}

func TestBuildTurnExtras_GuidanceMentionsReadAndReport(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	tasks := mock_orch_repo.NewMockDispatchRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, nil, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	// 非根任务(避免触发 flow 注入分支);FindBySession 返回带父任务。
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).Return(
		&orch_entity.Dispatch{ID: 11, RunID: 100, ParentDispatchID: 9, SessionID: 600}, nil).AnyTimes()

	a := enableOrch(&agent_entity.Agent{ID: 3})
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 600, 0)
	assert.True(t, ok)
	assert.True(t, strings.Contains(suffix, "read(dispatch_id"), "guidance should mention read(dispatch_id)")
	assert.True(t, strings.Contains(suffix, "report"), "guidance should mention report")
	// 共享待办清单工具应被引导语介绍(所有编排会话都拿得到,含被派发子任务)。
	assert.True(t, strings.Contains(suffix, "待办清单"), "guidance should introduce the shared checklist")
	assert.True(t, strings.Contains(suffix, "task_add"), "guidance should name task_add")
}
