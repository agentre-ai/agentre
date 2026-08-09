package chat_svc_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_backend_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/service/chat_svc"
)

// ── R15 / 任务 12：组织架构页需要「每一档」的可用性，不只是最终派发到哪一档 ──

// TestListExecTargetAvailability_GivenAllUnavailable_ThenEvaluatesEveryTarget 与
// PickExecTarget 的关键差异：PickExecTarget 找到第一个可用档就提前返回（第二档的
// Find 从不会被调用）；ListExecTargetAvailability 要给组织架构页展示全部档的状态，
// 因此就算前面已经有可用档，后面的档也必须照样判定——这里用「全部不可用」这个
// 已有场景验证顺序与个数，用一个「第一档可用」的场景验证后续档不会被跳过。
func TestListExecTargetAvailability_GivenAllUnavailable_ThenEvaluatesEveryTarget(t *testing.T) {
	ctx, m, svc := setupPickExecTargetTest(t)
	m.execTarget.EXPECT().ListByAgent(ctx, int64(38)).Return([]*agent_entity.AgentExecTarget{
		{ID: 13, AgentID: 38, AgentBackendID: 95, SortOrder: 0},
		{ID: 14, AgentID: 38, AgentBackendID: 96, SortOrder: 1},
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(95)).Return(&agent_backend_entity.AgentBackend{
		ID: 95, Type: string(agent_backend_entity.TypeClaudeCode), DeviceID: "13",
	}, nil)
	m.remoteDevice.EXPECT().Get(ctx, int64(13)).Return(nil, errors.New("not found"))
	m.backend.EXPECT().Find(ctx, int64(96)).Return(&agent_backend_entity.AgentBackend{
		ID: 96, Type: string(agent_backend_entity.TypeBuiltin), LLMProviderKey: "missing-key",
	}, nil)
	m.provider.EXPECT().FindByKey(ctx, "missing-key").Return(nil, nil)

	statuses, err := svc.ListExecTargetAvailability(ctx, 38, 0)
	require.NoError(t, err)
	if assert.Len(t, statuses, 2) {
		assert.Equal(t, int64(95), statuses[0].AgentBackendID)
		assert.False(t, statuses[0].Available)
		assert.Equal(t, chat_svc.BlockReasonExecTargetUnpaired, statuses[0].Reason)
		assert.NotEmpty(t, statuses[0].Hint)

		assert.Equal(t, int64(96), statuses[1].AgentBackendID)
		assert.False(t, statuses[1].Available)
		assert.Equal(t, chat_svc.BlockReasonBackendRequiresProvider, statuses[1].Reason)
	}
}

// TestListExecTargetAvailability_GivenFirstAvailable_ThenStillEvaluatesTheRest 锁住
// 「不提前返回」这个与 PickExecTarget 相反的行为：第一档可用，但第二档仍然要被判定
// （这里配成同样可用），两档都要出现在结果里。
func TestListExecTargetAvailability_GivenFirstAvailable_ThenStillEvaluatesTheRest(t *testing.T) {
	ctx, m, svc := setupPickExecTargetTest(t)
	m.execTarget.EXPECT().ListByAgent(ctx, int64(31)).Return([]*agent_entity.AgentExecTarget{
		{ID: 1, AgentID: 31, AgentBackendID: 51, SortOrder: 0},
		{ID: 2, AgentID: 31, AgentBackendID: 52, SortOrder: 1},
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(51)).Return(&agent_backend_entity.AgentBackend{
		ID: 51, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "",
	}, nil)
	m.backend.EXPECT().Find(ctx, int64(52)).Return(&agent_backend_entity.AgentBackend{
		ID: 52, Type: string(agent_backend_entity.TypeClaudeCode), LLMProviderKey: "",
	}, nil)

	statuses, err := svc.ListExecTargetAvailability(ctx, 31, 0)
	require.NoError(t, err)
	if assert.Len(t, statuses, 2) {
		assert.True(t, statuses[0].Available)
		assert.Equal(t, chat_svc.BlockReason(""), statuses[0].Reason)
		assert.True(t, statuses[1].Available)
	}
}

// TestListExecTargetAvailability_GivenEmptyTargetList_ThenReturnsEmptySlice 空列表
// 不是错误——R15 的「保存被拒」发生在写路径，读路径只需要如实报告「没有档」。
func TestListExecTargetAvailability_GivenEmptyTargetList_ThenReturnsEmptySlice(t *testing.T) {
	ctx, m, svc := setupPickExecTargetTest(t)
	m.execTarget.EXPECT().ListByAgent(ctx, int64(39)).Return(nil, nil)

	statuses, err := svc.ListExecTargetAvailability(ctx, 39, 0)
	require.NoError(t, err)
	assert.Empty(t, statuses)
}
