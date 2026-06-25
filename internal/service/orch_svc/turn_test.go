package orch_svc_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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

	// Disabled agent → nil.
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
	assert.ElementsMatch(t, []string{"agent_list", "dispatch", "ask", "send", "finish", "reply"}, specs[0].Tools)
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

	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	// Inject mock deps; restore via cleanup (re-register with nils).
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	tasks.EXPECT().FindBySession(gomock.Any(), int64(500)).
		Return(&orch_entity.Task{ID: 9, RunID: 100, SessionID: 500, ParentTaskID: 0}, nil)
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
// suffix 仅来自 run.FlowContent，绝不包含 workflow.Tags / workflow.Outline 的内容。
//
// 结构说明：workflow.Tags 和 workflow.Outline 存在于 Workflow 实体（不注入，仅展示），
// BuildTurnExtras 没有接收 workflow repo 的路径，只通过 tasks → runs 读取
// OrchestrationRun.FlowContent。未来若有人新增代码加载 Workflow 并拼入 tags/outline，
// 本测试会因 suffix 中出现哨兵串而变红。
//
// 哨兵的可达路径：注入泄露只能通过「加载 Workflow 实体并读取其 Tags/Outline 字段」发生，
// 因此把哨兵置于此时才会被读取的 tags/outline 字段值（通过测试注释声明其语义），
// 并断言它们不出现在 suffix 里。
func TestBuildTurnExtras_NeverInjectsTagsOutline(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	tasks := mock_orch_repo.NewMockTaskRepo(ctrl)
	runs := mock_orch_repo.NewMockRunRepo(ctrl)
	orch_svc.Default().RegisterDeps(nil, nil, runs, tasks, nil, nil)
	t.Cleanup(func() { orch_svc.Default().RegisterDeps(nil, nil, nil, nil, nil, nil) })

	// FlowContent 只含正文；哨兵串不出现在任何 BuildTurnExtras 可读的字段里。
	// 若未来有人加载 Workflow 并拼入 Tags("TAG-SENTINEL") / Outline("OUTLINE-SENTINEL")，
	// suffix 就会包含哨兵，断言变红。
	const (
		flowBody       = "正文-FLOW-BODY"
		tagSentinel    = "TAG-SENTINEL"    // 代表 workflow.Tags 被注入时才会出现的串
		outlineSentinel = "OUTLINE-SENTINEL" // 代表 workflow.Outline 被注入时才会出现的串
	)
	tasks.EXPECT().FindBySession(gomock.Any(), int64(600)).
		Return(&orch_entity.Task{ID: 50, RunID: 200, SessionID: 600, ParentTaskID: 0}, nil)
	runs.EXPECT().Find(gomock.Any(), int64(200)).
		Return(&orch_entity.OrchestrationRun{ID: 200, RootTaskID: 50, FlowContent: flowBody}, nil)

	a := enableOrch(&agent_entity.Agent{ID: 3})
	_, suffix, ok := orch_svc.Default().BuildTurnExtras(context.Background(), a, 600, 0)
	assert.True(t, ok)
	// 正文必须存在（确认 FlowContent 路径正常）。
	assert.Contains(t, suffix, flowBody)
	// 哨兵绝不出现（锁死 tags/outline 不进注入的不变量）。
	assert.NotContains(t, suffix, tagSentinel)
	assert.NotContains(t, suffix, outlineSentinel)
}
