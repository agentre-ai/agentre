package orch_entity_test

import (
	"testing"

	"github.com/cago-frame/cago/pkg/consts"
	"github.com/stretchr/testify/assert"

	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

func TestRun_TableName(t *testing.T) {
	assert.Equal(t, "orchestration_runs", (&orch_entity.OrchestrationRun{}).TableName())
}

func TestRun_IsActive(t *testing.T) {
	assert.True(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunRunning}).IsActive())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunStopped}).IsActive())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunDone}).IsActive())
	assert.False(t, (*orch_entity.OrchestrationRun)(nil).IsActive())
}

func TestRun_CanAdvance(t *testing.T) {
	assert.True(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunRunning}).CanAdvance())
	assert.False(t, (&orch_entity.OrchestrationRun{Status: orch_entity.RunPaused}).CanAdvance())
}

func TestRun_StatusConstsAreObjectiveLifecycle(t *testing.T) {
	// 守 spec：Run 不单设 error；客观生命周期仅这 5 个。
	all := []string{
		orch_entity.RunPending, orch_entity.RunRunning, orch_entity.RunPaused,
		orch_entity.RunDone, orch_entity.RunStopped,
	}
	assert.Len(t, all, 5)
	assert.NotContains(t, all, "error")
	_ = consts.ACTIVE
}

func TestOrchestrationRun_IsAgentAllowed(t *testing.T) {
	t.Run("空集合=全部允许", func(t *testing.T) {
		r := &orch_entity.OrchestrationRun{AllowedAgentIDs: ""}
		assert.True(t, r.IsAgentAllowed(9, 2))
		r2 := &orch_entity.OrchestrationRun{AllowedAgentIDs: "[]"}
		assert.True(t, r2.IsAgentAllowed(9, 2))
	})
	t.Run("集合内允许、集合外拒绝", func(t *testing.T) {
		r := &orch_entity.OrchestrationRun{AllowedAgentIDs: "[3,4]"}
		assert.True(t, r.IsAgentAllowed(3, 2))
		assert.False(t, r.IsAgentAllowed(9, 2))
	})
	t.Run("Leader 恒允许(即便不在集合)", func(t *testing.T) {
		r := &orch_entity.OrchestrationRun{AllowedAgentIDs: "[3,4]"}
		assert.True(t, r.IsAgentAllowed(2, 2))
	})
	t.Run("非法 JSON → 不限制", func(t *testing.T) {
		r := &orch_entity.OrchestrationRun{AllowedAgentIDs: "not-json"}
		assert.True(t, r.IsAgentAllowed(9, 2))
	})
}
