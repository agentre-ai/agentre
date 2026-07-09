package orch_svc

import (
	"context"
	"strings"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
	"github.com/agentre-ai/agentre/internal/pkg/agentruntime"
	"github.com/agentre-ai/agentre/internal/pkg/agenttool"
)

// orchGuidance 是注入给编排 agent 的框架语。
const orchGuidance = `你被授予编排能力(dispatch/ask/send/finish/report/read/status/cancel + agent_list)。模型:` +
	`一切结果都会回到你、由你决定下一步;` +
	`并行 dispatch 子任务,审核/测试/合并也是 dispatch,返工用 send,补信息用 ask,收口用 finish。` +
	`子任务完成/报错默认只给你一条轻量通知(task_done/task_error),要看输出用 read(task_id=…)按需拉全文;` +
	`read 运行中的子任务会返回它当前进展(peek)。用 status() 随时看整棵任务树(谁在跑/谁在等/谁已完成),两次回报之间不再全盲。` +
	`发现某子任务跑偏或卡住,用 cancel(task_id=…)中止它(会级联取消其子孙)。` +
	`子任务想主动汇报中途进展用 report、收口小结用 finish,才会把内容内联给你。` +
	`agent_list 即你本次可调度的全集。无次数/时长/成本上限——自己判断何时收口或换策略。用户可能随时插话。`

// BuildTurnMCP 实现 chat_svc.TurnMCPProvider：agent 开了 orchestrate 工具、或会话本身是
// 编排会话（根/被派发子任务）时返回注入 spec —— 后者让经编排创建的会话即便其 agent 未勾
// orchestrate 也拿到编排工具。gatewayBaseURL 未设置（bootstrap 尚未接线）则返回 nil，确保
// 不注入失效的 URL。
func (s *orchSvc) BuildTurnMCP(ctx context.Context, a *agent_entity.Agent, sessionID, _ int64) []agentruntime.MCPServerSpec {
	if a == nil || s.gatewayBaseURL == "" {
		return nil
	}
	// 编排会话(根/被派发子任务,即绑定了编排 Dispatch 的会话)无条件注入编排工具,不看
	// agent 的 orchestrate 开关 —— 否则经编排创建的会话拿不到 dispatch/report/finish 等
	// 能力,整条 Run 静默降级成普通聊天。普通会话仍按 agent 配置;短路:已开则不查库。
	if !a.ToolEnabled(agenttool.KeyOrchestrate) && !s.sessionInRun(ctx, sessionID) {
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

// BuildTurnExtras 实现 chat_svc.TurnExtrasProvider：给编排会话（或开了 orchestrate 工具的
// 会话）注入编排框架语 + 本次编排流程（流程正文仅根会话，被派发子任务只拿框架语）。
func (s *orchSvc) BuildTurnExtras(ctx context.Context, a *agent_entity.Agent, sessionID, _ int64) ([]agentruntime.MCPServerSpec, string, bool) {
	if a == nil {
		return nil, "", false
	}
	// 取该会话绑定的编排 Dispatch(非编排会话 → nil)。编排会话无条件注入编排指引,不看
	// agent 的 orchestrate 开关;普通会话仍按 agent 配置。
	var tk *orch_entity.Dispatch
	if s.dispatches != nil {
		tk, _ = s.dispatches.FindBySession(ctx, sessionID)
	}
	if !a.ToolEnabled(agenttool.KeyOrchestrate) && tk == nil {
		return nil, "", false
	}
	suffix := "\n\n### 编排指引\n" + orchGuidance
	// 本次编排流程正文仅归根(Leader)会话;被派发的子任务只拿指引,不拿整条 Run 的计划。
	if tk != nil && tk.ParentDispatchID == 0 && s.runs != nil {
		if run, _ := s.runs.Find(ctx, tk.RunID); run != nil && run.RootTaskID == tk.ID && strings.TrimSpace(run.FlowContent) != "" {
			suffix += "\n\n### 本次编排流程(用户首条消息可临时覆盖)\n" + strings.TrimSpace(run.FlowContent)
		}
	}
	return nil, suffix, true
}

// sessionInRun 报告该会话是否为编排会话(绑定了编排 Dispatch);dispatches 未接线时保守返回 false。
func (s *orchSvc) sessionInRun(ctx context.Context, sessionID int64) bool {
	if s.dispatches == nil {
		return false
	}
	tk, _ := s.dispatches.FindBySession(ctx, sessionID)
	return tk != nil
}
