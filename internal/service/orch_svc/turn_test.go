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
