package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// TurnExtrasProvider 为「群成员 backing session(groupID>0)」的 turn 补齐群上下文 ——
// group_send 等群工具 MCP + 群上下文 system-prompt 后缀。它解决设计问题⑥:用户直接对
// 成员 backing session 发起 Send/Edit/Regenerate(不经群调度 launchDelivery)时 extras
// 为零值、群上下文丢失。bootstrap 注册 group_svc 的实现;ok=false 表示不适用(非群会话 /
// 网关未就绪 / 该 agent 不是本群成员)。
//
// 与 scheduler 显式填 extras 的关系:fillGroupTurnExtras 只「填空」—— 调度路径已把 extras
// 填满,provider 被跳过不重复注入;直接路径 extras 为空,provider 补上。两边最终都调
// group_svc 的 buildGroupMCP/buildGroupSystemPrompt,构造逻辑单一。
type TurnExtrasProvider func(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) (mcpServers []agentruntime.MCPServerSpec, systemPromptSuffix string, ok bool)

var turnExtrasProviders []TurnExtrasProvider

// RegisterTurnExtrasProvider bootstrap 接线入口(可多次,按注册序,首个 ok 生效)。
func RegisterTurnExtrasProvider(p TurnExtrasProvider) {
	turnExtrasProviders = append(turnExtrasProviders, p)
}

// ResetTurnExtrasProviders 测试清理,防用例间串台;仅测试使用,生产代码勿调。
func ResetTurnExtrasProviders() { turnExtrasProviders = nil }

// fillGroupTurnExtras 对群成员 backing session(groupID>0)逐字段补齐缺失的群上下文。
// 永不覆盖 caller 已设置的字段(调度路径已填满则整体跳过),只填空位;emitTurnStartedBypass
// 由发起路径决定(调度=true / 直接发起=false),不归 provider 管。
func fillGroupTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64, extras turnExtras) turnExtras {
	if a == nil || groupID <= 0 {
		return extras
	}
	// 调度路径(launchDelivery)已显式填满 → 整体跳过,不重复咨询 provider。
	if len(extras.mcpServers) > 0 && extras.systemPromptSuffix != "" {
		return extras
	}
	for _, p := range turnExtrasProviders {
		mcpServers, systemPromptSuffix, ok := p(ctx, a, sessionID, groupID)
		if !ok {
			continue
		}
		if len(extras.mcpServers) == 0 {
			extras.mcpServers = mcpServers
		}
		if extras.systemPromptSuffix == "" {
			extras.systemPromptSuffix = systemPromptSuffix
		}
		break
	}
	return extras
}
