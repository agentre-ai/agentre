package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// TurnMCPProvider 按 (agent, session) 给 turn 注入额外 MCP server —— agent 级
// 内置工具体系的接缝。bootstrap 注册 orgtool_svc / group_svc 的实现;空列表 = 不注入。
// groupID 是 turn 所属 session 的 GroupID(>0 = 群成员 backing session),provider 据此
// 决定是否注入(如 group_create 不进群成员轮,防群中拉群套娃)。
// 与群聊的 extras.mcpServers 叠加;在 runTurn 单点生效,单聊/群聊/Regenerate 全覆盖。
type TurnMCPProvider func(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) []agentruntime.MCPServerSpec

var turnMCPProviders []TurnMCPProvider

// RegisterTurnMCPProvider bootstrap 接线入口(可多次,按注册序拼接)。
func RegisterTurnMCPProvider(p TurnMCPProvider) { turnMCPProviders = append(turnMCPProviders, p) }

// ResetTurnMCPProviders 测试清理,防用例间串台;仅测试使用,生产代码勿调。
func ResetTurnMCPProviders() { turnMCPProviders = nil }

// appendTurnMCP runTurn 在组装 RunRequest 时调用;capOK = runner 声明 CapMCPTools。
func appendTurnMCP(ctx context.Context, base []agentruntime.MCPServerSpec, a *agent_entity.Agent, sessionID, groupID int64, capOK bool) []agentruntime.MCPServerSpec {
	if !capOK {
		return base
	}
	for _, p := range turnMCPProviders {
		base = append(base, p(ctx, a, sessionID, groupID)...)
	}
	return base
}
