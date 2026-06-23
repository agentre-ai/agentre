package chat_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

func TestEnabledPluginsForTurn(t *testing.T) {
	a := &agent_entity.Agent{ID: 5}

	// provider 未注册 → nil
	RegisterEnabledPluginsProvider(nil)
	require.Nil(t, enabledPluginsForTurn(context.Background(), a, true))

	// 注册后 + 支持 CapSkills → 注入 provider 结果
	RegisterEnabledPluginsProvider(func(_ context.Context, ag *agent_entity.Agent) map[string]bool {
		require.Equal(t, int64(5), ag.ID)
		return map[string]bool{"x@m": true}
	})
	require.Equal(t, map[string]bool{"x@m": true}, enabledPluginsForTurn(context.Background(), a, true))

	// runner 不支持 CapSkills → 不注入(软降级)
	require.Nil(t, enabledPluginsForTurn(context.Background(), a, false))
	RegisterEnabledPluginsProvider(nil) // 清理,防测试间串台
}
