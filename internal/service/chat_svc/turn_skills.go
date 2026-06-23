package chat_svc

import (
	"context"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
)

// EnabledPluginsProvider 按 agent 给 turn 返回技能包覆盖 map(仅 agent 显式覆盖:
// 强制开=true / 强制关=false;未列出的 plugin 沿用全局 ~/.claude 配置=继承)。
// bootstrap 注册 skill_svc 的实现;nil = 不注入。在 runTurn 单点生效,单聊/群聊/
// Regenerate 全覆盖(与 turn_mcp 同一接缝)。
type EnabledPluginsProvider func(ctx context.Context, a *agent_entity.Agent) map[string]bool

var enabledPluginsProvider EnabledPluginsProvider

// RegisterEnabledPluginsProvider bootstrap 接线入口。
func RegisterEnabledPluginsProvider(p EnabledPluginsProvider) { enabledPluginsProvider = p }

// enabledPluginsForTurn runTurn 组 RunRequest 时调;capOK = runner 声明 CapSkills。
// 未注册 provider 或 cap 不支持 → nil(runtime 忽略)。
func enabledPluginsForTurn(ctx context.Context, a *agent_entity.Agent, capOK bool) map[string]bool {
	if enabledPluginsProvider == nil || !capOK {
		return nil
	}
	return enabledPluginsProvider(ctx, a)
}
