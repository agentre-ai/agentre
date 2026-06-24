package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
)

// TurnExtrasProvider 为需要额外 turn 上下文的会话(如编排会话)补齐 MCP server +
// system-prompt 后缀。bootstrap 注册实现;ok=false 表示不适用(本会话不需要补齐 /
// 依赖未就绪)。groupID 形参为历史残留(恒传 0),orch provider 忽略它。
//
// 与 scheduler 显式填 extras 的关系:fillGroupTurnExtras 只「填空」—— 调度路径已把 extras
// 填满,provider 被跳过不重复注入;直接路径 extras 为空,provider 补上,构造逻辑单一。
type TurnExtrasProvider func(ctx context.Context, a *agent_entity.Agent, sessionID, groupID int64) (mcpServers []agentruntime.MCPServerSpec, systemPromptSuffix string, ok bool)

var turnExtrasProviders []TurnExtrasProvider

// RegisterTurnExtrasProvider bootstrap 接线入口(可多次,按注册序,首个 ok 生效)。
func RegisterTurnExtrasProvider(p TurnExtrasProvider) {
	turnExtrasProviders = append(turnExtrasProviders, p)
}

// ResetTurnExtrasProviders 测试清理,防用例间串台;仅测试使用,生产代码勿调。
func ResetTurnExtrasProviders() { turnExtrasProviders = nil }

// fillGroupTurnExtras 对编排会话(runID>0)逐字段补齐缺失的 turn 上下文。
// 永不覆盖 caller 已设置的字段(调度路径已填满则整体跳过),只填空位;
// emitTurnStartedBypass 由发起路径决定(调度=true / 直接发起=false),不归 provider 管。
// 仅在 runID==0(普通会话)时整体跳过:编排会话(runID>0)的 orch provider 需经此处触发,
// 否则 Leader 拿不到编排指引/流程后缀(C2)。
func fillGroupTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, runID int64, extras turnExtras) turnExtras {
	if a == nil || runID <= 0 {
		return extras
	}
	// 调度路径已显式填满 → 整体跳过,不重复咨询 provider。
	if len(extras.mcpServers) > 0 && extras.systemPromptSuffix != "" {
		return extras
	}
	for _, p := range turnExtrasProviders {
		mcpServers, systemPromptSuffix, ok := p(ctx, a, sessionID, 0)
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
