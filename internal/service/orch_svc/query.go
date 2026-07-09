package orch_svc

import (
	"context"

	"github.com/cago-frame/cago/pkg/logger"
	"go.uber.org/zap"

	"github.com/agentre-ai/agentre/internal/model/entity/agent_entity"
	"github.com/agentre-ai/agentre/internal/model/entity/orch_entity"
)

// ErrRunNotActive 在 LoadRun 找不到 id 对应的 Run 时返回。
// 导出以便调用方(如 app 绑定层)用 errors.Is 判别。
var ErrRunNotActive = errRunNotActive

// RunLoadResult LoadRun 的返回值:Run 与其全部 Dispatch。
type RunLoadResult struct {
	Run        *orch_entity.OrchestrationRun
	Dispatches []*orch_entity.Dispatch
}

// ListAllowedAgents 返回调用者所在 Run 的可参与 agent（allowed∪{leader}；集合空/定位不到 Run → 全部）。
func (s *orchSvc) ListAllowedAgents(ctx context.Context, sessionID int64) ([]*agent_entity.Agent, error) {
	all, err := s.agents.List(ctx)
	if err != nil {
		return nil, err
	}
	// 定位不到 Run(会话无任务 / DB 错误)→ 放行全部(fail-open:可参与是团队编成、非安全边界)。
	tk, err := s.dispatches.FindBySession(ctx, sessionID)
	if err != nil {
		logger.Ctx(ctx).Warn("orch.ListAllowedAgents: 定位会话任务失败,放行全部", zap.Int64("session", sessionID), zap.Error(err))
		return all, nil
	}
	if tk == nil {
		return all, nil
	}
	run, err := s.runs.Find(ctx, tk.RunID)
	if err != nil {
		logger.Ctx(ctx).Warn("orch.ListAllowedAgents: 取 Run 失败,放行全部", zap.Int64("run", tk.RunID), zap.Error(err))
		return all, nil
	}
	if run == nil {
		return all, nil
	}
	set := run.AllowedSet()
	if len(set) == 0 {
		return all, nil
	}
	out := make([]*agent_entity.Agent, 0, len(all))
	for _, a := range all {
		if set[a.ID] || a.ID == run.LeaderAgentID {
			out = append(out, a)
		}
	}
	return out, nil
}

// AgentWithLoad 一个可参与 agent 及其当前负载(本 Run 内非终态派发数)。
type AgentWithLoad struct {
	Agent   *agent_entity.Agent
	Running int
}

// ListAllowedAgentsWithLoad 在 ListAllowedAgents 基础上,给每个 agent 附上它在当前 Run 内
// 正在跑(非终态)的派发数 —— 让 Leader 拆活时一眼看谁忙。定位不到 Run 时 Running 恒为 0
// (与 ListAllowedAgents 一致的 fail-open 语义:可参与是团队编成、非安全边界)。
func (s *orchSvc) ListAllowedAgentsWithLoad(ctx context.Context, sessionID int64) ([]AgentWithLoad, error) {
	list, err := s.ListAllowedAgents(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	var runID int64
	if tk, err := s.dispatches.FindBySession(ctx, sessionID); err != nil {
		logger.Ctx(ctx).Warn("orch.ListAllowedAgentsWithLoad: 定位会话任务失败,running 计数留 0",
			zap.Int64("session", sessionID), zap.Error(err))
	} else if tk != nil {
		runID = tk.RunID
	}
	out := make([]AgentWithLoad, 0, len(list))
	for _, a := range list {
		var running int
		if runID != 0 {
			n, err := s.dispatches.CountActiveByRunAgent(ctx, runID, a.ID)
			if err != nil {
				logger.Ctx(ctx).Warn("orch.ListAllowedAgentsWithLoad: 统计 running 失败,留 0",
					zap.Int64("run", runID), zap.Int64("agent", a.ID), zap.Error(err))
			} else {
				running = int(n)
			}
		}
		out = append(out, AgentWithLoad{Agent: a, Running: running})
	}
	return out, nil
}

// ListRuns 按更新时间倒序返回全部 Run。
func (s *orchSvc) ListRuns(ctx context.Context) ([]*orch_entity.OrchestrationRun, error) {
	return s.runs.List(ctx)
}

// LoadRun 取指定 Run 及其全部 Dispatch;Run 不存在时返回 ErrRunNotActive。
func (s *orchSvc) LoadRun(ctx context.Context, id int64) (*RunLoadResult, error) {
	run, err := s.runs.Find(ctx, id)
	if err != nil {
		return nil, err
	}
	if run == nil {
		return nil, errRunNotActive
	}
	dispatches, err := s.dispatches.ListByRun(ctx, id)
	if err != nil {
		return nil, err
	}
	return &RunLoadResult{Run: run, Dispatches: dispatches}, nil
}
