package orch_svc

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// orchGuidance 是注入给编排 agent 的框架语。
const orchGuidance = `你被授予编排能力(dispatch/ask/send/finish + agent_list)。模型:` +
	`一切结果都会回到你、由你决定下一步;` +
	`并行 dispatch 子任务,审核/测试/合并也是 dispatch,返工用 send,补信息用 ask,收口用 finish。` +
	`无次数/时长/成本上限——自己判断何时收口或换策略。用户可能随时插话。`

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider：agent 开启 orchestrate 工具时返回注入 spec。
// 若 gatewayBaseURL 未设置（bootstrap 尚未接线）则返回 nil，确保不注入失效的 URL。
func (s *orchSvc) BuildTurnMCP(_ context.Context, a *agent_entity.Agent, sessionID, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || !a.ToolEnabled(agenttool.KeyOrchestrate) || s.gatewayBaseURL == "" {
		return nil
	}
	def, ok := agenttool.Lookup(agenttool.KeyOrchestrate)
	if !ok {
		return nil
	}
	return []agentruntime.MCPServerSpec{{
		Name:    def.Key,
		URL:     strings.TrimRight(s.gatewayBaseURL, "/") + def.MCPPath,
		Headers: map[string]string{"Authorization": "Bearer " + s.MintToken(a.ID, sessionID)},
		Tools:   def.ToolNames,
	}}
}

// BuildTurnExtras 实现 chat_svc.TurnExtrasProvider：注入编排框架语 + 本次编排流程（根会话专属）。
func (s *orchSvc) BuildTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
	if a == nil || !a.ToolEnabled(agenttool.KeyOrchestrate) {
		return nil, "", false
	}
	suffix := "\n\n### 编排指引\n" + orchGuidance
	if s.tasks != nil {
		if tk, _ := s.tasks.FindBySession(ctx, sessionID); tk != nil && tk.ParentTaskID == 0 {
			if s.runs != nil {
				if run, _ := s.runs.Find(ctx, tk.RunID); run != nil && run.RootTaskID == tk.ID && strings.TrimSpace(run.FlowContent) != "" {
					suffix += "\n\n### 本次编排流程(用户首条消息可临时覆盖)\n" + strings.TrimSpace(run.FlowContent)
				}
			}
		}
	}
	return nil, suffix, true
}
