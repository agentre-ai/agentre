package chat_svc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

func TestFillGroupTurnExtras(t *testing.T) {
	a := &agent_entity.Agent{ID: 7}
	groupMCP := []agentruntime.MCPServerSpec{{Name: "group", Tools: []string{"group_send"}}}
	const suffix = "\n\n## 群聊「队」"
	provider := func(_ context.Context, _ *agent_entity.Agent, _, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
		return groupMCP, suffix, true
	}

	t.Run("非群会话(groupID=0)不咨询 provider,原样返回", func(t *testing.T) {
		ResetTurnExtrasProviders()
		defer ResetTurnExtrasProviders()
		RegisterTurnExtrasProvider(provider)
		got := fillGroupTurnExtras(context.Background(), a, 42, 0, turnExtras{})
		require.Empty(t, got.mcpServers)
		require.Equal(t, "", got.systemPromptSuffix)
	})

	t.Run("群成员 backing session 且 extras 为空 → provider 补齐 group_send MCP + 群 system-prompt 后缀", func(t *testing.T) {
		ResetTurnExtrasProviders()
		defer ResetTurnExtrasProviders()
		RegisterTurnExtrasProvider(provider)
		got := fillGroupTurnExtras(context.Background(), a, 42, 5, turnExtras{})
		require.Len(t, got.mcpServers, 1)
		require.Contains(t, got.mcpServers[0].Tools, "group_send")
		require.Equal(t, suffix, got.systemPromptSuffix)
	})

	t.Run("调度路径已填满 extras → provider 跳过,不覆盖", func(t *testing.T) {
		ResetTurnExtrasProviders()
		defer ResetTurnExtrasProviders()
		schedulerMCP := []agentruntime.MCPServerSpec{{Name: "group", Tools: []string{"group_send", "group_invite"}}}
		RegisterTurnExtrasProvider(func(_ context.Context, _ *agent_entity.Agent, _, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
			t.Fatal("provider 不应被调用:extras 已填满")
			return nil, "", false
		})
		got := fillGroupTurnExtras(context.Background(), a, 42, 5, turnExtras{
			mcpServers:         schedulerMCP,
			systemPromptSuffix: "scheduler-suffix",
		})
		require.Equal(t, schedulerMCP, got.mcpServers)
		require.Equal(t, "scheduler-suffix", got.systemPromptSuffix)
	})

	t.Run("provider 返回 ok=false → 不改 extras", func(t *testing.T) {
		ResetTurnExtrasProviders()
		defer ResetTurnExtrasProviders()
		RegisterTurnExtrasProvider(func(_ context.Context, _ *agent_entity.Agent, _, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
			return nil, "", false
		})
		got := fillGroupTurnExtras(context.Background(), a, 42, 5, turnExtras{})
		require.Empty(t, got.mcpServers)
		require.Equal(t, "", got.systemPromptSuffix)
	})
}
